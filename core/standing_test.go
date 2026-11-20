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

// Keeping the agent's state current, on the real kernel with the host runner: a
// standing_update reaches the model in the context block before a restart, and the system
// prompt after it; moving to another slot asks once for the note on the one left; a real
// compaction publishes agent.compacted and the seeded save-state reminds the agent with the
// next message.
func TestTheHostRunnerKeepsStandingAndNotesCurrent(t *testing.T) {
	model := newFakeModel(t)
	// The second request is large, so the thread passes compact_at and the third turn is
	// compacted first.
	model.inputs = map[int]int{2: 3000}
	hostConfig(t, model.srv.URL)
	cfgPath := os.Getenv("SURFACE_AGENT_CONFIG")
	var cfg map[string]any
	raw, _ := os.ReadFile(cfgPath) //nolint:gosec // the test's own file
	_ = json.Unmarshal(raw, &cfg)
	cfg["compact_at"] = 2000
	raw, _ = json.Marshal(cfg)
	if err := os.WriteFile(cfgPath, raw, 0o600); err != nil { //nolint:gosec // the test's own config file
		t.Fatal(err)
	}
	agents := t.TempDir()
	t.Setenv("SURFACE_AGENTS_DIR", agents)
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
		// The run log can say finished a moment before the conversation is free (409).
		code := 0
		a.until("the turn was taken", func() bool {
			code = do(t, a.srv, "POST", "/api/agent/conversations/"+a.conv+"/turns", `{"attachment":"`+a.att+`","text":"`+text+`"}`, nil)
			return code != 409
		})
		if code != 202 {
			t.Fatalf("turn %q: %d", text, code)
		}
		a.until("the turn's run ended", func() bool {
			runs := a.runs()
			return len(runs) > before && runs[0].Status == "finished"
		})
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
	if strings.Contains(system, "Use the cards.") {
		t.Fatal("the fixed system prompt changed without a restart")
	}
	if !strings.Contains(last, "<standing changed>") || !strings.Contains(last, "- Use the cards.\n</standing changed>") {
		t.Fatalf("no standing section after the update: %s", last)
	}
	if !strings.Contains(last, "You have just come from the general slot") {
		t.Fatalf("no ask for the note on the slot left: %s", last)
	}
	// The next turn is compacted first (the thread passed compact_at): the one after it
	// carries save-state's reminder. No second note ask for the same switch.
	_, last = turn(a, "and then")
	if strings.Contains(last, "You have just come from") {
		t.Fatalf("the note ask came twice for one switch: %s", last)
	}
	if strings.Contains(last, "Older turns were just summarized") {
		t.Fatalf("the reminder came before the compaction was told: %s", last)
	}
	// A standing rule in the owner's words: standing-rule reminds the agent in the same turn.
	_, last = turn(a, "From now on, copy Chief on those.")
	if !strings.Contains(last, "Reminder: Older turns were just summarized.") {
		t.Fatalf("no save-state reminder after the compaction: %s", last)
	}
	if !strings.Contains(last, "Reminder: The owner may have set a standing rule.") {
		t.Fatalf("no standing-rule reminder: %s", last)
	}
	var reflexes []struct {
		Name  string `json:"name"`
		Fired int    `json:"fired"`
	}
	do(t, srv, "GET", "/api/agent/reflexes", "", &reflexes)
	fired := map[string]int{}
	for _, r := range reflexes {
		fired[r.Name] = r.Fired
	}
	if fired["save-state"] != 1 || fired["standing-rule"] != 1 {
		t.Errorf("fired: %v, want save-state and standing-rule once each", fired)
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
