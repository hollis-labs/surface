package agent

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestTetherConfig(t *testing.T) {
	for _, c := range []TetherConfig{
		{URL: "file:///tmp/chat", Seat: "seat"},
		{URL: "http://localhost/chat/v1"},
		{Seat: "seat"},
		{URL: (&url.URL{Scheme: "http", Host: "localhost", Path: "/chat/v1", User: url.User("fixture")}).String(), Seat: "seat"},
		{URL: "http://localhost/chat/v1?secret=x", Seat: "seat"},
		{URL: "http://localhost/chat/v1", Seat: "../other"},
	} {
		if c.validate() == nil {
			t.Errorf("invalid config accepted: %+v", c)
		}
	}
	p := filepath.Join(t.TempDir(), "agent.json")
	if err := os.WriteFile(p, []byte(`{"tether":{"url":"http://localhost:9030/chat/v1","seat":"file-seat"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SURFACE_AGENT_CONFIG", p)
	t.Setenv("SURFACE_TETHER_SEAT", "env-seat")
	t.Setenv("SURFACE_TETHER_URL", "http://127.0.0.1:9031/chat/v1/")
	t.Setenv("SURFACE_TETHER_KEY_FILE", "")
	cfg, err := LoadConfig("test")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tether.Seat != "env-seat" || cfg.Tether.URL != "http://127.0.0.1:9031/chat/v1" {
		t.Fatalf("config: %+v", cfg.Tether)
	}
}
