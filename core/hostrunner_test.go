package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/hollis-labs/surface/module"
)

// fakeModel is an Anthropic-compatible gateway that answers every request with a short text
// and a usage that has cache reads and writes, and keeps what it was sent.
type fakeModel struct {
	srv  *httptest.Server
	mu   sync.Mutex
	sent []string
}

func newFakeModel(t *testing.T) *fakeModel {
	m := &fakeModel{}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		m.mu.Lock()
		m.sent = append(m.sent, string(body))
		n := len(m.sent)
		m.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		frame := func(typ, data string) { _, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ, data) }
		frame("message_start", `{"type":"message_start","message":{"usage":{"input_tokens":120,"cache_read_input_tokens":900,"cache_creation_input_tokens":40,"output_tokens":1}}}`)
		frame("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		frame("content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Answer %d."}}`, n))
		frame("content_block_stop", `{"type":"content_block_stop","index":0}`)
		frame("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":12}}`)
		frame("message_stop", `{"type":"message_stop"}`)
	}))
	t.Cleanup(m.srv.Close)
	return m
}

// request is the n-th request (from 1), or "".
func (m *fakeModel) request(n int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n < 1 || n > len(m.sent) {
		return ""
	}
	return m.sent[n-1]
}

func (m *fakeModel) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sent)
}

// sentWith is the first request whose body has s, or "".
func (m *fakeModel) sentWith(s string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.sent {
		if strings.Contains(b, s) {
			return b
		}
	}
	return ""
}

// hostConfig writes an agent config whose gateway is the fake model.
func hostConfig(t *testing.T, gateway string) {
	t.Helper()
	dir := t.TempDir()
	key := filepath.Join(dir, "key")
	if err := os.WriteFile(key, []byte("test-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "agent.json")
	raw, _ := json.Marshal(map[string]any{"model": "test-model", "gateway_url": gateway, "key_file": key, "max_steps": 6})
	if err := os.WriteFile(cfg, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SURFACE_AGENT_CONFIG", cfg)
	t.Setenv("SURFACE_AGENT_RUNNER", "host")
}

// The host runner on the real kernel: an owner's turn with usage in the run log and the
// context in front of the message, a late answer delivered, a module's errand, and a restart
// that resumes the same thread.
func TestTheHostRunnerOnTheConversationWire(t *testing.T) {
	model := newFakeModel(t)
	hostConfig(t, model.srv.URL)
	ctx := context.Background()
	data := t.TempDir()
	open := func() (*App, *httptest.Server) {
		app, err := New(ctx, Options{Name: "hostwire", Title: "Host wire", DataDir: data, Modules: []string{"scratch", "errandtest"}})
		if err != nil {
			t.Fatal(err)
		}
		if err = app.Start(ctx); err != nil {
			t.Fatal(err)
		}
		return app, httptest.NewServer(app.Handler())
	}
	app, srv := open()
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

	var setup struct {
		Wire string `json:"wire"`
	}
	do(t, srv, "GET", "/api/agent/setup", "", &setup)
	if setup.Wire != "conversation" {
		t.Fatalf("setup: %+v", setup)
	}
	attach := func(srv *httptest.Server) *lateApp {
		a := &lateApp{t: t, srv: srv}
		var conv struct {
			ID      string `json:"conversation_id"`
			Runner  string `json:"runner_ref"`
			Capable struct {
				Approvals bool `json:"approvals"`
			} `json:"capabilities"`
		}
		do(t, srv, "POST", "/api/agent/agents/hostwire/conversation", "{}", &conv)
		var att struct {
			ID string `json:"attachment_id"`
		}
		do(t, srv, "POST", "/api/agent/conversations/"+conv.ID+"/attachments", `{"role":"controller","stream":"committed"}`, &att)
		a.conv, a.att = conv.ID, att.ID
		return a
	}
	a := attach(srv)

	// The owner's turn: answered, in the run log with the cache apart.
	if code := do(t, srv, "POST", "/api/agent/conversations/"+a.conv+"/turns", `{"attachment":"`+a.att+`","text":"find the blue folder"}`, nil); code != 202 {
		t.Fatalf("turn: %d", code)
	}
	a.partsUntil("Answer 1.")
	first := model.request(1)
	var req struct {
		System   []struct{ Text string } `json:"system"`
		Messages []struct {
			Role    string `json:"role"`
			Content []struct{ Text string } `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(first), &req); err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 1 || len(req.Messages[0].Content) != 2 || !strings.HasPrefix(req.Messages[0].Content[0].Text, "<context>") ||
		req.Messages[0].Content[1].Text != "find the blue folder" {
		t.Fatalf("the first message is not the context block, then what was said: %s", first)
	}
	if strings.Contains(req.System[0].Text, "<context>\n") {
		t.Error("the context block is in the system prompt")
	}
	a.until("the run log has the turn's usage", func() bool {
		var runs []struct {
			Origin    string `json:"origin"`
			Status    string `json:"status"`
			Provider  string `json:"provider"`
			Uncached  int    `json:"uncached_input"`
			CacheRead int    `json:"cache_read"`
			CacheWrit int    `json:"cache_write"`
			Output    int    `json:"output"`
		}
		do(t, srv, "GET", "/api/agent/runs", "", &runs)
		return len(runs) == 1 && runs[0].Status == "finished" && runs[0].Origin == "owner" && runs[0].Provider == "agenthost" &&
			runs[0].Uncached == 120 && runs[0].CacheRead == 900 && runs[0].CacheWrit == 40 && runs[0].Output == 12
	})

	// A late answer to the agent's card reaches the agent as a turn.
	c := a.askAndStop("Folder color")
	if code := do(t, srv, "POST", "/api/cards/"+c.ID+"/response", `{"status":"submitted","data":{"color":"blue"}}`, nil); code != 200 {
		t.Fatalf("a late answer: %d", code)
	}
	a.until("the agent was sent the late answer", func() bool { return model.sentWith(`{\"color\":\"blue\"}`) != "" })
	if body := model.sentWith(`{\"color\":\"blue\"}`); !strings.Contains(body, `The owner's answer to your card \"Folder color\"`) {
		t.Errorf("the late answer's turn: %s", body)
	}
	a.until("the late answer's run ended", func() bool {
		for _, r := range a.runs() {
			if r.Origin == "errand:late-answer" && r.Status == "finished" {
				return true
			}
		}
		return false
	})

	// A module's errand runs as a turn on the same thread.
	run, err := theErrandMod.h.Agent().Ask(ctx, module.Ask{Title: "Mail", Text: "Draft the two replies."})
	if err != nil || run == "" {
		t.Fatalf("errand: %q %v", run, err)
	}
	a.until("the agent was sent the errand", func() bool { return model.sentWith("Draft the two replies.") != "" })
	if body := model.sentWith("Draft the two replies."); !strings.Contains(body, "find the blue folder") {
		t.Error("the errand is not on the conversation's thread")
	}
	a.until("the errand's run ended", func() bool {
		for _, r := range a.runs() {
			if r.Origin == "errand:errandtest" && r.Status == "finished" {
				return true
			}
		}
		return false
	})

	var before struct {
		Runner string `json:"runner_ref"`
	}
	do(t, srv, "POST", "/api/agent/agents/hostwire/conversation", "{}", &before)
	sentBefore := model.count()

	// A restart resumes the same thread.
	stop()
	app2, srv2 := open()
	t.Cleanup(func() {
		srv2.CloseClientConnections()
		srv2.Close()
		_ = app2.Stop(context.Background())
	})
	b := attach(srv2)
	if b.conv != a.conv {
		t.Fatalf("the conversation changed: %s %s", a.conv, b.conv)
	}
	var after struct {
		Runner string `json:"runner_ref"`
	}
	do(t, srv2, "POST", "/api/agent/agents/hostwire/conversation", "{}", &after)
	if before.Runner == "" || after.Runner != before.Runner {
		t.Fatalf("the thread changed across a restart: %q -> %q", before.Runner, after.Runner)
	}
	if code := do(t, srv2, "POST", "/api/agent/conversations/"+b.conv+"/turns", `{"attachment":"`+b.att+`","text":"which one was it?"}`, nil); code != 202 {
		t.Fatalf("turn after restart: %d", code)
	}
	b.until("the agent was asked after the restart", func() bool { return model.count() > sentBefore })
	resumed := model.request(sentBefore + 1)
	for _, want := range []string{"find the blue folder", "Draft the two replies.", "which one was it?"} {
		if !strings.Contains(resumed, want) {
			t.Errorf("the resumed thread does not have %q", want)
		}
	}
	// Not the first turn of the thread: no note about history from before.
	if strings.Contains(resumed, "went on before your history here begins") {
		t.Error("a resumed thread was told its history is elsewhere")
	}
}

