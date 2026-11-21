package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMailboxConfig(t *testing.T) {
	//nolint:gosec // G101: fixture paths, not credentials
	for _, c := range []*MailboxConfig{
		{CredentialFile: "/tmp/cred"},
		{URL: "file:///tmp/tether", CredentialFile: "/tmp/cred"},
		{URL: "https://tether.example/?token=x", CredentialFile: "/tmp/cred"},
		{URL: "https://tether.example"},
		{URL: "https://tether.example", CredentialFile: "/tmp/cred", PollSeconds: -1},
	} {
		if c.validate() == nil {
			t.Errorf("invalid mailbox accepted: %+v", c)
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	p := filepath.Join(t.TempDir(), "agent.json")
	if err = os.WriteFile(p, []byte(`{"mailbox":{"url":"https://tether.example/","credential_file":"~/bas.credential","poll_seconds":45}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SURFACE_AGENT_CONFIG", p)
	cfg, err := LoadConfig("test")
	if err != nil {
		t.Fatal(err)
	}
	if m := cfg.Mailbox; m == nil || m.URL != "https://tether.example" || m.CredentialFile != filepath.Join(home, "bas.credential") || m.PollSeconds != 45 {
		t.Fatalf("mailbox: %+v", cfg.Mailbox)
	}
}
