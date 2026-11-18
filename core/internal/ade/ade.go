// Package ade lets the agent show the owner things on the desk: point at a window or at a
// part of one (an anchor its view declared), spotlight it, draw an arrow between two, and
// walk the owner through a short guide.
//
// The server keeps what is asked for and the page draws it. A tool call publishes
// "ade.attention" or "ade.guide" on the bus; the page applies it to its attention hub
// (@hollis-labs/ade-core) and draws it with the registry's attention layer and guide bar.
// The page reports back the anchors each window's view declared and where a guide stands,
// so the agent can see what it can point at and whether the owner has finished.
package ade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hollis-labs/surface/core/internal/kernel"
	"github.com/hollis-labs/surface/module"
)

// Anchor is a part of a window that its view named, so it can be pointed at.
type Anchor struct {
	Name  string `json:"name"`
	Label string `json:"label,omitempty"`
}

// Target is what the agent asks to point at: a window (by id, or by view for the window of
// that view), and optionally an anchor in it.
type Target struct {
	Window string `json:"window,omitempty"`
	View   string `json:"view,omitempty"`
	Anchor string `json:"anchor,omitempty"`
	Label  string `json:"label,omitempty"`
}

// Point is a Target found: the window's id, and the anchor when there is one.
type Point struct {
	Window string `json:"window"`
	Anchor string `json:"anchor,omitempty"`
	Label  string `json:"label,omitempty"`
}

// Attention is what the agent is pointing at now.
type Attention struct {
	Style   string  `json:"style"`
	Targets []Point `json:"targets"`
	From    *Point  `json:"from,omitempty"`
	Note    string  `json:"note,omitempty"`
	// Until is when it lapses, in Unix milliseconds; 0 means until replaced or cleared.
	Until int64 `json:"until,omitempty"`
}

// Await is what a guide step waits for: the owner's go-ahead ("ack"), a window of a view
// being open ("open"), or being in front ("focus").
type Await struct {
	Kind string `json:"kind"`
	View string `json:"view,omitempty"`
}

// Step is one step of a guide.
type Step struct {
	ID    string  `json:"id"`
	Say   string  `json:"say"`
	Point []Point `json:"point,omitempty"`
	Style string  `json:"style,omitempty"`
	Await *Await  `json:"await,omitempty"`
	Hint  string  `json:"hint,omitempty"`
}

// Guide is a short walk-through the owner moves on.
type Guide struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Steps []Step `json:"steps"`
}

// Report is where the page says a guide stands.
type Report struct {
	Guide   string `json:"guide"`
	Index   int    `json:"index"`
	Step    string `json:"step,omitempty"`
	Status  string `json:"status"`
	Stalled string `json:"stalled,omitempty"`
	At      string `json:"at,omitempty"`
}

// State is everything the page needs to draw on load.
type State struct {
	Attention *Attention `json:"attention"`
	Guide     *Guide     `json:"guide"`
	Report    *Report    `json:"report"`
}

// Service keeps the attention, the guide and the anchors.
type Service struct {
	k   kernel.Kernel
	now func() time.Time

	mu        sync.Mutex
	anchors   map[string][]Anchor
	attention *Attention
	guide     *Guide
	report    *Report
	seq       int
}

// New makes the service.
func New(k kernel.Kernel) *Service {
	return &Service{k: k, now: time.Now, anchors: map[string][]Anchor{}}
}

// Close has nothing to let go of.
func (s *Service) Close(context.Context) error { return nil }

// Snapshot is the state now, with a lapsed attention dropped.
func (s *Service) Snapshot() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attention != nil && s.attention.Until != 0 && s.attention.Until <= s.now().UnixMilli() {
		s.attention = nil
	}
	return State{Attention: s.attention, Guide: s.guide, Report: s.report}
}

