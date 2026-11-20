package work

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hollis-labs/surface/module"
)

func TestAWorkItemResolvesFromTheCopyAndOpensInItsApp(t *testing.T) {
	f := seeded()
	h := start(t, f)
	call(t, h, "work_list", `{"workspace":"portfolio"}`) // the copy is read once, as a window would
	f.mu.Lock()
	before := len(f.calls)
	f.mu.Unlock()

	var r module.RefKind
	for _, k := range h.Manifest().Refs {
		if k.Kind == itemKind {
			r = k
		}
	}
	ctx := context.Background()
	got, err := r.Resolve(ctx, h, []string{"portfolio/task0001", "portfolio/zzzz0000", "nope/task0001"})
	if err != nil {
		t.Fatal(err)
	}
	e, ok := got["portfolio/task0001"]
	if !ok || e.Title != "Write the port" || e.Status.Label != "planned" || e.Open.Command != "work.open" || e.Open.Args["item"] != "task0001" {
		t.Fatalf("item: %+v", got)
	}
	if len(got) != 1 {
		t.Fatalf("missing items resolved: %+v", got)
	}
	rec, err := r.Record(ctx, h, "portfolio/task0001")
	if err != nil || !strings.Contains(rec.Body, "## Done when") {
		t.Fatalf("record: %v %q", err, rec.Body)
	}
	if _, err := r.Record(ctx, h, "portfolio/zzzz0000"); !errors.Is(err, module.ErrRefNotFound) {
		t.Fatalf("missing record: %v", err)
	}
	f.mu.Lock()
	after := len(f.calls)
	f.mu.Unlock()
	if after != before {
		t.Fatalf("resolving reached Caster: %d calls", after-before)
	}
}

// A caster:// reference to a workspace or one of its items opens in that workspace's app; one
// no workspace holds is refused, so the page shows the record instead.
func TestACasterRefOpensInItsWorkspace(t *testing.T) {
	h := start(t, seeded())
	call(t, h, "work_list", `{"workspace":"portfolio"}`)
	ctx := context.Background()
	if _, err := h.RunCommand(ctx, "work.open-ref", map[string]any{"ref": "caster://w-portfolio/i.task0001@2"}); err != nil {
		t.Fatal(err)
	}
	w, ok := h.DeskState.Focused(ctx)
	if !ok || w.Args["workspace"] != "portfolio" || w.Args["item"] != "task0001" {
		t.Fatalf("opened %+v", w)
	}
	if _, err := h.RunCommand(ctx, "work.open-ref", map[string]any{"ref": "caster://work/portfolio"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.RunCommand(ctx, "work.open-ref", map[string]any{"ref": "caster://chief/notes"}); err == nil {
		t.Fatal("a ref outside every workspace was opened")
	}
}
