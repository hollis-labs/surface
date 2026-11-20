// Package agent is the embedded agent of a Surface app: one agent the owner talks to in the
// agent panel, with every tool in the app, whose context changes with what is in front.
//
// Per message it: routes (router: a rule may run a call directly), puts the conversation to
// the reflexes (reflex: reminders, a halt, actions), chooses the active context slots
// (slots.Selector: today the window in front), assembles the <context> block from them
// (slots.Assemble: top-of-mind tools, instructions, the module's knowledge, the agent's
// note, where things are kept), and submits the message on the conversation wire.
//
// Nanite serves embedded agents on the durable conversation wire.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hollis-labs/libs/core/util/classify"
	"github.com/hollis-labs/libs/core/util/decide"

	"github.com/hollis-labs/surface/core/internal/classifier"
	"github.com/hollis-labs/surface/core/internal/kernel"
	"github.com/hollis-labs/surface/core/internal/reflex"
	"github.com/hollis-labs/surface/core/internal/router"
	"github.com/hollis-labs/surface/core/internal/slots"
	"github.com/hollis-labs/surface/module"
)

// Owner is the one person a Surface app serves. Surface apps are single-user and
// loopback-only; conversations belong to this owner.
const Owner = "owner"

// Prefix is where the agent's routes are served.
const Prefix = "/api/agent"

// Request is one call put to an Approver.
type Request struct {
	Tool   string
	Module string
	Level  module.Level
	Input  json.RawMessage
}

// Approver decides whether the agent may make a call that changes something (levels write
// and confirm). It is the hook for an outside approval service (Gatecrasher). With none, the
// runner asks the owner in the panel, which is the default.
type Approver interface {
	Approve(ctx context.Context, r Request) (bool, error)
}

// Options are the service's optional parts.
type Options struct {
	// Approver, when set, replaces the in-panel question for write and confirm calls.
	Approver Approver
	// Selector, when set, replaces the default: the focus selector, overruled by the
	// classifier when the setting surface.agent_selector is "classifier".
	Selector slots.Selector
	// Decider, when set, is the classifier's async tier instead of the one the config file
	// names (tests).
	Decider decide.Decider
	// Embedder, when set, is the classifier's embedder instead of the gateway's (tests).
	Embedder classify.Embedder
}

// Service is the embedded agent.
type Service struct {
	// runs is the run log (the conversation wire only).
	runs     *runLog
	k        kernel.Kernel
	log      *slog.Logger
	cfg      FileConfig
	cfgErr   error
	approver Approver
	selector slots.Selector
	pin      *slots.FocusSelector // the selector, when it is the focus one
	notes    *slots.Notes
	trace    *slots.Trace
	// cls labels each turn in shadow, beside the trace (classifier: act and slot).
	cls      *classifier.Classifier
	reflexes *reflex.Set
	router   *router.Router

	tools map[string]kernel.BoundTool
	// wire is the conversation wire, when a runner for it is chosen; nil otherwise.
	wire *wire
	// late delivers the owner's late answers to the agent's cards (late.go).
	late *lateDelivery
	// prof is the modules' agent profiles (profiles.go).
	prof *profiles

	wake    chan struct{}
	stop    func()
	done    chan struct{}
	closeMu sync.Once
	eventMu sync.Mutex
	eventWG sync.WaitGroup
	closing bool

	lastMu    sync.Mutex
	lastFocus string // the window last seen in front, for focus reflexes
	lastText  string // the newest traced turn's text, for a correction that follows it
	lastAt    time.Time
	turnSlot  string // the primary slot of the owner's last turn, for the note-on-leave ask

	// standingWrite serializes standing_update.
	standingWrite sync.Mutex

	// async runs the classifier's async tier off the turn's path; Close stops it.
	asyncCtx    context.Context
	asyncCancel context.CancelFunc
	asyncWG     sync.WaitGroup
	fitting     atomic.Bool // a centroid fit is running
	refitAgain  atomic.Bool // fit once more when the running fit ends
	// trained is closed when the classifier has learned the trace kept before start.
	trained chan struct{}
	lastAsm string // the slot and top of mind last published
}

