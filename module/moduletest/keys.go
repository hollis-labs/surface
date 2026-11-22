package moduletest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"slices"
	"sync"

	"github.com/hollis-labs/surface/module"
)

// Keys is an in-memory module.Keys for tests: set Host.KeyStore = moduletest.NewKeys() to
// give a module sign-in keys (module.KeysOf then reports true). It applies the same scope rule
// as an app, with the module's id standing for the app's name.
type Keys struct {
	mu      sync.Mutex
	byToken map[string]stored
}

type stored struct {
	id     string
	req    module.KeyRequest
	revoke bool
}

// NewKeys returns an empty key store.
func NewKeys() *Keys { return &Keys{byToken: map[string]stored{}} }

func token() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (k *Keys) create(app string, r module.KeyRequest) (module.Key, error) {
	if err := module.CheckKeyScopes(app, r.Scopes); err != nil {
		return module.Key{}, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	key := module.Key{ID: "key_" + token()[:12], Secret: "key_" + token()}
	k.byToken[key.Secret] = stored{id: key.ID, req: r}
	return key, nil
}

// Revoke implements module.Keys.
func (k *Keys) Revoke(_ context.Context, id string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	for t, s := range k.byToken {
		if s.id == id {
			s.revoke = true
			k.byToken[t] = s
			return nil
		}
	}
	return module.ErrKeyNotFound
}

// Verify implements module.Keys.
func (k *Keys) Verify(_ context.Context, tok string) (module.Caller, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	s, ok := k.byToken[tok]
	if !ok || s.revoke {
		return module.Caller{}, module.ErrBadKey
	}
	return module.Caller{ID: "key:" + s.id, Kind: "service", Name: s.req.Name, Scopes: slices.Clone(s.req.Scopes)}, nil
}

// Live lists the keys not revoked, by name (for assertions).
func (k *Keys) Live() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	var out []string
	for _, s := range k.byToken {
		if !s.revoke {
			out = append(out, s.req.Name)
		}
	}
	slices.Sort(out)
	return out
}

type boundKeys struct {
	k   *Keys
	app string
}

func (b boundKeys) Create(_ context.Context, r module.KeyRequest) (module.Key, error) {
	return b.k.create(b.app, r)
}
func (b boundKeys) Revoke(ctx context.Context, id string) error { return b.k.Revoke(ctx, id) }
func (b boundKeys) Verify(ctx context.Context, t string) (module.Caller, error) {
	return b.k.Verify(ctx, t)
}

// Keys gives the module the test's key store (nil, and so no keys, unless KeyStore is set).
func (h *Host) Keys() module.Keys {
	if h.KeyStore == nil {
		return nil
	}
	return boundKeys{k: h.KeyStore, app: h.man.ID}
}

// testPublicHosts records the names a module sets (Host.PublicHostNames).
type testPublicHosts struct{ h *Host }

func (p testPublicHosts) Set(names []string) []string {
	var keep, skipped []string
	for _, n := range names {
		if h, ok := module.NormalHostName(n); ok {
			keep = append(keep, h)
		} else {
			skipped = append(skipped, n)
		}
	}
	p.h.mu.Lock()
	p.h.PublicHostNames = keep
	p.h.mu.Unlock()
	return skipped
}

// PublicHosts records the host names the module adds (module.PublicHostsOf) in PublicHostNames.
func (h *Host) PublicHosts() module.PublicHosts { return testPublicHosts{h} }
