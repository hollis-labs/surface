package core

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/hollis-labs/surface/module"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The conversation wire on the real kernel with the library's scripted runner: the agent's
// conversation is served under /api/agent, a turn's events come on its stream with the
// user's message first, the host hook can refuse a turn or answer it, a turn started from
// inside the app is on the same stream, and a standing approval is kept.
func TestTheConversationWireOnTheRealKernel(t *testing.T) {
	t.Setenv("SURFACE_AGENT_CONFIG", filepath.Join(t.TempDir(), "absent.json"))
	t.Setenv("SURFACE_AGENT_RUNNER", "scripted")
	t.Setenv("SURFACE_AGENT_SCRIPT_DELAY", "0s")
	ctx := context.Background()
	app, err := New(ctx, Options{Name: "wiretest", Title: "Wire test", DataDir: t.TempDir(), Modules: []string{"scratch"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
		_ = app.Stop(context.Background())
	})
	const base = "/api/agent"

	var setup struct {
		Ready bool   `json:"ready"`
		Wire  string `json:"wire"`
		Agent string `json:"agent"`
	}
	do(t, srv, "GET", base+"/setup", "", &setup)
	if !setup.Ready || setup.Wire != "conversation" || setup.Agent != "wiretest" {
		t.Fatalf("setup: %+v", setup)
	}
	if code := do(t, srv, "POST", base+"/agents/someone-else/conversation", "{}", nil); code != 404 {
		t.Errorf("another agent's conversation: %d, want 404", code)
	}
	var conv struct {
		ID           string `json:"conversation_id"`
		Capabilities struct {
			Always bool `json:"always"`
		} `json:"capabilities"`
	}
	do(t, srv, "POST", base+"/agents/wiretest/conversation", "{}", &conv)
	if conv.ID == "" || !conv.Capabilities.Always {
		t.Fatalf("conversation: %+v; want an id and standing grants on offer", conv)
	}
	var att struct {
		ID string `json:"attachment_id"`
	}
	do(t, srv, "POST", base+"/conversations/"+conv.ID+"/attachments", `{"role":"controller","stream":"committed"}`, &att)

	// events reads the stream from the start until want has been seen, and returns the verbs
	// and texts in order.
	events := func(want string) []string {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+base+"/conversations/"+conv.ID+"/events?attachment="+att.ID+"&after=0", nil)
		rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		resp, err := http.DefaultClient.Do(req.WithContext(rctx))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var seen []string
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 1<<16), 1<<20)
		for sc.Scan() {
			line, ok := strings.CutPrefix(sc.Text(), "data: ")
			if !ok {
				continue
			}
			var ev struct {
				Verb  string `json:"verb"`
				Role  string `json:"role"`
				Final struct {
					Text string `json:"text"`
				} `json:"final"`
			}
			_ = json.Unmarshal([]byte(line), &ev)
			s := ev.Verb
			if ev.Role != "" {
				s += ":" + ev.Role
			}
			if ev.Final.Text != "" {
				s += ":" + ev.Final.Text
			}
			seen = append(seen, s)
			if strings.Contains(s, want) {
				return seen
			}
		}
		t.Fatalf("the stream ended before %q; saw %v", want, seen)
		return nil
	}
	has := func(seen []string, s string) bool {
		for _, x := range seen {
			if x == s {
				return true
			}
		}
		return false
	}

	// A turn: the user's message is on the stream before the answer.
	turn := func(text string) int {
		body, _ := json.Marshal(map[string]string{"attachment": att.ID, "text": text})
		return do(t, srv, "POST", base+"/conversations/"+conv.ID+"/turns", string(body), nil)
	}
	if code := turn("hello"); code != 202 {
		t.Fatalf("submit: %d", code)
	}
	seen := events("part.end:You said: hello")
	if !has(seen, "message.start:user") || !has(seen, "part.end:hello") {
		t.Errorf("the user's message is not on the stream: %v", seen)
	}
	events("run.finish")

	// The host hook refuses a turn (nothing is published) and answers one itself.
	if code := turn("/no thanks"); code != 422 {
		t.Errorf("a refused turn: %d, want 422", code)
	}
	if code := turn("/ping"); code != 202 {
		t.Fatalf("a host-answered turn: %d", code)
	}
	seen = events("part.end:pong")
	if has(seen, "part.end:/no thanks") {
		t.Errorf("the refused turn was published: %v", seen)
	}

	// A turn started inside the app (a reflex, another agent through the tools) is on the
	// same stream, with no attachment and no lease.
	deadline := time.Now().Add(3 * time.Second)
	for {
		var asker interface {
			AskFrom(context.Context, string, module.Ask) (string, error)
		}
		for _, service := range app.services {
			if candidate, ok := service.(interface {
				AskFrom(context.Context, string, module.Ask) (string, error)
			}); ok {
				asker = candidate
				break
			}
		}
		if asker == nil {
			t.Fatal("agent service missing")
		}
		run, err := asker.AskFrom(ctx, "scratch", module.Ask{Title: "A reflex", Text: "from inside"})
		if err == nil && run != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("a server-started turn: %v", err)
		}
		time.Sleep(20 * time.Millisecond) // the run before it is still ending
	}
	events("part.end:You said: [An errand from the scratch module, not a message from the owner: A reflex]\n\nfrom inside")

	// A standing approval is handed to the host and kept.
	for turn("approval") != 202 {
		if time.Now().After(deadline.Add(3 * time.Second)) {
			t.Fatal("the approval turn was not accepted")
		}
		time.Sleep(20 * time.Millisecond)
	}
	events("approval.request")
	if code := do(t, srv, "POST", base+"/conversations/"+conv.ID+"/approvals/ap1",
		`{"attachment":"`+att.ID+`","decision":"allow","scope":"always"}`, nil); code != 204 {
		t.Fatalf("answering the approval: %d", code)
	}
	var grants []struct {
		Agent  string `json:"agent"`
		Reason string `json:"reason"`
	}
	do(t, srv, "GET", base+"/grants", "", &grants)
	if len(grants) != 1 || grants[0].Agent != "wiretest" || grants[0].Reason == "" {
		t.Errorf("standing grants: %+v, want the one just given", grants)
	}
}
