package moduletest

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/hollis-labs/surface/module"
)

// Desk is an in-memory desk: it keeps windows, the focus and saved layouts.
type Desk struct {
	mu      sync.Mutex
	wins    []module.Window
	n       int
	layouts map[string]module.Layout
	// Mode is the last mode a layout's setup set.
	Mode string
	// Contexts, when set for a window id, is what Context returns for it.
	Contexts map[string]module.WindowContext
}

func newDesk() *Desk {
	return &Desk{layouts: map[string]module.Layout{}, Contexts: map[string]module.WindowContext{}}
}

func (d *Desk) focusLocked(id string) {
	for i := range d.wins {
		d.wins[i].Focused = d.wins[i].ID == id
	}
}

func (d *Desk) Open(_ context.Context, view string, o module.OpenOptions) (module.Window, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if o.Key != "" {
		for _, w := range d.wins {
			if w.View == view && w.Key == o.Key {
				if !o.Background {
					d.focusLocked(w.ID)
					w.Focused = true
				}
				return w, nil
			}
		}
	}
	d.n++
	w := module.Window{ID: fmt.Sprintf("%s-%d", view, d.n), View: view, Module: view, Title: o.Title, Key: o.Key, Args: o.Args}
	if w.Title == "" {
		w.Title = view
	}
	if w.Args == nil {
		w.Args = map[string]any{}
	}
	d.wins = append(d.wins, w)
	if !o.Background {
		d.focusLocked(w.ID)
		w.Focused = true
	}
	return w, nil
}

func (d *Desk) find(id string) (int, error) {
	for i, w := range d.wins {
		if w.ID == id {
			return i, nil
		}
	}
	return -1, fmt.Errorf("no window %q", id)
}

func (d *Desk) Close(_ context.Context, id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	i, err := d.find(id)
	if err == nil {
		d.wins = slices.Delete(d.wins, i, i+1)
	}
	return err
}

func (d *Desk) Focus(_ context.Context, id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, err := d.find(id); err != nil {
		return err
	}
	d.focusLocked(id)
	return nil
}

func (d *Desk) SetTitle(_ context.Context, id, title string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	i, err := d.find(id)
	if err == nil {
		d.wins[i].Title = title
	}
	return err
}

func (d *Desk) SetArgs(_ context.Context, id string, args map[string]any) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	i, err := d.find(id)
	if err == nil {
		d.wins[i].Args = args
	}
	return err
}

// SetState is the page reporting a window's state (desk.report).
func (d *Desk) SetState(id string, state map[string]any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if i, err := d.find(id); err == nil {
		d.wins[i].State = state
	}
}

func (d *Desk) Windows(context.Context) []module.Window {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.wins)
}

func (d *Desk) Focused(context.Context) (module.Window, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, w := range d.wins {
		if w.Focused {
			return w, true
		}
	}
	return module.Window{}, false
}

func (d *Desk) Context(ctx context.Context, id string) (module.WindowContext, bool) {
	w, ok := d.Focused(ctx)
	if id != "" {
		ok = false
		for _, x := range d.Windows(ctx) {
			if x.ID == id {
				w, ok = x, true
			}
		}
	}
	if !ok {
		return module.WindowContext{}, false
	}
	if c, ok := d.Contexts[w.ID]; ok {
		return c, true
	}
	c := module.WindowContext{Window: w.ID, View: w.View, Module: w.Module, Title: w.Title}
	c.Selection, _ = w.State["selection"].(string)
	return c, true
}

func (d *Desk) Arrange(ctx context.Context, l module.Layout) error {
	d.mu.Lock()
	d.wins = nil
	if m, ok := l.Setup["mode"].(string); ok {
		d.Mode = m
	}
	d.mu.Unlock()
	for _, lw := range l.Windows {
		if _, err := d.Open(ctx, lw.View, module.OpenOptions{Key: lw.Key, Title: lw.Title, Args: lw.Args, Background: !lw.Focus}); err != nil {
			return err
		}
	}
	return nil
}

func (d *Desk) Capture(ctx context.Context) module.Layout {
	l := module.Layout{Mode: "tile"}
	for _, w := range d.Windows(ctx) {
		l.Windows = append(l.Windows, module.LayoutWindow{View: w.View, Key: w.Key, Title: w.Title, Args: w.Args, Focus: w.Focused})
	}
	return l
}

func (d *Desk) SaveLayout(_ context.Context, l module.Layout) error {
	if l.Name == "" {
		return fmt.Errorf("a layout needs a name")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.layouts[l.Name] = l
	return nil
}

func (d *Desk) Layouts(context.Context) ([]module.Layout, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]module.Layout, 0, len(d.layouts))
	for _, l := range d.layouts {
		out = append(out, l)
	}
	slices.SortFunc(out, func(a, b module.Layout) int {
		if a.Name < b.Name {
			return -1
		}
		return 1
	})
	return out, nil
}

func (d *Desk) Layout(_ context.Context, name string) (module.Layout, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	l, ok := d.layouts[name]
	return l, ok, nil
}

func (d *Desk) DeleteLayout(_ context.Context, name string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.layouts, name)
	return nil
}
