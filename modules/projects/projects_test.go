package projects

import (
	"context"
	"strings"
	"testing"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
)

func start(t *testing.T) (*moduletest.Host, *mod) {
	t.Helper()
	m := &mod{}
	h := moduletest.Start(t, m)
	for _, def := range []string{
		`{"name":"basalisk","about":"the desktop app","match":["basalisk","apps/basalisk-wt"],"torque":"PRJ-1","repo":"hollis-labs/basalisk","instructions":"Local Dev builds it."}`,
		`{"name":"tether","match":["hollis-labs/tether"],"tesseract":"project/tether"}`,
	} {
		if _, err := h.Tool("project_define", def); err != nil {
			t.Fatal(err)
		}
	}
	return h, m
}

func TestTheWindowInFrontChoosesTheProject(t *testing.T) {
	h, _ := start(t)
	ctx := context.Background()
	if s, _ := h.Around(); s != "" {
		t.Fatalf("nothing in front, nothing chosen: %q", s)
	}
	w, _ := h.DeskState.Open(ctx, "files", module.OpenOptions{Title: "~/dev/hollis-labs/apps/basalisk-wt/main.go"})
	_ = h.DeskState.Focus(ctx, w.ID)
	s, _ := h.Around()
	for _, want := range []string{"Project: basalisk (the desktop app)", `matches "apps/basalisk-wt"`, "Torque PRJ-1; repo hollis-labs/basalisk", "instructions for it: Local Dev builds it.", "none yet"} {
		if !strings.Contains(s, want) {
			t.Errorf("around lacks %q:\n%s", want, s)
		}
	}
	// A window that selects nothing: the project holds.
	w2, _ := h.DeskState.Open(ctx, "notes", module.OpenOptions{Title: "Groceries"})
	_ = h.DeskState.Focus(ctx, w2.ID)
	if s, _ = h.Around(); !strings.Contains(s, "Project: basalisk") || !strings.Contains(s, "still") {
		t.Fatalf("held: %s", s)
	}
}

func TestNotesAndSwitching(t *testing.T) {
	h, _ := start(t)
	if _, err := h.Tool("project_note", `{"note":"x"}`); err == nil {
		t.Fatal("a note with no project current")
	}
	if _, err := h.Tool("project_switch", `{"name":"nope"}`); err == nil || !strings.Contains(err.Error(), "basalisk, tether") {
		t.Fatalf("%v", err)
	}
	if _, err := h.Tool("project_switch", `{"name":"tether"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Tool("project_note", `{"note":"PR 600 waits on review."}`); err != nil {
		t.Fatal(err)
	}
	s, _ := h.Around()
	if !strings.Contains(s, "Project: tether") || !strings.Contains(s, "PR 600 waits on review.") || !strings.Contains(s, "you, by hand") {
		t.Fatalf("%s", s)
	}
	// Redefining keeps the note.
	if _, err := h.Tool("project_define", `{"name":"tether","about":"messaging"}`); err != nil {
		t.Fatal(err)
	}
	if s, _ = h.Around(); !strings.Contains(s, "PR 600") || !strings.Contains(s, "(messaging)") {
		t.Fatalf("%s", s)
	}
	if _, err := h.Tool("project_switch", `{"name":""}`); err != nil {
		t.Fatal(err)
	}
	if s, _ = h.Around(); s != "" {
		t.Fatalf("cleared: %s", s)
	}
}

func TestADefinitionNeedsAName(t *testing.T) {
	h, _ := start(t)
	if _, err := h.Tool("project_define", `{"name":" "}`); err == nil {
		t.Fatal("saved a nameless project")
	}
}
