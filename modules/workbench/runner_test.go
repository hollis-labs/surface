package workbench

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
)

// fakeGateway is an Anthropic-compatible gateway for tests: it answers by what the request
// holds. No real model is ever called.
type fakeGateway struct {
	mu       sync.Mutex
	requests int
	sawKey   bool
	usage    int // input tokens reported per request
}

func (f *fakeGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests++
	if r.Header.Get("Authorization") == "Bearer test-key" {
		f.sawKey = true
	}
	usage := f.usage
	f.mu.Unlock()
	if r.URL.Path != "/v1/messages" {
		http.NotFound(w, r)
		return
	}
	var req anthropicRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	var system strings.Builder
	for _, b := range req.System {
		system.WriteString(b.Text)
	}
	last := req.Messages[len(req.Messages)-1]
	toolResult := false
	userText := ""
	for _, b := range last.Content {
		if b.Type == "tool_result" {
			toolResult = true
		}
		userText += b.Text
	}
	reply := map[string]any{"stop_reason": "end_turn", "usage": map[string]int{"input_tokens": usage, "output_tokens": 20}}
	text := func(s string) { reply["content"] = []map[string]any{{"type": "text", "text": s}} }
	switch {
	case strings.Contains(system.String(), "You improve an AI agent's configuration"):
		diff := "--- a/agent.md\n+++ b/agent.md\n@@ -16,1 +16,3 @@\n When you are asked for a single fact, end your reply with a line `ANSWER: <the fact>`.\n+\n+Always mention the codename Juniper.\n"
		b, _ := json.Marshal(map[string]any{"proposals": []map[string]string{{"file": "agent.md", "diff": diff, "rationale": "The codename case wants Juniper named."}}})
		text("Here you go:\n" + string(b))
	case strings.Contains(system.String(), "You grade one answer"):
		text("PASS: it states the date.")
	case strings.Contains(userText, "launch date") && !toolResult:
		reply["stop_reason"] = "tool_use"
		reply["content"] = []map[string]any{{"type": "tool_use", "id": "t1", "name": "read_file", "input": map[string]any{"path": "notes/launch.md"}}}
	case toolResult:
		text("The launch moved a week.\nANSWER: 2026-12-08")
	case strings.Contains(userText, "codename"):
		if strings.Contains(system.String(), "Always mention the codename Juniper") {
			text("It is Juniper.\nANSWER: Juniper")
		} else {
			text("I do not know.\nANSWER: unknown")
		}
	default:
		text("Noted.")
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(reply)
}

const loopSuite = `name: loop
description: one case that passes, one that fails until the prompt names the codename
cases:
  - id: reads-the-note
    turns:
      - "We are planning the launch."
      - "What does notes/launch.md say the launch date is?"
    fixtures:
      notes/launch.md: "The launch moved from 2026-12-01 to 2026-12-08.\n"
    graders:
      - answer_line: "2026-12-08"
      - tool_called: {tool: read_file, args: {path: notes/launch.md}}
      - max_steps: 4
      - rubric: "States the new date."
  - id: codename
    turns:
      - "What is the release codename?"
    graders:
      - contains: Juniper
      - no_tool_called: {}
`

func withGateway(t *testing.T, h *moduletest.Host, f *fakeGateway) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	key := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(key, []byte("test-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.AgentState.GW = &module.Gateway{Model: "claude-test-1", URL: srv.URL, KeyFile: key}
}

func TestRunProposeAcceptRerunCompare(t *testing.T) {
	m, h := start(t)
	f := &fakeGateway{usage: 500}
	withGateway(t, h, f)
	if _, err := m.create("loop", "", "embedded", "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.run.SaveSuite("loop", []byte(loopSuite)); err != nil {
		t.Fatal(err)
	}
	// The estimate calls no model.
	code, body := h.Do("GET", "/agents/loop/suites/loop/estimate", "")
	if code != 200 || !strings.Contains(body, `"cases":2`) || !strings.Contains(body, `"ready":true`) || f.requests != 0 {
		t.Fatal(code, body, f.requests)
	}
	// A run needs the owner's go-ahead.
	if code, _ = h.Do("POST", "/agents/loop/suites/loop/run", `{"mode":"live"}`); code == 202 {
		t.Fatal("a run started without confirm")
	}
	code, body = h.Do("POST", "/agents/loop/suites/loop/run", `{"mode":"live","confirm":true}`)
	if code != 202 {
		t.Fatal(code, body)
	}
	m.run.Wait()
	runs, _ := m.run.Runs("loop")
	first, err := m.run.GetRun("loop", runs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "done" || first.Passed != 1 || first.Failed != 1 || !f.sawKey {
		t.Fatalf("first run: %+v", first)
	}
	reads := first.Cases[0]
	if !reads.Pass || len(reads.Graders) != 4 || reads.Steps != 2 || len(reads.Transcript) < 2 {
		t.Fatalf("reads case: %+v", reads)
	}
	// The prelude's exchange comes first, then the task's tool call.
	if reads.Transcript[0].Reply != "Noted." || len(reads.Transcript[1].Calls) == 0 || reads.Transcript[1].Calls[0].Name != "read_file" {
		t.Fatalf("transcript: %+v", reads.Transcript)
	}
	if first.Cases[1].Pass || !strings.Contains(first.Cases[1].Why, "Juniper") {
		t.Fatalf("codename case: %+v", first.Cases[1])
	}
	if first.Digest == "" || first.Version != 1 {
		t.Fatalf("not keyed by digest: %+v", first)
	}

	// Propose: one edit, pending until the owner decides.
	code, body = h.Do("POST", "/agents/loop/runs/"+first.ID+"/propose", `{"mode":"live","confirm":true}`)
	if code != 200 {
		t.Fatal(code, body)
	}
	var prop Proposal
	_ = json.Unmarshal([]byte(body), &prop)
	if len(prop.Items) != 1 || prop.Items[0].Status != "pending" {
		t.Fatalf("proposal: %s", body)
	}
	vs, _ := m.lib.Versions("loop")
	if len(vs) != 1 {
		t.Fatal("a proposal changed the agent before it was accepted")
	}
	code, body = h.Do("POST", "/agents/loop/proposals/"+prop.ID+"/1/accept", "")
	if code != 200 || !strings.Contains(body, `"status":"accepted"`) {
		t.Fatal(code, body)
	}
	vs, _ = m.lib.Versions("loop")
	if len(vs) != 2 || !strings.Contains(vs[1].Note, "accepted proposal") {
		t.Fatalf("versions: %+v", vs)
	}
	tree, _ := m.lib.Tree("loop")
	if !strings.Contains(string(tree["agent.md"]), "Always mention the codename Juniper.") {
		t.Fatal(string(tree["agent.md"]))
	}
	// Accepting twice is refused.
	if code, _ = h.Do("POST", "/agents/loop/proposals/"+prop.ID+"/1/accept", ""); code == 200 {
		t.Fatal("accepted twice")
	}

	// Re-run and compare: the codename case is fixed.
	code, body = h.Do("POST", "/agents/loop/runs/"+first.ID+"/rerun", `{"mode":"live","confirm":true}`)
	if code != 202 {
		t.Fatal(code, body)
	}
	m.run.Wait()
	runs, _ = m.run.Runs("loop")
	second, _ := m.run.GetRun("loop", runs[0].ID)
	if second.Previous != first.ID || second.Passed != 2 || second.Digest == first.Digest || second.Version != 2 {
		t.Fatalf("second run: %+v", second)
	}
	code, body = h.Do("GET", "/agents/loop/compare?a="+first.ID+"&b="+second.ID, "")
	if code != 200 || !strings.Contains(body, `"fixed":1`) || !strings.Contains(body, `"broken":0`) {
		t.Fatal(code, body)
	}
}

func TestTheRunTokenBudgetStopsARun(t *testing.T) {
	m, h := start(t)
	f := &fakeGateway{usage: 5000}
	withGateway(t, h, f)
	if err := h.Config().Set(t.Context(), "eval_run_tokens", 6000); err != nil {
		t.Fatal(err)
	}
	if _, err := m.create("budget", "", "embedded", "owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.run.SaveSuite("budget", []byte(loopSuite)); err != nil {
		t.Fatal(err)
	}
	if code, body := h.Do("POST", "/agents/budget/suites/loop/run", `{"mode":"live","confirm":true}`); code != 202 {
		t.Fatal(code, body)
	}
	m.run.Wait()
	runs, _ := m.run.Runs("budget")
	r, _ := m.run.GetRun("budget", runs[0].ID)
	if r.Status != "stopped" || !strings.Contains(r.Stopped, "token budget") || r.Passed != 0 {
		t.Fatalf("run: %+v", r)
	}
	// The first request spent 5,020 tokens, the second passed the budget and canceled the
	// run; nothing more was sent.
	if f.requests > 3 {
		t.Fatalf("%d requests after the budget was spent", f.requests)
	}
}

func TestNoGatewayNoRun(t *testing.T) {
	m, h := start(t)
	if _, err := m.create("solo", "", "embedded", "owner"); err != nil {
		t.Fatal(err)
	}
	code, body := h.Do("GET", "/agents/solo/suites/basics/estimate", "")
	if code != 200 || !strings.Contains(body, `"ready":false`) {
		t.Fatal(code, body)
	}
	if code, _ = h.Do("POST", "/agents/solo/suites/basics/run", `{"mode":"live","confirm":true}`); code == 202 {
		t.Fatal("a run started with no gateway")
	}
}

func TestToolsAreLevelledAndNoneAccepts(t *testing.T) {
	m, h := start(t)
	levels := map[string]module.Level{}
	for _, tool := range h.Manifest().Tools {
		levels[tool.Name] = tool.Level
		if strings.Contains(tool.Name, "accept") || strings.Contains(tool.Name, "decide") || strings.Contains(tool.Name, "mode") {
			t.Fatalf("%s: accepting a proposal or setting a mode is the owner's", tool.Name)
		}
	}
	if levels["workbench_run_eval"] != module.LevelConfirm || levels["workbench_propose"] != module.LevelConfirm || levels["workbench_write_file"] != module.LevelWrite {
		t.Fatalf("levels: %v", levels)
	}
	if _, err := h.Tool("workbench_create", `{"template":"blank","name":"tooled"}`); err != nil {
		t.Fatal(err)
	}
	out, err := h.Tool("workbench_write_file", `{"agent":"tooled","file":"context/notes.md","content":"# Notes\n","note":"add notes"}`)
	if err != nil || !out.(map[string]any)["saved"].(bool) {
		t.Fatal(out, err)
	}
	out, err = h.Tool("workbench_write_config", `{"agent":"tooled","file":"host.yaml","content":"kind: nope\n","note":"break"}`)
	if err != nil || out.(map[string]any)["saved"].(bool) {
		t.Fatal("an invalid write was saved", out, err)
	}
	// The owner is editing this agent: a delegate waits.
	w, err := h.DeskState.Open(t.Context(), "workbench", module.OpenOptions{Args: map[string]any{"agent": "tooled"}})
	if err != nil {
		t.Fatal(err)
	}
	h.DeskState.SetWorking(w.ID, true, false)
	if _, err := h.ToolAs(module.ActorAgent, "workbench_write_file", `{"agent":"tooled","file":"context/notes.md","content":"x","note":"x"}`); err == nil {
		t.Fatal("wrote over the owner's unsaved work")
	}
	_ = m
}

func TestOfflineRunNeedsNoGatewayAndSpendsNothing(t *testing.T) {
	m, h := start(t)
	if _, err := m.create("off", "", "embedded", "owner"); err != nil {
		t.Fatal(err)
	}
	// Offline is the default, needs no confirm and no gateway.
	code, body := h.Do("POST", "/agents/off/suites/basics/run", `{}`)
	if code != 202 || !strings.Contains(body, `"mode":"offline"`) {
		t.Fatal(code, body)
	}
	m.run.Wait()
	runs, _ := m.run.Runs("off")
	r, _ := m.run.GetRun("off", runs[0].ID)
	if r.Status != "done" || r.Passed != 1 || r.SpentUSD != 0 || r.Model != offlineModel {
		t.Fatalf("offline run: %+v", r)
	}
	if c := r.Cases[0]; c.Steps != 2 || len(c.Graders) != 3 {
		t.Fatalf("case: %+v", c)
	}
	// A live run without a gateway is refused.
	if code, _ = h.Do("POST", "/agents/off/suites/basics/run", `{"mode":"live","confirm":true}`); code == 202 {
		t.Fatal("live run with no gateway")
	}
}
