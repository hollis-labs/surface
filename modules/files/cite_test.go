package files

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
)

func pathRef(t *testing.T, h *moduletest.Host) module.RefKind {
	t.Helper()
	for _, r := range h.Manifest().Refs {
		if r.Kind == pathKind {
			return r
		}
	}
	t.Fatal("no path kind")
	return module.RefKind{}
}

func TestAPathResolvesInsideAPlaceOnly(t *testing.T) {
	h, home := start(t, map[string]string{"notes/a.md": "# Alpha\n\nbody", "src/main.go": "package main", ".ssh/id_rsa": "key", "dir/": ""})
	r := pathRef(t, h)
	ctx := context.Background()
	abs := filepath.Join(home.Path, "src", "main.go")
	ids := []string{abs, "~/notes/a.md", "~/dir", "~/.ssh/id_rsa", "~/nope.txt", "/elsewhere/x.go"}
	for _, id := range ids {
		if !r.ID.MatchString(id) {
			t.Fatalf("%s does not match the kind", id)
		}
	}
	got, err := r.Resolve(ctx, h, ids)
	if err != nil {
		t.Fatal(err)
	}
	if e := got[abs]; e.Title != "main.go" || e.Open == nil || e.Open.Command != "files.open" || e.Open.Args["path"] != "src/main.go" || e.Open.Args["place"] != home.ID {
		t.Fatalf("absolute: %+v", e)
	}
	if e := got["~/notes/a.md"]; e.Title != "a.md" || !strings.HasPrefix(e.Subtitle, "File in") {
		t.Fatalf("home: %+v", e)
	}
	if e := got["~/dir"]; !strings.HasPrefix(e.Subtitle, "Folder in") {
		t.Fatalf("folder: %+v", e)
	}
	if _, ok := got["~/.ssh/id_rsa"]; ok {
		t.Fatal("a key file resolved")
	}
	if _, ok := got["~/nope.txt"]; ok {
		t.Fatal("a missing file resolved")
	}
	if e := got["/elsewhere/x.go"]; e.Subtitle != "Not in a Files place" || e.Open != nil {
		t.Fatalf("outside every place: %+v", e)
	}

	rec, err := r.Record(ctx, h, abs)
	if err != nil || !strings.Contains(rec.Body, "```go\npackage main\n```") {
		t.Fatalf("record: %v %q", err, rec.Body)
	}
	if rec, _ := r.Record(ctx, h, "~/notes/a.md"); !strings.HasPrefix(rec.Body, "# Alpha") {
		t.Fatalf("markdown record: %q", rec.Body)
	}
	if _, err := r.Record(ctx, h, "~/.ssh/id_rsa"); !errors.Is(err, module.ErrRefNotFound) {
		t.Fatalf("key record: %v", err)
	}
}

func TestARemotePathResolvesOverTheHost(t *testing.T) {
	f := &fakeRunner{reply: func(op string, args []string, _ []byte) runResult {
		switch {
		case op == "stat" && args[0] == "gone.txt":
			return runResult{out: []byte("m\t0\t0\t/srv/box/gone.txt")}
		case op == "stat":
			return runResult{out: []byte("f\t3\t0\t/srv/box/" + args[0])}
		}
		return runResult{code: 2}
	}}
	home := tree(t, nil)
	t.Setenv("HOME", home.Path)
	h := moduletest.Start(t, &mod{runner: f})
	ctx := context.Background()
	if err := h.Settings.Set(ctx, "remotes", `[{"id":"box","label":"box","host":"box","path":"/srv/box"}]`); err != nil {
		t.Fatal(err)
	}
	r := pathRef(t, h)
	got, err := r.Resolve(ctx, h, []string{"remote:box/src/a.go", "remote:box/gone.txt", "remote:other/x"})
	if err != nil {
		t.Fatal(err)
	}
	if e := got["remote:box/src/a.go"]; e.Title != "a.go" || e.Open.Args["place"] != "remote:box" {
		t.Fatalf("remote: %+v", e)
	}
	if len(got) != 1 {
		t.Fatalf("missing or unknown remote resolved: %+v", got)
	}
	for _, op := range f.ops() {
		if op != "stat" {
			t.Fatalf("resolving ran %s on the host", op)
		}
	}
}