// New builds the agent service from the kernel. A missing agent config is not an error:
// the panel says the agent is not set up, and everything else works.
func New(k kernel.Kernel) (*Service, error) { return NewWith(k, Options{}) }

// NewWith is New with options.
func NewWith(k kernel.Kernel, o Options) (*Service, error) {
	ctx := context.Background()
	log := k.Log().With("part", "agent")
	cfg, err := LoadConfig(k.AppName())
	if err != nil {
		log.Warn("agent config not read; the agent is off until it is fixed", "err", err)
	}
	s := &Service{k: k, log: log, cfg: cfg, cfgErr: err, approver: o.Approver, tools: map[string]kernel.BoundTool{},
		wake: make(chan struct{}, 1), done: make(chan struct{})}

	if s.notes, err = slots.OpenNotes(ctx, k.DB()); err != nil {
		return nil, fmt.Errorf("agent: slot notes: %w", err)
	}
	if s.trace, err = slots.OpenTrace(ctx, k.DB()); err != nil {
		return nil, fmt.Errorf("agent: context trace: %w", err)
	}
	if s.cls, err = classifier.New(); err != nil {
		return nil, fmt.Errorf("agent: classifier: %w", err)
	}
	s.setupAsync(cfg, o.Decider, o.Embedder)
	s.trained = make(chan struct{})
	go s.trainClassifier(ctx, time.Now().UTC())
	if s.reflexes, err = reflex.Open(ctx, k.DB(), doer{s}, func(msg string, args ...any) { log.Warn(msg, args...) }); err != nil {
		return nil, fmt.Errorf("agent: reflexes: %w", err)
	}
	if s.router, err = router.Open(ctx, k.DB()); err != nil {
		return nil, fmt.Errorf("agent: router: %w", err)
	}
	if err = openProfilePins(ctx, k.DB()); err != nil {
		return nil, fmt.Errorf("agent: profile pins: %w", err)
	}
	s.loadProfiles()
	s.selector = o.Selector
	if s.selector == nil {
		s.pin = slots.NewFocusSelector()
		// The focus selector, which the classifier overrules when surface.agent_selector says so.
		s.selector = &classifiedSelector{focus: s.pin, cls: s.cls, mode: s.selectorMode}
	}
	for _, t := range k.Tools() {
		s.tools[t.Name] = t
	}
	// An unconfigured app remains usable without opening a model.
	explicitRunner := strings.TrimSpace(os.Getenv("SURFACE_AGENT_RUNNER")) != ""
	if runner := runnerChoice(); cfg.Tether.URL == "" && (explicitRunner || (cfg.Model != "" && cfg.GatewayURL != "" && cfg.KeyFile != "")) {
		if s.wire, err = s.openWire(ctx, runner); err != nil {
			return nil, err
		}
		log.Info("the agent is on the conversation wire", "runner", runner)
	}
	if len(s.prof.order) > 0 && !s.profilesOn() {
		log.Info("module agent profiles need the Nanite runner; every turn runs as the app's own seat", "runner", runnerChoice(), "profiles", s.prof.order)
	}

	// The desk and the modules' events drive the focus and event reflexes and the panel's
	// view of the context. Subscribers must not block, so they only wake the worker.
	unsub := k.Events().Subscribe("", func(ev module.Event) {
		if strings.HasPrefix(ev.Topic, "agent.") {
			// The agent's own events stay out of the trace and the desk's refresh; a
			// compaction still reaches the event reflexes (save-state).
			if ev.Topic == TopicCompacted {
				s.launchEvent(func() { s.onEvent(ev) })
			}
			return
		}
		go s.traceEvent(ev)
		s.launchEvent(func() { s.onEvent(ev) })
		select {
		case s.wake <- struct{}{}:
		default:
		}
	})
	wctx, cancel := context.WithCancel(context.Background())
	s.stop = func() { unsub(); cancel() }
	go s.worker(wctx)
	s.startLate()
	if s.wire == nil {
		log.Info("the agent has no model configured", "config", ConfigPath(k.AppName()))
	}
	return s, nil
}

