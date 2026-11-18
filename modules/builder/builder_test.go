package builder

import (
	"context"
	"strings"
	"testing"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
)

func TestSaveRefineRestore(t *testing.T) {
	mm := &mod{}
	h := moduletest.Start(t, mm)
	loaded := 0
	h.OnCommand("layout.load", func(ctx context.Context, args map[string]any) (any, error) {
		loaded++
		l, _, _ := h.DeskState.Layout(ctx, args["name"].(string))
		return nil, h.DeskState.Arrange(ctx, l)
	})
	h.OnCommand("surface.catalog", func(context.Context, map[string]any) (any, error) {
		return map[string]any{"app": "studio", "title": "Studio", "modules": []any{
			map[string]any{"id": "drafts", "title": "Drafts", "views": []any{map[string]any{"id": "drafts", "title": "Drafts"}},
				"slots": []any{map[string]any{"name": "drafts", "about": "writing"}}},
		}}, nil
	})
	// The Builder window is open; it must not end up in the flow.
	_, _ = h.DeskState.Open(t.Context(), "builder", module.OpenOptions{})

	_, err := h.Tool("builder_flow_save", `{"name":"Write an Article","goal":"draft and research an article",
		"arrange":{"mode":"tile","ratio":0.6,"windows":[{"view":"drafts","focus":true},{"view":"browser"},{"view":"builder"}]},
		"mode":"writing","instructions":"Keep the owner's voice.","reflexes":["article.cite"]}`)
	if err != nil {
		t.Fatal(err)
	}
	f, err := mm.get(t.Context(), "write an article")
	if err != nil || f.Version != 1 || len(f.Layout.Windows) != 2 || f.Layout.Setup["mode"] != "writing" {
		t.Fatalf("%+v %v", f, err)
	}
	if h.DeskState.Mode != "writing" || loaded != 1 {
		t.Fatalf("not put in use: mode %q loaded %d", h.DeskState.Mode, loaded)
	}
	// Refine: a new version; what is not given is kept.
	if _, err := h.Tool("builder_flow_save", `{"name":"write an article","note":"browser first","arrange":{"windows":[{"view":"browser"},{"view":"drafts","focus":true}]}}`); err != nil {
		t.Fatal(err)
	}
	f, _ = mm.get(t.Context(), "write an article")
	if f.Version != 2 || f.Goal != "draft and research an article" || f.Layout.Windows[0].View != "browser" || f.Layout.Setup["instructions"] != "Keep the owner's voice." || len(f.Versions) != 2 {
		t.Fatalf("refine: %+v", f)
	}
	if _, err := h.Tool("builder_flow_restore", `{"name":"write an article","version":1}`); err != nil {
		t.Fatal(err)
	}
	f, _ = mm.get(t.Context(), "write an article")
	if f.Version != 3 || f.Layout.Windows[0].View != "drafts" {
		t.Fatalf("restore: %+v", f.Layout.Windows)
	}
	cat, _ := h.Tool("builder_catalog", "")
	if !strings.Contains(cat.(string), "views: drafts (Drafts)") {
		t.Fatal(cat)
	}
	if _, err := h.Tool("builder_flow_save", `{"name":"new one"}`); err == nil {
		t.Fatal("a new flow without a goal was saved")
	}
}
