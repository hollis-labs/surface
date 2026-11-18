package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupProblemSaysWhyTheKeyFileFails(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "agent-key")
	s := &Service{cfg: FileConfig{Path: filepath.Join(dir, "agent.json"), Model: "m", GatewayURL: "https://gw.example", KeyFile: key}}

	if got := s.SetupProblem(); !strings.Contains(got, "does not exist") {
		t.Fatalf("missing key: %q", got)
	}
	if err := os.WriteFile(key, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := s.SetupProblem(); !strings.Contains(got, "is empty") {
		t.Fatalf("empty key: %q", got)
	}
	if err := os.WriteFile(key, []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(key, 0o000); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() != 0 {
		if got := s.SetupProblem(); !strings.Contains(got, "permission denied") {
			t.Fatalf("unreadable key: %q", got)
		}
	}
	if err := os.Chmod(key, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := s.SetupProblem(); got != "" {
		t.Fatalf("readable key: %q", got)
	}
	if got := (&Service{cfg: FileConfig{Path: "x", Model: "m"}}).SetupProblem(); !strings.Contains(got, "gateway") {
		t.Fatalf("no gateway: %q", got)
	}
	if got := (&Service{custom: true}).SetupProblem(); got != "" {
		t.Fatalf("a custom engine is not the config's: %q", got)
	}
}