// Close stops the runner and the worker.
func (s *Service) Close(context.Context) error {
	s.closeMu.Do(func() {
		s.eventMu.Lock()
		s.closing = true
		s.eventMu.Unlock()
		s.stop()
		s.eventWG.Wait()
		<-s.done
		s.stopLate()
		if s.asyncCancel != nil {
			s.asyncCancel()
			s.asyncWG.Wait()
		}
		if s.wire != nil {
			s.wire.close(context.Background())
		}
	})
	return nil
}

// worker republishes the context when the desk changes, at most every 150ms.
func (s *Service) worker(ctx context.Context) {
	defer close(s.done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(150 * time.Millisecond):
		}
		asm := s.Preview(ctx)
		key := asm.Slot + "|" + strings.Join(asm.TopOfMind, ",") + "|" + asm.Block
		s.lastMu.Lock()
		changed := key != s.lastAsm
		s.lastAsm = key
		s.lastMu.Unlock()
		if changed {
			s.k.Events().Publish("agent.context", asm)
		}
	}
}

// onEvent puts an app event to the event and focus reflexes.
func (s *Service) onEvent(ev module.Event) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	focusView := ""
	if w, ok := s.k.Desk().Focused(ctx); ok {
		s.lastMu.Lock()
		if w.ID != s.lastFocus {
			s.lastFocus = w.ID
			focusView = w.View
		}
		s.lastMu.Unlock()
	}
	sit := s.k.Situation(ctx)
	if s.wire != nil && s.wire.nanite != nil {
		s.steerHostEvent(ctx, ev.Topic, focusView)
		return
	}
	s.reflexes.OnEvent(ctx, ev, focusView, s.layoutReflexes(ctx, sit))
}

// Preview is the context the next message would carry, without reflexes or routing.
func (s *Service) Preview(ctx context.Context) slots.Assembly {
	sit := s.k.Situation(ctx)
	sel := s.selector.Select(ctx, sit, s.k.Slots())
	extra := s.extra(ctx, sit)
	choice := s.chooseProfile(ctx, s.conversationID(ctx), sit)
	extra.Profile, extra.ProfileID = s.profileLine(choice), choice.ID
	return slots.Assemble(ctx, sit, sel, s.allTools(), s.notes, extra)
}

// allTools is every tool the agent has: the app's and its own.
func (s *Service) allTools() []kernel.BoundTool {
	out := append([]kernel.BoundTool(nil), s.k.Tools()...)
	have := map[string]bool{}
	for _, t := range out {
		have[t.Name] = true
	}
	for _, t := range s.ownTools() {
		if !have[t.Name] { // the kernel offers them too once the core has added them
			out = append(out, t)
		}
	}
	return out
}

// layout returns the setup of the layout in use.
func (s *Service) layout(ctx context.Context, sit module.Situation) map[string]any {
	if sit.Layout == "" {
		return nil
	}
	if l, ok, err := s.k.Desk().Layout(ctx, sit.Layout); err == nil && ok {
		return l.Setup
	}
	// Arranged under a name but not saved: what the desk holds of it.
	return s.k.Desk().Capture(ctx).Setup
}

func (s *Service) layoutReflexes(ctx context.Context, sit module.Situation) []string {
	setup := s.layout(ctx, sit)
	var out []string
	if list, ok := setup["reflexes"].([]any); ok {
		for _, v := range list {
			if name, ok := v.(string); ok {
				out = append(out, name)
			}
		}
	}
	if list, ok := setup["reflexes"].([]string); ok {
		out = append(out, list...)
	}
	return out
}

func (s *Service) layoutInstructions(ctx context.Context, sit module.Situation) string {
	v, _ := s.layout(ctx, sit)["instructions"].(string)
	return strings.TrimSpace(v)
}

