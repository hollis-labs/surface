package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	smodule "github.com/hollis-labs/substrate/module"
	"github.com/hollis-labs/substrate/modules/auth"
	"github.com/hollis-labs/substrate/modules/identity"

	"github.com/hollis-labs/surface/core"
	"github.com/hollis-labs/surface/module"
)

// Sign-in. An app whose surface.yaml says auth: identity runs Substrate's identity module
// (people, sessions, API keys, roles) in front of everything it serves:
//
//   - Substrate's own handler serves the identity routes (/api/setup, /api/login,
//     /api/identity/..., /api/token, /api/pair) behind its guard and scope table;
//   - every other /api and /mcp request must carry a credential the identity module verifies
//     (the session cookie, which identity.Middleware turns into the bearer, an API key, or a
//     token minted from either), and the caller needs "<app>.read" to read and "<app>.use" to
//     change anything; module routes marked module.Public are let through;
//   - the page itself and its assets are served to anyone (it shows the sign-in).
//
// Loopback is not trusted: agents run on the same hosts. The CLI client signs its calls with
// an operator key it keeps in the data directory (operatorKey).

// AuthIdentity is the Config.Auth value that turns sign-in on.
const AuthIdentity = "identity"

// identityPaths are the routes Substrate's identity module serves, which go to Substrate's
// handler (with /healthz).
var identityPaths = []string{"/healthz", "/api/setup", "/api/login", "/api/logout", "/api/password", "/api/token", "/api/pair", "/api/passkey/", "/api/invite/", "/api/identity/"}

// authHandler wraps the core's handler in sign-in, for Config.Auth == AuthIdentity.
func authHandler(cfg Config, sapp *smodule.App, a *core.App, substrateHandler http.Handler) (http.Handler, error) {
	svc := identity.From(sapp)
	if svc == nil || auth.Guard(sapp) == nil {
		return nil, errors.New("auth: identity needs Substrate's auth and identity modules: regenerate the app (`surface generate`) after setting auth: identity")
	}
	mux := http.NewServeMux()
	for _, p := range identityPaths {
		mux.Handle(p, substrateHandler)
	}
	mux.Handle("/", &gate{name: cfg.Name, sapp: sapp, svc: svc, core: a, next: a.Handler()})
	return svc.Middleware(mux), nil
}

// gate lets a request reach the core only with a credential, except the page and public
// module routes.
type gate struct {
	name string
	sapp *smodule.App
	svc  *identity.Service
	core *core.App
	next http.Handler
}

func (g *gate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	guarded := strings.HasPrefix(path, "/api/") || path == "/mcp" || strings.HasPrefix(path, "/mcp/")
	if !guarded || g.core.PublicRoute(r) {
		g.next.ServeHTTP(w, r)
		return
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || strings.TrimSpace(token) == "" {
		authFail(w, http.StatusUnauthorized, "unauthorized", "sign in first")
		return
	}
	p, err := auth.Verify(r.Context(), g.sapp, strings.TrimSpace(token))
	if err != nil || !p.Verified {
		authFail(w, http.StatusUnauthorized, "unauthorized", "the session ended or the credential is not valid: sign in again")
		return
	}
	need := g.name + ".use"
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		need = g.name + ".read"
	}
	if !p.HasScope(need) {
		authFail(w, http.StatusForbidden, "scope_required", "this needs "+need)
		return
	}
	u := module.User{ID: p.ID, Kind: p.Kind, DisplayName: p.Display, Scopes: p.Scopes}
	if id, ok := strings.CutPrefix(p.ID, "user:"); ok {
		u.ID, u.Kind = id, "user"
		if pu, err := g.svc.User(r.Context(), id); err == nil {
			u.Username, u.DisplayName, u.Role = pu.Username, pu.DisplayName, pu.Role
		}
	} else if u.Kind == "" {
		u.Kind = "service"
	}
	g.next.ServeHTTP(w, r.WithContext(module.WithUser(r.Context(), u)))
}

func authFail(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": msg, "error": msg})
}

// operatorKeyFile is where the CLI keeps its key, in the data directory.
const operatorKeyFile = "operator.key"

// operatorKey is the CLI's credential for an app with sign-in: an API key with every scope,
// kept 0600 in the data directory, made on first use. Whoever can run the binary against the
// data already controls the app (the identity module's trust statement); the key makes that
// authority usable over the API without trusting loopback.
func operatorKey(ctx context.Context, sapp *smodule.App, dataDir string) (string, error) {
	path := filepath.Join(dataDir, operatorKeyFile)
	if b, err := os.ReadFile(path); err == nil { //nolint:gosec // the app's own data directory
		if k := strings.TrimSpace(string(b)); k != "" {
			return k, nil
		}
	}
	svc := identity.From(sapp)
	if svc == nil {
		return "", errors.New("auth: the identity module is not started")
	}
	key, _, err := svc.CreateKey(ctx, identity.KeyInput{Name: "local operator (CLI)", Scopes: []string{"*"}, CreatedBy: identity.LocalOperator})
	if errors.Is(err, identity.ErrNoOwner) {
		return "", fmt.Errorf("no owner yet: create one first (`%s identity setup-link --url <app URL>`)", sapp.Name)
	}
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // the app's own data directory
	if err != nil {
		return "", fmt.Errorf("keep the operator key: %w", err)
	}
	if _, err := f.WriteString(key + "\n"); err != nil {
		_ = f.Close()
		return "", err
	}
	return key, f.Close()
}
