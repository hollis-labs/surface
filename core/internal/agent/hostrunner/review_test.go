package hostrunner

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	chat "github.com/hollis-labs/libs/agent/chat"
	chatstream "github.com/hollis-labs/libs/ui/ui-go/chatstream"
)

// slowCollect reads a run's events, pausing on each, as a Service that is slow to publish.
func slowCollect(t *testing.T, run chat.Run, pause time.Duration) []chatstream.Event {
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
			time.Sleep(pause)
		}
	}()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("the run did not end")
	}
	return evs
}

// A reader slower than the run gets every event: the host's subscription never falls
// behind and closes.
func TestASlowReaderGetsEveryEvent(t *testing.T) {
	f := newFixture(t, openDB(t), 0)
	chunks := make([]string, 600)
	for i := range chunks {
		chunks[i] = "x"
	}
	f.g.script(answer{text: chunks, in: 10, out: 600})
	run, err := f.r.Start(context.Background(), chat.Turn{ConversationID: "c1", RunID: "r1", Text: "long"})
	if err != nil {
		t.Fatal(err)
	}
	evs := slowCollect(t, run, 3*time.Millisecond)
	if got := text(evs); len(got) != 600 {
		t.Errorf("read %d of 600 deltas", len(got))
	}
	if l := last(evs); l.Verb != chatstream.VerbRunFinish {
		t.Errorf("ended with %+v", l)
	}
}

// cutWriter fails every write after frames SSE frames: the host's handler then stops, as
// it does when it closes a reader that fell behind.
type cutWriter struct {
	http.ResponseWriter
	frames int
}

func (c *cutWriter) Write(b []byte) (int, error) {
	if c.frames <= 0 {
		return 0, errors.New("cut")
	}
	c.frames -= strings.Count(string(b), "\n\n")
	return c.ResponseWriter.Write(b)
}

func (c *cutWriter) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// interrupt wraps the runner's handler: the first replies are refused (a lookup on a busy
// database) or cut short, then it serves as usual.
func interrupt(r *Runner, refuse, cut int) *int {
	inner := r.handler
	var mu sync.Mutex
	calls := 0
	r.handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		switch {
		case n <= refuse:
			http.Error(w, `{"message":"No such run."}`, 404)
		case n <= refuse+cut:
			inner.ServeHTTP(&cutWriter{ResponseWriter: w, frames: 5}, req)
		default:
			inner.ServeHTTP(w, req)
		}
	})
	return &calls
}

// A read that ends early, or a reply that is not the stream, is taken up again where it
// stopped: the turn is whole, nothing twice.
func TestAReadThatEndsEarlyIsResumed(t *testing.T) {
	resumeWait = 5 * time.Millisecond
	f := newFixture(t, openDB(t), 0)
	calls := interrupt(f.r, 2, 3)
	chunks := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	f.g.script(answer{text: chunks, in: 10, out: 10})
	evs := f.turn("c1", "go", "", nil)
	if got := text(evs); got != "abcdefghij" {
		t.Errorf("text %q", got)
	}
	if l := last(evs); l.Verb != chatstream.VerbRunFinish {
		t.Fatalf("ended with %+v (%s)", l, verbs(evs))
	}
	if *calls < 6 {
		t.Errorf("%d reads; the early ends were not resumed", *calls)
	}
	starts := strings.Count(verbs(evs), "run.start")
	if starts != 1 {
		t.Errorf("%d run.start events", starts)
	}
}

