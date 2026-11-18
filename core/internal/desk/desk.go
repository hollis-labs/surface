// Package desk is the window manager: which windows are open, which is in front, how they
// are laid out, and the layouts the user saved. The state lives here, in the server; the
// page draws it and sends actions. That is what lets the agent, a command and an outside
// client arrange the desk the same way the owner does.
//
// The windows are arranged in a tree (tree.go): tiled side by side and one above another,
// tabbed together in a stack, or floating over the rest. Opening a window tiles it the way
// a tiling desktop does (the first on the left, the rest stacked on the right); from there
// the owner drags windows where they want them and the page posts the tree back. Sizes in
// the tiling are shares of the room, so the page works out the pixels from its own size.
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
	Dirty  bool           `json:"dirty,omitempty"`
}

// State is the whole desk.
type State struct {
	// Wins are the open windows, in the order they were opened.
	Wins  []Win  `json:"wins"`
	Focus string `json:"focus"`
	// Tree is how the windows are arranged.
	Tree Tree `json:"tree"`
	// Layout is "tile" or "float": how the desk was last arranged as a whole.
	Layout string `json:"layout"`
	// Ratio is the first window's share of the width when a second is tiled beside it.
	Ratio float64 `json:"ratio"`
	// Active is the saved layout in use, when the desk was arranged from one.
	Active string `json:"active,omitempty"`
	// Mode is the user's mode ("writing", "triage"), when one is set.
	Mode string `json:"mode,omitempty"`
	// Setup is what goes with the arrangement in use (instructions, reflexes, a module's
	// own keys), saved or not: it is set when the desk is arranged and kept until the next.
	Setup map[string]any `json:"setup,omitempty"`
	// Trail is the focus history, newest last, for returning to the previous window.
	Trail []string `json:"trail,omitempty"`
	Seq   int      `json:"seq"`
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
	// state, rect, dirty, layout, ratio, cycle, swap, mode, tree
	State  map[string]any `json:"state,omitempty"`
	Rect   *module.Rect   `json:"rect,omitempty"`
	Dirty  bool           `json:"dirty,omitempty"`
	Layout string         `json:"layout,omitempty"`
	Ratio  float64        `json:"ratio,omitempty"`
	Delta  int            `json:"delta,omitempty"`
	Other  string         `json:"other,omitempty"`
	Mode   string         `json:"mode,omitempty"`
	// Tree is the arrangement as the page has it after the owner dragged something; Focus
	// is the window that has the focus there.
	Tree  *Tree  `json:"tree,omitempty"`
	Focus string `json:"focus,omitempty"`
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
	d := &Desk{db: db, pub: pub, views: map[string]ViewInfo{}, st: State{Layout: "tile", Ratio: 0.56, Wins: []Win{}, Tree: newTree()}}
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
			// A desk kept by an older build has no tree; a tree can also name windows that
			// are gone. Either way the windows are tiled into a sound one.
			d.st.Tree.normalize(d.ids(), d.st.Ratio, d.name)
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
	s.Tree = d.st.Tree.clone()
	return s
}

// ids lists the open windows' ids.
func (d *Desk) ids() []string {
	out := make([]string, 0, len(d.st.Wins))
	for _, w := range d.st.Wins {
		out = append(out, w.ID)
	}
	return out
}

// name makes an id for a new stack or float.
func (d *Desk) name(kind string) string {
	d.st.Seq++
	return fmt.Sprintf("%s-%d", kind, d.st.Seq)
}

