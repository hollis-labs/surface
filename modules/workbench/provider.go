package workbench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hollis-labs/libs/llm/core/llmcontracts"
	"github.com/hollis-labs/libs/llm/core/llmtypes"
)

// gatewayProvider is a small Anthropic Messages client over the app's model gateway: the
// model provider the Workbench hands Nanite's evals (evals.Config.Provider) and uses for the
// improvement loop.
//
// It exists because Nanite v0.6.0 builds its own providers in internal/provider, which a
// host cannot import, while nanite/evals needs a provider value for a live run. When Nanite
// exports a constructor this file goes and runtime.go calls it instead.
//
// It sends one non-streaming request per call and replays the reply as stream events. The
// key is read from the key file at each request and sent as a bearer token; it is never
// logged, and error text from the gateway is cut short and scrubbed.
type gatewayProvider struct {
	url     string
	keyFile string
	client  *http.Client
	// record, when set, sees each request and the reply that came back.
	record func(req llmtypes.ChatRequest, reply anthropicReply)
}

var _ llmcontracts.Provider = (*gatewayProvider)(nil)

func newGatewayProvider(url, keyFile string) *gatewayProvider {
	return &gatewayProvider{url: strings.TrimRight(url, "/"), keyFile: keyFile, client: &http.Client{Timeout: 5 * time.Minute}}
}

type anthropicBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type anthropicMessage struct {
	Role    string           `json:"role"`
	Content []anthropicBlock `json:"content"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    []anthropicBlock   `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
}

type anthropicReply struct {
	Content    []anthropicBlock `json:"content"`
	StopReason string           `json:"stop_reason"`
	Usage      struct {
		InputTokens         int `json:"input_tokens"`
		OutputTokens        int `json:"output_tokens"`
		CacheCreationTokens int `json:"cache_creation_input_tokens"`
		CacheReadTokens     int `json:"cache_read_input_tokens"`
	} `json:"usage"`
}

// defaultMaxTokens is the output allowance of a request that names none.
const defaultMaxTokens = 4096

func toAnthropic(req llmtypes.ChatRequest) anthropicRequest {
	out := anthropicRequest{Model: req.Model, MaxTokens: req.MaxTokens}
	if out.MaxTokens <= 0 {
		out.MaxTokens = defaultMaxTokens
	}
	if req.SystemPrompt != "" {
		out.System = append(out.System, anthropicBlock{Type: "text", Text: req.SystemPrompt})
	}
	for _, s := range req.SlotBlocks {
		if s.Content != "" {
			out.System = append(out.System, anthropicBlock{Type: "text", Text: s.Content})
		}
	}
	for _, m := range req.Messages {
		role := "user"
		if m.Role == "assistant" {
			role = "assistant"
		}
		var blocks []anthropicBlock
		if len(m.ContentBlocks) == 0 && m.Content != "" {
			blocks = append(blocks, anthropicBlock{Type: "text", Text: m.Content})
		}
		for _, b := range m.ContentBlocks {
			switch b.Type {
			case "text":
				if b.Text != "" {
					blocks = append(blocks, anthropicBlock{Type: "text", Text: b.Text})
				}
			case "tool_use":
				in := json.RawMessage(`{}`)
				if b.Input != nil {
					if raw, err := json.Marshal(*b.Input); err == nil {
						in = raw
					}
				}
				blocks = append(blocks, anthropicBlock{Type: "tool_use", ID: b.ID, Name: b.Name, Input: in})
			case "tool_result":
				blocks = append(blocks, anthropicBlock{Type: "tool_result", ToolUseID: b.ToolUseID, Content: b.Content, IsError: b.IsError})
			}
		}
		if len(blocks) == 0 {
			continue // the Messages API refuses an empty message
		}
		out.Messages = append(out.Messages, anthropicMessage{Role: role, Content: blocks})
	}
	for _, t := range req.Tools {
		schema := t.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		out.Tools = append(out.Tools, anthropicTool{Name: t.Name, Description: t.Description, InputSchema: schema})
	}
	return out
}

var scrubRe = regexp.MustCompile(`(?i)(sk-[a-z0-9_-]{6,}|bearer\s+\S+|(api[_-]?key|token|secret)["':= ]+\S+)`)

// scrub makes gateway error text fit to show: credentials removed, cut to 300 characters.
func scrub(s string) string {
	s = scrubRe.ReplaceAllString(s, "[redacted]")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 300 {
		s = s[:297] + "..."
	}
	return s
}