// When the events cannot be read at all, the host's run is stopped (so the conversation is
// free) and the turn ends with run.error.
func TestAnUnreadableRunIsStoppedAndLetsGo(t *testing.T) {
	resumeWait = 5 * time.Millisecond
	f := newFixture(t, openDB(t), 0)
	inner := f.r.handler
	f.r.handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "busy", 404) })
	f.g.script(answer{hold: true, in: 10})
	run, err := f.r.Start(context.Background(), chat.Turn{ConversationID: "c1", RunID: "r1", Text: "stuck"})
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, run, nil)
	if l := last(evs); l.Verb != chatstream.VerbRunError || l.Code != CodeTurnFailed {
		t.Fatalf("ended with %+v", l)
	}
	f.r.handler = inner
	f.g.script(answer{text: []string{"free"}, in: 1, out: 1})
	deadline := time.Now().Add(5 * time.Second)
	for {
		next, err := f.r.Start(context.Background(), chat.Turn{ConversationID: "c1", RunID: "r2", Text: "next"})
		if err == nil {
			if got := text(collect(t, next, nil)); got != "free" {
				t.Errorf("the next turn: %q", got)
			}
			return
		}
		if !errors.Is(err, chat.ErrBusy) || time.Now().After(deadline) {
			t.Fatalf("the conversation was not let go: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A tool runs on its own clock: one past its time is given up and the model told so. A tool
// that waits for the owner gets 70 minutes, any other 5.
func TestAToolPastItsTimeIsGivenUp(t *testing.T) {
	if (Tool{}).timeout() != 5*time.Minute || (Tool{WaitsForOwner: true}).timeout() != 70*time.Minute ||
		(Tool{Timeout: time.Second, WaitsForOwner: true}).timeout() != time.Second {
		t.Fatal("tool timeouts")
	}
	f := newFixture(t, openDB(t), 0)
	f.g.script(answer{tool: "stuck", input: `{}`, in: 1, out: 1}, answer{text: []string{"ok"}, in: 1, out: 1})
	start := time.Now()
	evs := f.turn("c1", "stuck", "", nil)
	if time.Since(start) > 10*time.Second {
		t.Fatal("the tool was not given up")
	}
	for _, e := range evs {
		if e.Verb == chatstream.VerbPartEnd && strings.HasSuffix(e.PartID, "#result") {
			if !strings.Contains(string(e.Final), "given up") || !strings.Contains(string(e.Final), `"is_error":true`) {
				t.Errorf("the result: %s", e.Final)
			}
			return
		}
	}
	t.Fatalf("no result: %s", verbs(evs))
}

// A gzipped answer does not hide the usage: the tap asks for none.
func TestTheUsageSplitSurvivesAGzippingGateway(t *testing.T) {
	f := newFixture(t, openDB(t), 0)
	f.g.gzip = true
	f.g.script(answer{text: []string{"zip"}, in: 70, read: 300, write: 20, out: 5})
	evs := f.turn("c1", "hi", "", nil)
	if text(evs) != "zip" {
		t.Fatalf("text %q", text(evs))
	}
	if u := last(evs).Usage; u == nil || u.UncachedInput != 70 || u.CacheRead != 300 || u.CacheWrite != 20 {
		t.Errorf("usage %+v", u)
	}
	if ae := f.g.requests()[0].header.Get("Accept-Encoding"); ae != "" && ae != "gzip" {
		t.Errorf("Accept-Encoding %q", ae)
	}
}

func summaries(g *gateway) int {
	n := 0
	for _, r := range g.requests() {
		if strings.Contains(r.system(), "running summary") {
			n++
		}
	}
	return n
}

// A thread whose fixed part alone passes CompactAt is not summarized on every turn, nor
// one that a compaction barely shrank.
func TestCompactionDoesNotLoop(t *testing.T) {
	f := newFixture(t, openDB(t), 500)
	for i := range 4 {
		f.g.script(answer{text: []string{"t"}, in: 600, out: 5})
		f.turn("c1", "turn "+string(rune('a'+i)), "", nil)
	}
	if n := summaries(f.g); n != 0 {
		t.Errorf("%d summaries for a thread that cannot get under CompactAt", n)
	}

	g := newFixture(t, openDB(t), 500)
	g.g.script(answer{text: []string{"1"}, in: 100, out: 5}, answer{text: []string{"2"}, in: 600, out: 5},
		answer{text: []string{"3"}, in: 560, out: 5}, answer{text: []string{"4"}, in: 570, out: 5})
	for _, s := range []string{"one", "two", "three", "four"} {
		g.turn("c1", s, "", nil)
	}
	if n := summaries(g.g); n != 1 {
		t.Errorf("%d summaries, want 1: the second would gain under a tenth", n)
	}
}

// The compaction notice has its after even when the reader is slow.
func TestCompactionNoticeWithASlowReader(t *testing.T) {
	f := newFixture(t, openDB(t), 500)
	f.g.script(answer{text: []string{"one"}, in: 100, out: 5}, answer{text: []string{"two"}, in: 600, out: 5},
		answer{text: []string{"a", "b", "c", "d"}, in: 120, read: 30, out: 5})
	f.turn("c1", "first", "", nil)
	f.turn("c1", "second", "", nil)
	run, err := f.r.Start(context.Background(), chat.Turn{ConversationID: "c1", RunID: "r3", Text: "third"})
	if err != nil {
		t.Fatal(err)
	}
	evs := slowCollect(t, run, 30*time.Millisecond)
	for _, e := range evs {
		var v map[string]any
		if e.Kind == ActivityCompact && json.Unmarshal(e.Value, &v) == nil && v["state"] == "done" {
			if v["after"] != float64(150) {
				t.Errorf("after: %v", v)
			}
			return
		}
	}
	t.Fatalf("no notice: %s", verbs(evs))
}

// A failed compaction is reported and the turn goes on.
func TestAFailedCompaction(t *testing.T) {
	f := newFixture(t, openDB(t), 500)
	f.g.failSummary = true
	f.g.script(answer{text: []string{"1"}, in: 100, out: 5}, answer{text: []string{"2"}, in: 600, out: 5}, answer{text: []string{"3"}, in: 700, out: 5})
	f.turn("c1", "one", "", nil)
	f.turn("c1", "two", "", nil)
	evs := f.turn("c1", "three", "", nil)
	if !strings.Contains(verbs(evs), "activity:host.compact activity:host.compact") {
		t.Fatalf("events: %s", verbs(evs))
	}
	failed := false
	for _, e := range evs {
		var v map[string]any
		if e.Kind == ActivityCompact && json.Unmarshal(e.Value, &v) == nil && v["state"] == "failed" {
			failed = true
		}
	}
	if !failed || text(evs) != "3" || last(evs).Verb != chatstream.VerbRunFinish {
		t.Errorf("failed %v, text %q, end %+v", failed, text(evs), last(evs))
	}
}

// Several tool calls from one model request are one step.
func TestSeveralToolCallsInOneRequestAreOneStep(t *testing.T) {
	f := newFixture(t, openDB(t), 0)
	f.g.script(answer{tool: "look", input: `{"n":1}`, more: []string{"look"}, in: 10, out: 1}, answer{text: []string{"both"}, in: 10, out: 1})
	evs := f.turn("c1", "two looks", "", nil)
	// The answer has no text: its usage comes before anything opens the step.
	want := "run.start usage step.start message.start part.start part.end part.start part.end part.start part.end part.start part.end message.end step.finish " +
		"step.start message.start part.start part.delta part.end usage message.end step.finish run.finish"
	if got := verbs(evs); got != want {
		t.Fatalf("events:\n got %s\nwant %s", got, want)
	}
}

// The context put in front of a message is clipped as the host clips its own.
func TestTheContextIsClipped(t *testing.T) {
	f := newFixture(t, openDB(t), 0)
	f.g.script(answer{text: []string{"ok"}, in: 1, out: 1})
	f.turn("c1", "big", strings.Repeat("é", 30_000), nil)
	got := []rune(f.g.requests()[0].Messages[0].Content[0].Text)
	if len(got) != maxContextChars+1 || got[len(got)-1] != '…' {
		t.Errorf("context of %d characters", len(got))
	}
}

// An answer the host refused leaves the approval open, to be answered again.
func TestAnApprovalIsResolvedOnlyWhenTheHostTakesIt(t *testing.T) {
	f := newFixture(t, openDB(t), 0)
	x := newRun(f.r, chat.Turn{ConversationID: "c1"}, OwnerKey("c1"), "thr", 0)
	x.hostRun = "run_none"
	x.asked("apr_1")
	for range 2 {
		err := x.Answer(context.Background(), "apr_1", chat.Decision{Allow: true})
		if err == nil || strings.Contains(err.Error(), "already resolved") {
			t.Fatalf("answer: %v", err)
		}
	}
}

// A confirm tool asks every time: a standing grant does not cover it, and "always" stands
// for that call only.
func TestAConfirmToolAsksEveryTime(t *testing.T) {
	f := newFixture(t, openDB(t), 0)
	f.grants["risky"] = true
	for i := range 2 {
		f.g.script(answer{tool: "risky", input: `{}`, in: 1, out: 1}, answer{text: []string{"done"}, in: 1, out: 1})
		asked := false
		f.turn("c1", "risky "+string(rune('a'+i)), "", func(run chat.Run, ev chatstream.Event) {
			asked = true
			if strings.Contains(string(ev.Descriptor), `"standing":true`) {
				t.Error("a confirm tool offered a standing answer")
			}
			_ = run.Answer(context.Background(), ev.ApprovalID, chat.Decision{Allow: true, Scope: chat.ScopeAlways})
		})
		if !asked {
			t.Fatalf("call %d did not ask", i+1)
		}
		<-f.ran
	}
}

// "This conversation" covers later calls in the conversation, not in another.
func TestTheConversationScope(t *testing.T) {
	f := newFixture(t, openDB(t), 0)
	turn := func(conv string) bool {
		f.g.script(answer{tool: "change", input: `{}`, in: 1, out: 1}, answer{text: []string{"done"}, in: 1, out: 1})
		asked := false
		f.turn(conv, "change", "", func(run chat.Run, ev chatstream.Event) {
			asked = true
			_ = run.Answer(context.Background(), ev.ApprovalID, chat.Decision{Allow: true, Scope: chat.ScopeConversation})
		})
		<-f.ran
		return asked
	}
	if !turn("c1") {
		t.Fatal("the first call did not ask")
	}
	if turn("c1") {
		t.Error("asked again in the same conversation")
	}
	if !turn("c2") {
		t.Error("another conversation was covered")
	}
}

// A run stopped because the runner closes ends with the shutdown reason.
func TestShutdownAbortsWithItsReason(t *testing.T) {
	f := newFixture(t, openDB(t), 0)
	f.g.script(answer{hold: true, in: 1})
	run, err := f.r.Start(context.Background(), chat.Turn{ConversationID: "c1", RunID: "r1", Text: "wait"})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for len(f.g.requests()) == 0 {
			time.Sleep(10 * time.Millisecond)
		}
		_ = f.r.Close()
	}()
	evs := collect(t, run, nil)
	if l := last(evs); l.Verb != chatstream.VerbRunAbort || l.Reason != chat.AbortShutdown {
		t.Fatalf("ended with %+v", l)
	}
}
