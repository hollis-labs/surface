package scaffolding_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSurfaceAgentDirectory(t *testing.T) {
	dir := renderSurface(t, "surface-desk", map[string]any{"repo_name": "desk-agents"})
	config := readFile(t, filepath.Join(dir, "main.go"))
	for _, want := range []string{`"agents":{"dir":"agents/"}`, `"agent-directory"`} {
		if !strings.Contains(config, want) {
			t.Fatalf("missing config %s", want)
		}
	}
	if !strings.Contains(readFile(t, filepath.Join(dir, "internal/agentdir/agentdir.go")), "agents.Resolve(app.Config, configFile,") {
		t.Fatal("resolver bypasses Substrate helper")
	}
	agent := readFile(t, filepath.Join(dir, "agents/assistant/agent.md"))
	for _, want := range []string{`schema_version: "2"`, "name: assistant", "mode: durable", "definition_id: def:desk-agents-assistant"} {
		if !strings.Contains(agent, want) {
			t.Fatalf("agent.md lacks %s", want)
		}
	}
	host := readFile(t, filepath.Join(dir, "agents/assistant/host.yaml"))
	for _, want := range []string{"kind: embedded", "lifetime: application", "application_id: desk-agents", "runtime:"} {
		if !strings.Contains(host, want) {
			t.Fatalf("host.yaml lacks %s", want)
		}
	}
	if !strings.Contains(readFile(t, filepath.Join(dir, "agents/README.md")), "Nothing in the app opens these files yet") {
		t.Fatal("directory liveness note missing")
	}
	checkWithNanite(t, filepath.Join(dir, "agents/assistant"))
	plain := renderSurface(t, "surface-app", map[string]any{"repo_name": "plain-agents"})
	if _, err := os.Stat(filepath.Join(plain, "agents")); !os.IsNotExist(err) {
		t.Fatal("plain app rendered agents directory")
	}
}
func TestSurfaceGeneratedResolverBuilds(t *testing.T) {
	dir := renderSurface(t, "surface-desk", map[string]any{"repo_name": "resolver-check"})
	fixture := t.TempDir()
	if err := os.CopyFS(filepath.Join(fixture, "agentdir"), os.DirFS(filepath.Join(dir, "internal/agentdir"))); err != nil {
		t.Fatal(err)
	}
	mod := fmt.Sprintf("module resolvercheck\n\ngo 1.26.11\n\nrequire github.com/hollis-labs/substrate %s\n", "v0.1.1-0.20261119145946-6a4bb696c942")
	if err := os.WriteFile(filepath.Join(fixture, "go.mod"), []byte(mod), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-mod=mod", "./...")
	cmd.Dir = fixture
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated resolver: %v\n%s", err, out)
	}
}
func checkWithNanite(t *testing.T, agentDir string) {
	t.Helper()
	bin := os.Getenv("FOLIO_NANITE")
	if bin == "" {
		t.Log("FOLIO_NANITE not set: the Nanite loader check is skipped")
		return
	}
	work := filepath.Join(t.TempDir(), "assistant")
	if err := os.CopyFS(work, os.DirFS(agentDir)); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "--offline", "--dir", work) //nolint:gosec // the test names the offline example binary
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Nanite offline agent.Open: %v\n%s", err, out)
	}
	t.Logf("Nanite embed-host --offline --dir <generated agent>: %s", strings.TrimSpace(string(out)))
}
