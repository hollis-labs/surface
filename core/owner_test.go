package core

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/surface/module"
)

// ownerMod declares a setting only the owner changes, and shows a card of its own.
type ownerMod struct {
	module.Base
	h module.Host
}

func (m *ownerMod) Manifest() module.Manifest {
	return module.Manifest{ID: "ownertest", Title: "Owner test", Version: "0.0.1",
		Settings: []module.Setting{
			{Key: "url", Title: "URL", Type: module.SettingString, Default: "", Owner: true},
			{Key: "label", Title: "Label", Type: module.SettingString, Default: ""},
		}}
}
func (m *ownerMod) Start(_ context.Context, h module.Host) error { m.h = h; return nil }

var theOwnerMod = &ownerMod{}

func init() { module.Register(theOwnerMod) }

// The agent's tools cannot change an owner-only setting, nor rewrite or close a card it did
// not show; the owner's own routes still can.
func TestOwnerSettingsAndOthersCards(t *testing.T) {
	t.Setenv("SURFACE_AGENT_CONFIG", filepath.Join(t.TempDir(), "absent.json"))
	ctx := context.Background()
	app, err := New(ctx, Options{Name: "ownertest", Title: "Owner test", DataDir: t.TempDir(), Modules: []string{"ownertest"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	t.Cleanup(func() {
		srv.Close()
		_ = app.Stop(context.Background())
	})

	var res map[string]any
	if code := do(t, srv, "POST", "/api/tools/settings_set", `{"key":"ownertest.url","value":"https://elsewhere.example.test"}`, &res); code < 400 {
		t.Fatalf("settings_set changed an owner-only setting: %d %v", code, res)
	}
	if code := do(t, srv, "POST", "/api/tools/settings_set", `{"key":"ownertest.label","value":"fine"}`, nil); code >= 400 {
		t.Fatalf("an ordinary setting: %d", code)
	}
	if code := do(t, srv, "PUT", "/api/settings/ownertest.url", `{"value":"https://gate.example.test"}`, nil); code >= 400 {
		t.Fatalf("the owner's own route: %d", code)
	}

	c, err := theOwnerMod.h.Cards().Show(ctx, module.Card{Kind: "info", Title: "A module's", Payload: []byte(`{"body":"x"}`)})
	if err != nil {
		t.Fatal(err)
	}
	code := do(t, srv, "POST", "/api/tools/card_update", `{"id":"`+c.ID+`","payload":{"body":"rewritten"},"end":true}`, &res)
	got, _, _ := theOwnerMod.h.Cards().Get(ctx, c.ID)
	if code < 400 || got.Status != "" || strings.Contains(string(got.Payload), "rewritten") {
		t.Fatalf("card_update changed a module's card: %d %+v", code, got)
	}
}
