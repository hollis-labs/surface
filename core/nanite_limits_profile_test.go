package core

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/hollis-labs/surface/modules/workbench"
)

func TestNaniteWorkbenchProfileContextArrivesWhole(t *testing.T) {
	model := newFakeModel(t)
	naniteConfig(t, model.srv.URL)
	app, err := New(t.Context(), Options{Name: "profilefacts", DataDir: t.TempDir(), Modules: []string{"scratch", "workbench"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close(); _ = app.Stop(context.Background()) })
	var conv struct {
		ID string `json:"conversation_id"`
	}
	do(t, srv, "POST", "/api/agent/agents/profilefacts/conversation", "{}", &conv)
	var att struct {
		ID string `json:"attachment_id"`
	}
	do(t, srv, "POST", "/api/agent/conversations/"+conv.ID+"/attachments", `{"role":"controller","stream":"committed"}`, &att)
	a := &lateApp{t: t, srv: srv, conv: conv.ID, att: att.ID}
	if code := do(t, srv, "POST", "/api/agent/profiles/pin", `{"profile":"workbench"}`, nil); code != 200 {
		t.Fatalf("pin: %d", code)
	}
	if code := do(t, srv, "POST", "/api/agent/conversations/"+conv.ID+"/turns", `{"attachment":"`+att.ID+`","text":"review this agent"}`, nil); code != 202 {
		t.Fatalf("turn: %d", code)
	}
	a.partsUntil("Answer 1.")
	a.waitIdle()
	var req any
	if err = json.Unmarshal([]byte(model.request(1)), &req); err != nil {
		t.Fatal(err)
	}
	var texts []string
	var collect func(any)
	collect = func(v any) {
		switch v := v.(type) {
		case string:
			texts = append(texts, v)
		case []any:
			for _, x := range v {
				collect(x)
			}
		case map[string]any:
			for _, x := range v {
				collect(x)
			}
		}
	}
	collect(req)
	sent := strings.Join(texts, "\n")
	files, err := filepath.Glob("../modules/workbench/profile/context/*.md")
	if err != nil || len(files) == 0 {
		t.Fatalf("profile files: %v", err)
	}
	for _, name := range files {
		raw, err := os.ReadFile(name) //nolint:gosec // paths come from the fixed repository fixture glob
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(sent, strings.TrimSpace(string(raw))) {
			t.Errorf("profile context was cut: %s", name)
		}
	}
	if strings.Contains(sent, "trimmed to fit the context slot") {
		t.Fatal("context was trimmed")
	}
}

func TestNaniteAgentConfigOverridesRuntimeLimits(t *testing.T) {
	model := newFakeModel(t)
	model.script("scratch_list")
	naniteConfig(t, model.srv.URL)
	path := os.Getenv("SURFACE_AGENT_CONFIG")
	raw, err := os.ReadFile(path) //nolint:gosec // naniteConfig just set this path to a test-owned temporary file
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err = json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg["max_tokens"], cfg["max_steps"] = 1234, 1
	raw, err = json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil { //nolint:gosec // the same test-owned config file created by naniteConfig
		t.Fatal(err)
	}
	app, err := New(t.Context(), Options{Name: "limits", DataDir: t.TempDir(), Modules: []string{"scratch"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close(); _ = app.Stop(context.Background()) })
	var conv struct {
		ID string `json:"conversation_id"`
	}
	do(t, srv, "POST", "/api/agent/agents/limits/conversation", "{}", &conv)
	var att struct {
		ID string `json:"attachment_id"`
	}
	do(t, srv, "POST", "/api/agent/conversations/"+conv.ID+"/attachments", `{"role":"controller","stream":"committed"}`, &att)
	a := &lateApp{t: t, srv: srv, conv: conv.ID, att: att.ID}
	if code := do(t, srv, "POST", "/api/agent/conversations/"+conv.ID+"/turns", `{"attachment":"`+att.ID+`","text":"look at scratch"}`, nil); code != 202 {
		t.Fatalf("turn: %d", code)
	}
	a.partsUntil("Answer 2.")
	a.waitIdle()
	if model.count() != 2 {
		t.Fatalf("requests=%d, want tool step and closing reply", model.count())
	}
	for n := 1; n <= 2; n++ {
		var req struct {
			MaxTokens  int `json:"max_tokens"`
			ToolChoice struct {
				Type string `json:"type"`
			} `json:"tool_choice"`
		}
		if err := json.Unmarshal([]byte(model.request(n)), &req); err != nil {
			t.Fatal(err)
		}
		if req.MaxTokens != 1234 {
			t.Errorf("request %d max_tokens=%d", n, req.MaxTokens)
		}
		if n == 2 && req.ToolChoice.Type != "none" {
			t.Fatal("max_steps did not force a closing reply without tools")
		}
	}
}
