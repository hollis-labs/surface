package desk

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/hollis-labs/surface/core/internal/storage"
	"github.com/hollis-labs/surface/module"
)

func views() []ViewInfo {
	return []ViewInfo{
		{View: module.View{ID: "notes", Title: "Notes"}, Module: "notes"},
		{View: module.View{ID: "scratch", Title: "Scratch"}, Module: "scratch"},
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
