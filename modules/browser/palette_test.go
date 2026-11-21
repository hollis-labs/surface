package browser

import (
	"context"
	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
	"testing"
)

func TestPaletteTabsScopeAndURLSearch(t *testing.T) {
	m := &mod{}
	h := moduletest.New(t, m)
	ctx := context.Background()
	w, err := h.Desk().Open(ctx, "browser", module.OpenOptions{Title: "Docs", Args: map[string]any{"url": "https://example.com/guide"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.Desk().Open(ctx, "notes", module.OpenOptions{Title: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := m.palette(ctx, h, module.PaletteQuery{Text: "GUIDE", Scope: "all", Limit: 20})
	if err != nil || len(rows) != 1 || rows[0].Window != w.ID || !rows[0].Close {
		t.Fatalf("%+v %v", rows, err)
	}
	rows, err = m.palette(ctx, h, module.PaletteQuery{Scope: "focused", FocusedWindow: "other", Limit: 20})
	if err != nil || len(rows) != 0 {
		t.Fatalf("%+v %v", rows, err)
	}
	rows, err = m.palette(ctx, h, module.PaletteQuery{Scope: "focused", FocusedWindow: w.ID, Limit: 20})
	if err != nil || len(rows) != 1 {
		t.Fatalf("%+v %v", rows, err)
	}
}
