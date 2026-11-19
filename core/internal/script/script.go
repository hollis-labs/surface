// Package script runs one walk-through three ways (docs/scripts.md):
//
//   - guide: on the owner's desk as an ADE guide, moved on by the owner (onboarding);
//   - demo:  on its own, step after step, pointing as it goes, for a recording;
//   - test:  without a page, checking what each step expects, and putting the desk back.
//
// A script is a guide with two additions per step: do (look-only tool calls that drive the
// desk, as a reflex may) and expect (what the desk should show after). Because every desk
// action is a tool here, the same script drives the app in all three modes; a recorder only
// has to watch.
package script

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hollis-labs/surface/core/internal/kernel"
	"github.com/hollis-labs/surface/core/internal/storage"
	"github.com/hollis-labs/surface/module"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Call is one tool call a step makes.
type Call struct {
	Tool  string          `json:"tool"`
	Input json.RawMessage `json:"input,omitempty"`
}

// Expect is what the desk should show after a step. Every field set must hold.
type Expect struct {
	Focused string   `json:"focused,omitempty"` // the view in front
	Open    []string `json:"open,omitempty"`    // views with a window open
	Closed  []string `json:"closed,omitempty"`  // views with none
	Mode    string   `json:"mode,omitempty"`
	Layout  string   `json:"layout,omitempty"`
	// Tool calls a look-only tool; its answer, as text, must contain Contains.
	Tool     *Call  `json:"tool,omitempty"`
	Contains string `json:"contains,omitempty"`
}

// Step is one step: what the owner is told, what is pointed at, what the step does first and
// what should hold after it.
type Step struct {
	Say    string            `json:"say"`
	Point  []json.RawMessage `json:"point,omitempty"` // ADE targets: {window|view, anchor?, label?}
	Style  string            `json:"style,omitempty"` // highlight or spotlight
	Await  json.RawMessage   `json:"await,omitempty"` // as an ADE guide step: {kind: ack|open|focus, view}
	Hint   string            `json:"hint,omitempty"`
	Do     []Call            `json:"do,omitempty"`
	Expect *Expect           `json:"expect,omitempty"`
	// HoldMS is how long a demo shows the step; 0 is worked out from what it says.
	HoldMS int `json:"hold_ms,omitempty"`
	// Zoom asks a recorder or the page to move the camera in on what the step points at.
	Zoom bool `json:"zoom,omitempty"`
}

// Script is a named walk-through.
type Script struct {
	Name    string `json:"name"`
	About   string `json:"about,omitempty"`
	Steps   []Step `json:"steps"`
	Updated string `json:"updated,omitempty"`
}

// StepResult is how one step of a test went.
type StepResult struct {
	Index  int      `json:"index"`
	Say    string   `json:"say"`
	OK     bool     `json:"ok"`
	Failed []string `json:"failed,omitempty"`
}

// Report is how a run went.
type Report struct {
	Script string       `json:"script"`
	Mode   string       `json:"mode"`
	Status string       `json:"status"` // running | passed | failed | done | stopped
	Steps  []StepResult `json:"steps,omitempty"`
}

// Service keeps scripts and runs them.
type Service struct {
	k     kernel.Kernel
	db    *sql.DB
	sleep func(ctx context.Context, d time.Duration) error

	mu      sync.Mutex
	stop    context.CancelFunc // the demo running, if any
	guide   *Script            // the script running as a guide, if any
	unwatch func()
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// New opens the store.
func New(ctx context.Context, k kernel.Kernel) (*Service, error) {
	if err := storage.Migrate(ctx, k.DB(), "script", migrations, "migrations"); err != nil {
		return nil, fmt.Errorf("script: %w", err)
	}
	s := &Service{k: k, db: k.DB(), sleep: func(ctx context.Context, d time.Duration) error {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			return nil
		}
	}}
	// In guide mode the owner moves the steps on; each step's do runs when it comes up.
	s.unwatch = k.Events().Subscribe("ade.guide.progress", func(ev module.Event) { go s.onProgress(ev) })
	return s, nil
}

