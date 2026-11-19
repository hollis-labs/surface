package core

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// boot starts an app with the given modules on a throwaway data directory.
func boot(t *testing.T, modules ...string) *httptest.Server {
	t.Helper()
	t.Setenv("SURFACE_AGENT_CONFIG", filepath.Join(t.TempDir(), "absent.json"))
	ctx := context.Background()
	app, err := New(ctx, Options{Name: "apitest", Title: "API test", DataDir: t.TempDir(), Modules: modules})
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	t.Cleanup(func() {
		srv.Close()
		_ = app.Stop(context.Background())
	})
	return srv
}

func do(t *testing.T, srv *httptest.Server, method, path, body string, into any) int {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if into != nil {
		if err := json.Unmarshal(raw, into); err != nil {
			t.Fatalf("%s %s: %v in %s", method, path, err, raw)
		}
	}
	return resp.StatusCode
}

// The seam, end to end: a module that knows only the contract shows up in the manifest,
// its view opens, its tool runs over the API and over MCP, its route serves, and its live
// setting changes what the route returns with nothing restarted.
func TestAModuleOnTheContractAloneWorksThroughTheCore(t *testing.T) {
	srv := boot(t, "scratch")

	var manifest struct {
		Modules []struct{ ID string }
		Views   []struct{ ID, Module string }
	}
	do(t, srv, "GET", "/api/app", "", &manifest)
	if len(manifest.Modules) != 1 || manifest.Modules[0].ID != "scratch" {
		t.Fatalf("modules: %+v", manifest.Modules)
	}
	hasView := false
	for _, v := range manifest.Views {
		hasView = hasView || (v.ID == "scratch" && v.Module == "scratch")
	}
	if !hasView {
		t.Fatalf("the scratch view is not in the manifest: %+v", manifest.Views)
	}

	var opened struct{ Window struct{ ID, View string } }
	if code := do(t, srv, "POST", "/api/desk/actions", `{"type":"open","view":"scratch"}`, &opened); code != 200 || opened.Window.View != "scratch" {
		t.Fatalf("open: %d %+v", code, opened)
	}

	var called struct{ Result string }
	if code := do(t, srv, "POST", "/api/tools/scratch_add", `{"text":"todo: ship it"}`, &called); code != 200 || !strings.Contains(called.Result, `"kind":"todo"`) {
		t.Fatalf("tool over the API: %d %s", code, called.Result)
	}
	var rpc struct {
		Result struct {
			IsError bool
			Content []struct{ Text string }
		}
	}
	do(t, srv, "POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"scratch_update","arguments":{"id":"`+idOf(t, called.Result)+`","done":true}}}`, &rpc)
	if !rpc.Result.IsError {
		t.Fatalf("write tool must be refused over MCP: %+v", rpc.Result)
	}

	do(t, srv, "POST", "/api/tools/scratch_update", `{"id":"`+idOf(t, called.Result)+`","done":true}`, nil)
	rpc.Result.IsError = false
	do(t, srv, "POST", "/mcp", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"scratch_list","arguments":{"done":true}}}`, &rpc)
	if rpc.Result.IsError || len(rpc.Result.Content) == 0 || !strings.Contains(rpc.Result.Content[0].Text, `"done":true`) {
		t.Fatalf("read tool over MCP: %+v", rpc.Result)
	}
	var items []struct{ Text string }
	do(t, srv, "GET", "/api/m/scratch/items", "", &items)
	if len(items) != 1 {
		t.Fatalf("the module's route lists %d items, want the finished one", len(items))
	}
	// The setting is read at the moment of use: the same route, the next call.
	if code := do(t, srv, "PUT", "/api/settings/scratch.show_done", `{"value":false}`, nil); code != 200 {
		t.Fatalf("changing a setting: %d", code)
	}
	do(t, srv, "GET", "/api/m/scratch/items", "", &items)
	if len(items) != 0 {
		t.Errorf("after turning show_done off the route still lists %d items", len(items))
	}
	if code := do(t, srv, "PUT", "/api/settings/scratch.show_done", `{"value":"no"}`, nil); code == 200 {
		t.Error("a setting of the wrong type was accepted")
	}

	var ctxOut struct {
		Context struct {
			Ref   string
			Extra map[string]any
		}
	}
	do(t, srv, "GET", "/api/context", "", &ctxOut)
	if ctxOut.Context.Ref != "scratch:items" || ctxOut.Context.Extra["done"] != float64(1) {
		t.Errorf("the window's context, as its module describes it: %+v", ctxOut.Context)
	}
}

func idOf(t *testing.T, itemJSON string) string {
	t.Helper()
	var it struct{ ID string }
	if err := json.Unmarshal([]byte(itemJSON), &it); err != nil || it.ID == "" {
		t.Fatalf("no id in %s", itemJSON)
	}
	return it.ID
}

// The page learns of every change from the stream: the desk's state first, then events.
func TestTheEventStreamStartsWithTheDeskAndCarriesChanges(t *testing.T) {
	srv := boot(t, "scratch")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data: ") {
				lines <- sc.Text()
			}
		}
		close(lines)
	}()
	next := func(want string) string {
		t.Helper()
		for {
			select {
			case l, ok := <-lines:
				if !ok {
					t.Fatalf("the stream ended before %q", want)
				}
				if strings.Contains(l, `"topic":"`+want+`"`) {
					return l
				}
			case <-ctx.Done():
				t.Fatalf("no %q on the stream", want)
			}
		}
	}
	next("desk.changed")
	do(t, srv, "POST", "/api/tools/scratch_add", `{"text":"hello"}`, nil)
	next("scratch.changed")
	do(t, srv, "PUT", "/api/settings/surface.theme", `{"value":"light"}`, nil)
	if l := next("config.changed"); !strings.Contains(l, `"surface.theme"`) {
		t.Errorf("the settings change on the stream: %s", l)
	}
}

func TestACrossOriginPageCannotDriveTheApp(t *testing.T) {
	srv := boot(t, "scratch")
	req, _ := http.NewRequest("POST", srv.URL+"/api/desk/actions", strings.NewReader(`{"type":"open","view":"scratch"}`))
	req.Header.Set("Origin", "https://example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("a write from another origin: %d, want 403", resp.StatusCode)
	}
}

func TestAnUnknownModuleStopsTheBoot(t *testing.T) {
	_, err := New(context.Background(), Options{Name: "apitest", DataDir: t.TempDir(), Modules: []string{"nope"}})
	if err == nil || !strings.Contains(err.Error(), "not compiled into this binary") {
		t.Errorf("booting with an unknown module: %v", err)
	}
}
