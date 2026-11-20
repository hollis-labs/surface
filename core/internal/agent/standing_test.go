package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hollis-labs/surface/module"
)

// standingDir points the agent directory at a temporary one with a standing file in it, and
// returns the app's directory in it.
func standingDir(t *testing.T, text string) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("SURFACE_AGENTS_DIR", root)
	dir := filepath.Join(root, "surface-test")
	if err := os.MkdirAll(filepath.Join(dir, "context"), 0o750); err != nil {
		t.Fatal(err)
	}
	if text != "" {
		if err := os.WriteFile(standingPath(dir), []byte(text+"\n"), 0o644); err != nil { //nolint:gosec // a test file
			t.Fatal(err)
		}
	}
	return dir
}

func callOwn(t *testing.T, s *Service, name string, in any) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(in)
	v, err := s.ownTool(name).Call(context.Background(), raw)
	return asText(v), err
}

// standing_update writes the file whole and atomically, keeps the text it replaced in the
// history (where the runner's context/*.md does not read it) with a log line saying why,
// and refuses what it should.
func TestStandingUpdateKeepsHistoryAndWritesAtomically(t *testing.T) {
	dir := standingDir(t, "# Standing\n- Chief goes first.\n- Use the cards.")
	s, _, _, _ := setup(t, Options{})

	if lvl := s.ownTool("standing_update").Level; lvl != module.LevelWrite {
		t.Fatalf("standing_update is %s, want write", lvl)
	}
	if lvl := s.ownTool("standing_get").Level; lvl != module.LevelRead {
		t.Fatalf("standing_get is %s, want read", lvl)
	}

	for _, bad := range []map[string]string{
		{"text": "  ", "why": "x"},
		{"text": strings.Repeat("a", maxStandingChars+1), "why": "x"},
		{"text": "fine", "why": " "},
	} {
		if _, err := callOwn(t, s, "standing_update", bad); err == nil {
			t.Errorf("accepted %d characters with why %q", len(bad["text"]), bad["why"])
		}
	}
	if b, _ := os.ReadFile(standingPath(dir)); !strings.Contains(string(b), "Use the cards.") {
		t.Fatalf("a refused update changed the file: %q", b)
	}

	next := "# Standing\n- Chief goes first.\n- Questions go on cards.\n- Log friction."
	got, err := callOwn(t, s, "standing_update", map[string]string{"text": next, "why": "the owner\nsaid cards"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"+2 -1 lines", "- - Use the cards.", "+ - Questions go on cards.", "+ - Log friction.", ".history/standing-"} {
		if !strings.Contains(got, want) {
			t.Errorf("the summary has no %q: %s", want, got)
		}
	}
	if b, _ := os.ReadFile(standingPath(dir)); string(b) != next+"\n" {
		t.Fatalf("standing.md: %q", b)
	}
	if fi, _ := os.Stat(standingPath(dir)); fi.Mode().Perm() != 0o644 {
		t.Errorf("the file's mode changed to %v", fi.Mode().Perm())
	}
	hist := filepath.Join(dir, "context", ".history")
	kept, _ := filepath.Glob(filepath.Join(hist, "standing-*.md"))
	if len(kept) != 1 {
		t.Fatalf("history: %v", kept)
	}
	if b, _ := os.ReadFile(kept[0]); !strings.Contains(string(b), "Use the cards.") {
		t.Fatalf("the kept version: %q", b)
	}
	logged, _ := os.ReadFile(filepath.Join(hist, "standing.log")) //nolint:gosec // the test's own directory
	if lines := strings.Split(strings.TrimSpace(string(logged)), "\n"); len(lines) != 1 ||
		!strings.Contains(lines[0], filepath.Base(kept[0])) || !strings.HasSuffix(lines[0], "\t+2 -1\tthe owner said cards") {
		t.Fatalf("standing.log: %q", logged)
	}
	// Nothing but standing.md is left in context/ (no temporary file), and the runner's
	// reading of the directory does not pick up the history.
	entries, _ := os.ReadDir(filepath.Join(dir, "context"))
	for _, e := range entries {
		if e.Name() != "standing.md" && e.Name() != ".history" {
			t.Errorf("left in context/: %s", e.Name())
		}
	}

	// A second update keeps the second version; the same text again changes nothing.
	if _, err = callOwn(t, s, "standing_update", map[string]string{"text": next + "\n- One more.", "why": "two"}); err != nil {
		t.Fatal(err)
	}
	if got, _ = callOwn(t, s, "standing_update", map[string]string{"text": next + "\n- One more.", "why": "same"}); !strings.Contains(got, "unchanged") {
		t.Errorf("the same text: %s", got)
	}
	if kept, _ = filepath.Glob(filepath.Join(hist, "standing-*.md")); len(kept) != 2 {
		t.Fatalf("history after two updates: %v", kept)
	}

	var st StandingState
	got, err = callOwn(t, s, "standing_get", map[string]any{})
	if err != nil || json.Unmarshal([]byte(got), &st) != nil || !strings.HasSuffix(st.Text, "- One more.") || !st.InSystemPrompt {
		t.Fatalf("standing_get: %s %v", got, err)
	}
}

// A first standing_update, with no file yet, creates it and keeps no history version.
func TestStandingUpdateCreatesTheFile(t *testing.T) {
	dir := standingDir(t, "")
	s, _, _, _ := setup(t, Options{})
	if _, err := callOwn(t, s, "standing_update", map[string]string{"text": "- Chief goes first.", "why": "first"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(standingPath(dir)); string(b) != "- Chief goes first.\n" {
		t.Fatalf("standing.md: %q", b)
	}
	if kept, _ := filepath.Glob(filepath.Join(dir, "context", ".history", "standing-*.md")); len(kept) != 0 {
		t.Fatalf("a version was kept of nothing: %v", kept)
	}
}

// The first owner turn in another slot asks once for the note on the slot left; an
// errand does not ask, nor count as the slot left.
func TestTheNoteOnLeavingASlotIsAskedOncePerSwitch(t *testing.T) {
	s, k, fe, _ := setup(t, Options{})
	ctx := context.Background()
	const ask = "You have just come from the"
	block := func(i int) string {
		var c string
		_ = json.Unmarshal(fe.Turns()[i].Context, &c)
		return c
	}
	send := func(text, auto string) {
		t.Helper()
		if _, err := sendTest(ctx, t, s, text, "c", auto); err != nil {
			t.Fatal(err)
		}
	}
	send("first", "") // notes; nothing before
	k.desk.focusOn("w2")
	send("an automatic turn in scratch", "auto") // not the owner's: no ask, and not counted
	send("now scratch", "")                      // the owner moved notes -> scratch
	send("still scratch", "")
	k.desk.focusOn("w1")
	send("back to notes", "")
	if strings.Contains(block(0), ask) || strings.Contains(block(1), ask) || strings.Contains(block(3), ask) {
		t.Fatalf("asked without a switch:\n%s\n%s\n%s", block(0), block(1), block(3))
	}
	if !strings.Contains(block(2), ask+" notes slot") || !strings.Contains(block(2), `context_note (slot "notes")`) {
		t.Fatalf("no ask after notes -> scratch: %s", block(2))
	}
	if !strings.Contains(block(4), ask+" scratch slot") {
		t.Fatalf("no ask after scratch -> notes: %s", block(4))
	}
}

// A primary slot's note over a day old, with the owner's messages there since, is flagged.
func TestAStaleNoteIsFlagged(t *testing.T) {
	s, _, fe, _ := setup(t, Options{})
	ctx := context.Background()
	if err := s.notes.Set(ctx, "notes", "Drafting the outline."); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)
	if _, err := s.k.DB().ExecContext(ctx, `UPDATE slot_notes SET updated_at = ? WHERE slot = 'notes'`, old); err != nil {
		t.Fatal(err)
	}
	for i := range staleTurns + 1 {
		if _, err := sendTest(ctx, t, s, "message", "c", ""); err != nil {
			t.Fatal(err)
		}
		var c string
		_ = json.Unmarshal(fe.Turns()[i].Context, &c)
		flagged := strings.Contains(c, "Your note on notes is from")
		if flagged != (i >= staleTurns) {
			t.Fatalf("turn %d flagged=%v: %s", i, flagged, c)
		}
	}
	// A fresh note is not.
	if err := s.notes.Set(ctx, "notes", "Outline done."); err != nil {
		t.Fatal(err)
	}
	if hint := s.staleNoteHint(ctx, "notes"); hint != "" {
		t.Fatalf("a fresh note flagged: %s", hint)
	}
}

// agent.compacted on the bus reaches the event reflexes (the seeded save-state), though the
// agent's own events are otherwise kept from them: the reminder rides the next message.
func TestACompactionRemindsTheAgentToSaveState(t *testing.T) {
	s, k, fe, _ := setup(t, Options{})
	ctx := context.Background()
	k.bus.Publish(TopicCompacted, map[string]any{"conversation_id": "c", "before": 120000, "after": 30000})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := sendTest(ctx, t, s, "go on", "c", ""); err != nil {
			t.Fatal(err)
		}
		var c string
		_ = json.Unmarshal(fe.Turns()[len(fe.Turns())-1].Context, &c)
		if strings.Contains(c, "Reminder: Older turns were just summarized.") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no save-state reminder: %s", c)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