// SetAnchors records what a window's view declared; none forgets the window.
func (s *Service) SetAnchors(window string, list []Anchor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(list) == 0 {
		delete(s.anchors, window)
		return
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	s.anchors[window] = list
}

// SetReport records where the page says the guide stands. A guide that has ended is let go.
func (s *Service) SetReport(r Report) {
	s.mu.Lock()
	if s.guide == nil || r.Guide != s.guide.ID {
		s.mu.Unlock()
		return
	}
	r.At = s.now().UTC().Format(time.RFC3339)
	s.report = &r
	if r.Status == "done" || r.Status == "stopped" {
		s.guide = nil
	}
	s.mu.Unlock()
	s.k.Events().Publish("ade.guide.progress", r)
}

// ClearAttention is the owner dismissing what the agent pointed at.
func (s *Service) ClearAttention() {
	s.mu.Lock()
	s.attention = nil
	s.mu.Unlock()
	s.k.Events().Publish("ade.attention", map[string]any{"attention": nil})
}

// find turns a target into a point: the window it names, and the anchor, which that
// window's view must have declared.
func (s *Service) find(ctx context.Context, t Target) (Point, error) {
	wins := s.k.Desk().Windows(ctx)
	var win *module.Window
	for i := range wins {
		w := &wins[i]
		if (t.Window != "" && w.ID == t.Window) || (t.Window == "" && t.View != "" && w.View == t.View) {
			if win == nil || w.Focused {
				win = w
			}
		}
	}
	if t.Window == "" && t.View == "" {
		if w, ok := s.k.Desk().Focused(ctx); ok {
			win = &w
		}
	}
	if win == nil {
		open := make([]string, 0, len(wins))
		for _, w := range wins {
			open = append(open, fmt.Sprintf("%s (%s)", w.ID, w.View))
		}
		return Point{}, fmt.Errorf("no open window matches %q; open: %s", firstOf(t.Window, t.View), strings.Join(open, ", "))
	}
	p := Point{Window: win.ID, Anchor: t.Anchor, Label: t.Label}
	if t.Anchor == "" {
		if p.Label == "" {
			p.Label = win.Title
		}
		return p, nil
	}
	s.mu.Lock()
	declared := s.anchors[win.ID]
	s.mu.Unlock()
	for _, a := range declared {
		if a.Name == t.Anchor {
			if p.Label == "" {
				p.Label = firstOf(a.Label, a.Name)
			}
			return p, nil
		}
	}
	names := make([]string, 0, len(declared))
	for _, a := range declared {
		names = append(names, a.Name)
	}
	if len(names) == 0 {
		return Point{}, fmt.Errorf("the %s window (%s) declares no anchors; point at the window itself", win.View, win.ID)
	}
	return Point{}, fmt.Errorf("the %s window has no anchor %q; it has: %s", win.View, t.Anchor, strings.Join(names, ", "))
}

func firstOf(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

var styles = map[string]bool{"highlight": true, "spotlight": true, "arrow": true}

func (s *Service) point(ctx context.Context, in json.RawMessage) (any, error) {
	var a struct {
		Targets []Target `json:"targets"`
		Style   string   `json:"style"`
		From    *Target  `json:"from"`
		Note    string   `json:"note"`
		Seconds int      `json:"seconds"`
	}
	if err := json.Unmarshal(in, &a); err != nil {
		return nil, err
	}
	if a.Style == "" {
		a.Style = "highlight"
	}
	if !styles[a.Style] {
		return nil, fmt.Errorf("style is highlight, spotlight or arrow, not %q", a.Style)
	}
	if len(a.Targets) == 0 {
		return nil, errors.New("say what to point at: targets")
	}
	att := Attention{Style: a.Style, Note: a.Note}
	for _, t := range a.Targets {
		p, err := s.find(ctx, t)
		if err != nil {
			return nil, err
		}
		att.Targets = append(att.Targets, p)
	}
	if a.Style == "arrow" {
		if a.From == nil {
			return nil, errors.New("an arrow needs from: where it starts")
		}
		p, err := s.find(ctx, *a.From)
		if err != nil {
			return nil, err
		}
		att.From = &p
	}
	if a.Seconds > 0 {
		att.Until = s.now().Add(time.Duration(a.Seconds) * time.Second).UnixMilli()
	}
	s.mu.Lock()
	s.attention = &att
	s.mu.Unlock()
	s.k.Events().Publish("ade.attention", map[string]any{"attention": att})
	labels := make([]string, 0, len(att.Targets))
	for _, p := range att.Targets {
		labels = append(labels, p.Label)
	}
	return fmt.Sprintf("Showing the owner (%s): %s.", a.Style, strings.Join(labels, ", ")), nil
}

func (s *Service) startGuide(ctx context.Context, in json.RawMessage) (any, error) {
	var a struct {
		Title string `json:"title"`
		Steps []struct {
			Say   string   `json:"say"`
			Point []Target `json:"point"`
			Style string   `json:"style"`
			Await *Await   `json:"await"`
			Hint  string   `json:"hint"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(in, &a); err != nil {
		return nil, err
	}
	if a.Title == "" || len(a.Steps) == 0 {
		return nil, errors.New("a guide needs a title and at least one step")
	}
	views := map[string]bool{}
	for _, v := range s.k.Desk().Windows(ctx) {
		views[v.View] = true
	}
	s.mu.Lock()
	s.seq++
	g := Guide{ID: fmt.Sprintf("guide-%d-%d", s.now().Unix(), s.seq), Title: a.Title}
	s.mu.Unlock()
	for i, st := range a.Steps {
		if strings.TrimSpace(st.Say) == "" {
			return nil, fmt.Errorf("step %d says nothing", i+1)
		}
		step := Step{ID: fmt.Sprintf("s%d", i+1), Say: st.Say, Style: st.Style, Hint: st.Hint, Await: st.Await}
		if step.Style != "" && step.Style != "highlight" && step.Style != "spotlight" {
			return nil, fmt.Errorf("step %d: a step's style is highlight or spotlight", i+1)
		}
		if w := step.Await; w != nil {
			switch w.Kind {
			case "", "ack":
				step.Await = nil
			case "open", "focus":
				if w.View == "" {
					return nil, fmt.Errorf("step %d waits for a view to %s: name the view", i+1, w.Kind)
				}
			default:
				return nil, fmt.Errorf("step %d: await is ack, open or focus, not %q", i+1, w.Kind)
			}
		}
		for _, t := range st.Point {
			p, err := s.find(ctx, t)
			if err != nil {
				return nil, fmt.Errorf("step %d: %w", i+1, err)
			}
			step.Point = append(step.Point, p)
		}
		g.Steps = append(g.Steps, step)
	}
	s.mu.Lock()
	s.guide = &g
	s.report = &Report{Guide: g.ID, Status: "running", Step: g.Steps[0].ID, At: s.now().UTC().Format(time.RFC3339)}
	s.attention = nil
	s.mu.Unlock()
	s.k.Events().Publish("ade.guide", map[string]any{"guide": g})
	return fmt.Sprintf("The guide %q is on screen with %d steps. The owner moves it on (or does what a step waits for); ade_guide_status says where it stands.", g.Title, len(g.Steps)), nil
}

func (s *Service) stopGuide() (any, error) {
	s.mu.Lock()
	g := s.guide
	s.guide = nil
	if g != nil && s.report != nil && s.report.Guide == g.ID {
		s.report.Status = "stopped"
	}
	s.mu.Unlock()
	if g == nil {
		return "No guide is running.", nil
	}
	s.k.Events().Publish("ade.guide", map[string]any{"guide": nil})
	return "Stopped the guide " + g.Title + ".", nil
}

func (s *Service) status() (any, error) {
	st := s.Snapshot()
	if st.Report == nil {
		return "No guide has run.", nil
	}
	out := map[string]any{"report": st.Report}
	if st.Guide != nil {
		out["title"] = st.Guide.Title
		out["steps"] = len(st.Guide.Steps)
	}
	return out, nil
}

func (s *Service) anchorList(ctx context.Context) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	type win struct {
		Window  string   `json:"window"`
		View    string   `json:"view"`
		Title   string   `json:"title"`
		Focused bool     `json:"focused,omitempty"`
		Anchors []Anchor `json:"anchors,omitempty"`
	}
	var out []win
	for _, w := range s.k.Desk().Windows(ctx) {
		out = append(out, win{Window: w.ID, View: w.View, Title: w.Title, Focused: w.Focused, Anchors: s.anchors[w.ID]})
	}
	return out
}

// Tools are the agent's: look at what can be pointed at, point, clear, and run a guide.
func (s *Service) Tools() []kernel.BoundTool {
	t := func(name, desc string, lvl module.Level, in string, run func(ctx context.Context, in json.RawMessage) (any, error)) kernel.BoundTool {
		return kernel.BoundTool{Tool: module.Tool{Name: name, Description: desc, Level: lvl, Input: json.RawMessage(in)}, Module: "ade", Call: run}
	}
	target := `{"type":"object","properties":{"window":{"type":"string","description":"a window id"},"view":{"type":"string","description":"or a view id: its window"},"anchor":{"type":"string","description":"a part the window's view declared (ade_anchors lists them)"},"label":{"type":"string"}}}`
	return []kernel.BoundTool{
		t("ade_anchors", "List the open windows and the parts of each you can point at (anchors their views declared).",
			module.LevelRead, `{"type":"object","properties":{}}`,
			func(ctx context.Context, _ json.RawMessage) (any, error) { return s.anchorList(ctx), nil }),
		t("ade_point", "Show the owner something on the desk: ring a window or an anchor in one (highlight), dim everything else around it (spotlight), or draw an arrow to it from another (arrow, with from). A note is shown beside it. It stays until you clear it, point elsewhere, or seconds pass. With no window or view, the window in front.",
			module.LevelAct, `{"type":"object","properties":{"targets":{"type":"array","items":`+target+`},"style":{"type":"string","enum":["highlight","spotlight","arrow"]},"from":`+target+`,"note":{"type":"string"},"seconds":{"type":"integer"}},"required":["targets"]}`,
			s.point),
		t("ade_clear", "Stop pointing at anything.", module.LevelAct, `{"type":"object","properties":{}}`,
			func(context.Context, json.RawMessage) (any, error) {
				s.ClearAttention()
				return "Cleared.", nil
			}),
		t("ade_guide_start", "Walk the owner through something in a few steps, shown in a bar over the desk with back, next and stop. Each step says one thing and may point at windows or anchors. A step waits for the owner's Next, or (await) for a view to be open or in front, and then moves on by itself. Starting a guide replaces the last.",
			module.LevelAct, `{"type":"object","properties":{"title":{"type":"string"},"steps":{"type":"array","items":{"type":"object","properties":{"say":{"type":"string"},"point":{"type":"array","items":`+target+`},"style":{"type":"string","enum":["highlight","spotlight"]},"await":{"type":"object","properties":{"kind":{"type":"string","enum":["ack","open","focus"]},"view":{"type":"string"}}},"hint":{"type":"string"}},"required":["say"]}}},"required":["title","steps"]}`,
			s.startGuide),
		t("ade_guide_status", "Where the guide stands: its step, and whether the owner is going, paused, done or stopped it.",
			module.LevelRead, `{"type":"object","properties":{}}`,
			func(context.Context, json.RawMessage) (any, error) { return s.status() }),
		t("ade_guide_stop", "Take the guide off the screen.", module.LevelAct, `{"type":"object","properties":{}}`,
			func(context.Context, json.RawMessage) (any, error) { return s.stopGuide() }),
	}
}
