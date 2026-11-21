package kernel

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/hollis-labs/surface/core/internal/desk"
	"github.com/hollis-labs/surface/module"
)

func TestPaletteCancellationAndBounds(t *testing.T) {
	// A provider ignoring cancellation must not hold the HTTP request open.
	release := make(chan struct{})
	started := make(chan struct{})
	a := &App{desk: &desk.Desk{}, paletteSlots: make(chan struct{}, 6), palette: map[string]boundPalette{}}
	a.palette["test/slow"] = boundPalette{info: PaletteInfo{Mode: "tabs"}, search: func(context.Context, module.Host, module.PaletteQuery) ([]module.PaletteResult, error) {
		close(started)
		<-release
		return nil, nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer close(release)
	done := make(chan error, 1)
	go func() { _, err := a.SearchPalette(ctx, "test/slow", module.PaletteQuery{}); done <- err }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("provider blocked cancellation")
	}
}

func TestPaletteFiltersInvalidRowsAndCapsResults(t *testing.T) {
	a := &App{desk: &desk.Desk{}, paletteSlots: make(chan struct{}, 6), palette: map[string]boundPalette{}}
	a.palette["test/rows"] = boundPalette{info: PaletteInfo{Mode: "tabs"}, search: func(_ context.Context, _ module.Host, q module.PaletteQuery) ([]module.PaletteResult, error) {
		if q.Limit != 20 || q.Scope != "all" {
			t.Errorf("query: %+v", q)
		}
		rows := []module.PaletteResult{{ID: "missing", Title: "No action"}, {ID: "ambiguous", Title: "Two", Window: "one", Command: &module.RefOpen{Command: "test.open"}}}
		for i := 0; i < 30; i++ {
			rows = append(rows, module.PaletteResult{ID: fmt.Sprint(i), Title: "Row", Ref: &module.PaletteRef{Kind: "path", Canonical: "a"}, Close: true})
		}
		return rows, nil
	}}
	rows, err := a.SearchPalette(context.Background(), "test/rows", module.PaletteQuery{})
	if err != nil || len(rows) != 20 || rows[0].ID != "0" || rows[0].Close {
		t.Fatalf("%+v %v", rows, err)
	}
}