// Close stops a running demo and stops following the guide.
func (s *Service) Close(context.Context) error {
	s.mu.Lock()
	if s.stop != nil {
		s.stop()
	}
	s.mu.Unlock()
	if s.unwatch != nil {
		s.unwatch()
	}
	return nil
}

// Save keeps a script, replacing one of the same name.
func (s *Service) Save(ctx context.Context, sc Script) error {
	if !nameRe.MatchString(sc.Name) {
		return errors.New("a script's name is lower-case letters, digits, - and _, up to 64")
	}
	if len(sc.Steps) == 0 {
		return errors.New("a script needs at least one step")
	}
	for i, st := range sc.Steps {
		if strings.TrimSpace(st.Say) == "" {
			return fmt.Errorf("step %d says nothing", i+1)
		}
		for _, c := range st.Do {
			if err := s.lookOnly(c.Tool); err != nil {
				return fmt.Errorf("step %d: %w", i+1, err)
			}
		}
		if st.Expect != nil && st.Expect.Tool != nil {
			if err := s.lookOnly(st.Expect.Tool.Tool); err != nil {
				return fmt.Errorf("step %d expects: %w", i+1, err)
			}
		}
	}
	raw, err := json.Marshal(sc.Steps)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO scripts (name, about, steps, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET about=excluded.about, steps=excluded.steps, updated_at=excluded.updated_at`,
		sc.Name, sc.About, string(raw), time.Now().UTC().Format(time.RFC3339))
	return err
}

// Get reads a script.
func (s *Service) Get(ctx context.Context, name string) (Script, error) {
	sc := Script{Name: name}
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT about, steps, updated_at FROM scripts WHERE name=?`, name).Scan(&sc.About, &raw, &sc.Updated)
	if errors.Is(err, sql.ErrNoRows) {
		return sc, fmt.Errorf("no script %q", name)
	}
	if err != nil {
		return sc, err
	}
	return sc, json.Unmarshal([]byte(raw), &sc.Steps)
}

// List lists the scripts.
func (s *Service) List(ctx context.Context) ([]Script, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, about, steps, updated_at FROM scripts ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Script{}
	for rows.Next() {
		var sc Script
		var raw string
		if err := rows.Scan(&sc.Name, &sc.About, &raw, &sc.Updated); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(raw), &sc.Steps)
		out = append(out, sc)
	}
	return out, rows.Err()
}

