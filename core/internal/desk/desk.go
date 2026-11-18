// Package desk is the window manager: which windows are open, which is in front, how they
// are laid out, and the layouts the user saved. The state lives here, in the server; the
// page draws it and sends actions. That is what lets the agent, a command and an outside
// client arrange the desk the same way the owner does.
//
// Two layouts, as on a tiling desktop: "tile" arranges the windows (the first is the
// master, the rest stack beside it; a window can float out), "float" leaves every window
// where it was put. Geometry of tiled windows is the page's to compute from its own size;
// the server keeps order, focus and the rectangles of floating windows.
package desk

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/hollis-labs/surface/module"
)

// Win is one window as the desk keeps it.
type Win struct {
	ID     string         `json:"id"`
	View   string         `json:"view"`
	Module string         `json:"module"`
	Title  string         `json:"title"`
	Key    string         `json:"key,omitempty"`
	Args   map[string]any `json:"args"`
	State  map[string]any `json:"state,omitempty"`
	// Floating geometry, used when the window floats.
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
	// Z is the stacking order among floating windows; higher is in front.
	Z         int  `json:"z"`
	Floating  bool `json:"floating"`
	Maximized bool `json:"maximized"`
	Minimized bool `json:"minimized"`
	Dirty     bool `json:"dirty,omitempty"`
}

// State is the whole desk.
type State struct {
	// Wins are in tile order: the first tiled window is the master.
	Wins  []Win  `json:"wins"`
	Focus string `json:"focus"`
	// Layout is "tile" or "float".
	Layout string `json:"layout"`
	// Ratio is the master's share of the width.
	Ratio float64 `json:"ratio"`
	// Active is the saved layout in use, when the desk was arranged from one.
	Active string `json:"active,omitempty"`
	// Mode is the user's mode ("writing", "triage"), when one is set.
	Mode string `json:"mode,omitempty"`
	// Trail is the focus history, newest last, for returning to the previous window.
	Trail []string `json:"trail,omitempty"`
	Seq   int      `json:"seq"`
	Z     int      `json:"z"`
	// Rev counts changes, so a page can tell a newer state from an older one.
	Rev int `json:"rev"`
}

// Action is one change to the desk, from the page, a command or a tool.
type Action struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
	// open
	View       string         `json:"view,omitempty"`
	Key        string         `json:"key,omitempty"`
	Title      string         `json:"title,omitempty"`
	Args       map[string]any `json:"args,omitempty"`
	Background bool           `json:"background,omitempty"`
	// state, rect, dirty, layout, ratio, cycle, swap, mode
	State  map[string]any `json:"state,omitempty"`
	Rect   *module.Rect   `json:"rect,omitempty"`
	Dirty  bool           `json:"dirty,omitempty"`
	Layout string         `json:"layout,omitempty"`
	Ratio  float64        `json:"ratio,omitempty"`
	Delta  int            `json:"delta,omitempty"`
	Other  string         `json:"other,omitempty"`
	Mode   string         `json:"mode,omitempty"`
	// Force closes a window that holds unsaved work.
	Force bool `json:"force,omitempty"`
}

// ViewInfo is what the desk needs to know of a view.
type ViewInfo struct {
	module.View
	Module string
}

// Publisher announces changes. The events bus satisfies it.
type Publisher interface {
	Publish(topic string, data any)
}

// ErrDirty is returned when a window holding unsaved work is closed without Force.
var ErrDirty = errors.New("the window holds unsaved work")

// Desk is the window manager.
type Desk struct {
	db    *sql.DB
	pub   Publisher
	views map[string]ViewInfo
	// describe lets the module that owns a view add to a window's context.
	describe func(ctx context.Context, w module.Window) (module.WindowContext, bool)

	mu sync.Mutex
	st State
}

