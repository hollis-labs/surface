// Package reflex holds the agent's reflexes: standing rules that watch the conversation and
// the app, and act when their trigger fires.
//
// A reflex is a trigger and an action. Triggers:
//
//	text       {"pattern": "(?i)triage"}       the owner's message matches a regular expression
//	predicate  {...}                          a libs agent/core/reflexes predicate over the conversation
//	event      {"topic": "notes.saved"}        an event on the app's bus (prefix match)
//	focus      {"view": "notes"}               a window of that view comes in front
//
// Actions:
//
//	remind       {"body": "..."}                       told to the agent with the next message
//	halt         {"message": "..."}                    the message is answered with this; the agent is not called
//	run_command  {"command": "...", "args": {...}}     an app command is run
//	call_tool    {"tool": "...", "input": {...}}       a tool is called (read and act levels only)
//
// Reflexes live in the core database, so the owner and the Interaction Builder can add, change
// and switch them on without a restart. A reflex is on when Enabled is set, or while a layout
// whose Setup["reflexes"] lists its name is in use.
package reflex

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	libreflex "github.com/hollis-labs/libs/agent/core/reflexes"

	"github.com/hollis-labs/surface/core/internal/storage"
	"github.com/hollis-labs/surface/module"
)

//go:embed migrations/*.sql
var migrations embed.FS

// The trigger kinds.
const (
	TriggerText      = "text"
	TriggerPredicate = "predicate"
	TriggerEvent     = "event"
	TriggerFocus     = "focus"
)

// The action kinds.
const (
	ActionRemind     = "remind"
	ActionHalt       = "halt"
	ActionRunCommand = "run_command"
	ActionCallTool   = "call_tool"
)

