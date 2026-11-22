package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/hollis-labs/substrate/modules/config"
	_ "github.com/hollis-labs/substrate/modules/health"
	_ "github.com/hollis-labs/substrate/modules/logging"

	"github.com/hollis-labs/surface/module"
)

// authProbe is a module with a route anyone may call and two that need a signed-in caller.
type authProbe struct{ module.Base }

func (authProbe) Manifest() module.Manifest {
	return module.Manifest{ID: "authprobe", Title: "Auth probe"}
}

func (authProbe) Start(_ context.Context, h module.Host) error {
	who := func(w http.ResponseWriter, r *http.Request) {
		u, ok := module.UserOf(r.Context())
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": ok, "user": u})
	}
	h.Handle("GET /who", http.HandlerFunc(who))
	h.Handle("POST /who", http.HandlerFunc(who))
	h.Handle("POST /hook", module.Public(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, signed := module.UserOf(r.Context())
		_ = json.NewEncoder(w).Encode(map[string]any{"public": true, "signed": signed})
	})))
	return nil
}

func init() { module.Register(authProbe{}) }

type authEnv struct {
	t    *testing.T
	base string
	cfg  Config
	env  map[string]string
}

func (e *authEnv) getenv(k string) string {
	if v, ok := e.env[k]; ok {
		return v
	}
	return os.Getenv(k)
}

func startAuthApp(t *testing.T) *authEnv {
	t.Helper()
	dir := t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	e := &authEnv{t: t, base: "http://" + addr, env: map[string]string{"AUTHAPP_DATA_DIR": dir, "AUTHAPP_ADDR": addr, "XDG_CONFIG_HOME": dir, "HOME": dir}}
	e.cfg = Config{
		Name: "authapp", Title: "Auth app", Modules: []string{"authprobe"}, Auth: AuthIdentity,
		SubstrateModules: []string{"config", "logging", "health", "database", "cli", "auth", "identity", "httphost"},
		SubstrateConfigJSON: `{"database":{"sqlite":{"path":"surface.db"}},"httphost":{"addr":"127.0.0.1:0"},"logging":{"format":"text","level":"warn"},` +
			`"auth":{"mode":"enforce"},"identity":{"key_file":"identity-signing.key","setup":"http","bcrypt_cost":4}}`,
	}
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	done := make(chan error, 1)
	cfg := e.cfg
	cfg.Ready = func(net.Addr) { close(ready) }
	go func() { done <- Run(ctx, cfg, e.getenv, []string{"serve"}) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	})
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("app exited: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("app did not start")
	}
	return e
}

type resp struct {
	code int
	body string
	res  *http.Response
}

func (e *authEnv) do(method, path string, body any, hdr map[string]string, cookies ...*http.Cookie) resp {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.base+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	return resp{code: res.StatusCode, body: string(b), res: res}
}

func sessionCookie(t *testing.T, r resp) *http.Cookie {
	t.Helper()
	for _, c := range r.res.Cookies() {
		if c.Name == "authapp_session" {
			return c
		}
	}
	t.Fatalf("no session cookie (%d %s)", r.code, r.body)
	return nil
}

