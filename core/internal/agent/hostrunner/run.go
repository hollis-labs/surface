package hostrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strconv"
	"sync"
	"time"

	chat "github.com/hollis-labs/libs/agent/chat"
	chatstream "github.com/hollis-labs/libs/ui/ui-go/chatstream"
	"github.com/hollis-labs/libs/ui/ui-go/ssekit"
)

// CodeTurnFailed is the run.error code of a turn whose stream could not be read.
const CodeTurnFailed = "turn_failed"

// How hard the runner tries to read a host run's events again when the read ends before
// the run did (the host closes a reader that falls behind; a lookup can fail on a busy
// database): resumeTries reads in a row that bring nothing, resumeWait apart.
var (
	resumeTries = 5
	resumeWait  = 100 * time.Millisecond
)

// usageStep is the usage of one model request, as the gateway reported it. The tap fills
// it as the response streams: input at message_start, output at message_delta. summary
// marks the host's compaction request.
type usageStep struct {
	mu                   sync.Mutex
	in, read, write, out int
	started, stopped     bool
	summary              bool
}

func (u *usageStep) snapshot() (in, read, write, out int, started, stopped bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.in, u.read, u.write, u.out, u.started, u.stopped
}

func (u *usageStep) isSummary() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.summary
}

// Approval states.
const (
	approvalOpen = iota
	approvalAnswering
	approvalAnswered
)

// run is one turn on the host.
type run struct {
	r             *Runner
	turn          chat.Turn
	owner, thread string
	hostRun       string
	// ctx ends when the turn is canceled or over: the tool calls it runs stop with it.
	ctx  context.Context
	stop context.CancelFunc
	// before is the size of the thread's last request when the turn began, in tokens.
	before int

	mu        sync.Mutex
	canceled  bool
	steps     []*usageStep   // the model requests of this run, as the tap saw them
	approvals map[string]int // approval ids the host asked, and their state

	t translator
}

func newRun(r *Runner, turn chat.Turn, owner, thread string, before int) *run {
	ctx, stop := context.WithCancel(r.ctx)
	x := &run{r: r, turn: turn, owner: owner, thread: thread, ctx: ctx, stop: stop, before: before,
		approvals: map[string]int{}}
	x.t = translator{x: x, callOf: map[string]string{}, textOf: map[string]string{}, results: map[string]*result{}, afterIdx: -1}
	return x
}

func (x *run) newStep(summary bool) *usageStep {
	u := &usageStep{summary: summary}
	x.mu.Lock()
	x.steps = append(x.steps, u)
	x.mu.Unlock()
	return u
}

// stepsFrom are the model requests from index i on.
func (x *run) stepsFrom(i int) []*usageStep {
	x.mu.Lock()
	defer x.mu.Unlock()
	if i >= len(x.steps) {
		return nil
	}
	return append([]*usageStep(nil), x.steps[i:]...)
}

func (x *run) asked(approvalID string) {
	x.mu.Lock()
	if _, ok := x.approvals[approvalID]; !ok {
		x.approvals[approvalID] = approvalOpen
	}
	x.mu.Unlock()
}

func (x *run) wasCanceled() bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.canceled
}

// Cancel asks the turn to stop. The host ends it with run.abort.
func (x *run) Cancel() {
	x.mu.Lock()
	x.canceled = true
	x.mu.Unlock()
	x.stop()
	_ = x.r.h.Cancel(x.owner, x.hostRun)
}

// Answer resolves an approval the host asked for in this run. It is resolved only once the
// host has taken the answer: an answer the host refused can be given again.
func (x *run) Answer(_ context.Context, approvalID string, d chat.Decision) error {
	x.mu.Lock()
	state, ok := x.approvals[approvalID]
	if ok && state == approvalOpen {
		x.approvals[approvalID] = approvalAnswering
	}
	x.mu.Unlock()
	if !ok {
		return fmt.Errorf("hostrunner: no approval %q in this run", approvalID)
	}
	if state != approvalOpen {
		return fmt.Errorf("hostrunner: approval %q is already resolved", approvalID)
	}
	decision := "deny"
	if d.Allow {
		switch d.Scope {
		case chat.ScopeConversation:
			decision = "session"
		case chat.ScopeAlways:
			decision = "always"
		default:
			decision = "once"
		}
	}
	err := x.r.h.Approve(x.owner, x.hostRun, approvalID, decision)
	x.mu.Lock()
	if err != nil {
		x.approvals[approvalID] = approvalOpen
	} else {
		x.approvals[approvalID] = approvalAnswered
	}
	x.mu.Unlock()
	if err != nil {
		return fmt.Errorf("hostrunner: approval %q: %w", approvalID, err)
	}
	return nil
}

