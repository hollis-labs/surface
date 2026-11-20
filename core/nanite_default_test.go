package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/surface/module"
)

func TestDefaultRunnerUsesNaniteAndSteersReflexesOnce(t *testing.T) {
	model := newFakeModel(t)
	naniteConfig(t, model.srv.URL)
	t.Setenv("SURFACE_AGENT_RUNNER", "")
	t.Setenv("NANITE_DEFAULT_MODEL", "test-model")
	app, err := New(t.Context(), Options{Name: "nanitedefault", Title: "Nanite", DataDir: t.TempDir(), Modules: []string{"scratch"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close(); _ = app.Stop(context.Background()) })
	var conv struct {
		ID     string `json:"conversation_id"`
		Runner string `json:"runner_ref"`
	}
	do(t, srv, "POST", "/api/agent/agents/nanitedefault/conversation", "{}", &conv)
	if !strings.HasPrefix(conv.Runner, "chat-") {
		t.Fatalf("default runner: %+v", conv)
	}
	var att struct {
		ID string `json:"attachment_id"`
	}
	do(t, srv, "POST", "/api/agent/conversations/"+conv.ID+"/attachments", `{"role":"controller","stream":"committed"}`, &att)
	if code := do(t, srv, "PUT", "/api/agent/reflexes/test-reminder", `{"name":"test-reminder","trigger_kind":"text","trigger":{"pattern":"find a folder"},"action_kind":"remind","action":{"body":"UNIQUE-STEERING-REMINDER"},"enabled":true}`, nil); code != 200 {
		t.Fatalf("reflex: %d", code)
	}
	a := &lateApp{t: t, srv: srv, conv: conv.ID, att: att.ID}
	if code := do(t, srv, "POST", "/api/agent/conversations/"+conv.ID+"/turns", `{"attachment":"`+att.ID+`","text":"find a folder"}`, nil); code != 202 {
		t.Fatalf("turn: %d", code)
	}
	a.partsUntil("Answer 1.")
	if got := strings.Count(model.request(1), "UNIQUE-STEERING-REMINDER"); got != 1 {
		t.Fatalf("reminder count=%d", got)
	}
	a.until("one reflex firing", func() bool {
		var list []struct {
			Name  string `json:"name"`
			Fired int    `json:"fired"`
		}
		do(t, srv, "GET", "/api/agent/reflexes", "", &list)
		for _, r := range list {
			if r.Name == "test-reminder" {
				return r.Fired == 1
			}
		}
		return false
	})
	var req struct {
		MaxTokens int `json:"max_tokens"`
	}
	if err := json.Unmarshal([]byte(model.request(1)), &req); err != nil {
		t.Fatal(err)
	}
	if req.MaxTokens != 32000 {
		t.Fatalf("max_tokens=%d", req.MaxTokens)
	}
}

func TestNaniteForcedCompactionKeepsContextAndWholeTurns(t *testing.T) {
	model := newFakeModel(t)
	model.inputs = map[int]int{1: 20000, 2: 20000, 3: 20000, 4: 20000, 5: 20000, 6: 20000}
	naniteConfig(t, model.srv.URL)
	t.Setenv("SURFACE_AGENT_RUNNER", "")
	agentsDir := t.TempDir()
	t.Setenv("SURFACE_AGENTS_DIR", agentsDir)
	dir := filepath.Join(agentsDir, "compacttest")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	starter, err := os.ReadFile("internal/agent/seat/agent.md")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "agent.md"), []byte(strings.ReplaceAll(string(starter), "{{app}}", "compacttest")), 0600); err != nil { //nolint:gosec // fixed filename inside this test's temporary directory
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "host.yaml"), []byte("kind: embedded\nlifetime: application\napplication_id: compacttest\nruntime:\n  provider: anthropic\n  compact_at: 18000\n  compact_keep_tokens: 6000\n"), 0600); err != nil {
		t.Fatal(err)
	}
	app, err := New(t.Context(), Options{Name: "compacttest", DataDir: t.TempDir(), Modules: []string{"scratch"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close(); _ = app.Stop(context.Background()) })
	compacted := make(chan struct{}, 10)
	unsub := app.k.Events().Subscribe("agent.compacted", func(module.Event) { compacted <- struct{}{} })
	defer unsub()
	var conv struct {
		ID string `json:"conversation_id"`
	}
	do(t, srv, "POST", "/api/agent/agents/compacttest/conversation", "{}", &conv)
	var att struct {
		ID string `json:"attachment_id"`
	}
	do(t, srv, "POST", "/api/agent/conversations/"+conv.ID+"/attachments", `{"role":"controller","stream":"committed"}`, &att)
	a := &lateApp{t: t, srv: srv, conv: conv.ID, att: att.ID}
	for i := 0; i < 6; i++ {
		if i == 5 {
			model.script("desk_windows")
		}
		text := fmt.Sprintf("WHOLE-TURN-%d-BEGIN %s WHOLE-TURN-%d-END", i, strings.Repeat("history detail ", 700), i)
		raw, _ := json.Marshal(map[string]string{"attachment": att.ID, "text": text})
		var receipt struct {
			ID string `json:"run_id"`
		}
		a.until("chat service accepts next turn", func() bool {
			code := do(t, srv, "POST", "/api/agent/conversations/"+conv.ID+"/turns", string(raw), &receipt)
			if code != 202 && code != 409 {
				t.Fatalf("turn %d: %d", i, code)
			}
			return code == 202
		})
		a.until("finished run", func() bool {
			var runs []struct {
				ID     string `json:"run_id"`
				Status string `json:"status"`
			}
			do(t, srv, "GET", "/api/agent/runs", "", &runs)
			for _, r := range runs {
				if r.ID == receipt.ID {
					return r.Status == "finished"
				}
			}
			return false
		})
	}
	select {
	case <-compacted:
	default:
		t.Fatalf("compaction never fired; model requests=%d last bytes=%d begins=%d", model.count(), len(model.request(model.count())), strings.Count(model.request(model.count()), "WHOLE-TURN-"))
	}
	last := model.request(model.count())
	var req struct {
		System   []struct{ Text string }
		Messages []struct {
			Role    string
			Content []struct {
				Type string
				Text string
			}
		}
	}
	if err := json.Unmarshal([]byte(last), &req); err != nil {
		t.Fatal(err)
	}
	hasContext := false
	for _, b := range req.System {
		if strings.Contains(b.Text, "<context>") {
			hasContext = true
		}
	}
	for _, m := range req.Messages {
		for _, b := range m.Content {
			if strings.Contains(b.Text, "<context>") {
				hasContext = true
			}
			if strings.Contains(b.Text, "WHOLE-TURN-") && strings.Contains(b.Text, "-BEGIN") && !strings.Contains(b.Text, "-END") {
				t.Fatal("a retained turn was split")
			}
		}
	}
	if !hasContext {
		t.Fatalf("current context missing after compaction; first=%t system tail=%s", strings.Contains(model.request(1), "context"), req.System[len(req.System)-1].Text)
	}
	if !strings.Contains(last, "WHOLE-TURN-5-END") {
		t.Fatal("latest turn was not retained")
	}
}
