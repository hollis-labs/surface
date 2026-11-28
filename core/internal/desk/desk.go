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
	"time"

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
	// Provenance (docs/desk-rules.md): who opened the window, the owner's window that was in
	// front then, what for, how long it is meant to stay, and whether it is the owner's.
	OpenedBy  string `json:"opened_by,omitempty"`
	Opener    string `json:"opener,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Retention string `json:"retention,omitempty"`
	Kept      bool   `json:"kept,omitempty"`
	// Composing is set while the owner has unsent input in the window (the page says so).
	// Held is a delegate's change to what the window shows, kept until the owner is done
	// (rule 7).
	Composing bool  `json:"composing,omitempty"`
	Held      *Held `json:"held,omitempty"`
}

// Held is a change a delegate asked for while the owner was working in the window.
type Held struct {
	Args  map[string]any `json:"args"`
	Title string         `json:"title,omitempty"`
	By    string         `json:"by"`
	At    string         `json:"at"`
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
	// Owned is the windows the owner has put in front, newest last: the trail without the
	// agent's comings and goings. A closed window gives the focus back to the newest of these.
	Owned []string `json:"owned,omitempty"`
	// PanelComposing is set while the owner has unsent input in the agent panel.
	PanelComposing bool `json:"panel_composing,omitempty"`
	Seq            int  `json:"seq"`
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
	// composing: whether the owner has unsent input. held: "apply" or "drop".
	Composing bool   `json:"composing,omitempty"`
	Do        string `json:"do,omitempty"`
	// open: what the window is for, and "transient" or "deliverable".
	Reason    string `json:"reason,omitempty"`
	Retention string `json:"retention,omitempty"`
	// Actor is who asks. It is never read from the request body: Apply takes it from the
	// context (module.ActorOf).
	Actor module.Actor `json:"-"`
	// Agent names the outside agent acting (a seat over MCP: module.AgentOf), if any.
	Agent string `json:"-"`
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

// ErrNotYours is returned when a delegate (the agent, a reflex, an outside client) tries to
// close the owner's window.
var ErrNotYours = errors.New("the window is the owner's: ask them to close it")

// ErrHeld is returned, after the fact, when a delegate's change to what a window shows was
// held because the owner is working in it (rule 7). The change is kept, not lost.
var ErrHeld = errors.New("held: the owner is typing or has unsaved changes in that window, so the change waits and applies when they are done; do not ask again")

// ErrOwnerBusy is returned when a delegate tries to take the focus from a window holding the
// owner's unsaved work or unsent input.
var ErrOwnerBusy = errors.New("the owner is working where the focus is (unsaved changes or unsent input): open in the background, or ask")

// Desk is the window manager.
type Desk struct {
	db    *sql.DB
	pub   Publisher
	views map[string]ViewInfo
	// describe lets the module that owns a view add to a window's context.
	describe func(ctx context.Context, w module.Window) (module.WindowContext, bool)

	mu sync.Mutex
	st State
	// held is set by reduce when it held a delegate's change, so Apply can say so.
	held bool
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
					w.Composing = false
					if w.Args == nil {
						w.Args = map[string]any{}
					}
					wins = append(wins, w)
				}
			}
			saved.Wins = wins
			saved.PanelComposing = false
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
			// Nobody is typing at start: what was held is applied now.
			for i := range d.st.Wins {
				d.applyHeld(i)
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
	s.Owned = append([]string{}, d.st.Owned...)
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
	a.Actor = module.ActorOf(ctx)
	a.Agent = module.AgentOf(ctx).Name
	d.mu.Lock()
	d.held = false
	win, changed, quiet, err := d.reduce(a)
	if err != nil {
		d.mu.Unlock()
		return Win{}, err
	}
	held := d.held
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
				focus := map[string]any{"window": snap.Focus, "by": string(a.Actor)}
				for _, w := range snap.Wins {
					if w.ID == snap.Focus {
						focus["view"] = w.View
					}
				}
				d.pub.Publish("desk.focus", focus)
			}
			if held {
				d.pub.Publish("desk.held", map[string]any{"window": win.ID, "by": string(a.Actor)})
			}
			if a.Type == "open" && !held {
				d.pub.Publish("desk.opened", map[string]any{"window": win.ID, "view": win.View, "opened_by": win.OpenedBy, "focused": snap.Focus == win.ID})
			}
		}
	}
	if held {
		return win, ErrHeld
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
				if a.Args != nil && a.Actor.Delegate() && d.busy(i) {
					// The owner is working in it: what it shows waits for them (rule 7).
					d.hold(i, a)
					return st.Wins[i], true, true, nil
				}
				if a.Args != nil {
					st.Wins[i].Args = a.Args
				}
				if a.Title != "" {
					st.Wins[i].Title = a.Title
				}
				if !d.background(a) {
					d.focus(w.ID, a.Actor)
				}
				return st.Wins[i], true, false, nil
			}
		}
		st.Seq++
		w := Win{
			ID: fmt.Sprintf("%s-%d", strings.ReplaceAll(a.View, ".", "-"), st.Seq), View: a.View, Module: v.Module,
			Title: firstNonEmpty(a.Title, v.Title), Key: key, Args: a.Args,
			OpenedBy: string(a.Actor), Opener: d.ownersWindow(), Reason: a.Reason, Kept: !a.Actor.Delegate(),
		}
		if a.Actor.Delegate() && a.Retention == "transient" {
			w.Retention = "transient"
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
		case a.Actor.Delegate() && st.Tree.beside(w.ID, d.name("stack"), w.Opener):
			// A delegate's window goes beside the owner's window it was opened from.
		default:
			st.Tree.add(w.ID, d.name("stack"), st.Ratio)
		}
		if !d.background(a) || st.Focus == "" {
			d.focus(w.ID, a.Actor)
		}
		return w, true, false, nil
	case "close":
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		if a.Actor.Delegate() && (st.Wins[i].Kept || !module.Actor(st.Wins[i].OpenedBy).Delegate()) {
			return st.Wins[i], false, false, ErrNotYours
		}
		if st.Wins[i].Dirty && !a.Force {
			return st.Wins[i], false, false, ErrDirty
		}
		win = st.Wins[i]
		st.Wins = append(st.Wins[:i], st.Wins[i+1:]...)
		st.Tree.remove(a.ID)
		d.dropFromTrail(a.ID)
		if st.Focus == a.ID {
			st.Focus = d.returnTo(win)
			st.Tree.show(st.Focus)
		}
		return win, true, false, nil
	case "keep":
		// The owner takes a window over: from now on it is theirs.
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		if a.Actor.Delegate() {
			return st.Wins[i], false, false, errors.New("desk: only the owner keeps a window")
		}
		if st.Wins[i].Kept {
			return st.Wins[i], false, true, nil
		}
		st.Wins[i].Kept = true
		return st.Wins[i], true, true, nil
	case "release":
		// A delegate is done with its transient windows: the one named, or all of them. A
		// window the owner kept, or one holding unsaved work, stays (rule 6).
		var gone []Win
		for j := 0; j < len(st.Wins); j++ {
			w := st.Wins[j]
			if (a.ID != "" && w.ID != a.ID) || w.Retention != "transient" || w.Kept || w.Dirty || !module.Actor(w.OpenedBy).Delegate() {
				continue
			}
			st.Wins = append(st.Wins[:j], st.Wins[j+1:]...)
			j--
			st.Tree.remove(w.ID)
			d.dropFromTrail(w.ID)
			gone = append(gone, w)
		}
		if len(gone) == 0 {
			return win, false, true, nil
		}
		for _, w := range gone {
			if st.Focus == w.ID {
				st.Focus = d.returnTo(w)
				st.Tree.show(st.Focus)
			}
		}
		return gone[0], true, false, nil
	case "focus":
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		if s, _, at := st.Tree.locate(a.ID); st.Focus == a.ID && s != nil && s.Selected == at {
			return st.Wins[i], false, false, nil
		}
		if a.Actor.Delegate() && a.ID != st.Focus && d.ownerBusy() {
			return st.Wins[i], false, false, ErrOwnerBusy
		}
		d.focus(a.ID, a.Actor)
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
		if a.Actor.Delegate() && d.busy(i) {
			d.hold(i, a)
			return st.Wins[i], true, true, nil
		}
		st.Wins[i].Args = a.Args
		return st.Wins[i], true, true, nil
	case "composing":
		// The owner has unsent input in a window, or in the agent panel when there is no id.
		if a.Actor.Delegate() {
			return win, false, false, errors.New("desk: only the owner composes")
		}
		if a.ID == "" {
			if st.PanelComposing == a.Composing {
				return win, false, true, nil
			}
			st.PanelComposing = a.Composing
			return win, true, true, nil
		}
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		if st.Wins[i].Composing == a.Composing {
			return st.Wins[i], false, true, nil
		}
		st.Wins[i].Composing = a.Composing
		d.applyHeld(i)
		return st.Wins[i], true, true, nil
	case "held":
		// The owner decides about a held change now: apply it, or drop it.
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		if a.Actor.Delegate() {
			return st.Wins[i], false, false, errors.New("desk: only the owner decides about a held change")
		}
		if st.Wins[i].Held == nil {
			return st.Wins[i], false, true, nil
		}
		switch a.Do {
		case "apply":
			h := st.Wins[i].Held
			st.Wins[i].Args, st.Wins[i].Held = h.Args, nil
			if h.Title != "" {
				st.Wins[i].Title = h.Title
			}
		case "drop":
			st.Wins[i].Held = nil
		default:
			return st.Wins[i], false, false, errors.New(`desk: held needs do: "apply" or "drop"`)
		}
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
		// Unsaved work is the owner's work: the window becomes theirs.
		if a.Dirty && !a.Actor.Delegate() {
			st.Wins[i].Kept = true
		}
		d.applyHeld(i)
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
		d.claim(i, a.Actor)
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
		d.claim(i, a.Actor)
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
		d.claim(i, a.Actor)
		d.focus(a.ID, a.Actor)
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
		// ID is the window the owner grabbed, when the page says: moving it makes it theirs.
		// The windows that only shifted to make room are not taken over.
		if j := d.find(a.ID); a.ID != "" && j >= 0 {
			d.claim(j, a.Actor)
		}
		if a.Focus != "" && a.Focus != st.Focus && d.find(a.Focus) >= 0 {
			d.focus(a.Focus, a.Actor)
			return st.Wins[d.find(a.Focus)], true, false, nil
		}
		return win, true, true, nil
	case "promote":
		// Give a window the first place in the tiling.
		i, err := need()
		if err != nil {
			return win, false, false, err
		}
		d.claim(i, a.Actor)
		if st.Tree.floating(a.ID) {
			st.Tree.remove(a.ID)
			st.Tree.add(a.ID, d.name("stack"), st.Ratio)
		}
		if tiled := st.Tree.tiled(); len(tiled) > 0 {
			st.Tree.swap(a.ID, tiled[0].Wins[tiled[0].Selected])
		}
		d.focus(a.ID, a.Actor)
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
		d.claim(i, a.Actor)
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
		d.claim(i, a.Actor)
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
		d.focus(next, a.Actor)
		return st.Wins[d.find(next)], true, false, nil
	case "back":
		// Return to the previous window.
		for len(st.Trail) > 0 {
			last := st.Trail[len(st.Trail)-1]
			st.Trail = st.Trail[:len(st.Trail)-1]
			if j := d.find(last); j >= 0 && last != st.Focus {
				cur := st.Focus
				d.focus(last, a.Actor)
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
// The owner's own moves are also kept in Owned, the trail a closed window returns along.
func (d *Desk) focus(id string, by module.Actor) {
	st := &d.st
	if st.Focus != "" && st.Focus != id {
		st.Trail = append(without(st.Trail, st.Focus), st.Focus)
		if len(st.Trail) > 32 {
			st.Trail = st.Trail[len(st.Trail)-32:]
		}
	}
	if !by.Delegate() {
		st.Owned = append(without(st.Owned, id), id)
		if len(st.Owned) > 32 {
			st.Owned = st.Owned[len(st.Owned)-32:]
		}
	}
	st.Focus = id
	st.Tree.show(id)
}

// claim makes a window the owner's when the owner is the one acting on it (rule 4).
func (d *Desk) claim(i int, by module.Actor) {
	if !by.Delegate() {
		d.st.Wins[i].Kept = true
	}
}

// background says whether an open should leave the focus where it is: when asked, or when a
// delegate would otherwise take it from the owner's unsaved work (rule 2).
// surface.cards opened in the foreground (an agent raising questions for the owner) takes the
// focus even from the owner's unsaved work, so its questions are not hidden behind it, but
// never while the owner is typing: then it opens behind, and the chat that asked says so.
//
// A named agent's window (a seat: over MCP, or one of the app's own) always opens behind: the
// owner may be reading or answering where they are, and an outside agent cannot see that. It
// shows in the tab bar and on the attention badge ("not seen yet") until the owner looks. The
// HTTP tools and the CLI, which name no agent, keep the delegate rules.
func (d *Desk) background(a Action) bool {
	typing := a.Actor.Delegate() && d.ownerTyping()
	if a.View == "surface.cards" && !a.Background {
		return typing
	}
	if a.Actor.Delegate() && a.Agent != "" {
		return true
	}
	return a.Background || (a.Actor.Delegate() && d.ownerBusy())
}

// ownerTyping reports unsent input where the owner is: in the agent panel or the window in front.
func (d *Desk) ownerTyping() bool {
	if d.st.PanelComposing {
		return true
	}
	i := d.find(d.st.Focus)
	return i >= 0 && d.st.Wins[i].Composing
}

// ownerBusy reports that the owner is working where the focus is: unsaved work or unsent
// input in the window in front, or unsent input in the agent panel.
func (d *Desk) ownerBusy() bool {
	if d.st.PanelComposing {
		return true
	}
	i := d.find(d.st.Focus)
	return i >= 0 && d.busy(i)
}

// busy reports that the owner has unsaved work or unsent input in a window.
func (d *Desk) busy(i int) bool { return d.st.Wins[i].Dirty || d.st.Wins[i].Composing }

// hold keeps a delegate's change to a window until the owner is done with it. A newer one
// replaces an older one: what applies is the latest the delegate asked for.
func (d *Desk) hold(i int, a Action) {
	d.st.Wins[i].Held = &Held{Args: a.Args, Title: a.Title, By: string(a.Actor), At: time.Now().UTC().Format(time.RFC3339)}
	d.held = true
}

// applyHeld applies a held change once the owner is no longer working in the window.
func (d *Desk) applyHeld(i int) {
	w := &d.st.Wins[i]
	if w.Held == nil || w.Dirty || w.Composing {
		return
	}
	w.Args = w.Held.Args
	if w.Held.Title != "" {
		w.Title = w.Held.Title
	}
	w.Held = nil
}

// ownersWindow is the owner's window in front, or the newest one they had there: what a new
// window was opened from, as far as the owner is concerned.
func (d *Desk) ownersWindow() string {
	for j := len(d.st.Owned) - 1; j >= 0; j-- {
		if d.find(d.st.Owned[j]) >= 0 {
			return d.st.Owned[j]
		}
	}
	return d.st.Focus
}

// returnTo picks where the focus goes when the window in front closes (rule 3): the newest
// window the owner had in front, then the closed window's opener, then the trail, then the
// newest window. The trail alone would follow wherever the agent last looked.
func (d *Desk) returnTo(closed Win) string {
	st := &d.st
	for j := len(st.Owned) - 1; j >= 0; j-- {
		if id := st.Owned[j]; id != closed.ID && d.find(id) >= 0 {
			return id
		}
	}
	if closed.Opener != "" && d.find(closed.Opener) >= 0 {
		return closed.Opener
	}
	for len(st.Trail) > 0 {
		last := st.Trail[len(st.Trail)-1]
		st.Trail = st.Trail[:len(st.Trail)-1]
		if d.find(last) >= 0 {
			return last
		}
	}
	if len(st.Wins) > 0 {
		return st.Wins[len(st.Wins)-1].ID
	}
	return ""
}

func (d *Desk) dropFromTrail(id string) {
	d.st.Trail = without(d.st.Trail, id)
	d.st.Owned = without(d.st.Owned, id)
}

// without drops id from a list, in place.
func without(list []string, id string) []string {
	out := list[:0]
	for _, t := range list {
		if t != id {
			out = append(out, t)
		}
	}
	return out
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
