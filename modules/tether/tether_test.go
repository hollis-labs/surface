package tether

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
	"github.com/hollis-labs/surface/module/tetherlink"
)

const agentURN = "msg://agent/agent-mux/agt_example001"

// fake is the gateway: it records each call and lists two tools.
type fake struct {
	mu     sync.Mutex
	calls  []string
	args   []map[string]any
	lists  int
	refuse string
}

func (f *fake) Call(_ context.Context, tool string, args map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, tool)
	f.args = append(f.args, args)
	if f.refuse != "" {
		return "", &tetherlink.RefusedError{Detail: f.refuse}
	}
	return `{"ok":true}`, nil
}

func (f *fake) List(context.Context) ([]tetherlink.ToolInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists++
	return []tetherlink.ToolInfo{
		{Name: "torque_task_list", Description: "List tasks, at length.", InputSchema: json.RawMessage(`{"type":"object","properties":{"status":{"type":"string"}}}`)},
		{Name: "tesseract_recall", Description: "Recall.", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}, nil
}

func start(t *testing.T) (*moduletest.Host, *fake) {
	t.Helper()
	f := &fake{}
	h := moduletest.Start(t, &mod{link: f})
	return h, f
}

func TestEveryCatalogToolIsOfferedAtItsLevel(t *testing.T) {
	h, _ := start(t)
	got := map[string]module.Level{}
	for _, tool := range h.Manifest().Tools {
		got[tool.Name] = tool.Level
	}
	for name, e := range catalog {
		if got[name] != e.level {
			t.Errorf("%s: level %q, want %q", name, got[name], e.level)
		}
	}
	for name := range mailCatalog {
		if _, ok := got[name]; !ok {
			t.Errorf("%s: not offered", name)
		}
	}
	if got[describeTool] != module.LevelRead {
		t.Errorf("%s: %q", describeTool, got[describeTool])
	}
	if got["torque_task_create"] != module.LevelWrite || got["registry_record_delete"] != module.LevelConfirm {
		t.Error("writes must ask")
	}
}

func TestACallReachesTheGateway(t *testing.T) {
	h, f := start(t)
	out, err := h.Tool("torque_task_get", `{"id":"T-1"}`)
	if err != nil || out != `{"ok":true}` {
		t.Fatalf("%v %v", out, err)
	}
	if f.calls[0] != "torque_task_get" || f.args[0]["id"] != "T-1" {
		t.Fatalf("%v %v", f.calls, f.args)
	}
}

func TestTheOwnersScopeIsNotWritten(t *testing.T) {
	h, f := start(t)
	for _, ns := range []string{"user/chris/memory/decisions", "USER", "system/x"} {
		if _, err := h.Tool("memory_write", `{"namespace":"`+ns+`","actor":"user"}`); err == nil {
			t.Errorf("%s: written", ns)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("reached the gateway: %v", f.calls)
	}
	if _, err := h.Tool("memory_write", `{"namespace":"project/surface/memory/decisions","actor":"user"}`); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.args[0]["actor"]; ok || f.args[0]["author_agent_id"] != "surface" {
		t.Fatalf("args: %v", f.args[0])
	}
}

func TestMailIsTheAgentsOwn(t *testing.T) {
	h, f := start(t)
	if _, err := h.Tool("tether_message_notify", `{"to":"msg://user/local/operator","body":"hi"}`); err == nil || !strings.Contains(err.Error(), "agent_address") {
		t.Fatalf("no address: %v", err)
	}
	if err := h.Settings.Set(context.Background(), "agent_address", agentURN); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Tool("tether_message_notify", `{"from":"msg://user/local/operator","to":"msg://agent/x","body":"hi"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Tool("tether_message_list", `{"to":"msg://user/local/operator"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Tool("tether_message_get", `{"id":"m1","as":"msg://user/local/operator"}`); err != nil {
		t.Fatal(err)
	}
	if f.args[0]["from"] != agentURN || f.args[1]["to"] != agentURN || f.args[2]["as"] != agentURN {
		t.Fatalf("args: %v", f.args)
	}
}

func TestARefusalSaysWhy(t *testing.T) {
	h, f := start(t)
	f.refuse = "status: must be one of todo, doing"
	_, err := h.Tool("torque_task_transition", `{"id":"T-1","status":"nope"}`)
	if err == nil || !strings.Contains(err.Error(), "must be one of") || !strings.Contains(err.Error(), describeTool) {
		t.Fatalf("%v", err)
	}
	if !errors.Is(&tetherlink.RefusedError{}, tetherlink.ErrRefused) {
		t.Fatal("a RefusedError is ErrRefused")
	}
}

func TestDescribeReturnsTheLiveDefinition(t *testing.T) {
	h, f := start(t)
	out, err := h.Tool(describeTool, `{"names":["torque_task_list","nope"]}`)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), "List tasks, at length.") || !strings.Contains(string(b), `"not_available":["nope"]`) {
		t.Fatalf("%s", b)
	}
	if _, err := h.Tool(describeTool, `{"names":["tesseract_recall"]}`); err != nil {
		t.Fatal(err)
	}
	if f.lists != 1 {
		t.Fatalf("listed %d times; the list is kept", f.lists)
	}
}

func TestNotConfigured(t *testing.T) {
	h := moduletest.Start(t, &mod{})
	if _, err := h.Tool("torque_task_get", `{"id":"T-1"}`); !errors.Is(err, errNotConfigured) {
		t.Fatalf("%v", err)
	}
}

func TestDescribeCallIsOneLine(t *testing.T) {
	got := describeCall("torque_task_create", json.RawMessage(`{"title":"Fix it","priority":"2"}`))
	if got != `torque_task_create priority="2" title="Fix it"` {
		t.Fatal(got)
	}
}

// A schema with no properties key is dropped whole by some providers (Anthropic's adapter in
// Nanite sends no input_schema, and the request is refused): every tool must name properties.
func TestEverySchemaNamesProperties(t *testing.T) {
	h, _ := start(t)
	for _, tool := range h.Manifest().Tools {
		var s map[string]any
		if err := json.Unmarshal(tool.Input, &s); err != nil || s["type"] != "object" || s["properties"] == nil {
			t.Errorf("%s: %s", tool.Name, tool.Input)
		}
	}
}
