package hostrunner

import (
	"compress/gzip"
	"context"
	"database/sql"
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
	"time"

	chat "github.com/hollis-labs/libs/agent/chat"
	"github.com/hollis-labs/libs/agent/host"
	chatstream "github.com/hollis-labs/libs/ui/ui-go/chatstream"
	_ "modernc.org/sqlite"
)

// gateway is a fake Anthropic-compatible model gateway: it keeps every request and answers
// each with the next scripted answer (or the summarizer's, for a summary request).
type gateway struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	reqs    []request
	answers []answer
	summary string
	// failSummary refuses the summary request; gzip compresses an answer when asked to.
	failSummary bool
	gzip        bool
}

type request struct {
	System   []struct{ Text string } `json:"system"`
	Messages []struct {
		Role    string `json:"role"`
		Content []struct {
			Type      string `json:"type"`
			Text      string `json:"text"`
			Name      string `json:"name"`
			ToolUseID string `json:"tool_use_id"`
			Content   any    `json:"content"`
		} `json:"content"`
	} `json:"messages"`
	Tools []struct {
		Name string `json:"name"`
	} `json:"tools"`
	raw    string
	header http.Header
}

func (r request) system() string {
	var b strings.Builder
	for _, s := range r.System {
		b.WriteString(s.Text)
	}
	return b.String()
}

type answer struct {
	text                 []string
	tool                 string   // a tool call: name
	input                string   // its input
	more                 []string // more tool calls in the same answer, by name, input {}
	in, read, write, out int
	hold                 bool
}

func newGateway(t *testing.T) *gateway {
	g := &gateway{t: t, summary: "SUMMARY-ONE"}
	g.srv = httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *gateway) script(a ...answer) {
	g.mu.Lock()
	g.answers = append(g.answers, a...)
	g.mu.Unlock()
}

func (g *gateway) requests() []request {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]request(nil), g.reqs...)
}

func (g *gateway) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req request
	if err := json.Unmarshal(body, &req); err != nil || r.URL.Path != "/v1/messages" {
		http.Error(w, "bad request", 400)
		return
	}
	req.raw, req.header = string(body), r.Header.Clone()
	g.mu.Lock()
	g.reqs = append(g.reqs, req)
	var a answer
	switch {
	case strings.Contains(req.system(), "running summary") && g.failSummary:
		g.mu.Unlock()
		http.Error(w, `{"error":{"message":"summary refused"}}`, 400)
		return
	case strings.Contains(req.system(), "running summary"):
		a = answer{text: []string{g.summary}, in: 400, out: 30}
	case len(g.answers) > 0:
		a, g.answers = g.answers[0], g.answers[1:]
	default:
		a = answer{text: []string{"(nothing scripted)"}, in: 1, out: 1}
	}
	g.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	fl := w.(http.Flusher)
	var out io.Writer = w
	flush := fl.Flush
	if g.gzip && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer func() { _ = gz.Close() }()
		out = gz
		flush = func() { _ = gz.Flush(); fl.Flush() }
	}
	send := func(typ string, v map[string]any) {
		v["type"] = typ
		raw, _ := json.Marshal(v)
		_, _ = fmt.Fprintf(out, "event: %s\ndata: %s\n\n", typ, raw)
		flush()
	}
	send("message_start", map[string]any{"message": map[string]any{"usage": map[string]any{
		"input_tokens": a.in, "cache_read_input_tokens": a.read, "cache_creation_input_tokens": a.write, "output_tokens": 1}}})
	if a.hold {
		<-r.Context().Done()
		return
	}
	i := 0
	if len(a.text) > 0 {
		send("content_block_start", map[string]any{"index": i, "content_block": map[string]any{"type": "text", "text": ""}})
		for _, s := range a.text {
			send("content_block_delta", map[string]any{"index": i, "delta": map[string]any{"type": "text_delta", "text": s}})
		}
		send("content_block_stop", map[string]any{"index": i})
		i++
	}
	stop := "end_turn"
	if a.tool != "" {
		stop = "tool_use"
		send("content_block_start", map[string]any{"index": i, "content_block": map[string]any{"type": "tool_use", "id": fmt.Sprintf("toolu_%d", len(g.reqs)), "name": a.tool}})
		send("content_block_delta", map[string]any{"index": i, "delta": map[string]any{"type": "input_json_delta", "partial_json": a.input}})
		send("content_block_stop", map[string]any{"index": i})
		i++
	}
	for k, name := range a.more {
		stop = "tool_use"
		send("content_block_start", map[string]any{"index": i, "content_block": map[string]any{"type": "tool_use", "id": fmt.Sprintf("toolu_%d_%d", len(g.reqs), k), "name": name}})
		send("content_block_delta", map[string]any{"index": i, "delta": map[string]any{"type": "input_json_delta", "partial_json": "{}"}})
		send("content_block_stop", map[string]any{"index": i})
		i++
	}
	send("message_delta", map[string]any{"delta": map[string]any{"stop_reason": stop}, "usage": map[string]any{"output_tokens": a.out}})
	send("message_stop", map[string]any{})
}

