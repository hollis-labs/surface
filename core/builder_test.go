package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// The Interaction Builder on the real kernel: a saved flow arranges the desk, its setup
// reaches the agent's context, a refinement is a new version and a restore brings an older
// one back. Milestone 6, without the model: the calls the agent would make, over the app's
// own HTTP API.
func TestBuilderFlowOnTheRealKernel(t *testing.T) {
	t.Setenv("SURFACE_AGENT_CONFIG", filepath.Join(t.TempDir(), "absent.json"))
	ctx := context.Background()
	app, err := New(ctx, Options{Name: "buildertest", Title: "Builder test", DataDir: t.TempDir(),
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
	tool := func(name, input string) {
		t.Helper()
		if code, body := call("POST", "/api/tools/"+name, input); code != 200 {
			t.Fatalf("%s: %d %s", name, code, body)
		}
	}
	type flow struct {
		Name    string `json:"name"`
		Version int    `json:"version"`
		Layout  struct {
			Ratio   float64 `json:"ratio"`
			Windows []struct {
				View string `json:"view"`
			} `json:"windows"`
			Setup map[string]any `json:"setup"`
		} `json:"layout"`
		Versions []struct {
			Version int    `json:"version"`
			Note    string `json:"note"`
		} `json:"versions"`
	}
	getFlow := func(name string) flow {
		t.Helper()
		code, body := call("GET", "/api/m/builder/flows/"+name, "")
		if code != 200 {
			t.Fatalf("flow %s: %d %s", name, code, body)
		}
		var f flow
		if err := json.Unmarshal([]byte(body), &f); err != nil {
			t.Fatal(err)
		}
		return f
	}
	views := func() []string {
		t.Helper()
		_, body := call("GET", "/api/desk", "")
		var d struct {
			Wins []struct {
				View string `json:"view"`
			} `json:"wins"`
		}
		_ = json.Unmarshal([]byte(body), &d)
		var out []string
		for _, w := range d.Wins {
			out = append(out, w.View)
		}
		return out
	}
	agentContext := func() (slot, layout, mode, block string) {
		t.Helper()
		_, body := call("GET", "/api/agent/context", "")
		var a struct{ Slot, Layout, Mode, Block string }
		if err := json.Unmarshal([]byte(body), &a); err != nil {
			t.Fatal(err)
		}
		return a.Slot, a.Layout, a.Mode, a.Block
	}

	// The Builder window is open, as when the owner describes a flow; the agent adds a
	// flow-only reflex, then saves the flow.
	tool("desk_open", `{"view":"builder"}`)
	tool("agent_reflex_put", `{"name":"research.cite","trigger_kind":"text","trigger":{"pattern":"(?i)summar"},"action_kind":"remind","action":{"body":"Cite each source."},"enabled":false}`)
	tool("builder_flow_save", `{"name":"Research","goal":"research a topic and keep notes",
		"arrange":{"mode":"tile","ratio":0.6,"windows":[{"view":"browser","args":{"url":"https://example.com/"}},{"view":"drafts","focus":true},{"view":"scratch"}]},
		"mode":"research","instructions":"Quote and link every source.","reflexes":["research.cite"]}`)

	// The desk is arranged as the flow says, without the Builder window.
	if got := strings.Join(views(), ","); got != "browser,drafts,scratch" {
		t.Fatalf("desk after save: %s", got)
	}
	f := getFlow("research")
	if f.Version != 1 || f.Layout.Ratio != 0.6 || len(f.Layout.Windows) != 3 || f.Layout.Setup["mode"] != "research" {
		t.Fatalf("saved flow: %+v", f)
	}

	// Its setup reaches the agent: the layout and mode, its instructions in the block, and
	// the slot of the window in front.
	slot, layout, mode, block := agentContext()
	if layout != "research" || mode != "research" || slot != "drafts" || !strings.Contains(block, "For this layout: Quote and link every source.") {
		t.Fatalf("context: slot %q layout %q mode %q\n%s", slot, layout, mode, block)
	}

	// A refinement is version 2; what it does not give is kept.
	tool("builder_flow_save", `{"name":"research","note":"draft first","arrange":{"ratio":0.5,"windows":[{"view":"drafts","focus":true},{"view":"browser"}]}}`)
	f = getFlow("research")
	if f.Version != 2 || len(f.Layout.Windows) != 2 || f.Layout.Windows[0].View != "drafts" || f.Layout.Setup["instructions"] != "Quote and link every source." {
		t.Fatalf("refined flow: %+v", f)
	}
	if got := strings.Join(views(), ","); got != "drafts,browser" {
		t.Fatalf("desk after refine: %s", got)
	}

	// Restoring version 1 brings its arrangement back, as version 3.
	tool("builder_flow_restore", `{"name":"research","version":1}`)
	f = getFlow("research")
	if f.Version != 3 || len(f.Layout.Windows) != 3 || f.Layout.Windows[0].View != "browser" || f.Layout.Ratio != 0.6 {
		t.Fatalf("restored flow: %+v", f)
	}
	if len(f.Versions) != 3 || f.Versions[0].Note != "restored version 1" {
		t.Fatalf("versions: %+v", f.Versions)
	}
	if got := strings.Join(views(), ","); got != "browser,drafts,scratch" {
		t.Fatalf("desk after restore: %s", got)
	}
	if _, layout, _, block = agentContext(); layout != "research" || !strings.Contains(block, "Quote and link every source.") {
		t.Fatalf("context after restore: %s\n%s", layout, block)
	}

	// Opened again later from the saved flows, it puts the same desk back.
	tool("desk_arrange", `{"windows":[{"view":"scratch"}]}`)
	if code, body := call("POST", "/api/m/builder/flows/research/open", ""); code != 200 {
		t.Fatalf("open: %d %s", code, body)
	}
	if got := strings.Join(views(), ","); got != "browser,drafts,scratch" {
		t.Fatalf("desk after open: %s", got)
	}
}
