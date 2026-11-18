package desk

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/hollis-labs/surface/module"
)

// The methods below make *Desk a module.Desk.

func (w Win) public(focus string) module.Window {
	return module.Window{ID: w.ID, View: w.View, Module: w.Module, Title: w.Title, Key: w.Key, Args: w.Args, State: w.State, Focused: w.ID == focus}
}

// Open opens a window of a view, or brings the matching one forward.
func (d *Desk) Open(ctx context.Context, view string, opts module.OpenOptions) (module.Window, error) {
	w, err := d.Apply(ctx, Action{Type: "open", View: view, Key: opts.Key, Title: opts.Title, Args: opts.Args, Background: opts.Background})
	if err != nil {
		return module.Window{}, err
	}
	return w.public(d.Snapshot().Focus), nil
}

// Close closes a window. A window holding unsaved work is not closed (ErrDirty).
func (d *Desk) Close(ctx context.Context, window string) error {
	_, err := d.Apply(ctx, Action{Type: "close", ID: window})
	return err
}

// Focus brings a window to the front.
func (d *Desk) Focus(ctx context.Context, window string) error {
	_, err := d.Apply(ctx, Action{Type: "focus", ID: window})
	return err
}

// SetTitle renames a window.
func (d *Desk) SetTitle(ctx context.Context, window, title string) error {
	_, err := d.Apply(ctx, Action{Type: "title", ID: window, Title: title})
	return err
}

// SetArgs replaces what a window was opened with.
func (d *Desk) SetArgs(ctx context.Context, window string, args map[string]any) error {
	_, err := d.Apply(ctx, Action{Type: "args", ID: window, Args: args})
	return err
}

// Windows lists the open windows in tile order.
func (d *Desk) Windows(context.Context) []module.Window {
	s := d.Snapshot()
	out := make([]module.Window, 0, len(s.Wins))
	for _, w := range s.Wins {
		out = append(out, w.public(s.Focus))
	}
	return out
}

// Focused is the window in front.
func (d *Desk) Focused(context.Context) (module.Window, bool) {
	s := d.Snapshot()
	for _, w := range s.Wins {
		if w.ID == s.Focus {
			return w.public(s.Focus), true
		}
	}
	return module.Window{}, false
}