type fixture struct {
	t       *testing.T
	g       *gateway
	r       *Runner
	db      *sql.DB
	ran     chan string
	grants  map[string]bool
	askTool string
}

func newFixture(t *testing.T, db *sql.DB, compactAt int) *fixture {
	t.Helper()
	g := newGateway(t)
	return openFixture(t, g, db, compactAt)
}

func openDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "app.db")+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func openFixture(t *testing.T, g *gateway, db *sql.DB, compactAt int) *fixture {
	t.Helper()
	key := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(key, []byte("test-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, g: g, db: db, ran: make(chan string, 8), grants: map[string]bool{}}
	var mu sync.Mutex
	tool := func(name, level string) Tool {
		return Tool{Name: name, Description: "The " + name + " tool.", Level: level,
			InputSchema: map[string]any{"type": "object"},
			Run: func(ctx context.Context, input json.RawMessage) (string, error) {
				f.ran <- name + " " + string(input)
				if name == "slow" {
					// Longer than nothing: it stops with the run.
					<-ctx.Done()
					return "", ctx.Err()
				}
				return name + " ran with " + string(input), nil
			}}
	}
	stuck := Tool{Name: "stuck", Description: "Never returns by itself.", Level: LevelRead, Timeout: 150 * time.Millisecond,
		InputSchema: map[string]any{"type": "object"},
		Run: func(ctx context.Context, _ json.RawMessage) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		}}
	r, err := Open(context.Background(), Options{
		Config: hostConfig(g.srv.URL, key, compactAt),
		DB:     db,
		Tools:  []Tool{tool("look", LevelRead), tool("change", LevelWrite), tool("risky", LevelConfirm), tool("slow", LevelAct), stuck},
		Ask: func(_ context.Context, c Call) bool {
			mu.Lock()
			defer mu.Unlock()
			return c.Tool == f.askTool
		},
		Granted: func(_ context.Context, c Call) bool {
			mu.Lock()
			defer mu.Unlock()
			return f.grants[c.Tool]
		},
		Seed: func(context.Context, string) string { return "" },
	})
	if err != nil {
		t.Fatal(err)
	}
	f.r = r
	t.Cleanup(func() { _ = r.Close() })
	return f
}

// turn runs one turn to its end. onApproval answers an approval request.
func (f *fixture) turn(conv, text, block string, onApproval func(chat.Run, chatstream.Event)) []chatstream.Event {
	f.t.Helper()
	ctxRaw, _ := json.Marshal(block)
	run, err := f.r.Start(context.Background(), chat.Turn{ConversationID: conv, RunID: "run-" + text, Text: text, Context: ctxRaw})
	if err != nil {
		f.t.Fatalf("start %q: %v", text, err)
	}
	return collect(f.t, run, onApproval)
}

func collect(t *testing.T, run chat.Run, onApproval func(chat.Run, chatstream.Event)) []chatstream.Event {
	t.Helper()
	var evs []chatstream.Event
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev, err := range run.Events() {
			if err != nil {
				t.Errorf("event error: %v", err)
				return
			}
			evs = append(evs, ev)
			if ev.Verb == chatstream.VerbApprovalRequest && onApproval != nil {
				go onApproval(run, ev)
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the run did not end")
	}
	return evs
}

func verbs(evs []chatstream.Event) string {
	var out []string
	for _, e := range evs {
		v := string(e.Verb)
		if e.Verb == chatstream.VerbActivity {
			v += ":" + e.Kind
		}
		out = append(out, v)
	}
	return strings.Join(out, " ")
}

func text(evs []chatstream.Event) string {
	var b strings.Builder
	for _, e := range evs {
		if e.Verb == chatstream.VerbPartDelta {
			b.WriteString(e.Text)
		}
	}
	return b.String()
}

func last(evs []chatstream.Event) chatstream.Event {
	if len(evs) == 0 {
		return chatstream.Event{}
	}
	return evs[len(evs)-1]
}

// Streaming text, the context ahead of the owner's message (not in the system prompt), and
// usage with the cache apart.
func TestStreamingTextContextAndUsage(t *testing.T) {
	f := newFixture(t, openDB(t), 0)
	f.g.script(answer{text: []string{"Hel", "lo"}, in: 100, read: 50, write: 10, out: 7},
		answer{text: []string{"Again"}, in: 20, read: 160, out: 3})
	evs := f.turn("c1", "hi", "<context>\nnotes window: groceries\n</context>", nil)

	want := "run.start step.start message.start part.start part.delta part.delta part.end usage message.end step.finish run.finish"
	if got := verbs(evs); got != want {
		t.Fatalf("events:\n got %s\nwant %s", got, want)
	}
	if got := text(evs); got != "Hello" {
		t.Errorf("text %q", got)
	}
	u := evs[7].Usage
	if u == nil || u.Scope != chatstream.UsageDelta || u.UncachedInput != 100 || u.CacheRead != 50 || u.CacheWrite != 10 || u.Output != 7 {
		t.Errorf("usage: %+v", u)
	}
	fin := last(evs)
	if fin.Reason != "stop" || fin.Usage == nil || fin.Usage.Scope != chatstream.UsageFinal || fin.Usage.CacheRead != 50 || fin.Usage.UncachedInput != 100 {
		t.Errorf("finish: %+v %+v", fin, fin.Usage)
	}
	if evs[0].Provider != Provider {
		t.Errorf("provider %q", evs[0].Provider)
	}

	reqs := f.g.requests()
	if len(reqs) != 1 {
		t.Fatalf("%d requests", len(reqs))
	}
	r := reqs[0]
	if strings.Contains(r.system(), "groceries") {
		t.Error("the context block is in the system prompt")
	}
	if len(r.Messages) != 1 || len(r.Messages[0].Content) != 2 || !strings.Contains(r.Messages[0].Content[0].Text, "groceries") || r.Messages[0].Content[1].Text != "hi" {
		t.Fatalf("the first message: %s", r.raw)
	}
	if r.header.Get(threadHeader) != "" {
		t.Error("the tap forwarded its thread header")
	}
	if r.header.Get("Authorization") != "Bearer test-key" {
		t.Error("the key did not reach the gateway")
	}

	// The next turn: the same system prompt, and the first message as it was, byte for byte.
	f.turn("c1", "again", "<context>\nnotes window: chores\n</context>", nil)
	reqs = f.g.requests()
	a, b := reqs[0], reqs[1]
	if a.system() != b.system() {
		t.Error("the system prompt changed between turns")
	}
	first := func(r request) string { raw, _ := json.Marshal(r.Messages[0]); return string(raw) }
	if first(a) != first(b) {
		t.Errorf("the stored first message changed:\n%s\n%s", first(a), first(b))
	}
	if len(b.Messages) != 3 || !strings.Contains(b.Messages[2].Content[0].Text, "chores") {
		t.Errorf("the second turn's context is not in front of its message: %s", b.raw)
	}
	// One thread per conversation, and another conversation has its own.
	info, _ := f.r.Info(context.Background(), "c1")
	other, _ := f.r.Info(context.Background(), "c2")
	if info.RunnerRef == "" || info.RunnerRef == other.RunnerRef {
		t.Errorf("threads: %q %q", info.RunnerRef, other.RunnerRef)
	}
}

// A tool call's round trip: the call, its result, and the model sent the result.
func TestAToolCallRoundTrip(t *testing.T) {
	f := newFixture(t, openDB(t), 0)
	f.g.script(answer{text: []string{"Looking."}, tool: "look", input: `{"q":"x"}`, in: 50, out: 5},
		answer{text: []string{"Found it."}, in: 80, read: 40, out: 4})
	evs := f.turn("c1", "find x", "", nil)
	want := "run.start step.start message.start part.start part.delta part.end usage part.start part.end part.start part.end message.end step.finish " +
		"step.start message.start part.start part.delta part.end usage message.end step.finish run.finish"
	if got := verbs(evs); got != want {
		t.Fatalf("events:\n got %s\nwant %s", got, want)
	}
	var call, res chatstream.Event
	for i, e := range evs {
		if e.Verb == chatstream.VerbPartStart && e.Kind == string(chatstream.PartToolCall) {
			call = e
			if string(evs[i+1].Final) != `{"q":"x"}` {
				t.Errorf("the call's input: %s", evs[i+1].Final)
			}
		}
		if e.Verb == chatstream.VerbPartEnd && strings.HasSuffix(e.PartID, "#result") {
			res = e
		}
	}
	if string(call.Meta["name"]) != `"look"` || string(call.Meta["level"]) != `"read"` {
		t.Errorf("the call: %+v", call.Meta)
	}
	if !strings.Contains(string(res.Final), `look ran with {\"q\":\"x\"}`) || res.PartID != call.PartID+"#result" {
		t.Errorf("the result: %s %s", res.PartID, res.Final)
	}
	if got := <-f.ran; got != `look {"q":"x"}` {
		t.Errorf("ran %q", got)
	}
	reqs := f.g.requests()
	if len(reqs) != 2 || !strings.Contains(reqs[1].raw, `"tool_result"`) || !strings.Contains(reqs[1].raw, "look ran with") {
		t.Errorf("the model was not sent the result: %s", reqs[len(reqs)-1].raw)
	}
	// Usage: one delta per request.
	fin := last(evs).Usage
	if fin.UncachedInput != 130 || fin.CacheRead != 40 || fin.Output != 9 {
		t.Errorf("final usage %+v", fin)
	}
}

// A write call asks first; the answer goes back to the host; a standing grant or the
// routing rule's ask-first change who is asked.
func TestApprovals(t *testing.T) {
	f := newFixture(t, openDB(t), 0)
	f.g.script(answer{tool: "change", input: `{"v":1}`, in: 10, out: 1}, answer{text: []string{"Changed."}, in: 10, out: 1})
	var asked chatstream.Event
	evs := f.turn("c1", "change it", "", func(run chat.Run, ev chatstream.Event) {
		asked = ev
		if err := run.Answer(context.Background(), ev.ApprovalID, chat.Decision{Allow: true, Scope: chat.ScopeOnce}); err != nil {
			t.Errorf("answer: %v", err)
		}
	})
	if asked.ApprovalID == "" || !strings.Contains(string(asked.Descriptor), `"tool":"change"`) || !strings.Contains(string(asked.Descriptor), `"standing":true`) {
		t.Fatalf("the approval request: %+v", asked)
	}
	if got := <-f.ran; got != `change {"v":1}` {
		t.Errorf("ran %q", got)
	}
	if !strings.Contains(verbs(evs), "activity:chat.approval") {
		t.Errorf("no approval outcome: %s", verbs(evs))
	}

	// Denied: it does not run, and the model is told.
	f.g.script(answer{tool: "change", input: `{"v":2}`, in: 10, out: 1}, answer{text: []string{"OK."}, in: 10, out: 1})
	f.turn("c1", "change again", "", func(run chat.Run, ev chatstream.Event) {
		_ = run.Answer(context.Background(), ev.ApprovalID, chat.Decision{Allow: false})
	})
	select {
	case got := <-f.ran:
		t.Fatalf("a denied call ran: %s", got)
	default:
	}
	if reqs := f.g.requests(); !strings.Contains(reqs[len(reqs)-1].raw, "declined") {
		t.Error("the model was not told it was declined")
	}

	// A standing grant: no question.
	f.grants["change"] = true
	f.g.script(answer{tool: "change", input: `{"v":3}`, in: 10, out: 1}, answer{text: []string{"Done."}, in: 10, out: 1})
	evs = f.turn("c1", "and again", "", func(chat.Run, chatstream.Event) { t.Error("asked despite a standing grant") })
	if strings.Contains(verbs(evs), "approval.request") {
		t.Error("asked despite a standing grant")
	}
	<-f.ran

	// A read tool runs at once, unless the routing rule asks first.
	f.askTool = "look"
	f.g.script(answer{tool: "look", input: `{}`, in: 10, out: 1}, answer{text: []string{"Seen."}, in: 10, out: 1})
	evs = f.turn("c1", "look", "", func(run chat.Run, ev chatstream.Event) {
		_ = run.Answer(context.Background(), ev.ApprovalID, chat.Decision{Allow: true, Scope: chat.ScopeOnce})
	})
	if !strings.Contains(verbs(evs), "approval.request") {
		t.Error("an ask-first read call did not ask")
	}
	<-f.ran
}

// Cancel stops the run: a model request in flight, or a tool call in progress.
func TestCancelStopsTheRun(t *testing.T) {
	f := newFixture(t, openDB(t), 0)
	f.g.script(answer{hold: true, in: 10})
	run, err := f.r.Start(context.Background(), chat.Turn{ConversationID: "c1", RunID: "r1", Text: "wait"})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for len(f.g.requests()) == 0 {
			time.Sleep(10 * time.Millisecond)
		}
		run.Cancel()
	}()
	evs := collect(t, run, nil)
	if l := last(evs); l.Verb != chatstream.VerbRunAbort || l.Reason != chat.AbortCanceled {
		t.Fatalf("ended with %+v (%s)", l, verbs(evs))
	}

	// A tool that waits stops with the run, not after the host's 90 seconds.
	f.g.script(answer{tool: "slow", input: `{}`, in: 10, out: 1})
	run, err = f.r.Start(context.Background(), chat.Turn{ConversationID: "c1", RunID: "r2", Text: "slow"})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		<-f.ran
		run.Cancel()
	}()
	evs = collect(t, run, nil)
	if l := last(evs); l.Verb != chatstream.VerbRunAbort {
		t.Fatalf("ended with %+v (%s)", l, verbs(evs))
	}
	// The conversation is free again.
	f.g.script(answer{text: []string{"Back."}, in: 10, out: 1})
	if got := text(f.turn("c1", "after", "", nil)); got != "Back." {
		t.Errorf("after a cancel: %q", got)
	}
}

