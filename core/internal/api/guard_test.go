package api

import (
	"github.com/hollis-labs/surface/module"

	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAllowedHost(t *testing.T) {
	extra := map[string]bool{"mymac.example.test": true}
	for host, want := range map[string]bool{
		"":                        true,
		"127.0.0.1:8190":          true,
		"[::1]:8190":              true,
		"localhost:8190":          true,
		"LOCALHOST.:8190":         true,
		"desk.localhost:8190":     true,
		"192.0.2.10:8190":         true, // an IP cannot be rebound
		"mymac.example.test:8190": true, // listed by the operator
		"evil.example:8190":       false,
		"evil.example":            false,
		"localhost.evil.example":  false,
		"127.0.0.1.evil.example":  false,
	} {
		if got := allowedHost(host, extra); got != want {
			t.Errorf("allowedHost(%q) = %v, want %v", host, got, want)
		}
	}
}

// A page whose name was rebound to this machine sends a matching Origin and Host: the Origin
// check passes, the Host check does not.
func TestGuardRefusesARebindingPage(t *testing.T) {
	h := guard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	for _, c := range []struct {
		host, origin string
		want         int
	}{
		{"evil.example:8190", "http://evil.example:8190", http.StatusMisdirectedRequest},
		{"127.0.0.1:8190", "http://127.0.0.1:8190", 200},
		{"127.0.0.1:8190", "http://evil.example", http.StatusForbidden},
	} {
		req := httptest.NewRequest("POST", "/api/m/terminal/sessions", nil)
		req.Host = c.host
		req.Header.Set("Origin", c.origin)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("host %s origin %s: %d, want %d", c.host, c.origin, rec.Code, c.want)
		}
	}
}

// The page is the owner; a request with no Origin (MCP, the tools, the CLI) is from outside.
func TestGuardSaysWhoIsAsking(t *testing.T) {
	var got module.Actor
	h := guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = module.ActorOf(r.Context()) }))
	for origin, want := range map[string]module.Actor{"http://127.0.0.1:8190": module.ActorOwner, "": module.ActorOutside} {
		req := httptest.NewRequest("POST", "/api/desk/actions", nil)
		req.Host = "127.0.0.1:8190"
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		h.ServeHTTP(httptest.NewRecorder(), req)
		if got != want {
			t.Errorf("origin %q: actor %q, want %q", origin, got, want)
		}
	}
}

// A listed name may be given with a port; it is compared without one.
func TestAllowedHostsFromEnv(t *testing.T) {
	t.Setenv("SURFACE_ALLOWED_HOSTS", " MyMac.example.test:8190 , other.example.test ")
	got := allowedHostsFromEnv()
	if !got["mymac.example.test"] || !got["other.example.test"] || len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}

type fakePublicHosts struct{ host, prefix string }

func (f fakePublicHosts) PublicHostAllowed(h string) bool { return h == f.host }
func (f fakePublicHosts) PublicRoute(r *http.Request) bool {
	return len(r.URL.Path) >= len(f.prefix) && r.URL.Path[:len(f.prefix)] == f.prefix
}

// A host name a module added (module.PublicHosts) is answered for its public routes only.
func TestGuardAnswersAModuleHostOnPublicRoutesOnly(t *testing.T) {
	h := guard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }),
		fakePublicHosts{host: "old.example:8470", prefix: "/api/m/hub/connect"})
	for _, c := range []struct {
		host, path string
		want       int
	}{
		{"old.example:8470", "/api/m/hub/connect", 200},
		{"old.example:8470", "/api/m/hub/machines", http.StatusMisdirectedRequest},
		{"old.example:8470", "/", http.StatusMisdirectedRequest},
		{"old.example:8470", "/api/identity/profile", http.StatusMisdirectedRequest},
		{"other.example:8470", "/api/m/hub/connect", http.StatusMisdirectedRequest},
	} {
		req := httptest.NewRequest("GET", c.path, nil)
		req.Host = c.host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s %s: %d, want %d", c.host, c.path, rec.Code, c.want)
		}
	}
}
