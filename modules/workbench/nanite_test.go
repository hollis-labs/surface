package workbench

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/hollis-labs/libs/llm/core/llmcontracts"
	"github.com/hollis-labs/libs/llm/core/llmtypes"
	"github.com/hollis-labs/surface/module"
)

const stubSuite = `name: stubs
cases:
  - id: lookup
    turns: [Look up the launch]
    stubs:
      - name: lookup_note
        description: Look up a note.
        read_only: true
        schema: {type: object, properties: {id: {type: string}}}
        results:
          - when: {id: launch}
            text: Launch is Tuesday.
          - text: No note.
            is_error: true
    script:
      - calls: [{tool: lookup_note, args: {id: launch}}]
      - say: Done.
    graders:
      - tool_called: {tool: lookup_note, args: {id: launch}}
`

func TestStubSuiteRoundTripAndExecution(t *testing.T) {
	m, _ := start(t)
	if _, err := m.create("stubs", "", "embedded", "owner"); err != nil {
		t.Fatal(err)
	}
	s, err := ParseSuite([]byte(stubSuite))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Suite
	if err = json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if err = decoded.Validate(); err != nil {
		t.Fatal(err)
	}
	if decoded.Cases[0].Stubs[0].Schema["type"] != "object" || !decoded.Cases[0].Stubs[0].Results[1].IsError {
		t.Fatalf("lost stub fields: %+v", decoded)
	}
	if _, err = m.run.SaveSuite("stubs", []byte(stubSuite)); err != nil {
		t.Fatal(err)
	}
	r, err := m.run.Start(t.Context(), "stubs", "stubs", "offline", Caps{}, "")
	if err != nil {
		t.Fatal(err)
	}
	m.run.Wait()
	got, err := m.run.GetRun("stubs", r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Passed != 1 || got.Cases[0].StubCalls["lookup_note"] != 1 {
		t.Fatalf("stub did not execute: %+v", got)
	}
	if len(got.Cases[0].Transcript) != 2 || !strings.Contains(got.Cases[0].Transcript[1].Sent, "Launch is Tuesday.") {
		t.Fatalf("wrong canned result: %+v", got.Cases[0].Transcript)
	}
}

func TestInvalidStubsRefused(t *testing.T) {
	for _, name := range []string{"mcp__notes__read", "read_file", "tool__script", "bad name"} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseSuite([]byte(strings.ReplaceAll(stubSuite, "lookup_note", name)))
			if err == nil {
				t.Fatalf("accepted reserved or invalid name %q", name)
			}
		})
	}
	for _, fragment := range []string{
		"    stubs: [{name: lookup, results: []}]\n",
		"    stubs: [{name: lookup, results: [{when: {}, text: x}]}]\n",
		"    stubs: [{name: lookup, results: [{text: x}]}, {name: lookup, results: [{text: y}]}]\n",
		"    stubs: [{name: lookup, schema: invalid, results: [{text: x}]}]\n",
	} {
		_, err := ParseSuite([]byte("name: invalid\ncases:\n  - id: one\n    turns: [hello]\n" + fragment + "    graders: [{contains: hi}]\n"))
		if err == nil {
			t.Fatalf("accepted invalid stubs: %s", fragment)
		}
	}
}

type requestSpy struct {
	llmcontracts.Provider
	mu      sync.Mutex
	prompts []string
}

func (s *requestSpy) StreamChat(ctx context.Context, req llmtypes.ChatRequest) (<-chan llmtypes.StreamEvent, error) {
	s.mu.Lock()
	text := req.SystemPrompt
	raw, _ := json.Marshal(req.Messages)
	text += string(raw)
	for _, b := range req.SlotBlocks {
		text += "\n" + b.Content
	}
	s.prompts = append(s.prompts, text)
	s.mu.Unlock()
	return s.Provider.StreamChat(ctx, req)
}

func TestSteeringSidecarAppliesInEval(t *testing.T) {
	tree, _, err := fromTemplate("embedded", "steering")
	if err != nil {
		t.Fatal(err)
	}
	tree[steeringFile] = []byte(`{"workbench_format":1,"presets":[{"name":"general","instructions":"Keep the cobalt convention."}],"reflexes":[{"name":"cite","enabled":true,"trigger_kind":"text","trigger":{"pattern":"launch"},"action_kind":"remind","action":{"body":"Remember the amber evidence."}}]}`)
	c := Case{ID: "one", Turns: []string{"launch"}, Script: []ScriptStep{{Say: "Done."}}, Graders: []Grader{{Contains: "Done."}}}
	spy := &requestSpy{Provider: &offlineProvider{c: c}}
	out, err := (&naniteRuntime{}).RunSuite(t.Context(), RunSpec{Tree: tree, Suite: Suite{Name: "steering", Cases: []Case{c}}, Model: offlineModel, MaxUSD: 10, Provider: func(string) llmcontracts.Provider { return spy }})
	if err != nil || out.Stopped != "" {
		t.Fatalf("run: %+v, %v", out, err)
	}
	spy.mu.Lock()
	defer spy.mu.Unlock()
	prompt := strings.Join(spy.prompts, "\n")
	if !strings.Contains(prompt, "cobalt convention") || !strings.Contains(prompt, "amber evidence") {
		t.Fatalf("steering missing from request: %s", prompt)
	}
}

func TestOpenAIProviderUsesGatewayAndResolvesKeyPerRequest(t *testing.T) {
	key := filepath.Join(t.TempDir(), "key")
	values := []string{"fixture-first-value", "fixture-second-value"}
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		seen = append(seen, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"fake","choices":[{"message":{"role":"assistant","content":"answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer srv.Close()
	p, err := newGatewayProvider(Tree{"host.yaml": []byte("runtime:\n  provider: openai\n")}, module.Gateway{URL: srv.URL, KeyFile: key})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range values {
		if err := os.WriteFile(key, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
		answer, err := p.Complete(t.Context(), llmtypes.ChatRequest{Model: "test-model", MaxTokens: 16, Messages: []llmtypes.ChatMessage{{Role: "user", Content: "hello"}}})
		if err != nil || answer != "answer" {
			t.Fatalf("completion %q, %v", answer, err)
		}
		if strings.Contains(scrub("gateway echoed "+value), value) {
			t.Fatalf("fixture key not scrubbed: %q; seen=%q", scrub("gateway echoed "+value), seen)
		}
	}
	if len(seen) != 2 || seen[0] != "Bearer "+values[0] || seen[1] != "Bearer "+values[1] {
		t.Fatal("key was not resolved per request")
	}
}

func TestGatewayMissingKeyDoesNotSendARequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("request sent without the app key") }))
	defer srv.Close()
	p, err := newGatewayProvider(Tree{}, module.Gateway{URL: srv.URL, KeyFile: filepath.Join(t.TempDir(), "missing")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Complete(t.Context(), llmtypes.ChatRequest{Model: "claude-test-1", MaxTokens: 16, Messages: []llmtypes.ChatMessage{{Role: "user", Content: "hello"}}})
	if err == nil {
		t.Fatal("missing gateway key accepted")
	}
}

// The exported parser knows newer provider uses that the old mirror rejected.
func TestAuthoredProviderDeskSelection(t *testing.T) {
	if _, err := checkProviders([]byte("use:\n  desk:\n    provider: anthropic\n    model: claude-test-1\n")); err != nil {
		t.Fatal(err)
	}
}