// Past CompactAt, the older part of the thread is summarized and the next request carries
// the summary and the recent turns; the panel is told, with before and after.
func TestCompaction(t *testing.T) {
	f := newFixture(t, openDB(t), 500)
	f.g.script(answer{text: []string{"one done"}, in: 100, out: 5},
		answer{text: []string{"two done"}, in: 600, out: 5}, // past CompactAt
		answer{text: []string{"three done"}, in: 120, read: 30, out: 5})
	f.turn("c1", "first ask", "", nil)
	f.turn("c1", "second ask", "", nil)
	evs := f.turn("c1", "third ask", "", nil)

	var start, done map[string]any
	for _, e := range evs {
		if e.Verb == chatstream.VerbActivity && e.Kind == ActivityCompact {
			var v map[string]any
			_ = json.Unmarshal(e.Value, &v)
			switch v["state"] {
			case "start":
				start = v
			case "done":
				done = v
			}
		}
	}
	if start == nil || done == nil {
		t.Fatalf("no compaction notice: %s", verbs(evs))
	}
	if done["before"] != float64(605) || done["after"] != float64(150) {
		t.Errorf("compaction sizes: %v", done)
	}

	reqs := f.g.requests()
	if len(reqs) != 4 {
		t.Fatalf("%d requests, want 3 turns and 1 summary", len(reqs))
	}
	sum, next := reqs[2], reqs[3]
	if !strings.Contains(sum.system(), "running summary") || !strings.Contains(sum.raw, "first ask") || strings.Contains(sum.raw, "third ask") {
		t.Errorf("the summary request: %s", sum.raw)
	}
	if len(next.Messages) == 0 || next.Messages[0].Content[0].Text != "<summary>\nSUMMARY-ONE\n</summary>" {
		t.Errorf("the next request does not carry the summary: %s", next.raw)
	}
	if strings.Contains(next.raw, "first ask") || !strings.Contains(next.raw, "second ask") || !strings.Contains(next.raw, "third ask") {
		t.Errorf("the next request is not the summary and the recent turns: %s", next.raw)
	}
	// The summary's own usage is reported too.
	fin := last(evs).Usage
	if fin.UncachedInput != 520 || fin.CacheRead != 30 || fin.Output != 35 {
		t.Errorf("final usage %+v", fin)
	}
}

