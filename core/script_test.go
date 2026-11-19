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
	"time"

	"github.com/hollis-labs/surface/module"
)

// One script, run as a test and as a demo, on the real kernel with real modules.
func TestAScriptRunsAsATestAndAsADemo(t *testing.T) {
	t.Setenv("SURFACE_AGENT_CONFIG", filepath.Join(t.TempDir(), "absent.json"))
	ctx := context.Background()
	app, err := New(ctx, Options{Name: "scripttest", Title: "Script test", DataDir: t.TempDir(),
		Modules: []string{"scratch", "drafts", "browser", "builder"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
	srv := httptest.NewServer(app.Handler())
	defer srv.Close()
	call := func(name, input string) (int, string) {
		t.Helper()
		resp, perr := http.Post(srv.URL+"/api/tools/"+name, "application/json", strings.NewReader(input))
		if perr != nil {
			t.Fatal(perr)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	tour := `{"name":"tour","about":"A first look","steps":[
		{"say":"This is Scratch, for quick notes.","do":[{"tool":"desk_open","input":{"view":"scratch"}}],"expect":{"focused":"scratch"},"hold_ms":5},
		{"say":"The Builder composes ways of working.","do":[{"tool":"desk_open","input":{"view":"builder"}}],"point":[{"view":"builder"}],"zoom":true,"expect":{"open":["scratch","builder"],"focused":"builder"},"hold_ms":5},
		{"say":"Scratch is still there.","await":{"kind":"open","view":"scratch"},"expect":{"tool":{"tool":"desk_windows"},"contains":"scratch"},"hold_ms":5}]}`
	if code, body := call("script_save", tour); code != 200 {
		t.Fatalf("save: %d %s", code, body)
	}

	// A script may not write for the owner.
	if code, body := call("script_save", `{"name":"bad","steps":[{"say":"x","do":[{"tool":"drafts_create","input":{"title":"t"}}]}]}`); code == 200 || !strings.Contains(body, "look-only") {
		t.Fatalf("a writing script was saved: %d %s", code, body)
	}

	// As a test: every step passes, and the desk is put back as it was (empty).
	code, body := call("script_run", `{"name":"tour","mode":"test"}`)
	if code != 200 || !strings.Contains(body, `\"status\":\"passed\"`) && !strings.Contains(body, `"status":"passed"`) {
		t.Fatalf("test run: %d %s", code, body)
	}
	if n := len(app.k.Desk().Windows(ctx)); n != 0 {
		t.Errorf("the test left %d windows open", n)
	}

	// A step whose expectation does not hold fails, and says why.
	broken := `{"name":"broken","steps":[{"say":"Nothing opens","expect":{"focused":"scratch"}}]}`
	if code, body = call("script_save", broken); code != 200 {
		t.Fatal(body)
	}
	if _, body = call("script_run", `{"name":"broken","mode":"test"}`); !strings.Contains(body, "failed") || !strings.Contains(body, "expected scratch in front") {
		t.Errorf("a failing test: %s", body)
	}

	// As a demo: it runs on its own, announces each step, and ends.
	steps := make(chan map[string]any, 8)
	done := make(chan struct{}, 1)
	stop := app.k.Events().Subscribe("script", func(ev module.Event) {
		var m map[string]any
		_ = json.Unmarshal(ev.Data, &m)
		switch ev.Topic {
		case "script.step":
			steps <- m
		case "script.done":
			if m["mode"] == "demo" {
				done <- struct{}{}
			}
		}
	})
	defer stop()
	if code, body = call("script_run", `{"name":"tour","mode":"demo"}`); code != 200 || !strings.Contains(body, "running") {
		t.Fatalf("demo: %d %s", code, body)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the demo did not finish")
	}
	if len(steps) != 3 {
		t.Fatalf("the demo announced %d steps, want 3", len(steps))
	}
	<-steps
	second := <-steps
	if second["zoom"] != true || second["say"] != "The Builder composes ways of working." {
		t.Errorf("second step: %v", second)
	}
	if w, ok := app.k.Desk().Focused(ctx); !ok || w.View != "builder" {
		t.Errorf("after the demo, %v is in front, want builder", w.View)
	}

	// As a guide: the first step's calls run before it starts; the next step's when the
	// owner reaches it.
	g := `{"name":"g","steps":[{"say":"Scratch","do":[{"tool":"desk_open","input":{"view":"scratch"}}],"point":[{"view":"scratch"}]},{"say":"Drafts","do":[{"tool":"desk_open","input":{"view":"drafts"}}]}]}`
	if code, body = call("script_save", g); code != 200 {
		t.Fatal(body)
	}
	if code, body = call("script_run", `{"name":"g","mode":"guide"}`); code != 200 {
		t.Fatalf("guide: %d %s", code, body)
	}
	resp, err := http.Get(srv.URL + "/api/ade")
	if err != nil {
		t.Fatal(err)
	}
	var st struct {
		Guide struct {
			ID string `json:"id"`
		} `json:"guide"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&st)
	_ = resp.Body.Close()
	if st.Guide.ID == "" {
		t.Fatal("no guide on the desk")
	}
	report := `{"guide":"` + st.Guide.ID + `","index":1,"status":"running"}`
	req, _ := http.NewRequest("POST", srv.URL+"/api/ade/guide", strings.NewReader(report))
	req.Header.Set("Content-Type", "application/json")
	if resp, err = http.DefaultClient.Do(req); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if w, ok := app.k.Desk().Focused(ctx); ok && w.View == "drafts" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the second step's calls did not run when the owner reached it")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
