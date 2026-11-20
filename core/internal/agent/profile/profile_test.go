package profile

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/hollis-labs/libs/agent/agentdir"
	"github.com/hollis-labs/nanite/agent/authored"
)

func TestLoadReadsAndChecksAProfile(t *testing.T) {
	p, err := Load("sample", "Sample", os.DirFS("testdata/sample"))
	if err != nil || p == nil {
		t.Fatalf("load: %v %v", p, err)
	}
	if p.Name != "sample-profile" || !strings.Contains(p.Body, "SAMPLE-PROFILE-MARKER") || strings.Contains(p.Body, "schema_version") {
		t.Errorf("agent.md: name %q body %q", p.Name, p.Body)
	}
	if len(p.Context) != 1 || p.Context[0].Name != "widgets.md" || !slices.Equal(p.Skills, []string{"greet"}) ||
		!slices.Equal(p.Variants, []string{"checker"}) || !slices.Equal(p.Tools, []string{"echo"}) || !p.Steering {
		t.Errorf("parts: %+v", p)
	}
	for name := range p.files {
		if strings.HasPrefix(name, "evals/") || name == "README.md" || name == "steering.json" {
			t.Errorf("%s is staged", name)
		}
	}
}

func TestAProfileMayNotCarryWhatTheAppOwns(t *testing.T) {
	agent := mustRead(t, "testdata/sample/agent.md")
	for _, f := range []string{"host.yaml", ".mcp.json", "permissions.yaml", "providers.yaml", "identity.yaml"} {
		_, err := Load("x", "X", fstest.MapFS{"agent.md": {Data: agent}, f: {Data: []byte("{}")}})
		if err == nil || !strings.Contains(err.Error(), f) {
			t.Errorf("%s: %v", f, err)
		}
	}
	if _, err := Load("x", "X", fstest.MapFS{"agent.md": {Data: agent}, "notes/a.md": {Data: []byte("a")}}); err == nil {
		t.Error("an unknown directory was accepted")
	}
}

func TestAnEmptySlotIsNoProfile(t *testing.T) {
	for name, fsys := range map[string]fstest.MapFS{
		"empty":  {},
		"readme": {"README.md": {Data: []byte("# Coming")}},
		"evals":  {"evals/a.yaml": {Data: []byte("x: 1")}},
	} {
		if p, err := Load("x", "X", fsys); p != nil || err != nil {
			t.Errorf("%s: %v %v", name, p, err)
		}
	}
	if p, err := Load("x", "X", nil); p != nil || err != nil {
		t.Errorf("nil: %v %v", p, err)
	}
	if _, err := Load("x", "X", fstest.MapFS{"context/a.md": {Data: []byte("a")}}); err == nil || !strings.Contains(err.Error(), "agent.md") {
		t.Errorf("files without agent.md: %v", err)
	}
}