// A restart resumes the same thread, its summary included.
func TestARestartResumesTheThread(t *testing.T) {
	db := openDB(t)
	f := newFixture(t, db, 0)
	f.g.script(answer{text: []string{"noted"}, in: 10, out: 1})
	f.turn("c1", "remember the blue folder", "", nil)
	before, _ := f.r.Info(context.Background(), "c1")
	_ = f.r.Close()

	g := openFixture(t, f.g, db, 0)
	after, _ := g.r.Info(context.Background(), "c1")
	if before.RunnerRef != after.RunnerRef {
		t.Fatalf("thread %q became %q", before.RunnerRef, after.RunnerRef)
	}
	f.g.script(answer{text: []string{"the blue one"}, in: 10, out: 1})
	g.turn("c1", "which folder?", "", nil)
	reqs := f.g.requests()
	if !strings.Contains(reqs[len(reqs)-1].raw, "remember the blue folder") {
		t.Error("the resumed thread lost its history")
	}
}

// The first turn of a thread carries the seed; later turns and a recorded answer do not
// repeat it.
func TestTheSeedLeadsTheFirstTurnOnly(t *testing.T) {
	f := newFixture(t, openDB(t), 0)
	f.r.opts.Seed = func(context.Context, string) string { return "[earlier history is in the transcript]" }
	f.g.script(answer{text: []string{"a"}, in: 1, out: 1}, answer{text: []string{"b"}, in: 1, out: 1})
	f.turn("c1", "one", "<context>x</context>", nil)
	if err := f.r.Record(context.Background(), chat.Turn{ConversationID: "c1", Text: "ping"}, "pong"); err != nil {
		t.Fatal(err)
	}
	f.turn("c1", "two", "<context>y</context>", nil)
	reqs := f.g.requests()
	if n := strings.Count(reqs[1].raw, "earlier history is in the transcript"); n != 1 {
		t.Errorf("the seed is in the thread %d times", n)
	}
	if !strings.HasPrefix(reqs[0].Messages[0].Content[0].Text, "[earlier history") {
		t.Errorf("the first message: %s", reqs[0].raw)
	}
	if !strings.Contains(reqs[1].raw, `"ping"`) || !strings.Contains(reqs[1].raw, `"pong"`) {
		t.Error("the recorded exchange is not in the thread")
	}
	msgs, _ := f.r.History(context.Background(), "c1")
	if len(msgs) != 6 {
		t.Errorf("%d messages", len(msgs))
	}
}

func TestOpenNeedsAModel(t *testing.T) {
	if _, err := Open(context.Background(), Options{DB: openDB(t)}); err == nil {
		t.Fatal("opened with no model")
	}
}

func hostConfig(url, key string, compactAt int) host.Config {
	return host.Config{Model: "test-model", BaseURL: url, KeyFile: key, System: "You are a test agent.", CompactAt: compactAt}
}