// Reflex is one reflex.
type Reflex struct {
	Name        string          `json:"name"`
	About       string          `json:"about,omitempty"`
	TriggerKind string          `json:"trigger_kind"`
	Trigger     json.RawMessage `json:"trigger"`
	ActionKind  string          `json:"action_kind"`
	Action      json.RawMessage `json:"action"`
	// Cooldown is how long, in seconds, it stays quiet after firing. 0: no wait.
	Cooldown int `json:"cooldown_seconds,omitempty"`
	// Enabled turns it on everywhere. Off, it is on only in a layout that lists it.
	Enabled   bool   `json:"enabled"`
	LastFired string `json:"last_fired,omitempty"`
	Fired     int    `json:"fired,omitempty"`
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// Validate checks a reflex's shape.
func (r Reflex) Validate() error {
	if !nameRe.MatchString(r.Name) {
		return fmt.Errorf("reflex name %q: use lower case letters, digits, dots, dashes and underscores", r.Name)
	}
	obj := func(raw json.RawMessage) map[string]any {
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		return m
	}
	t := obj(r.Trigger)
	switch r.TriggerKind {
	case TriggerText:
		p, _ := t["pattern"].(string)
		if _, err := regexp.Compile(p); p == "" || err != nil {
			return fmt.Errorf("text trigger: pattern must be a regular expression")
		}
	case TriggerPredicate:
		if _, err := libreflex.EvaluateTrigger("predicate", string(r.Trigger), libreflex.State{}); err != nil && t == nil {
			return fmt.Errorf("predicate trigger: %w", err)
		}
	case TriggerEvent:
		if s, _ := t["topic"].(string); s == "" {
			return errors.New("event trigger: topic is required")
		}
	case TriggerFocus:
		if s, _ := t["view"].(string); s == "" {
			return errors.New("focus trigger: view is required")
		}
	default:
		return fmt.Errorf("trigger kind %q: use text, predicate, event or focus", r.TriggerKind)
	}
	a := obj(r.Action)
	switch r.ActionKind {
	case ActionRemind:
		if s, _ := a["body"].(string); s == "" {
			return errors.New("remind: body is required")
		}
	case ActionHalt:
		if s, _ := a["message"].(string); s == "" {
			return errors.New("halt: message is required")
		}
		if r.TriggerKind == TriggerEvent || r.TriggerKind == TriggerFocus {
			return errors.New("halt only answers a message; use a text or predicate trigger")
		}
	case ActionRunCommand:
		if s, _ := a["command"].(string); s == "" {
			return errors.New("run_command: command is required")
		}
	case ActionCallTool:
		if s, _ := a["tool"].(string); s == "" {
			return errors.New("call_tool: tool is required")
		}
	default:
		return fmt.Errorf("action kind %q: use remind, halt, run_command or call_tool", r.ActionKind)
	}
	return nil
}

// Doer carries out the actions that reach into the app. The agent service supplies it.
type Doer interface {
	RunCommand(ctx context.Context, id string, args map[string]any) (any, error)
	// CallTool calls a tool by name. It refuses tools above the act level: a reflex does
	// not get to skip the owner's say on a change.
	CallTool(ctx context.Context, name string, input json.RawMessage) (any, error)
}

// Set is the app's reflexes.
type Set struct {
	db   *sql.DB
	doer Doer
	log  func(msg string, args ...any)

	mu sync.Mutex
	// pending are reminders from event and focus reflexes, told with the next message.
	pending []string
}

// Open migrates the reflex table into db.
func Open(ctx context.Context, db *sql.DB, doer Doer, log func(msg string, args ...any)) (*Set, error) {
	if err := storage.Migrate(ctx, db, "reflex", migrations, "migrations"); err != nil {
		return nil, err
	}
	if log == nil {
		log = func(string, ...any) {}
	}
	return &Set{db: db, doer: doer, log: log}, nil
}

// List returns every reflex, by name.
func (s *Set) List(ctx context.Context) ([]Reflex, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, about, trigger_kind, trigger, action_kind, action, cooldown, enabled, last_fired, fired FROM reflexes ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Reflex
	for rows.Next() {
		var r Reflex
		var trig, act string
		if err := rows.Scan(&r.Name, &r.About, &r.TriggerKind, &trig, &r.ActionKind, &act, &r.Cooldown, &r.Enabled, &r.LastFired, &r.Fired); err != nil {
			return nil, err
		}
		r.Trigger, r.Action = json.RawMessage(trig), json.RawMessage(act)
		out = append(out, r)
	}
	return out, rows.Err()
}

// Get returns one reflex.
func (s *Set) Get(ctx context.Context, name string) (Reflex, bool, error) {
	all, err := s.List(ctx)
	if err != nil {
		return Reflex{}, false, err
	}
	for _, r := range all {
		if r.Name == name {
			return r, true, nil
		}
	}
	return Reflex{}, false, nil
}

// Put adds a reflex or replaces the one of the same name (keeping its firing record).
func (s *Set) Put(ctx context.Context, r Reflex) error {
	if err := r.Validate(); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO reflexes (name, about, trigger_kind, trigger, action_kind, action, cooldown, enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET about = excluded.about, trigger_kind = excluded.trigger_kind, trigger = excluded.trigger,
		action_kind = excluded.action_kind, action = excluded.action, cooldown = excluded.cooldown, enabled = excluded.enabled`,
		r.Name, r.About, r.TriggerKind, string(r.Trigger), r.ActionKind, string(r.Action), r.Cooldown, r.Enabled)
	return err
}

// SetEnabled switches a reflex on or off everywhere.
func (s *Set) SetEnabled(ctx context.Context, name string, on bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE reflexes SET enabled = ? WHERE name = ?`, on, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no reflex %q", name)
	}
	return nil
}

// Delete removes a reflex.
func (s *Set) Delete(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM reflexes WHERE name = ?`, name)
	return err
}

// Active returns the reflexes that are on now: the enabled ones, and those the layout in use
// lists (inLayout).
func (s *Set) Active(ctx context.Context, inLayout []string) []Reflex {
	all, err := s.List(ctx)
	if err != nil {
		s.log("reflexes not read", "err", err)
		return nil
	}
	var out []Reflex
	for _, r := range all {
		if r.Enabled || slices.Contains(inLayout, r.Name) {
			out = append(out, r)
		}
	}
	return out
}

// Outcome is what the reflexes did to one message.
type Outcome struct {
	// Reminders are told to the agent with the message.
	Reminders []string
	// Halt, when set, answers the message instead of the agent.
	Halt string
	// Fired names the reflexes that fired.
	Fired []string
}

// Message is the owner's message and the conversation before it, for the triggers.
type Message struct {
	Text string
	// Prior is what came before, most recent first.
	Prior []libreflex.MessageSignal
	Sit   module.Situation
}

// OnMessage puts a message to the active reflexes and carries out what fires. Reminders
// left by event and focus reflexes since the last message are given too.
func (s *Set) OnMessage(ctx context.Context, m Message, inLayout []string) Outcome {
	var out Outcome
	s.mu.Lock()
	out.Reminders, s.pending = s.pending, nil
	s.mu.Unlock()
	state := libreflex.State{SessionID: "owner", AgentID: "surface", Attrs: map[string]string{}}
	now := libreflex.MessageSignal{Role: "user", Content: m.Text}
	state.Messages = append([]libreflex.MessageSignal{now}, m.Prior...)
	state.UserMessages = []libreflex.MessageSignal{now}
	for _, p := range m.Prior {
		if p.Role == "user" {
			state.UserMessages = append(state.UserMessages, p)
		}
	}
	if f := m.Sit.Focused; f != nil {
		state.Attrs["focused.view"] = f.View
		state.Attrs["focused.module"] = f.Module
		state.Attrs["selection"] = map[bool]string{true: "yes", false: "no"}[strings.TrimSpace(f.Selection) != ""]
	}
	state.Attrs["mode"] = m.Sit.Mode
	state.Attrs["layout"] = m.Sit.Layout
	for _, r := range s.Active(ctx, inLayout) {
		var fired bool
		switch r.TriggerKind {
		case TriggerText:
			var t struct {
				Pattern string `json:"pattern"`
			}
			_ = json.Unmarshal(r.Trigger, &t)
			if re, err := regexp.Compile(t.Pattern); err == nil {
				fired = re.MatchString(m.Text)
			}
		case TriggerPredicate:
			ok, err := libreflex.EvaluateTrigger("predicate", string(r.Trigger), state)
			if err != nil {
				s.log("reflex trigger not evaluated", "reflex", r.Name, "err", err)
			}
			fired = ok
		default:
			continue
		}
		if !fired || s.cooling(r) {
			continue
		}
		s.fire(ctx, r, &out)
	}
	return out
}

// OnEvent puts an app event to the active event and focus reflexes. focusView is the view
// that came in front when the event is a focus change, else "".
func (s *Set) OnEvent(ctx context.Context, ev module.Event, focusView string, inLayout []string) {
	for _, r := range s.Active(ctx, inLayout) {
		var t struct {
			Topic string `json:"topic"`
			View  string `json:"view"`
		}
		_ = json.Unmarshal(r.Trigger, &t)
		switch {
		case r.TriggerKind == TriggerEvent && t.Topic != "" && (ev.Topic == t.Topic || strings.HasPrefix(ev.Topic, t.Topic+".")):
		case r.TriggerKind == TriggerFocus && focusView != "" && focusView == t.View:
		default:
			continue
		}
		if s.cooling(r) {
			continue
		}
		var out Outcome
		s.fire(ctx, r, &out)
		if len(out.Reminders) > 0 {
			s.mu.Lock()
			s.pending = append(s.pending, out.Reminders...)
			if len(s.pending) > 10 {
				s.pending = s.pending[len(s.pending)-10:]
			}
			s.mu.Unlock()
		}
	}
}

func (s *Set) cooling(r Reflex) bool {
	if r.Cooldown <= 0 || r.LastFired == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, r.LastFired)
	return err == nil && time.Since(t) < time.Duration(r.Cooldown)*time.Second
}

func (s *Set) fire(ctx context.Context, r Reflex, out *Outcome) {
	var a struct {
		Body    string          `json:"body"`
		Message string          `json:"message"`
		Command string          `json:"command"`
		Args    map[string]any  `json:"args"`
		Tool    string          `json:"tool"`
		Input   json.RawMessage `json:"input"`
	}
	_ = json.Unmarshal(r.Action, &a)
	switch r.ActionKind {
	case ActionRemind:
		out.Reminders = append(out.Reminders, a.Body)
	case ActionHalt:
		if out.Halt == "" {
			out.Halt = a.Message
		}
	case ActionRunCommand:
		if s.doer != nil {
			if _, err := s.doer.RunCommand(ctx, a.Command, a.Args); err != nil {
				s.log("reflex command failed", "reflex", r.Name, "command", a.Command, "err", err)
			}
		}
	case ActionCallTool:
		if s.doer != nil {
			in := a.Input
			if len(in) == 0 {
				in = json.RawMessage("{}")
			}
			if _, err := s.doer.CallTool(ctx, a.Tool, in); err != nil {
				s.log("reflex tool call failed", "reflex", r.Name, "tool", a.Tool, "err", err)
			}
		}
	}
	out.Fired = append(out.Fired, r.Name)
	_, _ = s.db.ExecContext(context.WithoutCancel(ctx), `UPDATE reflexes SET last_fired = ?, fired = fired + 1 WHERE name = ?`,
		time.Now().UTC().Format(time.RFC3339), r.Name)
}
