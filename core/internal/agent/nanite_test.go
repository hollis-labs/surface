package agent

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	chat "github.com/hollis-labs/libs/agent/chat"
	nanite "github.com/hollis-labs/nanite/agent"
	_ "modernc.org/sqlite"
)

func TestAgentDirDefaultsBesideTheConfig(t *testing.T) {
	t.Setenv("SURFACE_AGENTS_DIR", "")
	c := FileConfig{Path: filepath.Join("/etc", "desk", "agent.json")}
	if got, want := c.AgentDir("desk"), filepath.Join("/etc", "desk", "agents", "desk"); got != want {
		t.Fatalf("default: got %s, want %s", got, want)
	}
	c.Agents.Dir = "/srv/agents"
	if got, want := c.AgentDir("desk"), filepath.Join("/srv/agents", "desk"); got != want {
		t.Fatalf("configured: got %s, want %s", got, want)
	}
	t.Setenv("SURFACE_AGENTS_DIR", "/tmp/a")
	if got, want := c.AgentDir("desk"), filepath.Join("/tmp/a", "desk"); got != want {
		t.Fatalf("env: got %s, want %s", got, want)
	}
}

// The starter is a directory Nanite opens, it is written once, and it says nothing about a
// layout: those instructions travel in the turn's context.
func TestPlantSeatWritesAStarterNaniteOpensAndNeverOverwrites(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agents", "desk")
	planted, err := plantSeat(dir, "desk")
	if err != nil || !planted {
		t.Fatalf("plant: %v %v", planted, err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "agent.md")) //nolint:gosec // a file this test wrote
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{"name: desk", "card_ask", "context_note"} {
		if !strings.Contains(text, want) {
			t.Errorf("agent.md does not mention %q", want)
		}
	}
	for _, not := range []string{"{{app}}", "layout", "ask_user"} {
		if strings.Contains(text, not) {
			t.Errorf("agent.md mentions %q", not)
		}
	}
	a, err := nanite.Open(context.Background(), nanite.Options{Dir: dir, DataDir: filepath.Join(t.TempDir(), "state"), GlobalSkillsDir: "-"})
	if err != nil {
		t.Fatalf("Nanite does not open the starter: %v", err)
	}
	info, err := a.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	runtime := info.Definition.Profile.Runtime
	if runtime.CompactAt != 110000 || runtime.CompactKeepTokens != 22000 {
		t.Fatalf("starter compaction: got %d/%d, want 110000/22000", runtime.CompactAt, runtime.CompactKeepTokens)
	}
	_ = a.Close()

	if err = os.WriteFile(filepath.Join(dir, "agent.md"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	if planted, err = plantSeat(dir, "desk"); err != nil || planted {
		t.Fatalf("second plant: %v %v", planted, err)
	}
	if body, _ = os.ReadFile(filepath.Join(dir, "agent.md")); string(body) != "mine" { //nolint:gosec // a file this test wrote
		t.Fatal("an edited agent.md was overwritten")
	}
}

func TestBuiltinGroupsAreNoneByDefaultAndChecked(t *testing.T) {
	g, err := builtinGroups(nil)
	if err != nil || g == nil || len(g) != 0 {
		t.Fatalf("default: %v %v (must be an empty list, not nil: nil is Nanite's own default)", g, err)
	}
	if g, err = builtinGroups([]string{"files-read", " Skills "}); err != nil || len(g) != 2 {
		t.Fatalf("two groups: %v %v", g, err)
	}
	if g, err = builtinGroups([]string{"all"}); err != nil || len(g) != len(nanite.AllBuiltins()) {
		t.Fatalf("all: %v %v", g, err)
	}
	if _, err = builtinGroups([]string{"everything"}); err == nil {
		t.Fatal("an unknown group was accepted")
	}
}

func TestAStandingGrantIsFoundByItsToolAndAgent(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err = openGrants(ctx, db); err != nil {
		t.Fatal(err)
	}
	if ok, _ := granted(ctx, db, "desk", "notes_write"); ok {
		t.Fatal("granted before any grant")
	}
	// The descriptor is the runner's: the tool is read from it.
	err = grants{db: db}.Grant(ctx, chat.Grant{Agent: "desk", ApprovalID: "a1", Reason: "r",
		Descriptor: []byte(`{"tool":"notes_write","standing":true,"description":"Write a note."}`)})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		agent, tool string
		want        bool
	}{{"desk", "notes_write", true}, {"desk", "notes_delete", false}, {"studio", "notes_write", false}, {"desk", "", false}} {
		if ok, err := granted(ctx, db, c.agent, c.tool); err != nil || ok != c.want {
			t.Errorf("granted(%q, %q) = %v, %v; want %v", c.agent, c.tool, ok, err, c.want)
		}
	}
}

func TestAskFirstCoversOneToolForTheTurnInProgress(t *testing.T) {
	var a asks
	a.set("c1", "desk_windows")
	if !a.has("c1", "desk_windows") || a.has("c1", "desk_open") || a.has("c2", "desk_windows") {
		t.Fatal("ask-first is for the routed tool of that conversation only")
	}
	a.set("c1", "") // the next turn matched no ask rule
	if a.has("c1", "desk_windows") {
		t.Fatal("ask-first outlived its turn")
	}
}
