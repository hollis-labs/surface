package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func bindingFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, raw := range map[string][]byte{"agent.md": mustRead(t, "testdata/sample/agent.md"), "host.yaml": []byte(standInHost)} {
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
func TestBindingValidatesStandardAgentFolder(t *testing.T) {
	dir := bindingFixture(t)
	p, err := LoadBinding("sample", "Sample", dir)
	if err != nil || p.Name != "sample-profile" {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := LoadBinding("sample", "Sample", "relative"); err == nil {
		t.Fatal("relative path accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "host.yaml"), []byte("runtime: [invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBinding("sample", "Sample", dir); err == nil {
		t.Fatal("invalid host accepted")
	}
}
func TestBindingRefusesAuthorityFilesAndInvalidAuthoredParts(t *testing.T) {
	for _, name := range []string{".mcp.json", "providers.yaml", "identity.yaml", "permissions.yaml", "variants/bad.md", "steering.json"} {
		t.Run(name, func(t *testing.T) {
			dir := bindingFixture(t)
			if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, name), []byte("---\ninvalid: [\n---\nbody"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadBinding("sample", "Sample", dir); err == nil {
				t.Fatalf("%s accepted", name)
			}
		})
	}
}
func TestBindingRefusesSymlinkAndMissingDefinition(t *testing.T) {
	dir := bindingFixture(t)
	if err := os.Remove(filepath.Join(dir, "agent.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBinding("sample", "Sample", dir); err == nil || !strings.Contains(err.Error(), "agent.md") {
		t.Fatalf("%v", err)
	}
	if err := os.Symlink(filepath.Join(bindingFixture(t), "agent.md"), filepath.Join(dir, "agent.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBinding("sample", "Sample", dir); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestBindingStagesSnapshotWithoutRuntime(t *testing.T) {
	dir := bindingFixture(t)
	// Put runtime keys in a valid runtime mapping.
	if err := os.WriteFile(filepath.Join(dir, "host.yaml"), []byte("kind: embedded\nlifetime: application\napplication_id: binding-test\nruntime:\n  model: ignored-model\n  max_steps: 3\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "steering.json"), mustRead(t, "testdata/sample/steering.json"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := LoadBinding("sample", "Sample", dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(p.IgnoredRuntime, ",") != "max_steps,model" {
		t.Fatalf("%v", p.IgnoredRuntime)
	}
	root := t.TempDir()
	first, err := p.StageBinding(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(first, "host.yaml")); !os.IsNotExist(err) {
		t.Fatalf("runtime staged: %v", err)
	}
	if _, err = os.Stat(filepath.Join(first, "steering.json")); err != nil {
		t.Fatal(err)
	}
	raw := mustRead(t, filepath.Join(dir, "agent.md"))
	if err = os.WriteFile(filepath.Join(dir, "agent.md"), append(raw, []byte("\nCHANGED")...), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := LoadBinding("sample", "Sample", dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := changed.StageBinding(root)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || strings.Contains(string(mustRead(t, filepath.Join(first, "agent.md"))), "CHANGED") {
		t.Fatal("running snapshot was overwritten")
	}
}
