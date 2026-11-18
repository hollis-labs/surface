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

// ADE's overlays on the real kernel: the agent sees the anchors a view declared, points at
// one, and runs a guide whose progress the page reports back.
func TestAdeOverlaysOnTheRealKernel(t *testing.T) {
	t.Setenv("SURFACE_AGENT_CONFIG", filepath.Join(t.TempDir(), "absent.json"))
	ctx := context.Background()
	app, err := New(ctx, Options{Name: "adetest", Title: "ADE test", DataDir: t.TempDir(), Modules: []string{"scratch", "drafts"}})
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
	tool := func(name, input string) (int, string) {
		t.Helper()
		return call("POST", "/api/tools/"+name, input)
	}
	state := func() map[string]any {
		t.Helper()
		_, body := call("GET", "/api/ade", "")
		var s map[string]any
		if err := json.Unmarshal([]byte(body), &s); err != nil {
			t.Fatal(err)
		}
		return s
	}

	if code, body := tool("desk_open", `{"view":"drafts"}`); code != 200 {
		t.Fatalf("open: %d %s", code, body)
	}
	_, body := tool("desk_windows", `{}`)
	var wins struct{ Result string }
	_ = json.Unmarshal([]byte(body), &wins)
	var listing struct {
		Windows []struct{ ID, View string }
	}
	_ = json.Unmarshal([]byte(wins.Result), &listing)
	list := listing.Windows
	if len(list) != 1 {
		t.Fatalf("windows: %s", body)
	}
	win := list[0].ID

	// The page reports what the view declared; the agent sees it.
	if code, body := call("PUT", "/api/ade/anchors/"+win, `[{"name":"marks","label":"The marks"},{"name":"modes"}]`); code != 204 {
		t.Fatalf("anchors: %d %s", code, body)
	}
	if _, body := tool("ade_anchors", `{}`); !strings.Contains(body, `\"marks\"`) {
		t.Fatalf("ade_anchors: %s", body)
	}

	// Pointing at a declared anchor by view works; an undeclared one says what there is.
	if code, body := tool("ade_point", `{"targets":[{"view":"drafts","anchor":"marks"}],"style":"spotlight","note":"Your marks are here."}`); code != 200 || !strings.Contains(body, "The marks") {
		t.Fatalf("point: %d %s", code, body)
	}
	att, _ := state()["attention"].(map[string]any)
	if att["style"] != "spotlight" || att["note"] != "Your marks are here." {
		t.Fatalf("attention: %v", att)
	}
	if code, body := tool("ade_point", `{"targets":[{"view":"drafts","anchor":"nope"}]}`); code == 200 || !strings.Contains(body, "marks, modes") {
		t.Fatalf("bad anchor: %d %s", code, body)
	}
	if code, body := tool("ade_point", `{"targets":[{"view":"notes"}]}`); code == 200 || !strings.Contains(body, "no open window") {
		t.Fatalf("closed view: %d %s", code, body)
	}
	if code, body := tool("ade_point", `{"targets":[{"view":"drafts"}],"style":"arrow"}`); code == 200 || !strings.Contains(body, "from") {
		t.Fatalf("arrow without from: %d %s", code, body)
	}
	tool("ade_clear", `{}`)
	if state()["attention"] != nil {
		t.Fatal("clear left the attention")
	}

	// A guide: validated, put on screen, reported back, stopped.
	if code, body := tool("ade_guide_start", `{"title":"Tour","steps":[{"say":"x","await":{"kind":"open"}}]}`); code == 200 || !strings.Contains(body, "name the view") {
		t.Fatalf("guide without a view: %d %s", code, body)
	}
	if code, body := tool("ade_guide_start", `{"title":"Tour","steps":[{"say":"These are the modes.","point":[{"view":"drafts","anchor":"modes"}]},{"say":"Open Scratch.","await":{"kind":"open","view":"scratch"}}]}`); code != 200 {
		t.Fatalf("guide: %d %s", code, body)
	}
	g, _ := state()["guide"].(map[string]any)
	id, _ := g["id"].(string)
	steps, _ := g["steps"].([]any)
	if id == "" || len(steps) != 2 {
		t.Fatalf("guide state: %v", g)
	}
	call("POST", "/api/ade/guide", `{"guide":"`+id+`","index":1,"step":"s2","status":"running"}`)
	if _, body := tool("ade_guide_status", `{}`); !strings.Contains(body, `\"index\":1`) {
		t.Fatalf("status: %s", body)
	}
	call("POST", "/api/ade/guide", `{"guide":"`+id+`","index":1,"step":"s2","status":"done"}`)
	if state()["guide"] != nil {
		t.Fatal("a finished guide is still on")
	}
	if _, body := tool("ade_guide_status", `{}`); !strings.Contains(body, "done") {
		t.Fatalf("status after done: %s", body)
	}
}