// Delete removes a script.
func (s *Service) Delete(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM scripts WHERE name=?`, name)
	return err
}

// tool finds a tool the app has.
func (s *Service) tool(name string) (kernel.BoundTool, bool) {
	for _, t := range s.k.Tools() {
		if t.Name == name {
			return t, true
		}
	}
	return kernel.BoundTool{}, false
}

// lookOnly refuses a tool a script may not call: one that changes data. A script drives what
// is on screen, as a reflex may; it never writes for the owner.
func (s *Service) lookOnly(name string) error {
	if strings.HasPrefix(name, "script_") {
		return fmt.Errorf("a script may not run scripts (%s)", name)
	}
	t, ok := s.tool(name)
	if !ok {
		return fmt.Errorf("no tool %q", name)
	}
	if t.Level != module.LevelRead && t.Level != module.LevelAct {
		return fmt.Errorf("a script may only call look-only tools; %s changes data (%s)", name, t.Level)
	}
	return nil
}

// call runs one tool call as the agent: the desk holds a script to the agent's etiquette.
func (s *Service) call(ctx context.Context, c Call) (any, error) {
	if err := s.lookOnly(c.Tool); err != nil {
		return nil, err
	}
	t, _ := s.tool(c.Tool)
	in := c.Input
	if len(in) == 0 {
		in = json.RawMessage(`{}`)
	}
	return t.Call(module.WithActor(ctx, module.ActorAgent), in)
}

// do runs a step's calls, stopping at the first that fails.
func (s *Service) do(ctx context.Context, st Step) error {
	for _, c := range st.Do {
		if _, err := s.call(ctx, c); err != nil {
			return fmt.Errorf("%s: %w", c.Tool, err)
		}
	}
	return nil
}

// check says what of a step's expectations does not hold.
func (s *Service) check(ctx context.Context, st Step) []string {
	var failed []string
	sit := s.k.Situation(ctx)
	open := map[string]bool{}
	for _, w := range sit.Windows {
		open[w.View] = true
	}
	// An await is something the step waits for the owner to do; in a test, the script's own
	// calls must have done it.
	if len(st.Await) > 0 {
		var a struct{ Kind, View string }
		_ = json.Unmarshal(st.Await, &a)
		switch a.Kind {
		case "open":
			if !open[a.View] {
				failed = append(failed, fmt.Sprintf("waits for %s to be open; it is not", a.View))
			}
		case "focus":
			if sit.Focused == nil || sit.Focused.View != a.View {
				failed = append(failed, fmt.Sprintf("waits for %s to be in front; it is not", a.View))
			}
		}
	}
	e := st.Expect
	if e == nil {
		return failed
	}
	if e.Focused != "" && (sit.Focused == nil || sit.Focused.View != e.Focused) {
		got := "nothing"
		if sit.Focused != nil {
			got = sit.Focused.View
		}
		failed = append(failed, fmt.Sprintf("expected %s in front, found %s", e.Focused, got))
	}
	for _, v := range e.Open {
		if !open[v] {
			failed = append(failed, fmt.Sprintf("expected %s open", v))
		}
	}
	for _, v := range e.Closed {
		if open[v] {
			failed = append(failed, fmt.Sprintf("expected %s closed", v))
		}
	}
	if e.Mode != "" && sit.Mode != e.Mode {
		failed = append(failed, fmt.Sprintf("expected mode %q, found %q", e.Mode, sit.Mode))
	}
	if e.Layout != "" && sit.Layout != e.Layout {
		failed = append(failed, fmt.Sprintf("expected layout %q, found %q", e.Layout, sit.Layout))
	}
	if e.Tool != nil {
		v, err := s.call(ctx, *e.Tool)
		text := ""
		if err == nil {
			raw, _ := json.Marshal(v)
			text = string(raw)
			if str, ok := v.(string); ok {
				text = str
			}
		}
		switch {
		case err != nil:
			failed = append(failed, fmt.Sprintf("%s failed: %v", e.Tool.Tool, err))
		case e.Contains != "" && !strings.Contains(text, e.Contains):
			failed = append(failed, fmt.Sprintf("%s does not say %q", e.Tool.Tool, e.Contains))
		}
	}
	return failed
}

// Test runs a script without a page: each step's calls, then its checks. The desk is put
// back as it was afterwards, so a test can run on the owner's own desk.
func (s *Service) Test(ctx context.Context, name string) (Report, error) {
	sc, err := s.Get(ctx, name)
	if err != nil {
		return Report{}, err
	}
	desk := s.k.Desk()
	before := desk.Capture(ctx)
	defer func() { _ = desk.Arrange(context.WithoutCancel(ctx), before) }()

	rep := Report{Script: sc.Name, Mode: "test", Status: "passed"}
	for i, st := range sc.Steps {
		r := StepResult{Index: i + 1, Say: st.Say, OK: true}
		if err := s.do(ctx, st); err != nil {
			r.Failed = append(r.Failed, err.Error())
		}
		r.Failed = append(r.Failed, s.check(ctx, st)...)
		if len(r.Failed) > 0 {
			r.OK = false
			rep.Status = "failed"
		}
		rep.Steps = append(rep.Steps, r)
	}
	s.k.Events().Publish("script.done", rep)
	return rep, nil
}

// HoldFor is how long a demo shows a step: what it asks, or long enough to read what it says.
func HoldFor(st Step) time.Duration {
	if st.HoldMS > 0 {
		return time.Duration(st.HoldMS) * time.Millisecond
	}
	ms := 1500 + 45*len([]rune(st.Say))
	return time.Duration(min(ms, 9000)) * time.Millisecond
}

// Demo runs a script on its own, step after step, in the background: each step's calls,
// then what it points at and says, held long enough to read. It announces each step
// (script.step) so a recorder can follow, and script.done at the end.
func (s *Service) Demo(ctx context.Context, name string) (Script, error) {
	sc, err := s.Get(ctx, name)
	if err != nil {
		return sc, err
	}
	run, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.mu.Lock()
	if s.stop != nil {
		s.stop()
	}
	s.stop = cancel
	s.mu.Unlock()
	s.k.Events().Publish("script.started", map[string]any{"script": sc.Name, "mode": "demo", "steps": len(sc.Steps)})
	go func() {
		defer cancel()
		rep := Report{Script: sc.Name, Mode: "demo", Status: "done"}
		for i, st := range sc.Steps {
			r := StepResult{Index: i + 1, Say: st.Say, OK: true}
			if err := s.do(run, st); err != nil {
				r.OK, r.Failed = false, []string{err.Error()}
			}
			hold := HoldFor(st)
			s.k.Events().Publish("script.step", map[string]any{"script": sc.Name, "index": i + 1, "of": len(sc.Steps),
				"say": st.Say, "point": st.Point, "zoom": st.Zoom, "hold_ms": hold.Milliseconds()})
			s.show(run, st)
			rep.Steps = append(rep.Steps, r)
			if err := s.sleep(run, hold); err != nil {
				rep.Status = "stopped"
				break
			}
		}
		_, _ = s.call(context.WithoutCancel(run), Call{Tool: "ade_clear"})
		s.k.Events().Publish("script.done", rep)
	}()
	return sc, nil
}

// show points at what a step points at, with what it says beside it; with nothing to point
// at, it points at the window in front.
func (s *Service) show(ctx context.Context, st Step) {
	targets := st.Point
	if len(targets) == 0 {
		if w, ok := s.k.Desk().Focused(ctx); ok {
			raw, _ := json.Marshal(map[string]string{"window": w.ID})
			targets = []json.RawMessage{raw}
		}
	}
	if len(targets) == 0 {
		return
	}
	style := st.Style
	if style == "" {
		style = "spotlight"
	}
	in, _ := json.Marshal(map[string]any{"targets": targets, "style": style, "note": st.Say})
	_, _ = s.call(ctx, Call{Tool: "ade_point", Input: in})
}

// Stop stops a running demo.
func (s *Service) Stop() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stop == nil {
		return false
	}
	s.stop()
	s.stop = nil
	return true
}

// Guide puts a script on the owner's desk as an ADE guide: the owner moves it on, and each
// step's calls run as it comes up. The first step's calls run before the guide starts, so
// what it points at is there.
func (s *Service) Guide(ctx context.Context, name string) (any, error) {
	sc, err := s.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if err = s.do(ctx, sc.Steps[0]); err != nil {
		return nil, fmt.Errorf("step 1: %w", err)
	}
	type gstep struct {
		Say   string            `json:"say"`
		Point []json.RawMessage `json:"point,omitempty"`
		Style string            `json:"style,omitempty"`
		Await json.RawMessage   `json:"await,omitempty"`
		Hint  string            `json:"hint,omitempty"`
	}
	steps := make([]gstep, 0, len(sc.Steps))
	for _, st := range sc.Steps {
		steps = append(steps, gstep{Say: st.Say, Point: st.Point, Style: st.Style, Await: st.Await, Hint: st.Hint})
	}
	in, _ := json.Marshal(map[string]any{"title": firstNonEmpty(sc.About, sc.Name), "steps": steps})
	out, err := s.call(ctx, Call{Tool: "ade_guide_start", Input: in})
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.guide = &sc
	s.mu.Unlock()
	s.k.Events().Publish("script.started", map[string]any{"script": sc.Name, "mode": "guide", "steps": len(sc.Steps)})
	return out, nil
}

// onProgress runs a guide step's calls when the owner reaches it.
func (s *Service) onProgress(ev module.Event) {
	var r struct {
		Index  int    `json:"index"`
		Status string `json:"status"`
	}
	if json.Unmarshal(ev.Data, &r) != nil {
		return
	}
	s.mu.Lock()
	sc := s.guide
	if r.Status == "done" || r.Status == "stopped" {
		s.guide = nil
	}
	s.mu.Unlock()
	if sc == nil || r.Status != "running" || r.Index <= 0 || r.Index >= len(sc.Steps) {
		return
	}
	_ = s.do(context.Background(), sc.Steps[r.Index])
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
