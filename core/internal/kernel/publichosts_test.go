package kernel

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/hollis-labs/surface/module"
)

func TestModulePublicHostsAreLiveAndPerModule(t *testing.T) {
	a := &App{routes: http.NewServeMux(), public: http.NewServeMux()}
	hub := &host{app: a, id: "hub"}
	other := &host{app: a, id: "other"}
	ok := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	hub.Handle("GET /connect", module.Public(ok))
	hub.Handle("GET /machines", ok)

	ph, has := module.PublicHostsOf(hub)
	if !has {
		t.Fatal("the kernel host has no PublicHosts")
	}
	skipped := ph.Set([]string{"Old.Example.Net.", "192.0.2.1", "*.example", "bad host", "x.example:8470"})
	if !slices.Equal(skipped, []string{"192.0.2.1", "*.example", "bad host", "x.example:8470"}) {
		t.Fatalf("skipped %v", skipped)
	}
	other.PublicHosts().Set([]string{"other.example"})
	for hostport, want := range map[string]bool{
		"old.example.net:8470": true,
		"OLD.example.net":      true,
		"other.example":           true,
		"new.example":             false,
		"192.0.2.1:8470":          false, // not listed (and an IP passes the guard anyway)
	} {
		if got := a.PublicHostAllowed(hostport); got != want {
			t.Errorf("PublicHostAllowed(%q) = %v", hostport, got)
		}
	}
	// Only public routes: the guard asks both.
	if !a.PublicRoute(httptest.NewRequest("GET", "/api/m/hub/connect", nil)) || a.PublicRoute(httptest.NewRequest("GET", "/api/m/hub/machines", nil)) {
		t.Fatal("PublicRoute")
	}
	// Removed live, without touching another module's names.
	ph.Set(nil)
	if a.PublicHostAllowed("old.example.net") || !a.PublicHostAllowed("other.example") {
		t.Fatal("after Set(nil)")
	}
}
