package scaffolding_test

import (
	"io/fs"
	"reflect"
	"testing"

	"github.com/hollis-labs/substrate/folio/service"
	"github.com/hollis-labs/surface/scaffolding"
)

func TestSurfacePresetDiscovery(t *testing.T) {
	entries, err := fs.ReadDir(scaffolding.Presets, "presets")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	if !reflect.DeepEqual(ids, []string{"surface-app", "surface-desk", "surface-ui"}) {
		t.Fatalf("pack presets: %v", ids)
	}
	svc := service.New(service.Options{BundledFS: surfaceCatalog(t), UserDir: t.TempDir()})
	got, warnings, listErr := svc.ListPresets()
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(warnings) != 0 {
		t.Fatal(warnings)
	}
	found := map[string]bool{}
	for _, item := range got {
		for _, id := range ids {
			if item.ID == id {
				found[id] = true
				if item.Version == "" || item.Description == "" || item.Author == "" || item.LayerOnly != (id == "surface-ui") {
					t.Fatalf("incomplete preset metadata: %+v", item)
				}
			}
		}
	}
	if len(found) != 3 {
		t.Fatalf("missing Surface presets: %v", found)
	}
}
