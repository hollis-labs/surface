package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	_ "github.com/hollis-labs/surface/modules/browser"
	_ "github.com/hollis-labs/surface/modules/builder"
	_ "github.com/hollis-labs/surface/modules/drafts"
	_ "github.com/hollis-labs/surface/modules/scratch"
)

// The agent's context follows the window in front, on the real kernel with real modules:
// milestone 5, end to end over the app's own HTTP API.
func TestAgentContextFollowsFocus(t *testing.T) {
	t.Setenv("SURFACE_AGENT_CONFIG", filepath.Join(t.TempDir(), "absent.json"))
	ctx := context.Background()
	app, err := New(ctx, Options{Name: "coretest", Title: "Core test", DataDir: t.TempDir(),
		Modules: []string{"scratch", "drafts", "browser", "builder"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
	srv := httptest.NewServer(app.Handler())
	defer srv.Close()

	call := func(method, path, body string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	type assembly struct {
		Slot      string   `json:"slot"`
		Why       string   `json:"why"`
		TopOfMind []string `json:"top_of_mind"`
		Layout    string   `json:"layout"`
		Mode      string   `json:"mode"`
		Block     string   `json:"block"`
	}
	context := func() assembly {
		t.Helper()
		code, body := call("GET", "/api/agent/context", "")
		if code != 200 {
			t.Fatalf("context: %d %s", code, body)
		}
		var a assembly
		if err := json.Unmarshal([]byte(body), &a); err != nil {
			t.Fatal(err)
		}
		return a
	}
	tool := func(name, input string) {
		t.Helper()
		if code, body := call("POST", "/api/tools/"+name, input); code != 200 {
			t.Fatalf("%s: %d %s", name, code, body)
		}
	}

	// Nothing open: the general slot.
	if a := context(); a.Slot != "general" {
		t.Fatalf("empty desk: %+v", a)
	}

	// A draft in front: the drafts slot, its tools, and the draft itself in the block.
	tool("drafts_create", `{"title":"Pitch","body":"Surface is the ADE framework.\n"}`)
	a := context()
	if a.Slot != "drafts" || !slices.Contains(a.TopOfMind, "drafts_edit") || !strings.Contains(a.Block, "Surface is the ADE framework.") {
		t.Fatalf("draft in front: %+v", a)
	}

	// Scratch in front: everything changes with it.
	tool("scratch_add", `{"text":"call the printer"}`)
	tool("desk_open", `{"view":"scratch"}`)
	a = context()
	if a.Slot != "scratch" || slices.Contains(a.TopOfMind, "drafts_edit") || !strings.Contains(a.Block, "call the printer") {
		t.Fatalf("scratch in front: %+v", a)
	}

	// The builder slot puts the agent's own reflex tools top of mind.
	tool("desk_open", `{"view":"builder"}`)
	if a = context(); a.Slot != "builder" || !slices.Contains(a.TopOfMind, "agent_reflex_put") {
		t.Fatalf("builder in front: %+v", a)
	}

	// A named arrangement with a setup: the layout's instructions and mode reach the block.
	tool("desk_arrange", `{"name":"writing","windows":[{"view":"drafts","focus":true}],"setup":{"mode":"writing"}}`)
	tool("desk_layout_save", `{"name":"writing","setup":{"mode":"writing","instructions":"Keep the owner's voice."}}`)
	a = context()
	if a.Layout != "writing" || a.Mode != "writing" || !strings.Contains(a.Block, "For this layout: Keep the owner's voice.") {
		t.Fatalf("layout setup: %+v", a)
	}

	// No model configured: a message is refused with where to configure one.
	if code, body := call("POST", "/api/agent/turns", `{"text":"hi"}`); code != 503 || !strings.Contains(body, "absent.json") {
		t.Fatalf("turn without a model: %d %s", code, body)
	}
}
