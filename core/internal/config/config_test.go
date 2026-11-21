package config

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/hollis-labs/surface/core/internal/storage"
	"github.com/hollis-labs/surface/module"
)

var decl = []module.Setting{
	{Key: "theme", Type: module.SettingEnum, Default: "dark", Options: []string{"dark", "light"}},
	{Key: "gap", Type: module.SettingInt, Default: 10},
	{Key: "confirm", Type: module.SettingBool, Default: true},
	{Key: "home", Type: module.SettingString, Default: ""},
}

type recorder struct{ changed []string }

func (r *recorder) Publish(topic string, data any) {
	if topic == "config.changed" {
		r.changed = append(r.changed, data.(map[string]any)["key"].(string))
	}
}

func TestSettingsAreLiveValidatedAndKept(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "s.db")
	db, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.MigrateCore(ctx, db); err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	s, err := Open(ctx, db, rec)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Declare("surface", decl); err != nil {
		t.Fatal(err)
	}
	cfg := s.Scoped("surface")
	if cfg.String("theme") != "dark" || cfg.Int("gap") != 10 || !cfg.Bool("confirm") {
		t.Fatal("defaults are not what was declared")
	}

	var heard any
	stop := cfg.Watch("theme", func(v any) { heard = v })
	if err = cfg.Set(ctx, "theme", "light"); err != nil {
		t.Fatal(err)
	}
	// No restart, no reload: the next read has it, the watcher heard it, the bus said so.
	if cfg.String("theme") != "light" || heard != "light" || len(rec.changed) != 1 || rec.changed[0] != "surface.theme" {
		t.Errorf("after Set: read %q, watcher %v, bus %v", cfg.String("theme"), heard, rec.changed)
	}
	stop()
	_ = cfg.Set(ctx, "theme", "dark")
	if heard != "light" {
		t.Error("a stopped watcher was still called")
	}

	for _, bad := range []struct {
		key string
		v   any
	}{{"theme", "neon"}, {"gap", "wide"}, {"gap", 1.5}, {"confirm", "yes"}, {"unknown", 1}} {
		if cfg.Set(ctx, bad.key, bad.v) == nil {
			t.Errorf("Set(%q, %v) was accepted", bad.key, bad.v)
		}
	}
	// A whole number that arrived as JSON is still an int.
	if err = cfg.Set(ctx, "gap", float64(14)); err != nil || cfg.Int("gap") != 14 {
		t.Errorf("gap from JSON: %v, %d", err, cfg.Int("gap"))
	}

	// A new process finds the stored value.
	_ = db.Close()
	db2, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	s2, err := Open(ctx, db2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.Declare("surface", decl); err != nil {
		t.Fatal(err)
	}
	if got := s2.Scoped("surface").Int("gap"); got != 14 {
		t.Errorf("after reopening, gap is %d, want 14", got)
	}
	if err := s2.Reset(ctx, "surface.gap"); err != nil || s2.Scoped("surface").Int("gap") != 10 {
		t.Errorf("reset: %v, gap %d", err, s2.Scoped("surface").Int("gap"))
	}
}

func TestADefaultMustFitItsType(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.MigrateCore(ctx, db); err != nil {
		t.Fatal(err)
	}
	s, _ := Open(ctx, db, nil)
	if err := s.Declare("m", []module.Setting{{Key: "n", Type: module.SettingInt, Default: "ten"}}); err == nil {
		t.Error("a default of the wrong type was accepted")
	}
}

func TestObjectValidationRejectsBeforePersistenceOrPublication(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err = storage.MigrateCore(ctx, db); err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	s, err := Open(ctx, db, rec)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Declare("agent", []module.Setting{{Key: "bindings", Type: module.SettingObject, Default: map[string]any{}, Validate: func(v any) error {
		for _, path := range v.(map[string]any) {
			if path != "/agents/example" {
				return fmt.Errorf("invalid folder")
			}
		}
		return nil
	}}}); err != nil {
		t.Fatal(err)
	}
	value := map[string]string{"example": "/agents/example"}
	if err = s.Set(ctx, "agent.bindings", value); err != nil {
		t.Fatal(err)
	}
	value["example"] = "mutated"
	if err = s.Set(ctx, "agent.bindings", map[string]string{"example": "bad"}); err == nil {
		t.Fatal("invalid accepted")
	}
	got, _ := s.Get("agent.bindings")
	if got.(map[string]any)["example"] != "/agents/example" || len(rec.changed) != 1 {
		t.Fatalf("%v %v", got, rec.changed)
	}
	got.(map[string]any)["example"] = "mutated"
	again, _ := s.Get("agent.bindings")
	if again.(map[string]any)["example"] != "/agents/example" {
		t.Fatal("Get exposed stored map")
	}
	reopened, err := Open(ctx, db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = reopened.Declare("agent", []module.Setting{{Key: "bindings", Type: module.SettingObject, Default: map[string]any{}}}); err != nil {
		t.Fatal(err)
	}
	persisted, _ := reopened.Get("agent.bindings")
	if persisted.(map[string]any)["example"] != "/agents/example" {
		t.Fatal("invalid value persisted")
	}
}
