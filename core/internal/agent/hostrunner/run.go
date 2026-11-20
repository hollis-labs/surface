package hostrunner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"sync"

	chat "github.com/hollis-labs/libs/agent/chat"
	chatstream "github.com/hollis-labs/libs/ui/ui-go/chatstream"
	"github.com/hollis-labs/libs/ui/ui-go/ssekit"
)

// CodeTurnFailed is the run.error code of a turn whose stream could not be read.
const CodeTurnFailed = "turn_failed"

// usageStep is the usage of one model request, as the gateway reported it. The tap fills
// it as the response streams: input at message_start, output at message_delta.
type usageStep struct {
	mu                   sync.Mutex
	in, read, write, out int
	started, stopped     bool
}

func (u *usageStep) snapshot() (in, read, write, out int, started, stopped bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.in, u.read, u.write, u.out, u.started, u.stopped
}

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
	steps     []*usageStep    // the model requests of this run, as the tap saw them
	approvals map[string]bool // approval ids the host asked; true once answered

	t translator
}

func newRun(r *Runner, turn chat.Turn, owner, thread string, before int) *run {
	ctx, stop := context.WithCancel(r.ctx)
	x := &run{r: r, turn: turn, owner: owner, thread: thread, ctx: ctx, stop: stop, before: before,
		approvals: map[string]bool{}}
	x.t = translator{x: x, callOf: map[string]string{}, textOf: map[string]string{}, results: map[string]*result{}}
	return x
}

func (x *run) newStep() *usageStep {
	u := &usageStep{}
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
	x.approvals[approvalID] = false
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

// Answer resolves an approval the host asked for in this run.
func (x *run) Answer(_ context.Context, approvalID string, d chat.Decision) error {
	x.mu.Lock()
	answered, ok := x.approvals[approvalID]
	if ok && !answered {
		x.approvals[approvalID] = true
	}
	x.mu.Unlock()
	if !ok {
		return fmt.Errorf("hostrunner: no approval %q in this run", approvalID)
	}
	if answered {
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
	if err := x.r.h.Approve(x.owner, x.hostRun, approvalID, decision); err != nil {
		return fmt.Errorf("hostrunner: approval %q: %w", approvalID, err)
	}
	return nil
}

// pipeWriter is the response of the host's own event route, read in process.
type pipeWriter struct {
	header http.Header
	w      *io.PipeWriter
}

func (p *pipeWriter) Header() http.Header         { return p.header }
func (p *pipeWriter) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p *pipeWriter) WriteHeader(int)             {}
func (p *pipeWriter) Flush()                      {}

// hostEvents reads the host run's chatstream events from its handler, in process. A
// finished run is kept by the host for a while, so nothing is missed by reading late.
func (x *run) hostEvents(ctx context.Context) iter.Seq2[chatstream.Event, error] {
	return func(yield func(chatstream.Event, error) bool) {
		pr, pw := io.Pipe()
		defer func() { _ = pr.Close() }()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, handlerPrefix+"/runs/"+x.hostRun+"/events", nil)
		if err != nil {
			yield(chatstream.Event{}, err)
			return
		}
		req.Header.Set(ownerHeader, x.owner)
		go func() {
			x.r.handler.ServeHTTP(&pipeWriter{header: http.Header{}, w: pw}, req)
			_ = pw.Close()
		}()
		for sse, err := range ssekit.Read(pr) {
			if err != nil {
				yield(chatstream.Event{}, err)
				return
			}
			if len(sse.Data) == 0 {
				continue
			}
			var ev chatstream.Event
			if json.Unmarshal(sse.Data, &ev) != nil {
				continue
			}
			if !yield(ev, nil) {
				return
			}
		}
	}
}

// Events yields the turn's events on the conversation wire. It is read once.
func (x *run) Events() iter.Seq2[chatstream.Event, error] {
	return func(yield func(chatstream.Event, error) bool) {
		defer x.r.release(x)
		defer x.stop()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		for ev, err := range x.hostEvents(ctx) {
			if err != nil {
				x.r.log.Warn("hostrunner: the run's events were not read", "run", x.turn.RunID, "err", err)
				break
			}
			terminal := false
			for _, out := range x.t.translate(ev) {
				terminal = terminal || out.IsTerminal()
				if !yield(out, nil) {
					// The Service is done with this run: stop it.
					_ = x.r.h.Cancel(x.owner, x.hostRun)
					return
				}
			}
			if terminal {
				return
			}
		}
		for _, out := range x.t.end(chatstream.Event{Verb: chatstream.VerbRunError, Code: CodeTurnFailed, Message: "the turn ended without a result"}) {
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
	x           *run
	step        int
	stepOpen    bool
	afterResult bool // the open step has a tool result: the next content is a new step
	part        string
	parts       int
	callOf      map[string]string  // host tool_call part id -> the call's id
	textOf      map[string]string  // host text part id -> the wire's
	results     map[string]*result // host tool_result part id -> the result so far

	// Usage: the tap's steps reported so far, and the totals reported.
	usedSteps                  int
	uncached, read, write, out int
	// compacted: the thread was compacted in this run and the request after it has not
	// been seen yet; done is the notice waiting for its size.
	compacted bool
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
	t.stepOpen, t.afterResult = false, false
	return out
}

// content makes sure a step and its assistant message are open for model output. Output
// that follows a tool result belongs to the next model step.
func (t *translator) content(out []chatstream.Event) []chatstream.Event {
	if t.stepOpen && t.afterResult {
		out = t.closeStep(out)
	}
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
	for _, u := range t.x.stepsFrom(t.usedSteps) {
		if in, read, write, _, started, _ := u.snapshot(); started {
			v["after"] = in + read + write
			break
		}
	}
	if _, ok := v["after"]; !ok && !final {
		return out // not yet
	}
	t.compacted = false
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

	case chatstream.VerbMessageStart, chatstream.VerbMessageEnd:
		// The host's message per model request: steps open on content and close after
		// results, as on the rest of the wire.
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
			t.afterResult = true
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
