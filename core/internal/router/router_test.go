package router

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/hollis-labs/surface/core/internal/storage"
)

func TestBuiltinOpen(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := r.Route(ctx, Fields{Text: "/open scratch", View: "notes"})
	if err != nil || rt == nil || rt.Name != "desk_open" || rt.Input["view"] != "scratch" {
		t.Fatalf("%+v %v", rt, err)
	}
	if rt, _ := r.Route(ctx, Fields{Text: "open the pod bay doors"}); rt != nil {
		t.Fatalf("plain text routed: %+v", rt)
	}
}
