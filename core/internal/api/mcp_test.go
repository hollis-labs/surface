package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hollis-labs/surface/core/internal/kernel"
	"github.com/hollis-labs/surface/module"
)

var mcpRuns atomic.Int32

type mcpTestModule struct{ module.Base }

func (mcpTestModule) Manifest() module.Manifest {
	run := func(context.Context, module.Host, json.RawMessage) (any, error) { mcpRuns.Add(1); return "done", nil }
	in := json.RawMessage(`{"type":"object"}`)
	return module.Manifest{ID: "mcptest", Title: "MCP test", Tools: []module.Tool{
		{Name: "mcptest_read", Level: module.LevelRead, Description: "r", Input: in, Run: run},
		{Name: "mcptest_write", Level: module.LevelWrite, Description: "w", Input: in, Run: run,
			Describe: func(json.RawMessage) string { return "write a thing" }},
		{Name: "mcptest_confirm", Level: module.LevelConfirm, Description: "c", Input: in, Run: run},
	}}
}

func init() { module.Register(mcpTestModule{}) }

func mcpApp(t *testing.T) (*kernel.App, *httptest.Server) {
	t.Helper()
	ctx := context.Background()
	a, err := kernel.New(ctx, kernel.Options{Name: "mcpt", Title: "MCP test", DataDir: t.TempDir(), Modules: []string{"mcptest"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Handler(a, nil))
	t.Cleanup(srv.Close)
	return a, srv
}

func mcpCall(t *testing.T, srv *httptest.Server, caller, body string) map[string]any {
	t.Helper()
	req, _ := http.NewRequest("POST", srv.URL+"/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if caller != "" {
		req.Header.Set("X-Surface-Agent", caller)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var v map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode (%d): %v", resp.StatusCode, err)
	}
	return v
}

// answerNext answers the next open card with o, once it appears.
func answerNext(t *testing.T, a *kernel.App, o module.CardOutcome) <-chan module.Card {
	ch := make(chan module.Card, 1)
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			open, _ := a.Cards().List(context.Background(), false)
			for _, c := range open {
				if strings.HasPrefix(c.Subject, "mcp:") {
					_, _ = a.Cards().Respond(context.Background(), c.ID, o)
					ch <- c
					return
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		close(ch)
	}()
	return ch
}

func isError(v map[string]any) bool {
	r, _ := v["result"].(map[string]any)
	return r != nil && r["isError"] == true
}

func TestMCPListsAllLevels(t *testing.T) {
	_, srv := mcpApp(t)
	v := mcpCall(t, srv, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	names := map[string]bool{}
	for _, x := range v["result"].(map[string]any)["tools"].([]any) {
		names[x.(map[string]any)["name"].(string)] = true
	}
	for _, n := range []string{"mcptest_read", "mcptest_write", "mcptest_confirm"} {
		if !names[n] {
			t.Errorf("%s not listed", n)
		}
	}
}

func TestMCPWriteAsksAndAlwaysStands(t *testing.T) {
	a, srv := mcpApp(t)
	before := mcpRuns.Load()
	// a read runs at once
	if v := mcpCall(t, srv, "seat-host:skills", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mcptest_read"}}`); isError(v) {
		t.Fatalf("read: %v", v)
	}
	// a write asks; "deny" refuses it
	got := answerNext(t, a, module.CardOutcome{Status: "submitted", Answers: []module.CardAnswer{{QuestionID: "choice", Value: "deny"}}})
	if v := mcpCall(t, srv, "seat-host:skills", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mcptest_write"}}`); !isError(v) {
		t.Fatalf("a denied write must fail: %v", v)
	}
	c := <-got
	if c.Kind != "choice" || !strings.Contains(c.Title, "mcptest_write") || !strings.Contains(string(c.Payload), "seat-host:skills wants to: write a thing") {
		t.Fatalf("card = %+v %s", c, c.Payload)
	}
	// "always" runs it and stands for that caller
	answerNext(t, a, module.CardOutcome{Status: "submitted", Answers: []module.CardAnswer{{QuestionID: "choice", Value: "always"}}})
	if v := mcpCall(t, srv, "seat-host:skills", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"mcptest_write"}}`); isError(v) {
		t.Fatalf("allowed write: %v", v)
	}
	if v := mcpCall(t, srv, "seat-host:skills", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"mcptest_write"}}`); isError(v) {
		t.Fatalf("standing grant should run without asking: %v", v)
	}
	if open, _ := a.Cards().List(context.Background(), false); len(open) != 0 {
		t.Fatalf("a standing grant asked again: %d open cards", len(open))
	}
	// another caller is asked again
	answerNext(t, a, module.CardOutcome{Status: "submitted", Answers: []module.CardAnswer{{QuestionID: "choice", Value: "once"}}})
	if v := mcpCall(t, srv, "seat-host:other", `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"mcptest_write"}}`); isError(v) {
		t.Fatalf("once: %v", v)
	}
	if n := mcpRuns.Load() - before; n != 4 {
		t.Fatalf("runs = %d, want 4 (read, always, standing, once)", n)
	}
}

func TestMCPConfirmAsksEveryTime(t *testing.T) {
	a, srv := mcpApp(t)
	for i, action := range []string{"approve", "reject"} {
		got := answerNext(t, a, module.CardOutcome{Status: "submitted", Decisions: []module.CardDecision{{ItemID: "approval", Action: action}}})
		v := mcpCall(t, srv, "seat-host:skills", `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"mcptest_confirm"}}`)
		if c := <-got; c.Kind != "approval" || c.Risk != "high" {
			t.Fatalf("call %d: card %+v", i, c)
		}
		if (action == "approve") == isError(v) {
			t.Fatalf("call %d (%s): %v", i, action, v)
		}
	}
}
