package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestKeyEventsReachModulesAndReadyzIsOpen(t *testing.T) {
	keySeen.Lock()
	keySeen.topics = nil // the recorder is shared with the other tests' apps
	keySeen.Unlock()
	e := startAuthApp(t)
	page := map[string]string{"X-Requested-With": "XMLHttpRequest", "Origin": e.base}

	// /readyz needs no sign-in and says only the status.
	if r := e.do("GET", "/readyz", nil, nil); r.code != 200 || strings.TrimSpace(r.body) != `{"status":"ok"}` {
		t.Fatalf("readyz: %d %s", r.code, r.body)
	}

	r := e.do("POST", "/api/setup", map[string]string{"username": "owner", "password": "correct horse battery"}, page)
	own := sessionCookie(t, r)
	// A person's key through the self-service route, then revoked; a module's key.
	r = e.do("POST", "/api/identity/me/keys", map[string]any{"name": "laptop agent", "scopes": []string{"authapp.read"}, "ttl_hours": 24}, page, own)
	if r.code != 201 {
		t.Fatalf("my key: %d %s", r.code, r.body)
	}
	var made struct {
		Credential struct{ ID string } `json:"credential"`
	}
	_ = json.Unmarshal([]byte(r.body), &made)
	if rr := e.do("DELETE", "/api/identity/me/keys/"+made.Credential.ID, nil, page, own); rr.code != 204 {
		t.Fatalf("revoke: %d %s", rr.code, rr.body)
	}
	if rr := e.do("POST", "/api/m/authprobe/mint", map[string]any{}, page, own); rr.code != 200 {
		t.Fatalf("mint: %d %s", rr.code, rr.body)
	}

	var seen []string
	if rr := e.do("GET", "/api/m/authprobe/keyevents", nil, nil, own); rr.code != 200 || json.Unmarshal([]byte(rr.body), &seen) != nil {
		t.Fatalf("keyevents: %d %s", rr.code, rr.body)
	}
	joined := strings.Join(seen, "\n")
	if len(seen) != 3 ||
		!strings.HasPrefix(seen[0], `identity.key.created {"action":"created","id":"`+made.Credential.ID+`","name":"laptop agent"`) ||
		!strings.Contains(seen[0], `"by":"user:`) || !strings.Contains(seen[0], `"expires_at":`) ||
		!strings.HasPrefix(seen[1], "identity.key.revoked ") ||
		!strings.Contains(seen[2], `"name":"device"`) ||
		strings.Contains(joined, "key_") { // never a secret
		t.Fatalf("events:\n%s", joined)
	}
}
