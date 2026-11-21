package ops

import (
	"encoding/json"
	"testing"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
)

func TestAgentsRouteToolAndViewRefreshShareReadOnlySource(t *testing.T) {
	f := sessionFixture()
	m := &mod{caller: f}
	h := moduletest.Start(t, m)
	code, body := h.Do("GET", "/agents", "")
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	var result SessionsResult
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Sessions) != 3 {
		t.Fatalf("%+v", result)
	}
	if _, err := h.Tool("ops_agents", `{}`); err != nil {
		t.Fatal(err)
	}
	if f.calls["tether_agents_live"] != 1 {
		t.Fatalf("tool bypassed cache: %v", f.calls)
	}
	for _, tool := range m.Manifest().Tools {
		if tool.Name == "ops_agents" && tool.Level != module.LevelRead {
			t.Fatal("agents tool is not read-only")
		}
	}
	if m.sessionViewOpen(t.Context()) {
		t.Fatal("refresh without an open view")
	}
	w, err := h.DeskState.Open(t.Context(), "ops", module.OpenOptions{Args: map[string]any{"layout": "kanban"}})
	if err != nil {
		t.Fatal(err)
	}
	if m.sessionViewOpen(t.Context()) {
		t.Fatal("refresh on unrelated board tab")
	}
	if err = h.DeskState.SetArgs(t.Context(), w.ID, map[string]any{"layout": "agents"}); err != nil {
		t.Fatal(err)
	}
	if !m.sessionViewOpen(t.Context()) {
		t.Fatal("agents tab not observed")
	}
	wc, err := m.describeBoard(t.Context(), h, module.Window{Args: map[string]any{"layout": "agents", "parent": "unrelated"}})
	if err != nil || wc.Ref != "ops" || wc.Extra["layout"] != "agents" {
		t.Fatalf("%+v %v", wc, err)
	}
}
