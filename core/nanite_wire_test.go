package core

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/hollis-labs/libs/agent/chat/sqlitestore"
	"github.com/hollis-labs/surface/module"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeModel is an Anthropic-compatible gateway that answers every request with a short text
// and a usage that has cache reads and writes, and keeps what it was sent.
type fakeModel struct {
	srv  *httptest.Server
	mu   sync.Mutex
	sent []string
	// next is what the next answers are instead of text: a tool to call, or "hold" (an
	// answer that never ends).
	next []string
	// inputs, when set, are the uncached input tokens the n-th request (from 1) reports
	// instead of 120.
	inputs map[int]int
}

func (m *fakeModel) script(next ...string) {
	m.mu.Lock()
	m.next = append(m.next, next...)
	m.mu.Unlock()
}

func newFakeModel(t *testing.T) *fakeModel {
	m := &fakeModel{}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		m.mu.Lock()
		m.sent = append(m.sent, string(body))
		n := len(m.sent)
		next := ""
		if len(m.next) > 0 {
			next, m.next = m.next[0], m.next[1:]
		}
		in := 120
		if v, ok := m.inputs[n]; ok {
			in = v
		}
		m.mu.Unlock()
		var request struct {
			Stream bool `json:"stream"`
		}
		_ = json.Unmarshal(body, &request)
		if !request.Stream {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"id":"summary","type":"message","role":"assistant","content":[{"type":"text","text":"Earlier turns discussed folders."}],"stop_reason":"end_turn","usage":{"input_tokens":100,"output_tokens":10}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		frame := func(typ, data string) {
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ, data)
			w.(http.Flusher).Flush()
		}
		frame("message_start", fmt.Sprintf(`{"type":"message_start","message":{"usage":{"input_tokens":%d,"cache_read_input_tokens":900,"cache_creation_input_tokens":40,"output_tokens":1}}}`, in))
		switch next {
		case "":
		case "hold":
			<-r.Context().Done()
			return
		default:
			frame("content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_%d","name":%q}}`, n, next))
			input := "{}"
			if next == "agent_reflex_put" {
				input = `{"name":"test-reflex","trigger_kind":"text","trigger":{"pattern":"never-match"},"action_kind":"remind","action":{"body":"a reminder"}}`
			}
			encoded, _ := json.Marshal(input)
			frame("content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":%s}}`, encoded))
			frame("content_block_stop", `{"type":"content_block_stop","index":0}`)
			frame("message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":12}}`)
			frame("message_stop", `{"type":"message_stop"}`)
			return
		}
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

// waitIdle observes the service's completion boundary, after the terminal
// event has been consumed and the active run released. Text and run-log updates
// happen earlier and do not mean the next turn can start yet.
func (a *lateApp) waitIdle() {
	a.t.Helper()
	a.until("chat service released the run", func() bool {
		var view struct {
			ActiveRun *string `json:"active_run"`
		}
		if code := do(a.t, a.srv, "GET", "/api/agent/conversations/"+a.conv, "", &view); code != 200 {
			a.t.Fatalf("conversation: %d", code)
		}
		return view.ActiveRun == nil
	})
}

func TestNaniteStartsFreshForFormerHostAndPreservesTranscript(t *testing.T) {
	model := newFakeModel(t)
	naniteConfig(t, model.srv.URL)
	t.Setenv("SURFACE_AGENT_RUNNER", "nanite")
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
	a.partsUntil("Answer 1.")
	a.waitIdle()
	srv.CloseClientConnections()
	srv.Close()
	_ = app.Stop(ctx)

	// Represent the last deployed runner's host thread without depending on its runtime.
	oldStore, err := sqlitestore.Open(ctx, filepath.Join(data, "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err = oldStore.SetRunnerRef(ctx, a.conv, "thr_legacy"); err != nil {
		t.Fatal(err)
	}
	if err = oldStore.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SURFACE_AGENT_RUNNER", "nanite")
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
	for i, text := range []string{"first on Nanite", "second on Nanite"} {
		if code := do(t, srv, "POST", "/api/agent/conversations/"+b.conv+"/turns", `{"attachment":"`+b.att+`","text":"`+text+`"}`, nil); code != 202 {
			t.Fatalf("turn: %d", code)
		}
		b.partsUntil(fmt.Sprintf("Answer %d.", i+2))
		b.waitIdle()
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
	if parts := b.partsUntil("Answer 3."); !strings.Contains(strings.Join(parts, "\n"), "the old runner's turn") {
		t.Fatal("earlier transcript was lost")
	}
	first, second := model.request(2), model.request(3)
	if !strings.Contains(first, "went on before your history here begins") {
		t.Errorf("the host thread's first turn has no note: %s", first)
	}
	if strings.Contains(first, "the old runner's turn") {
		t.Error("the other runner's transcript was put in the thread")
	}
	if n := strings.Count(second, "went on before your history here begins"); n != 1 {
		t.Errorf("the note is in the thread %d times, want once (the first turn only)", n)
	}

	srv.CloseClientConnections()
	srv.Close()
	_ = app.Stop(ctx)
	app, srv = open()
	c := attach(srv)
	if c.conv != a.conv {
		t.Fatal("restart changed the chat ID")
	}
	if code := do(t, srv, "POST", "/api/agent/conversations/"+c.conv+"/turns", `{"attachment":"`+c.att+`","text":"after restart"}`, nil); code != 202 {
		t.Fatalf("restart turn: %d", code)
	}
	c.partsUntil("Answer 4.")
	resumed := model.request(4)
	if !strings.Contains(resumed, "second on Nanite") || strings.Contains(resumed, "the old runner's turn") || strings.Count(resumed, "went on before your history here begins") != 1 {
		t.Fatal("restart did not retain exactly the upgraded Nanite history")
	}
}

// streamUntil reads the conversation's stream from the start until a data line has all of want,
// and returns the data lines read.
func (a *lateApp) streamUntil(want ...string) []string {
	t := a.t
	t.Helper()
	rctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(rctx, "GET", a.srv.URL+"/api/agent/conversations/"+a.conv+"/events?attachment="+a.att+"&after=0", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var lines []string
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<22)
	for sc.Scan() {
		line, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok {
			continue
		}
		lines = append(lines, line)
		all := true
		for _, w := range want {
			all = all && strings.Contains(line, w)
		}
		if all {
			return lines
		}
	}
	t.Fatalf("the stream ended before %q", want)
	return nil
}

func countWith(lines []string, s string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(l, s) {
			n++
		}
	}
	return n
}

// Through the Service: an approval answered "always" is kept and covers the next call; a
// cancel stops a run whose model has not answered, and the conversation goes on.
func TestNaniteApprovalsAndCancelThroughTheService(t *testing.T) {
	model := newFakeModel(t)
	naniteConfig(t, model.srv.URL)
	ctx := context.Background()
	app, err := New(ctx, Options{Name: "hostappr", Title: "Host approvals", DataDir: t.TempDir(), Modules: []string{"scratch"}})
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
	a := &lateApp{t: t, srv: srv}
	var conv struct {
		ID string `json:"conversation_id"`
	}
	do(t, srv, "POST", "/api/agent/agents/hostappr/conversation", "{}", &conv)
	var att struct {
		ID string `json:"attachment_id"`
	}
	do(t, srv, "POST", "/api/agent/conversations/"+conv.ID+"/attachments", `{"role":"controller","stream":"committed"}`, &att)
	a.conv, a.att = conv.ID, att.ID
	turn := func(text string) string {
		var r struct {
			RunID string `json:"run_id"`
		}
		if code := do(t, srv, "POST", "/api/agent/conversations/"+a.conv+"/turns", `{"attachment":"`+a.att+`","text":"`+text+`"}`, &r); code != 202 {
			t.Fatalf("turn %q: %d", text, code)
		}
		return r.RunID
	}
	ended := func(run string) {
		a.until("run "+run+" ended", func() bool {
			for _, r := range a.runs() {
				if r.RunID == run && r.Status != "" {
					return true
				}
			}
			return false
		})
	}

	// agent_reflex_put is a write tool: it asks.
	model.script("agent_reflex_put")
	run := turn("add a reflex")
	lines := a.streamUntil(`"approval.request"`)
	var req struct {
		ApprovalID string `json:"approval_id"`
	}
	_ = json.Unmarshal([]byte(lines[len(lines)-1]), &req)
	if req.ApprovalID == "" || !strings.Contains(lines[len(lines)-1], `agent_reflex_put`) {
		t.Fatalf("the approval request: %s", lines[len(lines)-1])
	}
	if code := do(t, srv, "POST", "/api/agent/conversations/"+a.conv+"/approvals/"+req.ApprovalID,
		`{"attachment":"`+a.att+`","decision":"allow","scope":"always"}`, nil); code != 204 {
		t.Fatalf("answering: %d", code)
	}
	ended(run)
	a.waitIdle()
	var grants []struct {
		Tool string `json:"tool"`
	}
	do(t, srv, "GET", "/api/agent/grants", "", &grants)
	if len(grants) != 1 || grants[0].Tool != "agent_reflex_put" {
		t.Fatalf("grants: %+v", grants)
	}
	// The next call is covered: no question.
	model.script("agent_reflex_put")
	run = turn("another reflex")
	ended(run)
	a.waitIdle()
	if n := countWith(a.streamUntil(run, `"verb":"run.finish"`), `"approval.request"`); n != 1 {
		t.Errorf("%d approval requests, want 1", n)
	}

	// Cancel a run the model is still answering.
	sent := model.count()
	model.script("hold")
	run = turn("wait for it")
	a.until("the model was asked", func() bool { return model.count() > sent })
	if code := do(t, srv, "POST", "/api/agent/conversations/"+a.conv+"/runs/"+run+"/cancel", `{"attachment":"`+a.att+`"}`, nil); code != 202 {
		t.Fatalf("cancel: %d", code)
	}
	a.streamUntil(run, `"verb":"run.abort"`)
	ended(run)
	a.waitIdle()
	next := model.count() + 1
	turn("after the cancel")
	a.partsUntil(fmt.Sprintf("Answer %d.", next))
}

func TestRetiredHostRunnerFailsClearly(t *testing.T) {
	t.Setenv("SURFACE_AGENT_CONFIG", filepath.Join(t.TempDir(), "absent.json"))
	t.Setenv("SURFACE_AGENT_RUNNER", "host")
	_, err := New(t.Context(), Options{Name: "retiredrunner", DataDir: t.TempDir(), Modules: []string{"scratch"}})
	if err == nil || !strings.Contains(err.Error(), `no runner "host"`) || !strings.Contains(err.Error(), "nanite, scripted") {
		t.Fatalf("retired runner error: %v", err)
	}
}

func TestUnconfiguredAppHasNoLegacyRuntime(t *testing.T) {
	t.Setenv("SURFACE_AGENT_CONFIG", filepath.Join(t.TempDir(), "absent.json"))
	t.Setenv("SURFACE_AGENT_RUNNER", "")
	app, err := New(t.Context(), Options{Name: "unconfigured", DataDir: t.TempDir(), Modules: []string{"scratch"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	t.Cleanup(func() { srv.Close(); _ = app.Stop(context.Background()) })
	var setup struct {
		Ready  bool
		Reason string
	}
	do(t, srv, "GET", "/api/agent/setup", "", &setup)
	if setup.Ready || setup.Reason == "" {
		t.Fatalf("setup: %+v", setup)
	}
	var n int
	if err = app.k.DB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name LIKE 'agenthost_%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("legacy runtime created %d tables", n)
	}
	if code := do(t, srv, "POST", "/api/agent/turns", `{"text":"hello"}`, nil); code != 503 {
		t.Fatalf("unconfigured turn: %d", code)
	}
}

func TestNaniteConversationUsageErrandsLateAnswersAndRestart(t *testing.T) {
	model := newFakeModel(t)
	naniteConfig(t, model.srv.URL)
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
	a.waitIdle()
	first := model.request(1)
	var req struct {
		System   []struct{ Text string } `json:"system"`
		Messages []struct {
			Role    string                  `json:"role"`
			Content []struct{ Text string } `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(first), &req); err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 1 {
		t.Fatalf("first turn messages: %d", len(req.Messages))
	}
	contextPresent := false
	for _, block := range req.System {
		contextPresent = contextPresent || strings.Contains(block.Text, "<context>")
	}
	for _, message := range req.Messages {
		for _, block := range message.Content {
			contextPresent = contextPresent || strings.Contains(block.Text, "<context>")
		}
	}
	if !contextPresent {
		t.Fatal("model received no context")
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
		return len(runs) == 1 && runs[0].Status == "finished" && runs[0].Origin == "owner" && runs[0].Provider != "" &&
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

	a.waitIdle()
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
