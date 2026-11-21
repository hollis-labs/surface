package agent

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/hollis-labs/libs/agent/chat/sqlitestore"
	"github.com/hollis-labs/libs/llm/core/llmtypes"
	nanite "github.com/hollis-labs/nanite/agent"
	"github.com/hollis-labs/nanite/chatrunner"
)

// The first response consumes the only tool step; the second is the closing reply.
type historyLimitProvider struct{ calls atomic.Int32 }

func (p *historyLimitProvider) StreamChat(_ context.Context, _ llmtypes.ChatRequest) (<-chan llmtypes.StreamEvent, error) {
	out := make(chan llmtypes.StreamEvent, 2)
	switch p.calls.Add(1) {
	case 1:
		out <- llmtypes.StreamEvent{Type: llmtypes.EventToolUse, ToolUse: &llmtypes.ToolUseBlock{ID: "call", Name: "unknown"}}
	case 2:
		out <- llmtypes.StreamEvent{Type: llmtypes.EventDelta, Content: "Work remains."}
	default:
		return nil, errors.New("unexpected model request")
	}
	out <- llmtypes.StreamEvent{Type: llmtypes.EventDone}
	close(out)
	return out, nil
}
func (*historyLimitProvider) Complete(context.Context, llmtypes.ChatRequest) (string, error) {
	return "", errors.New("unexpected completion request")
}
func (*historyLimitProvider) Capabilities() llmtypes.ProviderCapabilities {
	return llmtypes.ProviderCapabilities{}
}

func TestNanitePriorMessagesExcludesStepLimitPrompt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "seat")
	if _, err := plantSeat(dir, "seat"); err != nil {
		t.Fatal(err)
	}
	provider := &historyLimitProvider{}
	runner, err := chatrunner.Open(t.Context(), chatrunner.Options{Agent: nanite.Options{
		Dir: dir, DataDir: t.TempDir(), GlobalSkillsDir: "-", Provider: provider, MaxSteps: 1,
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runner.Close() }()
	store, err := sqlitestore.Open(t.Context(), filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	service := &Service{wire: &wire{nanite: runner, store: store}}
	c, err := runner.Agent().EnsureConversation(t.Context(), chatrunner.ConversationID("history"))
	if err != nil {
		t.Fatal(err)
	}
	// Marker-like text from the actual user must not be filtered with the internal prompt.
	const request = "Explain nanite_closing_reply:max_steps"
	run, err := c.Send(t.Context(), nanite.Input{Text: request})
	if err != nil {
		t.Fatal(err)
	}
	for range run.Events() {
	}
	result, err := run.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != "max_steps" || result.Text != "Work remains." || provider.calls.Load() != 2 {
		t.Fatalf("did not exercise step-limit closing: %+v, calls=%d", result, provider.calls.Load())
	}
	prior := service.priorMessages(t.Context(), "history")
	// Most recent first: closing answer, tool result, tool call, genuine user request.
	// The internal closing prompt must be absent.
	if len(prior) != 4 {
		t.Fatalf("history has %d messages, want 4: %+v", len(prior), prior)
	}
	if prior[0].Role != "assistant" || prior[0].Content != "Work remains." ||
		prior[1].Role != "tool" ||
		prior[2].Role != "assistant" || prior[2].ToolCalls != 1 ||
		prior[3].Role != "user" || prior[3].Content != request {
		t.Fatalf("unexpected prior messages: %+v", prior)
	}
}