// bufPipe is an in-memory pipe whose writer never waits for the reader: the host's
// handler writes as fast as its run goes, so its subscription never falls behind however
// slowly the Service takes the events.
type bufPipe struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    bytes.Buffer
	closed bool // by the writer
	gone   bool // by the reader
}

func newBufPipe() *bufPipe {
	p := &bufPipe{}
	p.cond = sync.NewCond(&p.mu)
	return p
}

func (p *bufPipe) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.gone {
		return 0, io.ErrClosedPipe
	}
	n, _ := p.buf.Write(b)
	p.cond.Broadcast()
	return n, nil
}

func (p *bufPipe) Read(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for p.buf.Len() == 0 && !p.closed && !p.gone {
		p.cond.Wait()
	}
	if p.buf.Len() > 0 {
		return p.buf.Read(b)
	}
	if p.gone {
		return 0, io.ErrClosedPipe
	}
	return 0, io.EOF
}

func (p *bufPipe) closeWrite() { p.mu.Lock(); p.closed = true; p.cond.Broadcast(); p.mu.Unlock() }
func (p *bufPipe) closeRead() {
	p.mu.Lock()
	p.gone = true
	p.buf.Reset()
	p.cond.Broadcast()
	p.mu.Unlock()
}

// respWriter is the response of the host's own event route, read in process.
type respWriter struct {
	header http.Header
	p      *bufPipe
}

func (w *respWriter) Header() http.Header         { return w.header }
func (w *respWriter) Write(b []byte) (int, error) { return w.p.Write(b) }
func (w *respWriter) WriteHeader(int)             {}
func (w *respWriter) Flush()                      {}

// readOnce reads the host run's events after the event with id after, handing each to fn
// with its id, until the reply ends or fn returns false. It reports how many it read.
func (x *run) readOnce(ctx context.Context, after uint64, fn func(chatstream.Event, uint64) bool) int {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, handlerPrefix+"/runs/"+x.hostRun+"/events", nil)
	if err != nil {
		return 0
	}
	req.Header.Set(ownerHeader, x.owner)
	if after > 0 {
		req.Header.Set("Last-Event-ID", strconv.FormatUint(after, 10))
	}
	p := newBufPipe()
	defer p.closeRead()
	go func() {
		defer p.closeWrite()
		x.r.handler.ServeHTTP(&respWriter{header: http.Header{}, p: p}, req)
	}()
	n := 0
	for sse, err := range ssekit.Read(p) {
		if err != nil {
			return n
		}
		if len(sse.Data) == 0 {
			continue
		}
		var ev chatstream.Event
		if json.Unmarshal(sse.Data, &ev) != nil || ev.Verb == "" {
			continue
		}
		id, _ := strconv.ParseUint(sse.ID, 10, 64)
		n++
		if !fn(ev, id) {
			return n
		}
	}
	return n
}

// Events yields the turn's events on the conversation wire. It is read once.
//
// A read of the host's events that ends before the run's terminal event is taken up again
// where it stopped (Last-Event-ID). Only when that keeps failing is the host's run stopped,
// so it frees the conversation, and the turn ended with run.error.
func (x *run) Events() iter.Seq2[chatstream.Event, error] {
	return func(yield func(chatstream.Event, error) bool) {
		defer x.r.release(x)
		defer x.stop()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var after uint64
		done, gone := false, false
		handle := func(ev chatstream.Event, id uint64) bool {
			if id > after {
				after = id
			}
			for _, out := range x.t.translate(ev) {
				done = done || out.IsTerminal()
				if !yield(out, nil) {
					gone = true
					return false
				}
			}
			return !done
		}
		fails, stopped := 0, false
		for !done && !gone {
			if x.readOnce(ctx, after, handle) > 0 {
				fails = 0
				continue
			}
			if done || gone {
				break
			}
			if fails++; fails < resumeTries {
				time.Sleep(time.Duration(fails) * resumeWait)
				continue
			}
			if stopped {
				break
			}
			// The run's events cannot be read: stop it, so it lets go of the conversation, and
			// try once more for its own end.
			x.r.log.Warn("hostrunner: the run's events could not be read; stopping it", "run", x.turn.RunID, "host_run", x.hostRun, "after", after)
			_ = x.r.h.Cancel(x.owner, x.hostRun)
			stopped, fails = true, 0
		}
		if gone {
			// The Service is done with this run: stop it.
			_ = x.r.h.Cancel(x.owner, x.hostRun)
			return
		}
		if done {
			return
		}
		for _, out := range x.t.end(chatstream.Event{Verb: chatstream.VerbRunError, Code: CodeTurnFailed, Message: "the turn's events could not be read"}) {
			if !yield(out, nil) {
				return
			}
		}
	}
}