// Open loads the desk as it was when the app last ran, dropping windows whose view is gone.
func Open(ctx context.Context, db *sql.DB, pub Publisher, views []ViewInfo) (*Desk, error) {
	d := &Desk{db: db, pub: pub, views: map[string]ViewInfo{}, st: State{Layout: "tile", Ratio: 0.56, Z: 1, Wins: []Win{}}}
	for _, v := range views {
		if _, dup := d.views[v.ID]; dup {
			return nil, fmt.Errorf("desk: view %q is declared twice", v.ID)
		}
		d.views[v.ID] = v
	}
	var raw string
	err := db.QueryRowContext(ctx, `SELECT state FROM desk_state WHERE id=1`).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if raw != "" {
		var saved State
		if json.Unmarshal([]byte(raw), &saved) == nil {
			wins := saved.Wins[:0]
			for _, w := range saved.Wins {
				if _, ok := d.views[w.View]; ok {
					w.Dirty = false
					if w.Args == nil {
						w.Args = map[string]any{}
					}
					wins = append(wins, w)
				}
			}
			saved.Wins = wins
			if saved.Layout != "float" {
				saved.Layout = "tile"
			}
			if saved.Ratio < 0.2 || saved.Ratio > 0.8 {
				saved.Ratio = 0.56
			}
			if saved.Wins == nil {
				saved.Wins = []Win{}
			}
			d.st = saved
			if d.find(d.st.Focus) < 0 {
				d.st.Focus = ""
			}
		}
	}
	return d, nil
}

// SetDescriber sets how a window's module adds to its context.
func (d *Desk) SetDescriber(fn func(ctx context.Context, w module.Window) (module.WindowContext, bool)) {
	d.describe = fn
}

// Views lists the views the desk can open.
func (d *Desk) Views() []ViewInfo {
	out := make([]ViewInfo, 0, len(d.views))
	for _, v := range d.views {
		out = append(out, v)
	}
	return out
}

// Snapshot returns a copy of the state.
func (d *Desk) Snapshot() State {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.copyState()
}

func (d *Desk) copyState() State {
	s := d.st
	s.Wins = append([]Win{}, d.st.Wins...)
	s.Trail = append([]string{}, d.st.Trail...)
	return s
}

func (d *Desk) find(id string) int {
	for i, w := range d.st.Wins {
		if w.ID == id {
			return i
		}
	}
	return -1
}

// Apply makes one change, keeps it and announces it. It returns the window the action
// concerned, when there is one.
func (d *Desk) Apply(ctx context.Context, a Action) (Win, error) {
	d.mu.Lock()
	win, changed, quiet, err := d.reduce(a)
	if err != nil {
		d.mu.Unlock()
		return Win{}, err
	}
	var snap State
	if changed {
		d.st.Rev++
		snap = d.copyState()
	}
	d.mu.Unlock()
	if changed {
		d.persist(ctx, snap)
		if d.pub != nil {
			d.pub.Publish("desk.changed", snap)
			if !quiet {
				d.pub.Publish("desk.focus", map[string]any{"window": snap.Focus})
			}
			if a.Type == "open" {
				d.pub.Publish("desk.opened", map[string]any{"window": win.ID, "view": win.View})
			}
		}
	}
	return win, nil
}

func (d *Desk) persist(ctx context.Context, s State) {
	raw, err := json.Marshal(s)
	if err != nil {
		return
	}
	_, _ = d.db.ExecContext(context.WithoutCancel(ctx), `INSERT INTO desk_state (id, state) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET state=excluded.state`, string(raw))
}