// runRoute carries out a routed call and says what the agent should be told.
func (s *Service) runRoute(ctx context.Context, r *router.Route) string {
	input, _ := json.Marshal(r.Input)
	switch r.Kind {
	case "tool":
		t, ok := s.tools[r.Name]
		if !ok {
			return fmt.Sprintf("The owner's message matched the rule %q, for the tool %s, which this app does not have. Tell them.", r.Rule, r.Name)
		}
		if lvl := level(t.Level); r.Ask || (lvl != string(module.LevelRead) && lvl != string(module.LevelAct)) {
			return fmt.Sprintf("The owner's message asks for %s with %s (rule %q). Call it.", r.Name, input, r.Rule)
		}
		v, err := t.Call(ctx, input)
		if err != nil {
			return fmt.Sprintf("Done for the owner by the rule %q: %s(%s) failed: %v. Tell them in a line.", r.Rule, r.Name, input, err)
		}
		say := r.Say
		if say == "" {
			say = fmt.Sprintf("Called %s(%s).", r.Name, input)
		}
		return fmt.Sprintf("Already done for the owner (rule %q, no need to repeat it): %s Result: %s\nConfirm it in a line, and answer anything else the message asks.",
			r.Rule, say, clip(asText(v), 2000))
	case "command":
		return fmt.Sprintf("The owner's message asks for the command %s (rule %q). Run it with desk_run_command.", r.Name, r.Rule)
	}
	return ""
}

// toolBindings is every tool the agent gets: the app's, then the agent's own.
func (s *Service) toolBindings() []ToolBinding {
	var out []ToolBinding
	for _, t := range s.k.Tools() {
		out = append(out, s.bind(t))
	}
	for _, t := range s.ownTools() {
		out = append(out, s.bind(t))
	}
	return out
}

// bind turns a BoundTool into a ToolBinding, with the approver in front when there is one.
func (s *Service) bind(t kernel.BoundTool) ToolBinding {
	schema := map[string]any{"type": "object"}
	if len(t.Input) > 0 {
		_ = json.Unmarshal(t.Input, &schema)
	}
	lvl := level(t.Level)
	call := t.Call
	if s.approver != nil && (lvl == string(module.LevelWrite) || lvl == string(module.LevelConfirm)) {
		// The approver decides; the runner must not ask again.
		mod, tl := t.Module, t.Level
		name := t.Name
		inner := call
		call = func(ctx context.Context, input json.RawMessage) (any, error) {
			ok, err := s.approver.Approve(ctx, Request{Tool: name, Module: mod, Level: tl, Input: input})
			if err != nil {
				return nil, fmt.Errorf("approval: %w", err)
			}
			if !ok {
				return nil, errors.New("the owner's approver declined this call; do not try it again, ask what they would like instead")
			}
			return inner(ctx, input)
		}
		lvl = string(module.LevelAct)
	}
	return ToolBinding{Name: t.Name, Description: t.Description, InputSchema: schema, Level: lvl,
		Run: func(ctx context.Context, input json.RawMessage) (string, error) {
			// Whatever the runner runs, the agent does: the desk holds it to the agent's etiquette.
			v, err := call(module.WithActor(ctx, module.ActorAgent), input)
			if err != nil {
				return "", err
			}
			return asText(v), nil
		}}
}

// level is a tool's level as the runner takes it; unknown is write.
func level(l module.Level) string {
	switch l {
	case module.LevelRead, module.LevelAct, module.LevelWrite, module.LevelConfirm:
		return string(l)
	}
	return string(module.LevelWrite)
}

func asText(v any) string {
	switch x := v.(type) {
	case nil:
		return "done"
	case string:
		return x
	case []byte:
		return string(x)
	case json.RawMessage:
		return string(x)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// doer is how reflexes reach into the app.
type doer struct{ s *Service }

func (d doer) RunCommand(ctx context.Context, id string, args map[string]any) (any, error) {
	return d.s.k.RunCommand(module.WithActor(ctx, module.ActorReflex), id, args)
}

func (d doer) CallTool(ctx context.Context, name string, input json.RawMessage) (any, error) {
	t, ok := d.s.tools[name]
	if !ok {
		return nil, fmt.Errorf("no tool %q", name)
	}
	if lvl := level(t.Level); lvl != string(module.LevelRead) && lvl != string(module.LevelAct) {
		return nil, fmt.Errorf("a reflex may not call %s: it changes data (%s); remind the agent instead", name, lvl)
	}
	return t.Call(module.WithActor(ctx, module.ActorReflex), input)
}

// launchEvent tracks host callbacks so shutdown drains them before closing the runner.
func (s *Service) launchEvent(fn func()) {
	s.eventMu.Lock()
	defer s.eventMu.Unlock()
	if s.closing {
		return
	}
	s.eventWG.Add(1)
	go func() { defer s.eventWG.Done(); fn() }()
}