// result is a tool result the host is streaming.
type result struct {
	call    string
	isError bool
	text    []byte
}

// translator maps the host's run events to the conversation wire's, in the shape Nanite's
// runner gives them: each model step is a step with one assistant message that holds its
// text, its tool calls and their results. It is used only by the goroutine reading Events.
type translator struct {
	x        *run
	step     int
	stepOpen bool
	part     string
	parts    int
	callOf   map[string]string  // host tool_call part id -> the call's id
	textOf   map[string]string  // host text part id -> the wire's
	results  map[string]*result // host tool_result part id -> the result so far

	// Usage: the tap's steps reported so far, and the totals reported.
	usedSteps                  int
	uncached, read, write, out int
	// compacted: the thread was compacted in this run and the notice waits for the size
	// of the request after it, the step at afterIdx.
	compacted bool
	afterIdx  int
}

func raw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return b
}

func activity(kind string, value any) chatstream.Event {
	return chatstream.Event{Verb: chatstream.VerbActivity, Kind: kind, Value: raw(value)}
}

func (t *translator) stepID() string    { return fmt.Sprintf("step-%d", t.step) }
func (t *translator) messageID() string { return fmt.Sprintf("msg-%d", t.step) }

func (t *translator) closePart(out []chatstream.Event) []chatstream.Event {
	if t.part == "" {
		return out
	}
	out = append(out, chatstream.Event{Verb: chatstream.VerbPartEnd, PartID: t.part})
	t.part = ""
	return out
}

func (t *translator) closeStep(out []chatstream.Event) []chatstream.Event {
	if !t.stepOpen {
		return out
	}
	out = t.closePart(out)
	out = append(out,
		chatstream.Event{Verb: chatstream.VerbMessageEnd, MessageID: t.messageID()},
		chatstream.Event{Verb: chatstream.VerbStepFinish, StepID: t.stepID()})
	t.stepOpen = false
	return out
}

// content makes sure a step and its assistant message are open for model output. A step is
// one model request: it closes when the host starts the next (message.start).
func (t *translator) content(out []chatstream.Event) []chatstream.Event {
	if !t.stepOpen {
		t.step++
		t.stepOpen = true
		out = append(out,
			chatstream.Event{Verb: chatstream.VerbStepStart, StepID: t.stepID()},
			chatstream.Event{Verb: chatstream.VerbMessageStart, MessageID: t.messageID(), Role: "assistant"})
	}
	return out
}

// usage reports what the tap has seen of the run's requests since the last report, as
// deltas; then whatever the host counted that the tap did not see, as uncached input.
func (t *translator) usage(out []chatstream.Event, hostTotal *chatstream.Usage, all bool) []chatstream.Event {
	for _, u := range t.x.stepsFrom(t.usedSteps) {
		in, read, write, o, started, stopped := u.snapshot()
		if !stopped && !all {
			break // still streaming: reported with a later event
		}
		t.usedSteps++
		if !started && o == 0 {
			continue
		}
		if !u.isSummary() {
			t.x.r.observe(t.x.thread, in+read+write)
		}
		t.uncached += in
		t.read += read
		t.write += write
		t.out += o
		out = append(out, chatstream.Event{Verb: chatstream.VerbUsage, Usage: &chatstream.Usage{Scope: chatstream.UsageDelta,
			UncachedInput: in, CacheRead: read, CacheWrite: write, Output: o}})
	}
	if hostTotal != nil {
		// The host counts every token it was sent as input. What it counted beyond what the
		// tap reported is a request the tap did not see.
		hostIn := hostTotal.UncachedInput + hostTotal.CacheRead + hostTotal.CacheWrite
		missIn := hostIn - (t.uncached + t.read + t.write)
		missOut := hostTotal.Output - t.out
		if missIn > 0 || missOut > 0 {
			missIn, missOut = max(missIn, 0), max(missOut, 0)
			if missIn > 0 {
				t.x.r.log.Warn("hostrunner: usage the tap did not see is counted as uncached input", "run", t.x.turn.RunID, "tokens", missIn)
			}
			t.uncached += missIn
			t.out += missOut
			out = append(out, chatstream.Event{Verb: chatstream.VerbUsage, Usage: &chatstream.Usage{Scope: chatstream.UsageDelta,
				UncachedInput: missIn, Output: missOut}})
		}
	}
	return out
}

