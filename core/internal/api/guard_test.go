package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAllowedHost(t *testing.T) {
	extra := map[string]bool{"mymac.tailnet.ts.net": true}
	for host, want := range map[string]bool{
		"":                          true,
		"127.0.0.1:8190":            true,
		"[::1]:8190":                true,
		"localhost:8190":            true,
		"LOCALHOST.:8190":           true,
		"desk.localhost:8190":       true,
		"100.64.1.2:8190":           true, // an IP cannot be rebound
		"mymac.tailnet.ts.net:8190": true, // listed by the operator
		"evil.example:8190":         false,
		"evil.example":              false,
		"localhost.evil.example":    false,
		"127.0.0.1.evil.example":    false,
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