// A conversation that had runs on another runner (Nanite in the owner's Desk; the scripted
// runner here) starts a fresh host thread, whose first turn says where the earlier history
// is. Nothing of the other runner's transcript is put in the thread.
func TestTheHostRunnerSeedsAConversationFromAnotherRunner(t *testing.T) {
	model := newFakeModel(t)
	hostConfig(t, model.srv.URL)
	t.Setenv("SURFACE_AGENT_RUNNER", "scripted")
	t.Setenv("SURFACE_AGENT_SCRIPT_DELAY", "0s")
	ctx := context.Background()
	data := t.TempDir()
	open := func() (*App, *httptest.Server) {
		app, err := New(ctx, Options{Name: "hostseed", Title: "Host seed", DataDir: data, Modules: []string{"scratch"}})
		if err != nil {
			t.Fatal(err)
		}
		if err = app.Start(ctx); err != nil {
			t.Fatal(err)
		}
		return app, httptest.NewServer(app.Handler())
	}
	attach := func(srv *httptest.Server) *lateApp {
		a := &lateApp{t: t, srv: srv}
		var conv struct {
			ID string `json:"conversation_id"`
		}
		do(t, srv, "POST", "/api/agent/agents/hostseed/conversation", "{}", &conv)
		var att struct {
			ID string `json:"attachment_id"`
		}
		do(t, srv, "POST", "/api/agent/conversations/"+conv.ID+"/attachments", `{"role":"controller","stream":"committed"}`, &att)
		a.conv, a.att = conv.ID, att.ID
		return a
	}
	app, srv := open()
	a := attach(srv)
	if code := do(t, srv, "POST", "/api/agent/conversations/"+a.conv+"/turns", `{"attachment":"`+a.att+`","text":"the old runner's turn"}`, nil); code != 202 {
		t.Fatalf("turn: %d", code)
	}
	a.partsUntil("You said: the old runner's turn")
	srv.CloseClientConnections()
	srv.Close()
	_ = app.Stop(ctx)

	t.Setenv("SURFACE_AGENT_RUNNER", "host")
	app, srv = open()
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
		_ = app.Stop(context.Background())
	})
	b := attach(srv)
	if b.conv != a.conv {
		t.Fatalf("the conversation changed: %s %s", a.conv, b.conv)
	}
	for i, text := range []string{"first on host", "second on host"} {
		if code := do(t, srv, "POST", "/api/agent/conversations/"+b.conv+"/turns", `{"attachment":"`+b.att+`","text":"`+text+`"}`, nil); code != 202 {
			t.Fatalf("turn: %d", code)
		}
		b.partsUntil(fmt.Sprintf("Answer %d.", i+1))
		b.until("the turn's run ended", func() bool {
			n := 0
			for _, r := range b.runs() {
				if r.Status == "finished" {
					n++
				}
			}
			return n == i+2 // and the old runner's
		})
	}
	first, second := model.request(1), model.request(2)
	if !strings.Contains(first, "went on before your history here begins") {
		t.Errorf("the host thread's first turn has no note: %s", first)
	}
	if strings.Contains(first, "the old runner's turn") {
		t.Error("the other runner's transcript was put in the thread")
	}
	if n := strings.Count(second, "went on before your history here begins"); n != 1 {
		t.Errorf("the note is in the thread %d times, want once (the first turn only)", n)
	}
}