// compactDone reports the compaction once the first request after it has begun: its input
// is the size of the thread as the model is now sent it.
func (t *translator) compactDone(out []chatstream.Event, final bool) []chatstream.Event {
	if !t.compacted {
		return out
	}
	v := map[string]any{"state": "done", "before": t.x.before}
	if t.afterIdx >= 0 {
		if next := t.x.stepsFrom(t.afterIdx); len(next) > 0 {
			if in, read, write, _, started, _ := next[0].snapshot(); started {
				v["after"] = in + read + write
			}
		}
	}
	after, ok := v["after"].(int)
	if !ok && !final {
		return out // not yet
	}
	t.compacted = false
	t.x.r.compacted(t.x.thread, t.x.before, after)
	return append(out, activity(ActivityCompact, v))
}

// end closes whatever is open and appends the terminal event, with the usage before it.
func (t *translator) end(terminal chatstream.Event) []chatstream.Event {
	out := t.compactDone(nil, true)
	out = t.usage(out, nil, true)
	out = t.closeStep(out)
	if terminal.Verb == chatstream.VerbRunFinish {
		terminal.Usage = &chatstream.Usage{Scope: chatstream.UsageFinal, UncachedInput: t.uncached, CacheRead: t.read,
			CacheWrite: t.write, Output: t.out}
	}
	return append(out, terminal)
}