func TestSignInGuardsTheApp(t *testing.T) {
	e := startAuthApp(t)
	page := map[string]string{"X-Requested-With": "XMLHttpRequest", "Origin": e.base}

	// Signed out: the API is closed, a public module route and the identity routes are open.
	if r := e.do("GET", "/api/m/authprobe/who", nil, nil); r.code != 401 {
		t.Fatalf("signed out: %d %s", r.code, r.body)
	}
	if r := e.do("GET", "/api/desk", nil, nil); r.code != 401 {
		t.Fatalf("desk signed out: %d", r.code)
	}
	if r := e.do("POST", "/mcp", map[string]any{}, nil); r.code != 401 {
		t.Fatalf("mcp signed out: %d", r.code)
	}
	if r := e.do("POST", "/api/m/authprobe/hook", map[string]any{}, nil); r.code != 200 || !strings.Contains(r.body, `"signed":false`) {
		t.Fatalf("public route: %d %s", r.code, r.body)
	}
	if r := e.do("GET", "/api/setup", nil, nil); r.code != 200 || !strings.Contains(r.body, `"setup_required":true`) {
		t.Fatalf("setup status: %d %s", r.code, r.body)
	}

	// The owner signs up (setup: http in this test) and is let in with their role.
	r := e.do("POST", "/api/setup", map[string]string{"username": "owner", "display_name": "The Owner", "password": "correct horse battery"}, page)
	if r.code != 200 {
		t.Fatalf("setup: %d %s", r.code, r.body)
	}
	own := sessionCookie(t, r)
	r = e.do("GET", "/api/m/authprobe/who", nil, nil, own)
	if r.code != 200 || !strings.Contains(r.body, `"role":"owner"`) || !strings.Contains(r.body, `"username":"owner"`) || !strings.Contains(r.body, `"kind":"user"`) {
		t.Fatalf("owner who: %d %s", r.code, r.body)
	}
	if r := e.do("GET", "/api/desk", nil, nil, own); r.code != 200 {
		t.Fatalf("owner desk: %d %s", r.code, r.body)
	}
	// A cookie write needs the page's header and origin (identity.Middleware).
	if r := e.do("POST", "/api/m/authprobe/who", map[string]any{}, nil, own); r.code != 403 {
		t.Fatalf("cookie write without X-Requested-With: %d", r.code)
	}
	if r := e.do("POST", "/api/m/authprobe/who", map[string]any{}, page, own); r.code != 200 {
		t.Fatalf("owner write: %d %s", r.code, r.body)
	}
	// A WebSocket handshake with the cookie from another site is refused.
	ws := map[string]string{"Upgrade": "websocket", "Connection": "Upgrade", "Origin": "http://evil.example"}
	if r := e.do("GET", "/api/m/authprobe/who", nil, ws, own); r.code != 403 {
		t.Fatalf("cross-site upgrade: %d", r.code)
	}

	// A guest reads, but cannot change anything; a member can.
	for _, u := range []struct{ name, role string }{{"guest", "guest"}, {"member", "member"}} {
		if r := e.do("POST", "/api/identity/users", map[string]string{"username": u.name, "password": "correct horse battery", "role": u.role}, page, own); r.code != 201 {
			t.Fatalf("create %s: %d %s", u.name, r.code, r.body)
		}
	}
	guest := sessionCookie(t, e.do("POST", "/api/login", map[string]string{"username": "guest", "password": "correct horse battery"}, page))
	member := sessionCookie(t, e.do("POST", "/api/login", map[string]string{"username": "member", "password": "correct horse battery"}, page))
	if r := e.do("GET", "/api/m/authprobe/who", nil, nil, guest); r.code != 200 {
		t.Fatalf("guest read: %d", r.code)
	}
	if r := e.do("POST", "/api/m/authprobe/who", map[string]any{}, page, guest); r.code != 403 {
		t.Fatalf("guest write: %d %s", r.code, r.body)
	}
	if r := e.do("POST", "/api/m/authprobe/who", map[string]any{}, page, member); r.code != 200 || !strings.Contains(r.body, `"role":"member"`) {
		t.Fatalf("member write: %d %s", r.code, r.body)
	}

	// Signed out again, the cookie stops working.
	if r := e.do("POST", "/api/logout", map[string]any{}, page, member); r.code >= 300 {
		t.Fatalf("logout: %d %s", r.code, r.body)
	}
	if r := e.do("GET", "/api/m/authprobe/who", nil, nil, member); r.code != 401 {
		t.Fatalf("after logout: %d", r.code)
	}

	// The CLI signs with the operator key it keeps (0600) in the data directory.
	out := captureStdout(t, func() {
		if err := Run(context.Background(), e.cfg, e.getenv, []string{"desk"}); err != nil {
			t.Fatalf("cli desk: %v", err)
		}
	})
	if !strings.Contains(out, "wins") {
		t.Fatalf("cli desk output %q", out)
	}
	st, err := os.Stat(filepath.Join(e.env["AUTHAPP_DATA_DIR"], operatorKeyFile))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("operator key: %v %v", st, err)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = wr
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(rd)
		done <- string(b)
	}()
	fn()
	_ = wr.Close()
	os.Stdout = old
	return <-done
}
