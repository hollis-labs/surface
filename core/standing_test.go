package core

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Nanite reloads standing instructions between turns and keeps Surface slot-note prompts.
func TestNaniteReloadsStandingAndKeepsNotesCurrent(t *testing.T) {
	model := newFakeModel(t)
	naniteConfig(t, model.srv.URL)
	agents := t.TempDir()
	t.Setenv("SURFACE_AGENTS_DIR", agents)
	seatDir := filepath.Join(agents, "hoststanding")
	if err := os.MkdirAll(seatDir, 0700); err != nil {
		t.Fatal(err)
	}
	starter, err := os.ReadFile("internal/agent/seat/agent.md")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(seatDir, "agent.md"), []byte(strings.ReplaceAll(string(starter), "{{app}}", "hoststanding")), 0600); err != nil { //nolint:gosec // fixed filename inside test-owned temporary directory
		t.Fatal(err)
	}
	host, err := os.ReadFile("internal/agent/seat/host.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(seatDir, "host.yaml"), []byte(strings.ReplaceAll(string(host), "{{app}}", "hoststanding")), 0600); err != nil { //nolint:gosec // fixed filename inside test-owned temporary directory
		t.Fatal(err)
	}
	standing := filepath.Join(agents, "hoststanding", "context", "standing.md")
	if err := os.MkdirAll(filepath.Dir(standing), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(standing, []byte("- Chief goes first.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	data := t.TempDir()
	open := func() (*App, *httptest.Server, func()) {
		app, err := New(ctx, Options{Name: "hoststanding", Title: "Host standing", DataDir: data, Modules: []string{"scratch"}})
		if err != nil {
			t.Fatal(err)
		}
		if err = app.Start(ctx); err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewServer(app.Handler())
		stopped := false
		stop := func() {
			if !stopped {
				stopped = true
				srv.CloseClientConnections()
				srv.Close()
				_ = app.Stop(context.Background())
			}
		}
		t.Cleanup(stop)
		return app, srv, stop
	}
	attach := func(srv *httptest.Server) *lateApp {
		a := &lateApp{t: t, srv: srv}
		var conv struct {
			ID string `json:"conversation_id"`
		}
		do(t, srv, "POST", "/api/agent/agents/hoststanding/conversation", "{}", &conv)
		var att struct {
			ID string `json:"attachment_id"`
		}
		do(t, srv, "POST", "/api/agent/conversations/"+conv.ID+"/attachments", `{"role":"controller","stream":"committed"}`, &att)
		a.conv, a.att = conv.ID, att.ID
		return a
	}
	// turn sends a message, waits for the run to finish, and returns the last request the
	// model was sent: its system prompt and the newest user message.
	turn := func(a *lateApp, text string) (system, last string) {
		t.Helper()
		before := len(a.runs())
		a.waitIdle()
		code := do(t, a.srv, "POST", "/api/agent/conversations/"+a.conv+"/turns", `{"attachment":"`+a.att+`","text":"`+text+`"}`, nil)
		if code != 202 {
			t.Fatalf("turn %q: %d", text, code)
		}
		a.until("the turn's run ended", func() bool {
			runs := a.runs()
			return len(runs) > before && runs[0].Status == "finished"
		})
		a.waitIdle()
		var req struct {
			System   []struct{ Text string } `json:"system"`
			Messages []struct {
				Role    string                  `json:"role"`
				Content []struct{ Text string } `json:"content"`
			} `json:"messages"`
		}
		body := model.request(model.count())
		if err := json.Unmarshal([]byte(body), &req); err != nil {
			t.Fatal(err)
		}
		for _, s := range req.System {
			system += s.Text
		}
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if m := req.Messages[i]; m.Role == "user" {
				for _, c := range m.Content {
					last += c.Text + "\n"
				}
				break
			}
		}
		if !strings.Contains(last, text) {
			t.Fatalf("the last user message is not %q: %s", text, last)
		}
		return system, last
	}

	_, srv, stop := open()
	a := attach(srv)
	system, last := turn(a, "hello")
	if !strings.Contains(system, "- Chief goes first.") || strings.Contains(last, "<standing changed>") {
		t.Fatalf("first turn: system has standing=%v, section=%v", strings.Contains(system, "Chief goes first"), strings.Contains(last, "<standing changed>"))
	}

	// The owner's agent updates standing, and the owner moves to the scratch slot.
	var res struct{ Result string }
	if code := do(t, srv, "POST", "/api/tools/standing_update", `{"text":"- Chief goes first.\n- Use the cards.","why":"the owner said cards"}`, &res); code != 200 ||
		!strings.Contains(res.Result, "+1 -0 lines") {
		t.Fatalf("standing_update: %d %+v", code, res)
	}
	if code := do(t, srv, "POST", "/api/agent/slots/pin", `{"slot":"scratch"}`, nil); code != 200 {
		t.Fatalf("pin: %d", code)
	}
	system, last = turn(a, "now in scratch")
	if !strings.Contains(system, "Use the cards.") {
		t.Fatal("Nanite did not reload updated standing")
	}
	if !strings.Contains(system+last, "You have just come from the general slot") {
		t.Fatalf("no ask for the note on the slot left: %s", last)
	}
	system, last = turn(a, "and then")
	if strings.Contains(system+last, "You have just come from") {
		t.Fatal("duplicate note ask")
	}
	// After a restart the system prompt has the new text and the section stops.
	stop()
	_, srv2, _ := open()
	b := attach(srv2)
	system, last = turn(b, "after the restart")
	if !strings.Contains(system, "- Use the cards.") {
		t.Fatal("the system prompt does not have the updated standing after a restart")
	}
	if strings.Contains(last, "<standing changed>") {
		t.Fatalf("the section outlived the restart: %s", last)
	}
}
