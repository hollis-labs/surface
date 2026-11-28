package desk

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/hollis-labs/surface/core/internal/storage"
	"github.com/hollis-labs/surface/module"
)

func views() []ViewInfo {
	return []ViewInfo{
		{View: module.View{ID: "notes", Title: "Notes"}, Module: "notes"},
		{View: module.View{ID: "scratch", Title: "Scratch"}, Module: "scratch"},
		{View: module.View{ID: "surface.cards", Title: "Cards"}, Module: "surface"},
		{View: module.View{ID: "browser", Title: "Browser", Multi: true, DefaultKey: "main"}, Module: "browser"},
	}
}

type recorder struct{ topics []string }

func (r *recorder) Publish(topic string, _ any) { r.topics = append(r.topics, topic) }

func open(t *testing.T, path string) (*Desk, *recorder) {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = storage.MigrateCore(ctx, db); err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	d, err := Open(ctx, db, rec, views())
	if err != nil {
		t.Fatal(err)
	}
	return d, rec
}

func TestOpenBringsTheSameWindowForward(t *testing.T) {
	ctx := context.Background()
	d, _ := open(t, filepath.Join(t.TempDir(), "s.db"))
	a, err := d.Open(ctx, "notes", module.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Open(ctx, "scratch", module.OpenOptions{}); err != nil {
		t.Fatal(err)
	}
	again, err := d.Open(ctx, "notes", module.OpenOptions{Args: map[string]any{"path": "x.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != a.ID {
		t.Errorf("a second open of a single-window view made %s, want %s brought forward", again.ID, a.ID)
	}
	if f, _ := d.Focused(ctx); f.ID != a.ID || f.Args["path"] != "x.md" {
		t.Errorf("focused %+v, want %s with the new args", f, a.ID)
	}
	if n := len(d.Windows(ctx)); n != 2 {
		t.Errorf("%d windows, want 2", n)
	}
	if _, err := d.Open(ctx, "nope", module.OpenOptions{}); err == nil {
		t.Error("opening an unknown view was accepted")
	}
}

func TestMultiViewsAreToldApartByKey(t *testing.T) {
	ctx := context.Background()
	d, _ := open(t, filepath.Join(t.TempDir(), "s.db"))
	main1, _ := d.Open(ctx, "browser", module.OpenOptions{})
	main2, _ := d.Open(ctx, "browser", module.OpenOptions{})
	other, _ := d.Open(ctx, "browser", module.OpenOptions{Key: "docs"})
	if main1.ID != main2.ID {
		t.Error("a plain open of a multi view should bring its default window forward")
	}
	if other.ID == main1.ID {
		t.Error("a different key should make a second window")
	}
}

func TestCloseReturnsToTheWindowBefore(t *testing.T) {
	ctx := context.Background()
	d, _ := open(t, filepath.Join(t.TempDir(), "s.db"))
	a, _ := d.Open(ctx, "notes", module.OpenOptions{})
	b, _ := d.Open(ctx, "scratch", module.OpenOptions{})
	c, _ := d.Open(ctx, "browser", module.OpenOptions{})
	if err := d.Focus(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	// a is in front, and before it was c. Closing a returns to c, not to the last opened.
	if err := d.Close(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if f, _ := d.Focused(ctx); f.ID != c.ID {
		t.Errorf("after closing, %s is in front; want %s", f.ID, c.ID)
	}
	_ = b
}

func TestADirtyWindowIsNotClosedWithoutForce(t *testing.T) {
	ctx := context.Background()
	d, _ := open(t, filepath.Join(t.TempDir(), "s.db"))
	a, _ := d.Open(ctx, "notes", module.OpenOptions{})
	if _, err := d.Apply(ctx, Action{Type: "dirty", ID: a.ID, Dirty: true}); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(ctx, a.ID); !errors.Is(err, ErrDirty) {
		t.Fatalf("closing a dirty window: %v, want ErrDirty", err)
	}
	if _, err := d.Apply(ctx, Action{Type: "close", ID: a.ID, Force: true}); err != nil {
		t.Fatal(err)
	}
	if n := len(d.Windows(ctx)); n != 0 {
		t.Errorf("%d windows after a forced close, want 0", n)
	}
}

func TestStateSurvivesARestartAndDropsUnknownViews(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "s.db")
	d, _ := open(t, path)
	_, _ = d.Open(ctx, "notes", module.OpenOptions{Args: map[string]any{"path": "a.md"}})
	_, _ = d.Open(ctx, "scratch", module.OpenOptions{})
	_, _ = d.Apply(ctx, Action{Type: "layout", Layout: "float"})

	db, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// The next run of the app has no scratch module.
	d2, err := Open(ctx, db, nil, views()[:1])
	if err != nil {
		t.Fatal(err)
	}
	wins := d2.Windows(ctx)
	if len(wins) != 1 || wins[0].View != "notes" || wins[0].Args["path"] != "a.md" {
		t.Errorf("restored %+v, want the notes window with its args", wins)
	}
	if got := d2.Snapshot().Layout; got != "float" {
		t.Errorf("restored layout %q, want float", got)
	}
}

func TestLayoutsCaptureArrangeAndKeepTheirSetup(t *testing.T) {
	ctx := context.Background()
	d, rec := open(t, filepath.Join(t.TempDir(), "s.db"))
	_, _ = d.Open(ctx, "notes", module.OpenOptions{Args: map[string]any{"path": "article.md"}})
	_, _ = d.Open(ctx, "browser", module.OpenOptions{Key: "research"})
	_, _ = d.Apply(ctx, Action{Type: "ratio", Ratio: 0.6})

	l := d.Capture(ctx)
	l.Name = "write an article"
	l.Setup = map[string]any{"mode": "writing", "reflexes": []any{"cite-sources"}}
	if err := d.SaveLayout(ctx, l); err != nil {
		t.Fatal(err)
	}
	if err := d.SaveLayout(ctx, module.Layout{Name: "bad/name"}); err == nil {
		t.Error("a layout name with a slash was accepted")
	}

	// Something else entirely, with unsaved work in one window.
	for _, w := range d.Windows(ctx) {
		_ = d.Close(ctx, w.ID)
	}
	s, _ := d.Open(ctx, "scratch", module.OpenOptions{})
	_, _ = d.Apply(ctx, Action{Type: "dirty", ID: s.ID, Dirty: true})

	got, ok, err := d.Layout(ctx, "write an article")
	if err != nil || !ok {
		t.Fatalf("the saved layout is gone: %v", err)
	}
	if err := d.Arrange(ctx, got); err != nil {
		t.Fatal(err)
	}
	st := d.Snapshot()
	if st.Active != "write an article" || st.Mode != "writing" || st.Ratio != 0.6 {
		t.Errorf("after arranging: active %q mode %q ratio %v", st.Active, st.Mode, st.Ratio)
	}
	// The setup stays with the desk, so capturing it again (to save a refinement) keeps it.
	if again := d.Capture(ctx); again.Setup["mode"] != "writing" || again.Setup["reflexes"] == nil {
		t.Errorf("capture after arranging lost the setup: %+v", again.Setup)
	}
	var have []string
	for _, w := range st.Wins {
		have = append(have, w.View+":"+w.Key)
	}
	// The dirty scratch window stays; the layout's two come back with their keys and args.
	if len(have) != 3 || have[0] != "scratch:" || have[1] != "notes:" || have[2] != "browser:research" {
		t.Errorf("windows after arranging: %v", have)
	}
	if st.Wins[1].Args["path"] != "article.md" {
		t.Errorf("the notes window lost its args: %+v", st.Wins[1].Args)
	}
	arranged := false
	for _, topic := range rec.topics {
		arranged = arranged || topic == "desk.arranged"
	}
	if !arranged {
		t.Error("arranging did not announce desk.arranged")
	}
	if err := d.Arrange(ctx, module.Layout{Name: "x", Windows: []module.LayoutWindow{{View: "gone"}}}); err == nil {
		t.Error("a layout naming a view the app lacks was accepted")
	}
}

func TestContextMergesThePagesReportWithTheModules(t *testing.T) {
	ctx := context.Background()
	d, _ := open(t, filepath.Join(t.TempDir(), "s.db"))
	w, _ := d.Open(ctx, "notes", module.OpenOptions{})
	_, _ = d.Apply(ctx, Action{Type: "state", ID: w.ID, State: map[string]any{"kind": "document", "ref": "notes:a.md", "selection": "these words"}})
	d.SetDescriber(func(_ context.Context, w module.Window) (module.WindowContext, bool) {
		return module.WindowContext{Extra: map[string]any{"words": 12}}, true
	})
	wc, ok := d.Context(ctx, "")
	if !ok || wc.Ref != "notes:a.md" || wc.Selection != "these words" || wc.Extra["words"] != 12 || wc.View != "notes" {
		t.Errorf("context: %+v", wc)
	}
}

// shape draws a tree as text: rows in brackets, stacks as their windows' views joined by
// "+", the showing one starred when the stack has more than one.
func shape(d *Desk) string {
	s := d.Snapshot()
	view := map[string]string{}
	for _, w := range s.Wins {
		view[w.ID] = w.View
	}
	var draw func(n *Node) string
	draw = func(n *Node) string {
		if n.Type == "stack" {
			out := ""
			for i, w := range n.Wins {
				if i > 0 {
					out += "+"
				}
				out += view[w]
				if len(n.Wins) > 1 && i == n.Selected {
					out += "*"
				}
			}
			if n.Maximized {
				out += "!"
			}
			return out
		}
		out := "["
		for i, c := range n.Children {
			if i > 0 {
				out += " "
			}
			out += draw(c)
		}
		return out + "]"
	}
	out := draw(s.Tree.Root)
	for _, f := range s.Tree.Floats {
		out += " ~" + draw(f.Root)
	}
	return out
}

func TestWindowsTileFirstOnTheLeftTheRestStackedOnTheRight(t *testing.T) {
	ctx := context.Background()
	d, _ := open(t, filepath.Join(t.TempDir(), "s.db"))
	n, _ := d.Open(ctx, "notes", module.OpenOptions{})
	if got := shape(d); got != "[notes]" {
		t.Fatalf("one window: %s", got)
	}
	s, _ := d.Open(ctx, "scratch", module.OpenOptions{})
	b, _ := d.Open(ctx, "browser", module.OpenOptions{Key: "a"})
	if got := shape(d); got != "[notes [scratch browser]]" {
		t.Fatalf("three windows: %s", got)
	}
	root := d.Snapshot().Tree.Root
	if root.Children[0].Weight != 56 || root.Children[1].Weight != 44 {
		t.Errorf("weights %v and %v, want the ratio's 56 and 44", root.Children[0].Weight, root.Children[1].Weight)
	}

	// Floating takes a window out of the tiling; floating it again docks it back.
	_, _ = d.Apply(ctx, Action{Type: "float", ID: s.ID})
	if got := shape(d); got != "[notes browser] ~[scratch]" {
		t.Errorf("after floating scratch: %s", got)
	}
	_, _ = d.Apply(ctx, Action{Type: "rect", ID: s.ID, Rect: &module.Rect{X: 10, Y: 20, W: 100, H: 100}})
	if r := d.Snapshot().Tree.Floats[0].Rect; r.X != 10 || r.W != minFloatW || r.H != minFloatH {
		t.Errorf("the float's rectangle is %+v; want it moved and no smaller than the minimum", r)
	}
	_, _ = d.Apply(ctx, Action{Type: "float", ID: s.ID})
	if got := shape(d); got != "[notes [browser scratch]]" {
		t.Errorf("after docking scratch: %s", got)
	}

	// Maximizing covers the tiling; focusing a window it hides lifts it.
	_, _ = d.Apply(ctx, Action{Type: "maximize", ID: n.ID})
	if got := shape(d); got != "[notes! [browser scratch]]" {
		t.Errorf("after maximizing notes: %s", got)
	}
	_ = d.Focus(ctx, b.ID)
	if got := shape(d); got != "[notes [browser scratch]]" {
		t.Errorf("after focusing the browser behind it: %s", got)
	}

	_, _ = d.Apply(ctx, Action{Type: "tab", ID: s.ID, Other: n.ID})
	if got := shape(d); got != "[notes+scratch* browser]" {
		t.Errorf("after tabbing scratch with notes: %s", got)
	}
	_, _ = d.Apply(ctx, Action{Type: "float", ID: s.ID})
	_, _ = d.Apply(ctx, Action{Type: "float", ID: s.ID})
	_, _ = d.Apply(ctx, Action{Type: "promote", ID: b.ID})
	if got := shape(d); got != "[browser [notes scratch]]" {
		t.Errorf("after giving the browser the first place: %s", got)
	}
	_ = d.Close(ctx, n.ID)
	_ = d.Close(ctx, s.ID)
	if got := shape(d); got != "[browser]" {
		t.Errorf("after closing two: %s", got)
	}

	// Floating the whole desk, then tiling it again.
	_, _ = d.Open(ctx, "notes", module.OpenOptions{})
	_, _ = d.Apply(ctx, Action{Type: "layout", Layout: "float"})
	if got := shape(d); got != "[] ~[browser] ~[notes]" {
		t.Errorf("after floating the desk: %s", got)
	}
	_, _ = d.Open(ctx, "scratch", module.OpenOptions{})
	if got := shape(d); got != "[] ~[browser] ~[notes] ~[scratch]" {
		t.Errorf("a window opened on a floating desk: %s", got)
	}
	_, _ = d.Apply(ctx, Action{Type: "layout", Layout: "tile"})
	if got := shape(d); got != "[browser [notes scratch]]" {
		t.Errorf("after tiling the desk: %s", got)
	}
}

func TestThePagesArrangementIsKeptButNeverTrusted(t *testing.T) {
	ctx := context.Background()
	d, _ := open(t, filepath.Join(t.TempDir(), "s.db"))
	n, _ := d.Open(ctx, "notes", module.OpenOptions{})
	s, _ := d.Open(ctx, "scratch", module.OpenOptions{})
	b, _ := d.Open(ctx, "browser", module.OpenOptions{Key: "a"})

	// The owner drags scratch into a tab beside notes and floats the browser. The page's
	// tree also names a window that does not exist, names notes twice, and leaves a stack
	// without an id.
	tree := &Tree{
		Root: &Node{Type: "row", Children: []*Node{
			{Type: "stack", Wins: []string{n.ID, s.ID, "ghost-1", n.ID}, Selected: 1},
		}},
		Floats: []*Float{{Rect: module.Rect{X: 30, Y: 40, W: 500, H: 400}, Root: &Node{Type: "row", Children: []*Node{
			{Type: "stack", ID: "x", Wins: []string{b.ID}},
		}}}},
	}
	if _, err := d.Apply(ctx, Action{Type: "tree", Tree: tree, Focus: s.ID}); err != nil {
		t.Fatal(err)
	}
	if got := shape(d); got != "[notes+scratch*] ~[browser]" {
		t.Errorf("after the page's arrangement: %s", got)
	}
	st := d.Snapshot()
	if st.Focus != s.ID {
		t.Errorf("focus is %q, want scratch as the page said", st.Focus)
	}
	if id := st.Tree.Root.Children[0].ID; id == "" {
		t.Error("the stack the page sent without an id was not given one")
	}
	if got := d.Windows(ctx); len(got) != 3 || got[0].View != "notes" || got[2].View != "browser" {
		t.Errorf("windows in desk order: %+v", got)
	}

	// A tree that forgets a window does not lose it: it is tiled back in.
	if _, err := d.Apply(ctx, Action{Type: "tree", Tree: &Tree{Root: &Node{Type: "row", Children: []*Node{
		{Type: "stack", ID: "only", Wins: []string{n.ID}},
	}}}}); err != nil {
		t.Fatal(err)
	}
	if got := shape(d); got != "[notes [scratch browser]]" {
		t.Errorf("after a tree that forgot two windows: %s", got)
	}

	// Focusing a window behind another in a stack shows it.
	_, _ = d.Apply(ctx, Action{Type: "tree", Tree: &Tree{Root: &Node{Type: "row", Children: []*Node{
		{Type: "stack", ID: "only", Wins: []string{n.ID, s.ID, b.ID}},
	}}}})
	_ = d.Focus(ctx, b.ID)
	if got := shape(d); got != "[notes+scratch+browser*]" {
		t.Errorf("after focusing the tab behind: %s", got)
	}
}

func TestACapturedLayoutComesBackAsItWas(t *testing.T) {
	ctx := context.Background()
	d, _ := open(t, filepath.Join(t.TempDir(), "s.db"))
	n, _ := d.Open(ctx, "notes", module.OpenOptions{})
	s, _ := d.Open(ctx, "scratch", module.OpenOptions{})
	b, _ := d.Open(ctx, "browser", module.OpenOptions{Key: "a"})
	_, _ = d.Apply(ctx, Action{Type: "tree", Tree: &Tree{
		Root: &Node{Type: "row", Children: []*Node{
			{Type: "stack", ID: "l", Weight: 30, Wins: []string{s.ID, n.ID}, Selected: 1},
		}},
		Floats: []*Float{{ID: "f", Rect: module.Rect{X: 30, Y: 40, W: 500, H: 400}, Root: &Node{Type: "row", Children: []*Node{
			{Type: "stack", ID: "x", Wins: []string{b.ID}},
		}}}},
	}})
	l := d.Capture(ctx)
	l.Name = "mine"
	if l.Mode != "tile" || len(l.Windows) != 3 || !l.Windows[2].Floating || l.Windows[2].Rect == nil || len(l.Tree) == 0 {
		t.Fatalf("captured %+v", l)
	}
	if err := d.SaveLayout(ctx, l); err != nil {
		t.Fatal(err)
	}
	for _, w := range d.Windows(ctx) {
		_ = d.Close(ctx, w.ID)
	}
	_, _ = d.Open(ctx, "scratch", module.OpenOptions{})
	saved, _, _ := d.Layout(ctx, "mine")
	if err := d.Arrange(ctx, saved); err != nil {
		t.Fatal(err)
	}
	if got := shape(d); got != "[scratch+notes*] ~[browser]" {
		t.Errorf("arranged from the saved layout: %s", got)
	}
	if r := d.Snapshot().Tree.Floats[0].Rect; r != (module.Rect{X: 30, Y: 40, W: 500, H: 400}) {
		t.Errorf("the float came back at %+v", r)
	}

	// A layout a module composes has no tree: it is arranged by its mode.
	if err := d.Arrange(ctx, module.Layout{Name: "composed", Mode: "tile", Ratio: 0.7, Windows: []module.LayoutWindow{
		{View: "browser", Key: "a"}, {View: "notes"}, {View: "scratch", Floating: true},
	}}); err != nil {
		t.Fatal(err)
	}
	if got := shape(d); got != "[browser notes] ~[scratch]" {
		t.Errorf("arranged from a composed layout: %s", got)
	}
	if w := d.Snapshot().Tree.Root.Children[0].Weight; w != 70 {
		t.Errorf("the first window's weight is %v, want the layout's ratio of 70", w)
	}
}

// as is a context in which actor a is acting.
func as(a module.Actor) context.Context { return module.WithActor(context.Background(), a) }

func mustOpen(ctx context.Context, t *testing.T, d *Desk, view string, opts module.OpenOptions) module.Window {
	t.Helper()
	w, err := d.Open(ctx, view, opts)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestAWindowRecordsWhoOpenedItAndFromWhere(t *testing.T) {
	d, _ := open(t, filepath.Join(t.TempDir(), "s.db"))
	mine := mustOpen(as(module.ActorOwner), t, d, "notes", module.OpenOptions{})
	theirs := mustOpen(as(module.ActorAgent), t, d, "browser", module.OpenOptions{Reason: "the docs page", Retention: "transient"})
	if !mine.Kept || mine.OpenedBy != "owner" {
		t.Errorf("the owner's window: kept %v, opened by %q", mine.Kept, mine.OpenedBy)
	}
	if theirs.Kept || theirs.OpenedBy != "agent" || theirs.Opener != mine.ID || theirs.Reason != "the docs page" || theirs.Retention != "transient" {
		t.Errorf("the agent's window: %+v", theirs)
	}
	// Retention is the delegate's word; the owner's windows have none.
	own := mustOpen(as(module.ActorOwner), t, d, "scratch", module.OpenOptions{Retention: "transient"})
	if own.Retention != "" {
		t.Errorf("an owner's window got retention %q", own.Retention)
	}
}

func TestClosingTheAgentsWindowGoesBackToTheOwnersNotWhereTheAgentLooked(t *testing.T) {
	d, _ := open(t, filepath.Join(t.TempDir(), "s.db"))
	agent := as(module.ActorAgent)
	notes := mustOpen(as(module.ActorOwner), t, d, "notes", module.OpenOptions{})
	scratch := mustOpen(agent, t, d, "scratch", module.OpenOptions{Background: true})
	page := mustOpen(agent, t, d, "browser", module.OpenOptions{})
	// The agent looks around: the plain trail now ends on its own windows.
	if err := d.Focus(agent, scratch.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.Focus(agent, page.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(as(module.ActorOwner), page.ID); err != nil {
		t.Fatal(err)
	}
	if f := d.Snapshot().Focus; f != notes.ID {
		t.Errorf("closing the agent's window put %s in front, want the owner's %s", f, notes.ID)
	}
}

func TestClosingGoesBackToWhereTheOwnerLastWas(t *testing.T) {
	d, _ := open(t, filepath.Join(t.TempDir(), "s.db"))
	owner := as(module.ActorOwner)
	notes := mustOpen(owner, t, d, "notes", module.OpenOptions{})
	page := mustOpen(as(module.ActorAgent), t, d, "browser", module.OpenOptions{})
	// The owner goes elsewhere, then comes back to the agent's window and closes it.
	scratch := mustOpen(owner, t, d, "scratch", module.OpenOptions{})
	if err := d.Focus(owner, page.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(owner, page.ID); err != nil {
		t.Fatal(err)
	}
	if f := d.Snapshot().Focus; f != scratch.ID {
		t.Errorf("focus went to %s, want %s, where the owner last was (not the opener %s)", f, scratch.ID, notes.ID)
	}
}

func TestADelegateDoesNotTakeTheFocusFromUnsavedWork(t *testing.T) {
	d, rec := open(t, filepath.Join(t.TempDir(), "s.db"))
	owner, agent := as(module.ActorOwner), as(module.ActorAgent)
	notes := mustOpen(owner, t, d, "notes", module.OpenOptions{})
	if _, err := d.Apply(owner, Action{Type: "dirty", ID: notes.ID, Dirty: true}); err != nil {
		t.Fatal(err)
	}
	page := mustOpen(agent, t, d, "browser", module.OpenOptions{})
	if f := d.Snapshot().Focus; f != notes.ID {
		t.Errorf("the agent's open took the focus from unsaved work: %s in front", f)
	}
	if page.Focused {
		t.Error("the agent's window says it is focused")
	}
	if err := d.Focus(agent, page.ID); !errors.Is(err, ErrOwnerBusy) {
		t.Errorf("the agent focusing away from unsaved work: %v, want ErrOwnerBusy", err)
	}
	// The owner may go where they like.
	if err := d.Focus(owner, page.ID); err != nil {
		t.Errorf("the owner focusing: %v", err)
	}
	if len(rec.topics) == 0 {
		t.Error("nothing was announced")
	}
}

func TestADelegateClosesOnlyItsOwnWindows(t *testing.T) {
	d, _ := open(t, filepath.Join(t.TempDir(), "s.db"))
	owner, agent := as(module.ActorOwner), as(module.ActorAgent)
	notes := mustOpen(owner, t, d, "notes", module.OpenOptions{})
	legacy := mustOpen(context.Background(), t, d, "scratch", module.OpenOptions{})
	page := mustOpen(agent, t, d, "browser", module.OpenOptions{Key: "a"})
	taken := mustOpen(agent, t, d, "browser", module.OpenOptions{Key: "b"})

	for _, w := range []module.Window{notes, legacy} {
		if err := d.Close(agent, w.ID); !errors.Is(err, ErrNotYours) {
			t.Errorf("the agent closing %s (opened by %q): %v, want ErrNotYours", w.ID, w.OpenedBy, err)
		}
		if err := d.Close(as(module.ActorOutside), w.ID); !errors.Is(err, ErrNotYours) {
			t.Errorf("an outside client closing %s: %v, want ErrNotYours", w.ID, err)
		}
	}
	// Unsaved work makes the window the owner's; so does keeping it.
	if _, err := d.Apply(owner, Action{Type: "dirty", ID: taken.ID, Dirty: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Apply(owner, Action{Type: "dirty", ID: taken.ID, Dirty: false}); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(agent, taken.ID); !errors.Is(err, ErrNotYours) {
		t.Errorf("the agent closing a window the owner worked in: %v, want ErrNotYours", err)
	}
	if _, err := d.Apply(agent, Action{Type: "keep", ID: page.ID}); err == nil {
		t.Error("the agent kept a window for the owner")
	}
	if err := d.Close(agent, page.ID); err != nil {
		t.Errorf("the agent closing its own window: %v", err)
	}
	// The owner closes anything.
	if err := d.Close(owner, notes.ID); err != nil {
		t.Errorf("the owner closing their window: %v", err)
	}
}

func TestTheActorIsNeverReadFromTheRequest(t *testing.T) {
	var a Action
	if err := json.Unmarshal([]byte(`{"type":"close","id":"x","actor":"owner","Actor":"owner"}`), &a); err != nil {
		t.Fatal(err)
	}
	if a.Actor != "" {
		t.Errorf("a request body set the actor to %q", a.Actor)
	}
}

func TestProvenanceSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.db")
	d, _ := open(t, path)
	notes := mustOpen(as(module.ActorOwner), t, d, "notes", module.OpenOptions{})
	page := mustOpen(as(module.ActorAgent), t, d, "browser", module.OpenOptions{Reason: "docs"})
	again, _ := open(t, path)
	var got Win
	for _, w := range again.Snapshot().Wins {
		if w.ID == page.ID {
			got = w
		}
	}
	if got.OpenedBy != "agent" || got.Opener != notes.ID || got.Reason != "docs" || got.Kept {
		t.Errorf("after a restart: %+v", got)
	}
	if err := again.Close(as(module.ActorOwner), page.ID); err != nil {
		t.Fatal(err)
	}
	if f := again.Snapshot().Focus; f != notes.ID {
		t.Errorf("after a restart, closing went to %s, want %s", f, notes.ID)
	}
}

func TestADelegatesWindowOpensBesideTheOwnersWindow(t *testing.T) {
	d, _ := open(t, filepath.Join(t.TempDir(), "s.db"))
	owner, agent := as(module.ActorOwner), as(module.ActorAgent)
	notes := mustOpen(owner, t, d, "notes", module.OpenOptions{})
	// Notes fills the desk, so it is wide: the agent's window goes to its right.
	page := mustOpen(agent, t, d, "browser", module.OpenOptions{Key: "a"})
	root := d.Snapshot().Tree.Root
	if len(root.Children) != 2 || root.Children[0].Wins[0] != notes.ID || root.Children[1].Wins[0] != page.ID {
		t.Fatalf("want notes | page across the desk, got %+v", root.Children)
	}
	// Notes is now half the desk, taller than wide: the next one goes below it, not at
	// the end of the tiling.
	if err := d.Focus(owner, notes.ID); err != nil {
		t.Fatal(err)
	}
	below := mustOpen(agent, t, d, "browser", module.OpenOptions{Key: "b"})
	col := d.Snapshot().Tree.Root.Children[0]
	if col.Type != "row" || len(col.Children) != 2 || col.Children[0].Wins[0] != notes.ID || col.Children[1].Wins[0] != below.ID {
		t.Fatalf("want notes over the new window, got %+v", col)
	}
	// Each of those is wide again (half the width, half the height): the third goes to the
	// right of Notes, inside its column.
	if err := d.Focus(owner, notes.ID); err != nil {
		t.Fatal(err)
	}
	third := mustOpen(agent, t, d, "browser", module.OpenOptions{Key: "c"})
	pair := d.Snapshot().Tree.Root.Children[0].Children[0]
	if pair.Type != "row" || len(pair.Children) != 2 || pair.Children[1].Wins[0] != third.ID {
		t.Errorf("want notes | third inside the column, got %+v", pair)
	}
}

func TestMovingAWindowMakesItTheOwners(t *testing.T) {
	d, _ := open(t, filepath.Join(t.TempDir(), "s.db"))
	owner, agent := as(module.ActorOwner), as(module.ActorAgent)
	mustOpen(owner, t, d, "notes", module.OpenOptions{})
	for _, typ := range []string{"float", "maximize", "promote"} {
		w := mustOpen(agent, t, d, "browser", module.OpenOptions{Key: typ})
		// The agent moving its own window takes nothing over.
		if _, err := d.Apply(agent, Action{Type: typ, ID: w.ID}); err != nil {
			t.Fatal(err)
		}
		if err := d.Close(agent, w.ID); err != nil {
			t.Fatalf("%s by the agent: %v", typ, err)
		}
		w = mustOpen(agent, t, d, "browser", module.OpenOptions{Key: typ})
		if _, err := d.Apply(owner, Action{Type: typ, ID: w.ID}); err != nil {
			t.Fatal(err)
		}
		if err := d.Close(agent, w.ID); !errors.Is(err, ErrNotYours) {
			t.Errorf("after the owner's %s the agent closed it: %v", typ, err)
		}
	}
	// A drag: only the window the owner grabbed is taken over.
	grabbed := mustOpen(agent, t, d, "browser", module.OpenOptions{Key: "grabbed"})
	shifted := mustOpen(agent, t, d, "browser", module.OpenOptions{Key: "shifted"})
	tree := d.Snapshot().Tree
	if _, err := d.Apply(owner, Action{Type: "tree", ID: grabbed.ID, Tree: &tree}); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(agent, grabbed.ID); !errors.Is(err, ErrNotYours) {
		t.Errorf("the grabbed window: %v, want ErrNotYours", err)
	}
	if err := d.Close(agent, shifted.ID); err != nil {
		t.Errorf("a window that only shifted: %v", err)
	}
}

func TestReleasingClosesOnlyTransientWindowsNobodyKept(t *testing.T) {
	d, _ := open(t, filepath.Join(t.TempDir(), "s.db"))
	owner, agent := as(module.ActorOwner), as(module.ActorAgent)
	notes := mustOpen(owner, t, d, "notes", module.OpenOptions{})
	step := mustOpen(agent, t, d, "browser", module.OpenOptions{Key: "step", Retention: "transient"})
	kept := mustOpen(agent, t, d, "browser", module.OpenOptions{Key: "kept", Retention: "transient", Background: true})
	result := mustOpen(agent, t, d, "scratch", module.OpenOptions{Background: true})
	if _, err := d.Apply(owner, Action{Type: "keep", ID: kept.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Apply(agent, Action{Type: "release"}); err != nil {
		t.Fatal(err)
	}
	open := map[string]bool{}
	for _, w := range d.Snapshot().Wins {
		open[w.ID] = true
	}
	if open[step.ID] || !open[kept.ID] || !open[result.ID] || !open[notes.ID] {
		t.Errorf("after release: %v (want %s gone, the rest open)", open, step.ID)
	}
	if f := d.Snapshot().Focus; f != notes.ID {
		t.Errorf("focus went to %s, want the owner's %s", f, notes.ID)
	}
}

func TestADelegatesChangeWaitsWhileTheOwnerTypes(t *testing.T) {
	d, rec := open(t, filepath.Join(t.TempDir(), "s.db"))
	owner, agent := as(module.ActorOwner), as(module.ActorAgent)
	page := mustOpen(owner, t, d, "browser", module.OpenOptions{Args: map[string]any{"url": "https://a.example"}})
	if _, err := d.Apply(owner, Action{Type: "composing", ID: page.ID, Composing: true}); err != nil {
		t.Fatal(err)
	}
	// The agent navigates the window the owner is filling in: held, not applied.
	_, err := d.Apply(agent, Action{Type: "args", ID: page.ID, Args: map[string]any{"url": "https://b.example"}})
	if !errors.Is(err, ErrHeld) {
		t.Fatalf("args while the owner types: %v, want ErrHeld", err)
	}
	w := d.Snapshot().Wins[0]
	if w.Args["url"] != "https://a.example" || w.Held == nil || w.Held.Args["url"] != "https://b.example" || w.Held.By != "agent" {
		t.Fatalf("held: %+v / %+v", w.Args, w.Held)
	}
	if !slices.Contains(rec.topics, "desk.held") {
		t.Error("nothing announced the hold")
	}
	// The same through open, which brings the window forward with new args.
	if _, err = d.Open(agent, "browser", module.OpenOptions{Key: "main", Args: map[string]any{"url": "https://c.example"}}); !errors.Is(err, ErrHeld) {
		t.Fatalf("open while the owner types: %v, want ErrHeld", err)
	}
	if got := d.Snapshot().Wins[0].Held.Args["url"]; got != "https://c.example" {
		t.Errorf("the newest change should be held, got %v", got)
	}
	// The owner is done: the held change applies.
	if _, err = d.Apply(owner, Action{Type: "composing", ID: page.ID, Composing: false}); err != nil {
		t.Fatal(err)
	}
	w = d.Snapshot().Wins[0]
	if w.Args["url"] != "https://c.example" || w.Held != nil {
		t.Errorf("after the owner finished: %+v held %+v", w.Args, w.Held)
	}
	// The owner's own change is never held.
	if _, err = d.Apply(owner, Action{Type: "composing", ID: page.ID, Composing: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = d.Apply(owner, Action{Type: "args", ID: page.ID, Args: map[string]any{"url": "https://d.example"}}); err != nil {
		t.Errorf("the owner's own change: %v", err)
	}
}

func TestAHeldChangeWaitsForUnsavedWorkAndTheOwnerCanDropIt(t *testing.T) {
	d, _ := open(t, filepath.Join(t.TempDir(), "s.db"))
	owner, agent := as(module.ActorOwner), as(module.ActorAgent)
	notes := mustOpen(owner, t, d, "notes", module.OpenOptions{Args: map[string]any{"path": "a.md"}})
	if _, err := d.Apply(owner, Action{Type: "dirty", ID: notes.ID, Dirty: true}); err != nil {
		t.Fatal(err)
	}
	if err := d.SetArgs(agent, notes.ID, map[string]any{"path": "b.md"}); !errors.Is(err, ErrHeld) {
		t.Fatalf("set args over unsaved work: %v", err)
	}
	if _, err := d.Apply(agent, Action{Type: "held", ID: notes.ID, Do: "apply"}); err == nil {
		t.Error("the agent applied its own held change")
	}
	if _, err := d.Apply(owner, Action{Type: "held", ID: notes.ID, Do: "drop"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Apply(owner, Action{Type: "dirty", ID: notes.ID, Dirty: false}); err != nil {
		t.Fatal(err)
	}
	if w := d.Snapshot().Wins[0]; w.Args["path"] != "a.md" || w.Held != nil {
		t.Errorf("a dropped change applied: %+v", w)
	}
}

func TestTypingInThePanelKeepsTheFocusWhereItIs(t *testing.T) {
	d, _ := open(t, filepath.Join(t.TempDir(), "s.db"))
	owner, agent := as(module.ActorOwner), as(module.ActorAgent)
	notes := mustOpen(owner, t, d, "notes", module.OpenOptions{})
	if _, err := d.Apply(owner, Action{Type: "composing", Composing: true}); err != nil {
		t.Fatal(err)
	}
	mustOpen(agent, t, d, "scratch", module.OpenOptions{})
	if f := d.Snapshot().Focus; f != notes.ID {
		t.Errorf("the agent's window took the focus while the owner typed in the panel: %s", f)
	}
	if _, err := d.Apply(agent, Action{Type: "composing", Composing: false}); err == nil {
		t.Error("the agent cleared the owner's composing")
	}
}

// The Cards window does not take the focus from a window the owner is typing in.
func TestCardsDoNotTakeTheFocusWhileTheOwnerTypes(t *testing.T) {
	d, _ := open(t, filepath.Join(t.TempDir(), "s.db"))
	owner, agent := as(module.ActorOwner), as(module.ActorAgent)
	chat := mustOpen(owner, t, d, "notes", module.OpenOptions{})
	if _, err := d.Apply(owner, Action{Type: "composing", ID: chat.ID, Composing: true}); err != nil {
		t.Fatal(err)
	}
	mustOpen(agent, t, d, "surface.cards", module.OpenOptions{})
	if f := d.Snapshot().Focus; f != chat.ID {
		t.Fatalf("the Cards window took the focus while the owner typed: %s", f)
	}
	if _, err := d.Apply(owner, Action{Type: "composing", ID: chat.ID, Composing: false}); err != nil {
		t.Fatal(err)
	}
	cards := mustOpen(agent, t, d, "surface.cards", module.OpenOptions{})
	if f := d.Snapshot().Focus; f != cards.ID {
		t.Fatalf("not typing: the Cards window should come forward, focus %s", f)
	}
}

// An outside agent's window (a seat over MCP) opens behind and the owner's stays in front.
func TestAnOutsideClientsWindowOpensBehind(t *testing.T) {
	d, _ := open(t, filepath.Join(t.TempDir(), "s.db"))
	owner, outside := as(module.ActorOwner), as(module.ActorOutside)
	notes := mustOpen(owner, t, d, "notes", module.OpenOptions{})
	seat := module.WithAgent(outside, module.AgentRef{Name: "Skill Designer"})
	w := mustOpen(seat, t, d, "scratch", module.OpenOptions{})
	if f := d.Snapshot().Focus; f != notes.ID {
		t.Fatalf("an outside client's window took the focus: %s", f)
	}
	if len(d.Snapshot().Wins) != 2 || w.ID == "" {
		t.Fatal("the window was not opened")
	}
}
