package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/hollis-labs/libs/llm/core/llmtypes"
)

// offlineModel is the model name an offline run reports.
const offlineModel = "offline-script"

// offlineProvider is the stand-in model of an offline run: it plays a case's script, so the
// whole path (the agent opening, the fixtures, the tools, the graders) runs with no model
// called and nothing spent, as nanite/evals' own offline mode does. Usage is estimated from
// the request's size, four characters a token.
type offlineProvider struct {
	c      Case
	record func(llmtypes.ChatRequest, anthropicReply)
}

func textOf(m llmtypes.ChatMessage) (string, bool) {
	text := m.Content
	hasText := m.Content != ""
	for _, b := range m.ContentBlocks {
		if b.Type == "text" {
			text += b.Text
			hasText = true
		}
	}
	return text, hasText
}

func (p *offlineProvider) reply(req llmtypes.ChatRequest) anthropicReply {
	var r anthropicReply
	say := func(s string) { r.Content = []anthropicBlock{{Type: "text", Text: s}}; r.StopReason = "end_turn" }
	// A subtask's conversation starts "Goal: ..."; the stand-in returns at once.
	if len(req.Messages) > 0 {
		if first, _ := textOf(req.Messages[0]); strings.Contains(first, "Goal: ") {
			in, _ := json.Marshal(map[string]any{"status": "ok", "result": "(offline run: no model was called)"})
			r.Content = []anthropicBlock{{Type: "tool_use", ID: "offline_ret", Name: "return_result", Input: in}}
			r.StopReason = "tool_use"
			return r
		}
	}
	// Find the owner's last message and how many replies followed it.
	last, n := -1, 0
	for i := len(req.Messages) - 1; i >= 0; i-- {
		m := req.Messages[i]
		if m.Role == "user" {
			if _, ok := textOf(m); ok && !hasToolResult(m) {
				last = i
				break
			}
		}
	}
	for i := last + 1; i < len(req.Messages); i++ {
		if req.Messages[i].Role == "assistant" {
			n++
		}
	}
	task := strings.TrimSpace(p.c.Turns[len(p.c.Turns)-1])
	if last >= 0 {
		if text, _ := textOf(req.Messages[last]); !strings.Contains(text, task) {
			say("Noted.")
			return r
		}
	}
	switch {
	case len(p.c.Script) == 0 && n == 0:
		say("(offline run: no model was called, and this case has no script)")
	case n >= len(p.c.Script):
		say(fmt.Sprintf("(offline run: the script has no step %d)", n+1))
	default:
		st := p.c.Script[n]
		if st.Say != "" {
			say(st.Say)
			break
		}
		for i, call := range st.Calls {
			in, _ := json.Marshal(normalizeYAML(call.Args))
			if len(call.Args) == 0 {
				in = []byte(`{}`)
			}
			r.Content = append(r.Content, anthropicBlock{Type: "tool_use", ID: fmt.Sprintf("offline_%d_%d", n, i), Name: call.Tool, Input: in})
		}
		r.StopReason = "tool_use"
	}
	return r
}

func hasToolResult(m llmtypes.ChatMessage) bool {
	for _, b := range m.ContentBlocks {
		if b.Type == "tool_result" {
			return true
		}
	}
	return false
}

func (p *offlineProvider) StreamChat(ctx context.Context, req llmtypes.ChatRequest) (<-chan llmtypes.StreamEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r := p.reply(req)
	size := len(req.SystemPrompt)
	for _, s := range req.SlotBlocks {
		size += len(s.Content)
	}
	msgs, _ := json.Marshal(req.Messages)
	tools, _ := json.Marshal(req.Tools)
	r.Usage.InputTokens = (size + len(msgs) + len(tools)) / 4
	out, _ := json.Marshal(r.Content)
	r.Usage.OutputTokens = len(out)/4 + 1
	if p.record != nil {
		p.record(req, r)
	}
	return replyEvents(r), nil
}

func (p *offlineProvider) Complete(context.Context, llmtypes.ChatRequest) (string, error) {
	return "", errors.New("offline run: no completions")
}

func (p *offlineProvider) Capabilities() llmtypes.ProviderCapabilities {
	return llmtypes.ProviderCapabilities{SupportsToolCalling: true, MaxTokens: 8192, ContextWindowSize: 200000}
}
