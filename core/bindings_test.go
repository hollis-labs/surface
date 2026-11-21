package core

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestAgentBindingsValidationPersistenceAndReset(t *testing.T) {
	ctx := context.Background()
	data := t.TempDir()
	app, err := New(ctx, Options{Name: "bindings", DataDir: data, Modules: []string{"scratch"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	defer srv.Close()
	defer func() { _ = app.Stop(ctx) }()
	dir := t.TempDir()
	raw, err := os.ReadFile("testdata/profiletest/profile/agent.md")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "agent.md"), raw, 0600); err != nil { //nolint:gosec // fixed fixture filename under a test-owned temporary directory
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "host.yaml"), []byte("kind: embedded\nlifetime: application\napplication_id: binding-test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	set := func(v any) int {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"value": v})
		return do(t, srv, "PUT", "/api/settings/agent.bindings", string(raw), nil)
	}
	for _, bad := range []any{nil, "bad", map[string]any{"scratch": 9}, map[string]string{"missing": dir}, map[string]string{"scratch": ""}, map[string]string{"scratch": filepath.Join(dir, "missing")}} {
		if code := set(bad); code != 422 {
			t.Fatalf("invalid binding %v: %d", bad, code)
		}
	}
	if code := set(map[string]string{"scratch": dir}); code != 200 {
		t.Fatalf("bind: %d", code)
	}
	var st struct {
		Bindings []struct{ ID, Dir, Name string }
	}
	do(t, srv, "GET", "/api/agent/profiles", "", &st)
	if len(st.Bindings) != 1 || st.Bindings[0].Dir != dir || st.Bindings[0].Name == "" {
		t.Fatalf("state: %+v", st)
	}
	// Invalid replacement must preserve the previous binding.
	if code := set(map[string]string{"scratch": "relative"}); code != 422 {
		t.Fatalf("bad replacement: %d", code)
	}
	do(t, srv, "GET", "/api/agent/profiles", "", &st)
	if st.Bindings[0].Dir != dir {
		t.Fatal("invalid replacement changed the binding")
	}
	if code := do(t, srv, "DELETE", "/api/settings/agent.bindings", "", nil); code != 200 {
		t.Fatalf("reset: %d", code)
	}
	st.Bindings = nil
	do(t, srv, "GET", "/api/agent/profiles", "", &st)
	if st.Bindings[0].Dir != "" {
		t.Fatal("reset kept binding")
	}
}

func TestAgentBindingsAreValidatedAgainAtBoot(t *testing.T) {
	ctx := context.Background()
	data := t.TempDir()
	dir := t.TempDir()
	definition, err := os.ReadFile("testdata/profiletest/profile/agent.md")
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{"agent.md": definition, "host.yaml": []byte("kind: embedded\nlifetime: application\napplication_id: binding-test\n")} {
		if err = os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil { //nolint:gosec // fixed fixture filename under a test-owned temporary directory
			t.Fatal(err)
		}
	}
	opts := Options{Name: "binding-boot", DataDir: data, Modules: []string{"scratch"}}
	app, err := New(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err = app.k.Settings().Set(ctx, "agent.bindings", map[string]string{"scratch": dir}); err != nil {
		t.Fatal(err)
	}
	if err = app.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.Start(ctx); err != nil {
		t.Fatalf("valid binding did not survive restart: %v", err)
	}
	value, _ := restarted.k.Settings().Get("agent.bindings")
	if value.(map[string]any)["scratch"] != dir {
		t.Fatal("binding was not persisted")
	}
	if err = restarted.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "host.yaml"), []byte("runtime: ["), 0600); err != nil {
		t.Fatal(err)
	}
	invalid, err := New(ctx, opts)
	if err != nil {
		return
	} // Services are assembled during New.
	defer func() { _ = invalid.Stop(ctx) }()
	if err = invalid.Start(ctx); err == nil {
		t.Fatal("invalid saved binding accepted at boot")
	}
}
