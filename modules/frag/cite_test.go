package frag

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
)

func TestAFragmentResolvesFromTheReaderReadOnly(t *testing.T) {
	id := strings.Repeat("ab", 32)
	var writes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes.Add(1)
		}
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path != "/v1/reader/items/"+id {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(`{"schema_version":"fe.reader.item.v1","fragment_id":"` + id + `","revision":2,` +
			`"display":{"title":{"value":"A saved thought"},"summary":{"value":"Short."}},"article":{"preview_markdown":"# A saved thought\n\nAll of it."}}`))
	}))
	t.Cleanup(srv.Close)

	m := &Mod{noLoop: true}
	h := moduletest.Start(t, m)
	var kind module.RefKind
	for _, r := range h.Manifest().Refs {
		if r.Kind == fragmentKind {
			kind = r
		}
	}
	ctx := context.Background()
	if kind.Ready(ctx, h) {
		t.Fatal("ready before the address and key file are set")
	}
	key := filepath.Join(t.TempDir(), "fe-key")
	if err := os.WriteFile(key, []byte(testKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = h.Settings.Set(ctx, "fe_url", srv.URL)
	_ = h.Settings.Set(ctx, "fe_token_file", key)
	if !kind.Ready(ctx, h) {
		t.Fatal("not ready with an address and a key file (delivery off is fine for reading)")
	}

	missing := strings.Repeat("cd", 32)
	got, err := kind.Resolve(ctx, h, []string{id, missing})
	if err != nil {
		t.Fatal(err)
	}
	if e, ok := got[id]; !ok || e.Title != "A saved thought" || e.Body != "" || !strings.HasSuffix(e.URL, "/sysop/reader/"+id) {
		t.Fatalf("found: %+v", got)
	}
	if _, ok := got[missing]; ok {
		t.Fatal("a missing fragment was found")
	}
	rec, err := kind.Record(ctx, h, id)
	if err != nil || !strings.Contains(rec.Body, "All of it.") {
		t.Fatalf("record: %v %+v", err, rec)
	}
	if _, err := kind.Record(ctx, h, missing); !errors.Is(err, module.ErrRefNotFound) {
		t.Fatalf("missing record: %v", err)
	}
	if writes.Load() != 0 {
		t.Fatal("resolving wrote to FE")
	}

	// A key file that cannot be read fails the lookup without saying what is in it.
	_ = h.Settings.Set(ctx, "fe_token_file", filepath.Join(t.TempDir(), "absent"))
	if _, err := kind.Resolve(ctx, h, []string{id}); err == nil || strings.Contains(err.Error(), testKey) {
		t.Fatalf("unreadable key: %v", err)
	}
}