// Context is what a window is showing: what the desk knows, what the page last reported
// (Window.State keys kind, ref, url, selection) and what the window's module adds.
func (d *Desk) Context(ctx context.Context, window string) (module.WindowContext, bool) {
	s := d.Snapshot()
	if window == "" {
		window = s.Focus
	}
	for _, w := range s.Wins {
		if w.ID != window {
			continue
		}
		pub := w.public(s.Focus)
		wc := module.WindowContext{Window: w.ID, View: w.View, Module: w.Module, Title: w.Title}
		str := func(m map[string]any, k string) string { v, _ := m[k].(string); return v }
		wc.Kind, wc.Ref, wc.URL = str(w.State, "kind"), str(w.State, "ref"), str(w.State, "url")
		wc.Selection = clip(str(w.State, "selection"), 4000)
		if d.describe != nil {
			if more, ok := d.describe(ctx, pub); ok {
				wc.Kind = firstNonEmpty(more.Kind, wc.Kind)
				wc.Ref = firstNonEmpty(more.Ref, wc.Ref)
				wc.URL = firstNonEmpty(more.URL, wc.URL)
				wc.Selection = firstNonEmpty(more.Selection, wc.Selection)
				wc.Extra = more.Extra
			}
		}
		return wc, true
	}
	return module.WindowContext{}, false
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Capture returns the present arrangement as a Layout.
func (d *Desk) Capture(context.Context) module.Layout {
	s := d.Snapshot()
	l := module.Layout{Name: s.Active, Mode: s.Layout, Ratio: s.Ratio, Windows: []module.LayoutWindow{}}
	// The setup the desk was arranged with travels with it, so saving the desk as it is
	// keeps the instructions and reflexes it is running under. The mode is the present one.
	if len(s.Setup) > 0 || s.Mode != "" {
		l.Setup = map[string]any{}
		for k, v := range s.Setup {
			l.Setup[k] = v
		}
		delete(l.Setup, "mode")
		if s.Mode != "" {
			l.Setup["mode"] = s.Mode
		}
	}
	for _, w := range s.Wins {
		if w.Minimized {
			continue
		}
		lw := module.LayoutWindow{View: w.View, Key: w.Key, Title: w.Title, Args: w.Args, Floating: w.Floating, Focus: w.ID == s.Focus}
		if w.Floating || s.Layout == "float" {
			lw.Rect = &module.Rect{X: w.X, Y: w.Y, W: w.W, H: w.H}
		}
		l.Windows = append(l.Windows, lw)
	}
	return l
}

// Arrange replaces the open windows with a layout's. Windows holding unsaved work stay.
func (d *Desk) Arrange(ctx context.Context, l module.Layout) error {
	for _, lw := range l.Windows {
		if _, ok := d.views[lw.View]; !ok {
			return fmt.Errorf("desk: layout %q names a view this app does not have: %q", l.Name, lw.View)
		}
	}
	d.mu.Lock()
	kept := d.st.Wins[:0]
	for _, w := range d.st.Wins {
		if w.Dirty {
			kept = append(kept, w)
		}
	}
	d.st.Wins = append([]Win{}, kept...)
	d.st.Trail = nil
	d.st.Focus = ""
	if l.Mode == "tile" || l.Mode == "float" {
		d.st.Layout = l.Mode
	}
	if l.Ratio >= 0.2 && l.Ratio <= 0.8 {
		d.st.Ratio = l.Ratio
	}
	d.st.Active = l.Name
	d.st.Setup = l.Setup
	if m, ok := l.Setup["mode"].(string); ok {
		d.st.Mode = m
	} else {
		d.st.Mode = ""
	}
	focus := ""
	var firstErr error
	for _, lw := range l.Windows {
		w, _, _, err := d.reduce(Action{Type: "open", View: lw.View, Key: lw.Key, Title: lw.Title, Args: lw.Args, Rect: lw.Rect, Background: true})
		if err != nil {
			firstErr = err
			continue
		}
		if i := d.find(w.ID); i >= 0 {
			d.st.Wins[i].Floating = lw.Floating
		}
		if lw.Focus || focus == "" {
			focus = w.ID
		}
	}
	if focus != "" {
		d.st.Trail = nil
		d.st.Focus = focus
	}
	d.st.Rev++
	snap := d.copyState()
	d.mu.Unlock()
	d.persist(ctx, snap)
	if d.pub != nil {
		d.pub.Publish("desk.changed", snap)
		d.pub.Publish("desk.arranged", map[string]any{"layout": l.Name, "setup": l.Setup})
		d.pub.Publish("desk.focus", map[string]any{"window": snap.Focus})
	}
	return firstErr
}

var layoutNameRe = regexp.MustCompile(`^[^\s/][^/]{0,79}$`)

// SaveLayout keeps a layout under its name, replacing one of the same name.
func (d *Desk) SaveLayout(ctx context.Context, l module.Layout) error {
	if !layoutNameRe.MatchString(l.Name) {
		return errors.New("desk: a layout needs a name (up to 80 characters, no slash)")
	}
	if l.Windows == nil {
		l.Windows = []module.LayoutWindow{}
	}
	raw, err := json.Marshal(l)
	if err != nil {
		return err
	}
	if _, err = d.db.ExecContext(ctx, `INSERT INTO layouts (name, body, updated_at) VALUES (?, ?, datetime('now'))
		ON CONFLICT(name) DO UPDATE SET body=excluded.body, updated_at=excluded.updated_at`, l.Name, string(raw)); err != nil {
		return err
	}
	if d.pub != nil {
		d.pub.Publish("desk.layouts", map[string]any{"saved": l.Name})
	}
	return nil
}

// Layouts lists the saved layouts by name.
func (d *Desk) Layouts(ctx context.Context) ([]module.Layout, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT body FROM layouts ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []module.Layout{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var l module.Layout
		if json.Unmarshal([]byte(raw), &l) == nil {
			out = append(out, l)
		}
	}
	return out, rows.Err()
}

// Layout returns one saved layout.
func (d *Desk) Layout(ctx context.Context, name string) (module.Layout, bool, error) {
	var raw string
	err := d.db.QueryRowContext(ctx, `SELECT body FROM layouts WHERE name=?`, name).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return module.Layout{}, false, nil
	}
	if err != nil {
		return module.Layout{}, false, err
	}
	var l module.Layout
	if err := json.Unmarshal([]byte(raw), &l); err != nil {
		return module.Layout{}, false, err
	}
	return l, true, nil
}

// DeleteLayout removes a saved layout.
func (d *Desk) DeleteLayout(ctx context.Context, name string) error {
	_, err := d.db.ExecContext(ctx, `DELETE FROM layouts WHERE name=?`, name)
	if err == nil && d.pub != nil {
		d.pub.Publish("desk.layouts", map[string]any{"deleted": name})
	}
	return err
}

var _ module.Desk = (*Desk)(nil)