func (p *gatewayProvider) send(ctx context.Context, req llmtypes.ChatRequest) (anthropicReply, error) {
	var reply anthropicReply
	key, err := os.ReadFile(p.keyFile) //nolint:gosec // the key file the owner's agent config names
	if err != nil {
		return reply, errors.New("workbench: the gateway key file cannot be read")
	}
	body, err := json.Marshal(toAnthropic(req))
	if err != nil {
		return reply, err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url+"/v1/messages", bytes.NewReader(body)) //nolint:gosec // the gateway the owner's agent config names
	if err != nil {
		return reply, err
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("anthropic-version", "2023-06-01")
	hr.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(key)))
	resp, err := p.client.Do(hr) //nolint:gosec // the gateway named in the owner's agent config
	if err != nil {
		return reply, fmt.Errorf("workbench: gateway: %s", scrub(err.Error()))
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return reply, err
	}
	if resp.StatusCode/100 != 2 {
		return reply, fmt.Errorf("workbench: gateway answered %d: %s", resp.StatusCode, scrub(string(raw)))
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return reply, fmt.Errorf("workbench: gateway reply: %w", err)
	}
	if p.record != nil {
		p.record(req, reply)
	}
	return reply, nil
}

// StreamChat sends the request and replays the reply as events.
func (p *gatewayProvider) StreamChat(ctx context.Context, req llmtypes.ChatRequest) (<-chan llmtypes.StreamEvent, error) {
	reply, err := p.send(ctx, req)
	if err != nil {
		return nil, err
	}
	return replyEvents(reply), nil
}

// replyEvents replays a Messages API reply as stream events.
func replyEvents(reply anthropicReply) <-chan llmtypes.StreamEvent {
	var events []llmtypes.StreamEvent
	for i, b := range reply.Content {
		switch b.Type {
		case "text":
			if b.Text != "" {
				events = append(events, llmtypes.StreamEvent{Type: llmtypes.EventDelta, Content: b.Text, BlockID: fmt.Sprintf("b%d", i)})
			}
		case "tool_use":
			in := map[string]any{}
			if len(b.Input) > 0 {
				_ = json.Unmarshal(b.Input, &in)
			}
			events = append(events, llmtypes.StreamEvent{Type: llmtypes.EventToolUse, ToolUse: &llmtypes.ToolUseBlock{ID: b.ID, Name: b.Name, Input: in}})
		}
	}
	stop := reply.StopReason
	if stop == "" {
		stop = llmtypes.StopReasonEndTurn
	}
	u := reply.Usage
	events = append(events,
		llmtypes.StreamEvent{Type: llmtypes.EventUsage, Usage: &llmtypes.Usage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens,
			CacheCreationTokens: u.CacheCreationTokens, CacheReadTokens: u.CacheReadTokens, StopReason: stop}},
		llmtypes.StreamEvent{Type: llmtypes.EventDone})
	ch := make(chan llmtypes.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return ch
}

// Complete sends the request and returns the reply's text.
func (p *gatewayProvider) Complete(ctx context.Context, req llmtypes.ChatRequest) (string, error) {
	reply, err := p.send(ctx, req)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, c := range reply.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	return b.String(), nil
}

// Capabilities are the Messages API's.
func (p *gatewayProvider) Capabilities() llmtypes.ProviderCapabilities {
	return llmtypes.ProviderCapabilities{SupportsToolCalling: true, MaxTokens: 8192, ContextWindowSize: 200000}
}

// usageMeter counts the tokens a run has used and stops it at its budget.
type usageMeter struct {
	mu     sync.Mutex
	budget int
	used   int
	over   bool
	cancel context.CancelFunc
}

func (m *usageMeter) add(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.used += n
	if m.budget > 0 && m.used > m.budget && !m.over {
		m.over = true
		if m.cancel != nil {
			m.cancel()
		}
	}
}

func (m *usageMeter) state() (used int, over bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.used, m.over
}

// budgeted wraps a provider: each request is refused once the run's token budget is spent,
// and each reply's usage is counted.
type budgeted struct {
	llmcontracts.Provider
	meter *usageMeter
}

var errBudget = errors.New("workbench: the run's token budget is spent")

func (b *budgeted) StreamChat(ctx context.Context, req llmtypes.ChatRequest) (<-chan llmtypes.StreamEvent, error) {
	if _, over := b.meter.state(); over {
		return nil, errBudget
	}
	in, err := b.Provider.StreamChat(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make(chan llmtypes.StreamEvent, 16)
	go func() {
		defer close(out)
		for ev := range in {
			if ev.Type == llmtypes.EventUsage && ev.Usage != nil {
				u := ev.Usage
				b.meter.add(u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheCreationTokens)
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				for range in {
				}
				return
			}
		}
	}()
	return out, nil
}

func (b *budgeted) Complete(ctx context.Context, req llmtypes.ChatRequest) (string, error) {
	if _, over := b.meter.state(); over {
		return "", errBudget
	}
	text, err := b.Provider.Complete(ctx, req)
	if err == nil {
		// Complete reports no usage: count an estimate, four characters a token.
		n := len(req.SystemPrompt) + len(text)
		for _, s := range req.SlotBlocks {
			n += len(s.Content)
		}
		for _, m := range req.Messages {
			n += len(m.Content)
			for _, c := range m.ContentBlocks {
				n += len(c.Text) + len(c.Content)
			}
		}
		b.meter.add(n / 4)
	}
	return text, err
}
