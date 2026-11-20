package agent

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The host runner does not read the agent's directory the way Nanite does: its standing
// text (agent.md's body and context/*.md) must still reach the system prompt.
func TestAgentDirTextReachesTheSystemPrompt(t *testing.T) {
	dir := t.TempDir()
	must := func(p, s string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	must(filepath.Join(dir, "agent.md"), "---\nname: desk\nrevision: \"1\"\n---\nGive the thing itself.\n")
	must(filepath.Join(dir, "context", "b-seats.md"), "Chief is agt_x.\n")
	must(filepath.Join(dir, "context", "a-standing.md"), "Chief goes first.\n")
	must(filepath.Join(dir, "context", "notes.txt"), "not markdown, not read")
	got := agentDirText(dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if strings.Contains(got, "name: desk") {
		t.Fatalf("front matter leaked: %q", got)
	}
	ia, ib, ic := strings.Index(got, "Give the thing itself."), strings.Index(got, "Chief goes first."), strings.Index(got, "Chief is agt_x.")
	if ia < 0 || ib < 0 || ic < 0 || ia >= ib || ib >= ic {
		t.Fatalf("missing or out of order: %q", got)
	}
	if strings.Contains(got, "not markdown") {
		t.Fatalf("a non-.md file was read: %q", got)
	}
	sys := systemPrompt("desk", joinNonEmpty(got, "extra from config"))
	if !strings.Contains(sys, "Chief goes first.") || !strings.HasSuffix(sys, "extra from config") {
		t.Fatalf("system prompt: %q", sys[len(sys)-200:])
	}
	if agentDirText(filepath.Join(dir, "missing"), slog.New(slog.NewTextHandler(io.Discard, nil))) != "" {
		t.Fatal("a missing directory gave text")
	}
}