// spot is where the next floating window goes when nothing says: a cascade, sized as its
// view asks.
func (d *Desk) spot(view string) module.Rect {
	v := d.views[view]
	n := len(d.st.Tree.Floats)
	return module.Rect{X: 48 + (n%8)*28, Y: 36 + (n%8)*24, W: orInt(v.Width, 820), H: orInt(v.Height, 600)}
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
				if !a.Background {
					d.focus(w.ID)
				}
				return st.Wins[i], true, false, nil
			}
		}
		st.Seq++
		w := Win{
			ID: fmt.Sprintf("%s-%d", strings.ReplaceAll(a.View, ".", "-"), st.Seq), View: a.View, Module: v.Module,
			Title: firstNonEmpty(a.Title, v.Title), Key: key, Args: a.Args,
		}
		if w.Args == nil {
			w.Args = map[string]any{}
		}
		st.Wins = append(st.Wins, w)
		// A window given a rectangle floats there; so does any window while the desk floats.
		switch {
		case a.Rect != nil:
			st.Tree.float(w.ID, d.name("float"), d.name("stack"), *a.Rect)
		case st.Layout == "float":
			st.Tree.float(w.ID, d.name("float"), d.name("stack"), d.spot(a.View))
		default:
			st.Tree.add(w.ID, d.name("stack"), st.Ratio)
		}
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
		st.Tree.remove(a.ID)
		d.dropFromTrail(a.ID)
		if st.Focus == a.ID {
			st.Focus = ""
			// Return to the previous window, as the trail remembers it.
			for len(st.Trail) > 0 {
				last := st.Trail[len(st.Trail)-1]
				st.Trail = st.Trail[:len(st.Trail)-1]
				if d.find(last) >= 0 {
					st.Focus = last
					break
				}
			}
			if st.Focus == "" && len(st.Wins) > 0 {
				st.Focus = st.Wins[len(st.Wins)-1].ID
			}
			st.Tree.show(st.Focus)
		}
		return win, true, false, nil
	case "focus":
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		if s, _, at := st.Tree.locate(a.ID); st.Focus == a.ID && s != nil && s.Selected == at {
			return st.Wins[i], false, false, nil
		}
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
		// Float a window at a rectangle, or move the float it is in.
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		if a.Rect == nil {
			return win, false, false, errors.New("desk: rect needs a rectangle")
		}
		r := *a.Rect
		r.W, r.H = max(r.W, minFloatW), max(r.H, minFloatH)
		if _, f, _ := st.Tree.locate(a.ID); f != nil {
			f.Rect = r
		} else {
			st.Tree.float(a.ID, d.name("float"), d.name("stack"), r)
		}
		return st.Wins[i], true, true, nil
	case "float":
		// Take a window out of the tiling, or put it back.
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		if st.Tree.floating(a.ID) {
			st.Tree.remove(a.ID)
			st.Tree.add(a.ID, d.name("stack"), st.Ratio)
		} else {
			st.Tree.float(a.ID, d.name("float"), d.name("stack"), d.spot(st.Wins[i].View))
		}
		st.Tree.show(a.ID)
		return st.Wins[i], true, true, nil
	case "maximize":
		// A tiled window's stack covers the tiling, or goes back. A float is left alone.
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		d.focus(a.ID)
		if s, f, _ := st.Tree.locate(a.ID); s != nil && f == nil {
			s.Maximized = !s.Maximized
		}
		return st.Wins[i], true, true, nil
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
		// Arrange every window that way, from scratch: all tiled, or all floating.
		st.Layout = next
		floats := map[string]module.Rect{}
		if next == "float" {
			for n, w := range st.Wins {
				r := d.spot(w.View)
				r.X, r.Y = 48+(n%8)*28, 36+(n%8)*24
				floats[w.ID] = r
			}
		}
		st.Tree = compose(st.Tree.order(), floats, st.Ratio, d.name)
		st.Tree.show(st.Focus)
		return win, true, true, nil
	case "ratio":
		st.Ratio = min(0.8, max(0.2, a.Ratio))
		st.Tree.setRatio(st.Ratio)
		return win, true, true, nil
	case "tree":
		// The page's arrangement after the owner dragged, split, tabbed or resized.
		if a.Tree == nil {
			return win, false, false, errors.New("desk: tree needs an arrangement")
		}
		next := a.Tree.clone()
		next.normalize(d.ids(), st.Ratio, d.name)
		st.Tree = next
		if a.Focus != "" && a.Focus != st.Focus && d.find(a.Focus) >= 0 {
			d.focus(a.Focus)
			return st.Wins[d.find(a.Focus)], true, false, nil
		}
		return win, true, true, nil
	case "promote":
		// Give a window the first place in the tiling.
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		if st.Tree.floating(a.ID) {
			st.Tree.remove(a.ID)
			st.Tree.add(a.ID, d.name("stack"), st.Ratio)
		}
		if tiled := st.Tree.tiled(); len(tiled) > 0 {
			st.Tree.swap(a.ID, tiled[0].Wins[tiled[0].Selected])
		}
		d.focus(a.ID)
		return st.Wins[i], true, true, nil
	case "tab":
		// Put a window in another's stack, as the tab showing.
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		if a.Other == a.ID || d.find(a.Other) < 0 {
			return win, false, false, fmt.Errorf("desk: no other window %q to tab with", a.Other)
		}
		st.Tree.remove(a.ID)
		to, _, _ := st.Tree.locate(a.Other)
		to.Wins = append(to.Wins, a.ID)
		st.Tree.show(a.ID)
		return st.Wins[i], true, true, nil
	case "swap":
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		if d.find(a.Other) < 0 {
			return win, false, false, fmt.Errorf("desk: no window %q", a.Other)
		}
		st.Tree.swap(a.ID, a.Other)
		st.Tree.show(st.Focus)
		return st.Wins[i], true, true, nil
	case "cycle":
		// The next window, in the order they sit on the desk.
		order := st.Tree.order()
		if len(order) == 0 {
			return win, false, false, nil
		}
		at := 0
		for i, id := range order {
			if id == st.Focus {
				at = i
			}
		}
		delta := a.Delta
		if delta == 0 {
			delta = 1
		}
		next := order[((at+delta)%len(order)+len(order))%len(order)]
		d.focus(next)
		return st.Wins[d.find(next)], true, false, nil
	case "back":
		// Return to the previous window.
		for len(st.Trail) > 0 {
			last := st.Trail[len(st.Trail)-1]
			st.Trail = st.Trail[:len(st.Trail)-1]
			if j := d.find(last); j >= 0 && last != st.Focus {
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

// focus gives a window the focus, shows it where it sits, and remembers the one it replaced.
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
	st.Tree.show(id)
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
