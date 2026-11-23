package module

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Keys mints and checks API keys for an app with sign-in (surface.yaml auth: identity), for
// a module that hands a credential to something outside the page: an agent it launches, a
// device, a webhook. A key is an identity API key like any other (it shows in the app's key
// list, the owner can revoke it), so a module never runs a token store of its own.
//
// It is optional: KeysOf reports false when the app has no sign-in (or the host does not
// provide keys), and the module then falls back to whatever it did before.
type Keys interface {
	// Create mints a key with the given scopes. Scopes must be the app's own
	// ("<app>.<anything>"); "*" and other apps' scopes are refused. The key string is
	// returned once; only its hash is kept.
	Create(ctx context.Context, r KeyRequest) (Key, error)
	// Revoke ends a key at once (it is refused by its very next use). Revoking a key that
	// is already revoked is not an error; an unknown id is ErrKeyNotFound.
	Revoke(ctx context.Context, id string) error
	// Verify checks a bearer credential (a key, a session, a token minted from either) and
	// says whose it is. A module whose route is module.Public authenticates callers with it.
	Verify(ctx context.Context, token string) (Caller, error)
}

// KeyRequest is a key to mint.
type KeyRequest struct {
	// Name says what the key is for ("orchestrator s_1234"); it shows in the key list.
	Name   string
	Scopes []string
	// TTL is how long the key lives; 0 is until revoked.
	TTL time.Duration
	// CreatedBy is who the key acts for (a user id); "" records the module itself.
	CreatedBy string
}

// Key is a minted key: Secret is the bearer credential, shown only here.
type Key struct {
	ID     string `json:"id"`
	Secret string `json:"-"`
}

// Caller is who a verified credential belongs to.
type Caller struct {
	// ID is the principal: "user:<id>" for a person (or a token minted from their
	// session), "key:<id>" for an API key.
	ID     string
	Kind   string // user or service
	Name   string
	Scopes []string
	// For is the person an API key acts for ("user:<id>", the key's maker), "" for a person's
	// own session or token and for keys a module or the CLI made. Attribute a key's work to it.
	For string
}

// HasScope reports whether the caller holds want, with the same wildcard rules as User.
func (c Caller) HasScope(want string) bool { return User{Scopes: c.Scopes}.HasScope(want) }

// Errors.
var (
	ErrKeyNotFound = errors.New("module: no such key")
	ErrKeyScope    = errors.New("module: a key may carry only the app's own scopes")
	ErrBadKey      = errors.New("module: the credential is not valid")
)

// keysProvider is what a Host implements when it can mint keys.
type keysProvider interface{ Keys() Keys }

// KeysOf returns the host's key service, and false when the app has none (no sign-in).
func KeysOf(h Host) (Keys, bool) {
	p, ok := h.(keysProvider)
	if !ok {
		return nil, false
	}
	k := p.Keys()
	return k, k != nil
}

// CheckKeyScopes enforces Create's rule for an app named app: every scope is "<app>.…".
func CheckKeyScopes(app string, scopes []string) error {
	if len(scopes) == 0 {
		return ErrKeyScope
	}
	for _, s := range scopes {
		if s == "*" || !strings.HasPrefix(s, app+".") || len(s) == len(app)+1 {
			return ErrKeyScope
		}
	}
	return nil
}

// KeyEventTopic is the topic prefix of API key events: "identity.key.created" and
// "identity.key.revoked", published on the app's event bus (Host.Events) when sign-in is on,
// for any key: a person's, an admin's, a module's. Their data is a KeyEvent. identity.* events
// stay in the process: they are not streamed to the page.
const KeyEventTopic = "identity.key"

// KeyEvent is an API key made or revoked. It never carries the secret.
type KeyEvent struct {
	Action    string     `json:"action"` // created | revoked
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	CreatedBy string     `json:"created_by"`           // "user:<id>", or the module or CLI that made it
	ExpiresAt *time.Time `json:"expires_at,omitempty"` // nil: no expiry
	// By is who did it ("user:<id>", "key:<id>"), "" when not known.
	By string    `json:"by,omitempty"`
	At time.Time `json:"at"`
}