func TestBadPartsAreRefusedWithTheFile(t *testing.T) {
	agent := mustRead(t, "testdata/sample/agent.md")
	for name, c := range map[string]struct {
		file, body, want string
	}{
		"builtin variant name": {"variants/worker.md", "---\ndescription: x\n---\nDo it.", "worker"},
		"variant provider":     {"variants/fast.md", "---\nprovider: other\nmodel: m\n---\nDo it.", "provider"},
		"unknown variant key":  {"variants/odd.md", "---\nwings: 2\n---\nDo it.", "wings"},
		"empty variant":        {"variants/blank.md", "---\ndescription: x\n---\n", "variants/blank.md"},
		"bad steering":         {"steering.json", `{"reflexes":[{"name":"Bad Name"}]}`, "steering.json"},
		"bad tool":             {"tools/echo/tool.yaml", "name: echo\nkind: rocket\nentry: run.sh\n", "tools/echo/tool.yaml"},
	} {
		_, err := Load("x", "X", fstest.MapFS{"agent.md": {Data: agent}, c.file: {Data: []byte(c.body)}})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := Load("x", "X", fstest.MapFS{"agent.md": {Data: []byte("---\nname: x\n---\n")}}); err == nil {
		t.Error("an agent.md that agentdef refuses was accepted")
	}
}

// The staged directory is the seat with the profile laid over it, and Nanite's own loaders
// read it; staging the seat alone takes the profile away again.
func TestStageLaysTheProfileOverTheSeat(t *testing.T) {
	seat := filepath.Join(t.TempDir(), "seat")
	write(t, seat, "host.yaml", standInHost)
	write(t, seat, "agent.md", strings.Replace(string(mustRead(t, "testdata/sample/agent.md")), "You are the sample profile. Say SAMPLE-PROFILE-MARKER when asked who you are.", "You are the app's own agent. SEAT-MARKER.", 1))
	write(t, seat, "context/standing.md", "The owner's rules.")
	write(t, seat, "context/.history/standing-1.md", "old rules")
	write(t, seat, "skills/greet/SKILL.md", "---\nname: greet\ndescription: the seat's greeting\n---\nSEAT-GREET")
	write(t, seat, "skills/plan/SKILL.md", "---\nname: plan\ndescription: plan\n---\nPlan it.")

	p, err := Load("sample", "Sample", os.DirFS("testdata/sample"))
	if err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(t.TempDir(), "staged")
	if err := Stage(stage, seat, p); err != nil {
		t.Fatal(err)
	}
	// One definition: the seat's front matter, the profile's instructions under it.
	snap, err := agentdir.Load(stage)
	if err != nil {
		t.Fatalf("agentdir does not load the staged directory: %v", err)
	}
	agent := string(mustRead(t, filepath.Join(stage, "agent.md")))
	if !strings.Contains(agent, "SEAT-MARKER") || !strings.Contains(agent, "## Profile: Sample") || !strings.Contains(agent, "SAMPLE-PROFILE-MARKER") ||
		strings.Count(agent, "definition_id:") != 1 {
		t.Errorf("agent.md: %s", agent)
	}
	var ctxNames []string
	for _, c := range snap.Context {
		ctxNames = append(ctxNames, c.Name)
	}
	if !slices.Equal(ctxNames, []string{"profile.widgets.md", "standing.md"}) {
		t.Errorf("context: %v", ctxNames)
	}
	if greet := string(mustRead(t, filepath.Join(stage, "skills/greet/SKILL.md"))); strings.Contains(greet, "SEAT-GREET") {
		t.Error("the profile's skill did not replace the seat's")
	}
	if _, err := os.Stat(filepath.Join(stage, "skills/plan/SKILL.md")); err != nil {
		t.Error("the seat's own skill is gone")
	}
	if _, err := os.Stat(filepath.Join(stage, "context/.history")); err == nil {
		t.Error("the standing history was staged")
	}
	if v, err := authored.LoadVariants(stage); err != nil || v["checker"].Name != "checker" {
		t.Errorf("variants: %v %v", v, err)
	}
	if tools, err := authored.LoadTools(stage); err != nil || tools["echo"].Name != "echo" {
		t.Errorf("tool packages: %v %v", tools, err)
	}

	// The seat alone again: the profile's files go, the seat's come back as they are.
	if err := Stage(stage, seat, nil); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, filepath.Join(stage, "agent.md"))); got != string(mustRead(t, filepath.Join(seat, "agent.md"))) {
		t.Errorf("agent.md is not the seat's: %s", got)
	}
	for _, gone := range []string{"context/profile.widgets.md", "variants", "tools"} {
		if _, err := os.Stat(filepath.Join(stage, gone)); err == nil {
			t.Errorf("%s stayed", gone)
		}
	}
	if greet := string(mustRead(t, filepath.Join(stage, "skills/greet/SKILL.md"))); !strings.Contains(greet, "SEAT-GREET") {
		t.Error("the seat's skill did not come back")
	}
	// An edit to the seat reaches the next staging.
	write(t, seat, "context/standing.md", "New rules.")
	if err := Stage(stage, seat, p); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, filepath.Join(stage, "context/standing.md"))); got != "New rules." {
		t.Errorf("standing: %q", got)
	}
}

// Every profile a module in this repository ships loads as the core will load it.
func TestEveryModuleProfileValidates(t *testing.T) {
	dirs, err := filepath.Glob(filepath.Join("..", "..", "..", "..", "modules", "*", "profile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range dirs {
		id := filepath.Base(filepath.Dir(dir))
		t.Run(id, func(t *testing.T) {
			p, err := Load(id, id, os.DirFS(dir))
			if err != nil {
				t.Fatal(err)
			}
			if p == nil {
				t.Logf("%s declares an empty profile slot", id)
			}
		})
	}
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	f := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, f string) []byte {
	t.Helper()
	b, err := os.ReadFile(f) //nolint:gosec // a test fixture
	if err != nil {
		t.Fatal(err)
	}
	return b
}