// translate maps one host event to the wire's. The host writes no steps of its own (the
// translator does), no reasoning, sources or files, and its gaps never reach a reader that
// starts from the beginning of a run it keeps.
func (t *translator) translate(ev chatstream.Event) []chatstream.Event {
	out := t.compactDone(nil, false)
	switch ev.Verb { //nolint:exhaustive // see above
	case chatstream.VerbRunStart:
		return append(out, chatstream.Event{Verb: chatstream.VerbRunStart, Provider: Provider, Model: ev.Model})

	case chatstream.VerbMessageStart:
		// The host's message per model request: a step holds the request's text, its tool
		// calls and their results, which the host writes after its message.end.
		return t.closeStep(out)

	case chatstream.VerbMessageEnd:
		return out

	case chatstream.VerbPartStart:
		switch chatstream.PartKind(ev.Kind) { //nolint:exhaustive // the host writes text, tool calls and results
		case chatstream.PartText:
			out = t.closePart(t.content(out))
			t.parts++
			id := fmt.Sprintf("%s.text-%d", t.messageID(), t.parts)
			t.part, t.textOf[ev.PartID] = id, id
			return append(out, chatstream.Event{Verb: chatstream.VerbPartStart, PartID: id, Kind: string(chatstream.PartText)})
		case chatstream.PartToolCall:
			out = t.closePart(t.content(out))
			var name, call string
			_ = json.Unmarshal(ev.Meta[chatstream.MetaName], &name)
			_ = json.Unmarshal(ev.Meta[chatstream.MetaCallID], &call)
			if call == "" {
				call = ev.PartID
			}
			t.callOf[ev.PartID] = call
			meta := map[string]json.RawMessage{chatstream.MetaName: raw(name), chatstream.MetaCallID: raw(call)}
			if lvl := t.x.r.level(name); lvl != "" {
				meta["level"] = raw(lvl)
			}
			return append(out, chatstream.Event{Verb: chatstream.VerbPartStart, PartID: call, Kind: string(chatstream.PartToolCall), Meta: meta})
		case chatstream.PartToolResult:
			var callPart string
			var isError bool
			_ = json.Unmarshal(ev.Meta[chatstream.MetaCallID], &callPart)
			_ = json.Unmarshal(ev.Meta[chatstream.MetaIsError], &isError)
			call := t.callOf[callPart]
			if call == "" {
				call = callPart
			}
			t.results[ev.PartID] = &result{call: call, isError: isError}
			return out
		}
		return out

	case chatstream.VerbPartDelta:
		if id, ok := t.textOf[ev.PartID]; ok {
			return append(out, chatstream.Event{Verb: chatstream.VerbPartDelta, PartID: id, Text: ev.Text})
		}
		if res := t.results[ev.PartID]; res != nil {
			res.text = append(res.text, ev.Text...)
		}
		return out

	case chatstream.VerbPartEnd:
		if id, ok := t.textOf[ev.PartID]; ok {
			delete(t.textOf, ev.PartID)
			if t.part == id {
				return t.closePart(out)
			}
			return out
		}
		if call, ok := t.callOf[ev.PartID]; ok {
			input := ev.Final
			if len(input) == 0 {
				input = json.RawMessage(`{}`)
			}
			return append(out, chatstream.Event{Verb: chatstream.VerbPartEnd, PartID: call, Final: input})
		}
		if res := t.results[ev.PartID]; res != nil {
			delete(t.results, ev.PartID)
			if !t.stepOpen {
				out = t.content(out)
			}
			out = t.closePart(out)
			id := res.call + "#result"
			return append(out,
				chatstream.Event{Verb: chatstream.VerbPartStart, PartID: id, Kind: string(chatstream.PartToolResult),
					Meta: map[string]json.RawMessage{chatstream.MetaCallID: raw(res.call), chatstream.MetaIsError: raw(res.isError)}},
				chatstream.Event{Verb: chatstream.VerbPartEnd, PartID: id, Final: raw(map[string]any{"text": string(res.text), "is_error": res.isError})})
		}
		return out

	case chatstream.VerbApprovalRequest:
		t.x.asked(ev.ApprovalID)
		return append(out, chatstream.Event{Verb: chatstream.VerbApprovalRequest, ApprovalID: ev.ApprovalID, CallID: ev.CallID,
			Reason: ev.Reason, Descriptor: ev.Descriptor, Mode: ev.Mode, ExpiresAt: ev.ExpiresAt})

	case chatstream.VerbUsage:
		return t.usage(out, ev.Usage, false)

	case chatstream.VerbActivity:
		var v struct {
			State      string `json:"state"`
			ApprovalID string `json:"approval_id"`
			Decision   string `json:"decision"`
		}
		_ = json.Unmarshal(ev.Value, &v)
		switch ev.Kind {
		case "ade.compact":
			switch v.State {
			case "start":
				return append(out, activity(ActivityCompact, map[string]any{"state": "start", "before": t.x.before}))
			case "done":
				t.compacted = true
				// The request after the summary is the one after the last summary request.
				t.afterIdx = -1
				for i, u := range t.x.stepsFrom(0) {
					if u.isSummary() {
						t.afterIdx = i + 1
					}
				}
				out = t.usage(out, nil, false) // the summary's own request
				return t.compactDone(out, false)
			default:
				return append(out, activity(ActivityCompact, map[string]any{"state": "failed", "before": t.x.before}))
			}
		case "ade.approval":
			decision, scope, reason := "deny", chat.ScopeOnce, ""
			switch v.Decision {
			case "once":
				decision = "allow"
			case "session":
				decision, scope = "allow", chat.ScopeConversation
			case "always":
				decision, scope = "allow", chat.ScopeAlways
			case "timeout":
				reason = chat.ReasonExpired
			case "cancelled": //nolint:misspell // the host's word
				reason = "canceled"
			}
			// The Service has already published the outcome of an answer it delivered and
			// drops this one; it is what a viewer sees when the run ended the wait itself.
			a := activity(chat.ActivityApproval, map[string]string{"approval_id": v.ApprovalID, "decision": decision, "scope": string(scope), "reason": reason})
			a.ApprovalID = v.ApprovalID
			return append(out, a)
		}
		return out

	case chatstream.VerbRunFinish:
		return append(out, t.end(chatstream.Event{Verb: chatstream.VerbRunFinish, Reason: ev.Reason, RawReason: ev.RawReason})...)

	case chatstream.VerbRunAbort:
		reason := chat.AbortCanceled
		if !t.x.wasCanceled() && t.x.r.ctx.Err() != nil {
			reason = chat.AbortShutdown
		}
		return append(out, t.end(chatstream.Event{Verb: chatstream.VerbRunAbort, Reason: reason})...)

	case chatstream.VerbRunError:
		return append(out, t.end(chatstream.Event{Verb: chatstream.VerbRunError, Code: ev.Code, Message: ev.Message, Retryable: ev.Retryable})...)
	}
	return out
}
