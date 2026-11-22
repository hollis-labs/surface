package module_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
)

type keyless struct{ module.Base }

func (keyless) Manifest() module.Manifest { return module.Manifest{ID: "keyless"} }

func TestKeysOf(t *testing.T) {
	h := moduletest.New(t, keyless{})
	if _, ok := module.KeysOf(h); ok {
		t.Fatal("keys without a key store")
	}
	h.KeyStore = moduletest.NewKeys()
	k, ok := module.KeysOf(h)
	if !ok {
		t.Fatal("no keys with a key store")
	}
	for _, bad := range [][]string{nil, {"*"}, {"other.read"}, {"keyless."}} {
		if _, err := k.Create(context.Background(), module.KeyRequest{Name: "x", Scopes: bad}); !errors.Is(err, module.ErrKeyScope) {
			t.Errorf("scopes %v: %v", bad, err)
		}
	}
	key, err := k.Create(context.Background(), module.KeyRequest{Name: "agent", Scopes: []string{"keyless.orch"}})
	if err != nil {
		t.Fatal(err)
	}
	c, err := k.Verify(context.Background(), key.Secret)
	if err != nil || !c.HasScope("keyless.orch") || c.HasScope("keyless.admin") {
		t.Fatalf("verify %+v %v", c, err)
	}
	if err := k.Revoke(context.Background(), key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Verify(context.Background(), key.Secret); !errors.Is(err, module.ErrBadKey) {
		t.Fatalf("revoked key verified: %v", err)
	}
}
