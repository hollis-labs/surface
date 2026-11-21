package core

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"sync"
	"testing"

	"github.com/hollis-labs/surface/module"
)

// citeMod resolves one kind of reference from a fixed table, counting the batches it is asked.
type citeMod struct {
	module.Base
	mu      sync.Mutex
	batches [][]string
	down    bool
}

var theCiteMod = &citeMod{}

func init() { module.Register(theCiteMod) }

var citeTable = map[string]module.RefEntity{
	"T-1": {Title: "First", Status: &module.RefStatus{Label: "done", Tone: "success"}},
	"T-2": {Title: "Second"},
}

func (m *citeMod) Manifest() module.Manifest {
	return module.Manifest{ID: "citetest", Title: "Cite test", Refs: []module.RefKind{{
		Kind: "test.thing", Label: "Thing", ID: regexp.MustCompile(`^T-\d+$`),
		Resolve: func(_ context.Context, _ module.Host, ids []string) (map[string]module.RefEntity, error) {
			m.mu.Lock()
			defer m.mu.Unlock()
			if m.down {
				return nil, errors.New("down")
			}
			m.batches = append(m.batches, slices.Clone(ids))
			out := map[string]module.RefEntity{}
			for _, id := range ids {
				if e, ok := citeTable[id]; ok {
					e.Body = "never in a preview"
					out[id] = e
				}
			}
			return out, nil
		},
		Record: func(_ context.Context, _ module.Host, id string) (module.RefEntity, error) {
			e, ok := citeTable[id]
			if !ok {
				return e, module.ErrRefNotFound
			}
			e.Body = "the whole of " + id
			return e, nil
		},
	}}}
}

func TestReferencesResolveThroughTheModuleThatOwnsThem(t *testing.T) {
	srv := boot(t, "citetest")

	var kinds struct {
		Kinds []struct{ Kind, Label, Module string }
	}
	do(t, srv, "GET", "/api/cite", "", &kinds)
	if len(kinds.Kinds) != 1 || kinds.Kinds[0].Kind != "test.thing" || kinds.Kinds[0].Module != "citetest" {
		t.Fatalf("kinds: %+v", kinds)
	}

	// One batch for the ids the module has not answered; a malformed id never reaches it.
	var res struct {
		Results map[string]*module.RefEntity
	}
	if code := do(t, srv, "GET", "/api/cite/resolve?kind=test.thing&ids=T-1,T-2,T-9,nope", "", &res); code != 200 {
		t.Fatalf("resolve: %d", code)
	}
	if res.Results["T-1"] == nil || res.Results["T-1"].Title != "First" || res.Results["T-2"] == nil {
		t.Fatalf("found: %+v", res.Results)
	}
	if res.Results["T-1"].Body != "" {
		t.Fatal("a preview carried the record's body")
	}
	if e, ok := res.Results["T-9"]; !ok || e != nil {
		t.Fatalf("T-9 should be null (not found): %+v", res.Results)
	}
	if e, ok := res.Results["nope"]; !ok || e != nil {
		t.Fatalf("nope should be null: %+v", res.Results)
	}
	theCiteMod.mu.Lock()
	first := slices.Clone(theCiteMod.batches)
	theCiteMod.mu.Unlock()
	if len(first) != 1 || !slices.Equal(first[0], []string{"T-1", "T-2", "T-9"}) {
		t.Fatalf("batches: %v", first)
	}

	var palette []module.PaletteResult
	if code := do(t, srv, "GET", "/api/palette/search?provider=surface/cites&q=First", "", &palette); code != 200 || len(palette) != 1 || palette[0].Ref.Canonical != "T-1" {
		t.Fatalf("cited palette: %d %+v", code, palette)
	}
	if code := do(t, srv, "GET", "/api/palette/search?provider=missing&q=x", "", nil); code != 404 {
		t.Fatalf("unknown provider: %d", code)
	}

	// Asked again: answered from the cache, the module is not asked, even when it is down.
	theCiteMod.mu.Lock()
	theCiteMod.down = true
	theCiteMod.mu.Unlock()
	if code := do(t, srv, "GET", "/api/cite/resolve?kind=test.thing&ids=T-1,T-9", "", &res); code != 200 || res.Results["T-1"] == nil {
		t.Fatalf("cached: %d %+v", code, res.Results)
	}
	// A new id while the module is down: the lookup failed, which is not "not found".
	if code := do(t, srv, "GET", "/api/cite/resolve?kind=test.thing&ids=T-3", "", nil); code != 502 {
		t.Fatalf("down: %d, want 502", code)
	}
	theCiteMod.mu.Lock()
	theCiteMod.down = false
	theCiteMod.mu.Unlock()

	var rec module.RefEntity
	if code := do(t, srv, "GET", "/api/cite/record?kind=test.thing&id=T-2", "", &rec); code != 200 || rec.Body != "the whole of T-2" {
		t.Fatalf("record: %d %+v", code, rec)
	}
	if code := do(t, srv, "GET", "/api/cite/record?kind=test.thing&id=T-404", "", nil); code != 404 {
		t.Fatalf("missing record: %d", code)
	}
	if code := do(t, srv, "GET", "/api/cite/resolve?kind=no.such&ids=a", "", nil); code != 404 {
		t.Fatalf("unknown kind: %d", code)
	}
	if code := do(t, srv, "GET", "/api/cite/resolve?kind=test.thing&ids=", "", nil); code != 400 {
		t.Fatalf("no ids: %d", code)
	}

	// The agent's read tool reaches the same records.
	var called struct{ Result string }
	if code := do(t, srv, "POST", "/api/tools/cite_resolve", `{"kind":"test.thing","ids":["T-1"],"full":true}`, &called); code != 200 {
		t.Fatalf("tool: %d %+v", code, called)
	}
}
