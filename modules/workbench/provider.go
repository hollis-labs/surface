package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/hollis-labs/libs/agent/agentdir"
	"github.com/hollis-labs/libs/llm/core/llmcontracts"
	"github.com/hollis-labs/libs/llm/core/llmtypes"
	"github.com/hollis-labs/libs/security/redact"
	"github.com/hollis-labs/nanite/agent/authored"
	"github.com/hollis-labs/nanite/agent/providers"
	"github.com/hollis-labs/surface/module"
	"gopkg.in/yaml.v3"
)

// The scope used by Workbench's provider errors. Provider constructors add resolved
// keys here; scrub reads this same scope before an error is persisted or displayed.
var providerSecrets = redact.NewScope()
var providerRedactor, _ = redact.New(redact.WithScope(providerSecrets))

func newGatewayProvider(t Tree, gw module.Gateway) (llmcontracts.Provider, error) {
	var profile agentdir.Profile
	if err := yaml.Unmarshal(t["host.yaml"], &profile); err != nil {
		return nil, err
	}
	name := profile.Runtime.Provider
	if name == "" {
		name = providers.Anthropic
	}
	wire := authored.WireAnthropic
	auth := authored.AuthBearer
	url := strings.TrimRight(gw.URL, "/")
	switch name {
	case providers.Anthropic:
	case providers.OpenAI:
		wire = authored.WireOpenAI
		auth = authored.AuthDefault
		if !strings.HasSuffix(url, "/v1") {
			url += "/v1"
		}
	default:
		return nil, fmt.Errorf("workbench: unsupported gateway provider %q", name)
	}
	// NewNamed with an explicit key_ref has no fallback to an unrelated provider
	// key when the app's gateway file is unavailable.
	spec := authored.ProviderSpec{Name: "workbench-gateway", Wire: wire, BaseURL: url,
		KeyRef: "file://" + gw.KeyFile, Auth: auth}
	options := providers.Options{Redactor: providerSecrets}
	base, err := providers.NewNamed(spec, options)
	if err != nil {
		return nil, err
	}
	return &requestProvider{Provider: base, spec: spec, options: options}, nil
}

// requestProvider preserves per-request key resolution even when a wire client
// caches the first key it resolves. It stores configuration references only.
type requestProvider struct {
	llmcontracts.Provider
	spec    authored.ProviderSpec
	options providers.Options
}

func (p *requestProvider) StreamChat(ctx context.Context, req llmtypes.ChatRequest) (<-chan llmtypes.StreamEvent, error) {
	base, err := providers.NewNamed(p.spec, p.options)
	if err != nil {
		return nil, err
	}
	return base.StreamChat(ctx, req)
}

func (p *requestProvider) Complete(ctx context.Context, req llmtypes.ChatRequest) (string, error) {
	base, err := providers.NewNamed(p.spec, p.options)
	if err != nil {
		return "", err
	}
	answer, err := base.Complete(ctx, req)
	if err != nil {
		return "", errors.New(scrub(err.Error()))
	}
	return answer, nil
}

var scrubRe = regexp.MustCompile(`(?i)(sk-[a-z0-9_-]{6,}|bearer\s+\S+|(api[_-]?key|token|secret)["':= ]+\S+)`)

func scrub(s string) string {
	s = providerRedactor.Text(s)
	s = scrubRe.ReplaceAllString(s, "[redacted]")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 300 {
		s = s[:297] + "..."
	}
	return s
}

// These reply values only drive the deterministic offline model and test gateway.
type anthropicBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   any             `json:"content,omitempty"`
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
	Stream    bool               `json:"stream"`
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

// usageMeter keeps a run's tokens against its budget. Every request first reserves its
// most expensive outcome (its input, estimated generously, plus its whole output
// allowance); a request whose reservation would pass the budget is refused and the run is
// stopped. When the reply comes, the reservation is settled to what was used. So requests
// running side by side (parallel subtasks) cannot together pass the budget, as nanite/evals'
// dollar meter works.
type usageMeter struct {
	mu       sync.Mutex
	budget   int
	used     int
	reserved int
	over     bool
	cancel   context.CancelFunc
}

// reserve holds n tokens; false (and the run stopped) when that would pass the budget.
func (m *usageMeter) reserve(n int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.over || (m.budget > 0 && m.used+m.reserved+n > m.budget) {
		if !m.over && m.cancel != nil {
			m.cancel()
		}
		m.over = true
		return false
	}
	m.reserved += n
	return true
}

// settle releases a reservation and records what was used.
func (m *usageMeter) settle(reserved, used int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reserved -= reserved
	m.used += used
}

func (m *usageMeter) state() (used int, over bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.used, m.over
}

// worstTokens is a request's most expensive outcome: its input at three characters a token
// and its whole output allowance.
func worstTokens(req llmtypes.ChatRequest) int {
	n := len(req.SystemPrompt)
	for _, s := range req.SlotBlocks {
		n += len(s.Content)
	}
	msgs, _ := json.Marshal(req.Messages)
	tools, _ := json.Marshal(req.Tools)
	return (n+len(msgs)+len(tools))/3 + 1 + outAllowance(req)
}

func outAllowance(req llmtypes.ChatRequest) int {
	if req.MaxTokens > 0 {
		return req.MaxTokens
	}
	return defaultMaxTokens
}

// budgeted wraps a provider with the run's token budget.
type budgeted struct {
	llmcontracts.Provider
	meter *usageMeter
}

var errBudget = errors.New("workbench: the run's token budget is spent")

func (b *budgeted) StreamChat(ctx context.Context, req llmtypes.ChatRequest) (<-chan llmtypes.StreamEvent, error) {
	hold := worstTokens(req)
	if !b.meter.reserve(hold) {
		return nil, errBudget
	}
	in, err := b.Provider.StreamChat(ctx, req)
	if err != nil {
		b.meter.settle(hold, 0)
		return nil, err
	}
	out := make(chan llmtypes.StreamEvent, 16)
	go func() {
		defer close(out)
		used, reported := 0, false
		defer func() {
			if !reported {
				used = hold // no usage came back: charge the reservation
			}
			b.meter.settle(hold, used)
		}()
		for ev := range in {
			if ev.Type == llmtypes.EventUsage && ev.Usage != nil {
				u := ev.Usage
				used += u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheCreationTokens
				reported = true
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
	hold := worstTokens(req)
	if !b.meter.reserve(hold) {
		return "", errBudget
	}
	text, err := b.Provider.Complete(ctx, req)
	if err != nil {
		b.meter.settle(hold, hold)
		return text, err
	}
	// Complete reports no usage: charge the input estimate and the answer's length.
	b.meter.settle(hold, hold-outAllowance(req)+len(text)/4+1)
	return text, nil
}
