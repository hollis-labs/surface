package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNaniteDailyBudgetRefusalAndOwnerStopAll(t *testing.T) {
	model := newFakeModel(t)
	model.inputs = map[int]int{1: 100}
	naniteConfig(t, model.srv.URL)
	t.Setenv("SURFACE_AGENT_RUNNER", "")
	agents := t.TempDir()
	t.Setenv("SURFACE_AGENTS_DIR", agents)
	dir := filepath.Join(agents, "budgettest")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	starter, err := os.ReadFile("internal/agent/seat/agent.md")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "agent.md"), []byte(strings.ReplaceAll(string(starter), "{{app}}", "budgettest")), 0600); err != nil { //nolint:gosec // fixed filename inside this test's temporary directory
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "host.yaml"), []byte("kind: embedded\nlifetime: application\napplication_id: budgettest\nruntime:\n  provider: anthropic\n  daily_token_budget: 100\n"), 0600); err != nil {
		t.Fatal(err)
	}
	app, err := New(t.Context(), Options{Name: "budgettest", DataDir: t.TempDir(), Modules: []string{"scratch"}})
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
	do(t, srv, "POST", "/api/agent/agents/budgettest/conversation", "{}", &conv)
	base := "/api/agent/conversations/" + conv.ID
	var att struct {
		ID string `json:"attachment_id"`
	}
	do(t, srv, "POST", base+"/attachments", `{"role":"controller","stream":"committed"}`, &att)
	body := `{"attachment":"` + att.ID + `","text":"hello"}`
	if code := do(t, srv, "POST", base+"/turns", body, nil); code != 202 {
		t.Fatalf("first turn=%d", code)
	}
	a := &lateApp{t: t, srv: srv, conv: conv.ID, att: att.ID}
	a.partsUntil("Answer 1.")
	var refusal struct{ Code, Message string }
	a.until("budget refusal", func() bool { return do(t, srv, "POST", base+"/turns", body, &refusal) == 429 })
	if refusal.Code != "daily_budget" || !strings.Contains(refusal.Message, "00:00 UTC") {
		t.Fatalf("refusal: %+v", refusal)
	}
	req, _ := http.NewRequest("POST", srv.URL+"/api/agent/stop-all", nil)
	req.Header.Set("Origin", srv.URL)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var stopped struct {
		Restart bool `json:"restart_required"`
	}
	if err = json.NewDecoder(res.Body).Decode(&stopped); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 || !stopped.Restart {
		t.Fatalf("stop=%d %+v", res.StatusCode, stopped)
	}
	if code := do(t, srv, "POST", base+"/turns", body, &refusal); code != 503 || refusal.Code != "agent_stopped" {
		t.Fatalf("stopped turn=%d %+v", code, refusal)
	}
}