// reduce applies an action to the state. The caller holds the lock. quiet marks a change
// that is not worth a focus announcement.
func (d *Desk) reduce(a Action) (win Win, changed, quiet bool, err error) {
	st := &d.st
	need := func() (int, error) {
		i := d.find(a.ID)
		if i < 0 {
			return -1, fmt.Errorf("desk: no window %q", a.ID)
		}
		return i, nil
	}
	switch a.Type {
	case "open":
		v, ok := d.views[a.View]
		if !ok {
			return win, false, false, fmt.Errorf("desk: no view %q", a.View)
		}
		key := a.Key
		if key == "" && v.Multi {
			key = v.DefaultKey
		}
		// One window of a view, or of a view and key, is brought forward instead.
		for i, w := range st.Wins {
			if w.View != a.View {
				continue
			}
			if !v.Multi || (key != "" && w.Key == key) {
				if a.Args != nil {
					st.Wins[i].Args = a.Args
				}
				if a.Title != "" {
					st.Wins[i].Title = a.Title
				}
				st.Wins[i].Minimized = false
				if !a.Background {
					d.focus(w.ID)
				}
				return st.Wins[i], true, false, nil
			}
		}
		st.Seq++
		st.Z++
		w := Win{
			ID: fmt.Sprintf("%s-%d", strings.ReplaceAll(a.View, ".", "-"), st.Seq), View: a.View, Module: v.Module,
			Title: firstNonEmpty(a.Title, v.Title), Key: key, Args: a.Args,
			W: orInt(v.Width, 820), H: orInt(v.Height, 600), Z: st.Z,
		}
		if w.Args == nil {
			w.Args = map[string]any{}
		}
		n := len(st.Wins)
		w.X, w.Y = 48+(n%8)*28, 36+(n%8)*24
		if a.Rect != nil {
			w.X, w.Y, w.W, w.H = a.Rect.X, a.Rect.Y, a.Rect.W, a.Rect.H
		}
		st.Wins = append(st.Wins, w)
		if !a.Background || st.Focus == "" {
			d.focus(w.ID)
		}
		return w, true, false, nil
	case "close":
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		if st.Wins[i].Dirty && !a.Force {
			return st.Wins[i], false, false, ErrDirty
		}
		win = st.Wins[i]
		st.Wins = append(st.Wins[:i], st.Wins[i+1:]...)
		d.dropFromTrail(a.ID)
		if st.Focus == a.ID {
			st.Focus = ""
			// Return to the previous window, as the trail remembers it.
			for len(st.Trail) > 0 {
				last := st.Trail[len(st.Trail)-1]
				st.Trail = st.Trail[:len(st.Trail)-1]
				if j := d.find(last); j >= 0 && !st.Wins[j].Minimized {
					st.Focus = last
					break
				}
			}
			if st.Focus == "" {
				for j := len(st.Wins) - 1; j >= 0; j-- {
					if !st.Wins[j].Minimized {
						st.Focus = st.Wins[j].ID
						break
					}
				}
			}
		}
		return win, true, false, nil
	case "focus":
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		if st.Focus == a.ID && !st.Wins[i].Minimized {
			return st.Wins[i], false, false, nil
		}
		st.Wins[i].Minimized = false
		d.focus(a.ID)
		return st.Wins[i], true, false, nil
	case "title":
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		if st.Wins[i].Title == a.Title {
			return st.Wins[i], false, true, nil
		}
		st.Wins[i].Title = a.Title
		return st.Wins[i], true, true, nil
	case "args":
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		if a.Args == nil {
			a.Args = map[string]any{}
		}
		st.Wins[i].Args = a.Args
		return st.Wins[i], true, true, nil
	case "state":
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		st.Wins[i].State = a.State
		return st.Wins[i], true, true, nil
	case "dirty":
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		if st.Wins[i].Dirty == a.Dirty {
			return st.Wins[i], false, true, nil
		}
		st.Wins[i].Dirty = a.Dirty
		return st.Wins[i], true, true, nil
	case "rect":
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		if a.Rect == nil {
			return win, false, false, errors.New("desk: rect needs a rectangle")
		}
		w := &st.Wins[i]
		w.X, w.Y, w.W, w.H = a.Rect.X, a.Rect.Y, max(a.Rect.W, 320), max(a.Rect.H, 180)
		w.Maximized = false
		return *w, true, true, nil
	case "float":
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		st.Wins[i].Floating = !st.Wins[i].Floating
		st.Wins[i].Maximized = false
		st.Z++
		st.Wins[i].Z = st.Z
		return st.Wins[i], true, true, nil
	case "maximize":
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		st.Wins[i].Maximized = !st.Wins[i].Maximized
		d.focus(a.ID)
		return st.Wins[i], true, true, nil
	case "minimize":
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		st.Wins[i].Minimized = true
		st.Wins[i].Maximized = false
		if st.Focus == a.ID {
			st.Focus = ""
			for j := len(st.Wins) - 1; j >= 0; j-- {
				if !st.Wins[j].Minimized {
					st.Focus = st.Wins[j].ID
					break
				}
			}
		}
		return st.Wins[i], true, false, nil
	case "layout":
		next := a.Layout
		if next == "" {
			next = "float"
			if st.Layout == "float" {
				next = "tile"
			}
		}
		if next != "tile" && next != "float" {
			return win, false, false, fmt.Errorf("desk: no layout %q", next)
		}
		if st.Layout == next {
			return win, false, true, nil
		}
		st.Layout = next
		return win, true, true, nil
	case "ratio":
		st.Ratio = min(0.8, max(0.2, a.Ratio))
		return win, true, true, nil
	case "promote":
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		w := st.Wins[i]
		w.Floating = false
		st.Wins = append(st.Wins[:i], st.Wins[i+1:]...)
		st.Wins = append([]Win{w}, st.Wins...)
		d.focus(w.ID)
		return w, true, true, nil
	case "swap":
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		j := d.find(a.Other)
		if j < 0 {
			return win, false, false, fmt.Errorf("desk: no window %q", a.Other)
		}
		st.Wins[i], st.Wins[j] = st.Wins[j], st.Wins[i]
		return st.Wins[j], true, true, nil
	case "cycle":
		var visible []string
		for _, w := range st.Wins {
			if !w.Minimized {
				visible = append(visible, w.ID)
			}
		}
		if len(visible) == 0 {
			return win, false, false, nil
		}
		at := 0
		for i, id := range visible {
			if id == st.Focus {
				at = i
			}
		}
		delta := a.Delta
		if delta == 0 {
			delta = 1
		}
		next := visible[((at+delta)%len(visible)+len(visible))%len(visible)]
		d.focus(next)
		return st.Wins[d.find(next)], true, false, nil
	case "back":
		// Return to the previous window.
		for len(st.Trail) > 0 {
			last := st.Trail[len(st.Trail)-1]
			st.Trail = st.Trail[:len(st.Trail)-1]
			if j := d.find(last); j >= 0 && last != st.Focus {
				st.Wins[j].Minimized = false
				cur := st.Focus
				d.focus(last)
				// focus pushed the window we left; a "back" should not grow the trail.
				if n := len(st.Trail); n > 0 && st.Trail[n-1] == cur {
					st.Trail = st.Trail[:n-1]
				}
				return st.Wins[j], true, false, nil
			}
		}
		return win, false, false, nil
	case "active":
		// The saved layout in use.
		if st.Active == a.Layout {
			return win, false, true, nil
		}
		st.Active = a.Layout
		return win, true, true, nil
	case "mode":
		if st.Mode == a.Mode {
			return win, false, true, nil
		}
		st.Mode = a.Mode
		return win, true, true, nil
	}
	return win, false, false, fmt.Errorf("desk: unknown action %q", a.Type)
}

// focus brings a window to the front and remembers the one it replaced.
func (d *Desk) focus(id string) {
	st := &d.st
	if st.Focus != "" && st.Focus != id {
		d.dropFromTrail(st.Focus)
		st.Trail = append(st.Trail, st.Focus)
		if len(st.Trail) > 32 {
			st.Trail = st.Trail[len(st.Trail)-32:]
		}
	}
	st.Focus = id
	if i := d.find(id); i >= 0 {
		st.Z++
		st.Wins[i].Z = st.Z
	}
}

func (d *Desk) dropFromTrail(id string) {
	out := d.st.Trail[:0]
	for _, t := range d.st.Trail {
		if t != id {
			out = append(out, t)
		}
	}
	d.st.Trail = out
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func orInt(a, b int) int {
	if a > 0 {
		return a
	}
	return b
}
