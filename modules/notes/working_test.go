package notes

import (
	"strings"
	"testing"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
)

func TestTheAgentDoesNotWriteANoteTheOwnerIsTypingIn(t *testing.T) {
	h := moduletest.Start(t, &mod{})
	if _, err := h.Tool("notes_write", `{"path":"ideas","text":"# Ideas\n\none\n","open":true}`); err != nil {
		t.Fatal(err)
	}
	w, ok := h.DeskState.Focused(t.Context())
	if !ok {
		t.Fatal("the note did not open")
	}
	h.DeskState.SetWorking(w.ID, false, true)
	for _, call := range []struct{ tool, in string }{
		{"notes_edit", `{"path":"ideas.md","find":"one","replace":"1"}`},
		{"notes_write", `{"path":"ideas","text":"replaced","replace":true}`},
	} {
		if _, err := h.ToolAs(module.ActorAgent, call.tool, call.in); err == nil || !strings.Contains(err.Error(), "editing this note") {
			t.Errorf("%s by the agent while the owner types: %v", call.tool, err)
		}
	}
	// The owner's own call, and the agent's once the owner is done, go through.
	if _, err := h.ToolAs(module.ActorOwner, "notes_edit", `{"path":"ideas.md","find":"one","replace":"1"}`); err != nil {
		t.Errorf("the owner's edit: %v", err)
	}
	h.DeskState.SetWorking(w.ID, false, false)
	if _, err := h.ToolAs(module.ActorAgent, "notes_edit", `{"path":"ideas.md","find":"1","replace":"one"}`); err != nil {
		t.Errorf("the agent's edit after the owner finished: %v", err)
	}
}
