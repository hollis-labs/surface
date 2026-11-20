package workbench

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Workbench's own profile (profile/): the Agent Architect, the agent the app uses when the
// Workbench leads. It is a module profile: a Nanite agent directory without host.yaml or
// .mcp.json (the app owns the runtime), with steering.json and evals/ at its root.
func TestProfileValidates(t *testing.T) {
	tree, suites := Tree{}, map[string][]byte{}
	err := filepath.WalkDir("profile", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p) //nolint:gosec // G304: walking the repo's own profile directory
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(strings.TrimPrefix(p, "profile"+string(filepath.Separator)))
		switch {
		case rel == "steering.json":
			tree[steeringFile] = b
		case strings.HasPrefix(rel, "evals/"):
			suites[strings.TrimPrefix(rel, "evals/")] = b
		default:
			tree[rel] = b
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"host.yaml", ".mcp.json", "permissions.yaml", "providers.yaml"} {
		if _, ok := tree[f]; ok {
			t.Fatalf("a module profile does not carry %s: the app owns it", f)
		}
	}
	// The app supplies the host profile; a minimal embedded one stands in for validation.
	tree["host.yaml"] = []byte("kind: embedded\nlifetime: application\napplication_id: workbench\n")
	rep := Validate(tree)
	for _, p := range rep.Problems {
		t.Errorf("%s %s: %s", p.Severity, p.File, p.Message)
	}
	if !rep.OK {
		t.Fatal("the profile does not validate")
	}
	if rep.Summary == nil || len(rep.Summary.Skills) != 10 || len(rep.Summary.Variants) != 3 || rep.Summary.Reflexes == 0 {
		t.Fatalf("summary: %+v", rep.Summary)
	}
	if len(suites) == 0 {
		t.Fatal("no eval suites")
	}
	for name, raw := range suites {
		if _, err := ParseSuite(raw); err != nil {
			t.Fatalf("suite %s: %v", name, err)
		}
	}
}
