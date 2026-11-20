package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	chat "github.com/hollis-labs/libs/agent/chat"
	libreflex "github.com/hollis-labs/libs/agent/core/reflexes"
	"github.com/hollis-labs/libs/agent/host"
	chatstream "github.com/hollis-labs/libs/ui/ui-go/chatstream"

	"github.com/hollis-labs/surface/core/internal/agent/hostrunner"
)

// The host runner for the conversation wire: the app's agent on libs agent/host (Basalisk's
// runtime), behind the same wire as Nanite's, with every feature of the wire (late answers,
// module errands, the input lease, the run log, approvals in the chat).
//
// What it does with context, as Basalisk did:
//
//   - The system prompt is fixed: Surface's own, with the config's system after it.
//   - The context block for the turn goes in front of the owner's message, in the same stored
//     message, so the history the model is sent stays byte-stable and the cache holds.
//   - When the thread's last request passed compact_at tokens (default 110,000), the older
//     part is summarized once and the recent turns are kept whole.
//
// One host thread serves one conversation; the threads, their summaries and runs are the
// host's tables in the app's database, so a restart resumes the same thread.

// TopicCompacted is published on the app's bus when the host runner has summarized the older
// part of a conversation's thread: {conversation_id, before, after}, sizes in tokens.
const TopicCompacted = "agent.compacted"

// RunnerHost names the host runner.
const RunnerHost = "host"

// hostSeed leads the first turn of a conversation's thread when the conversation already
// had runs on another runner (Nanite): the thread starts fresh, and the agent is told where
// what came before is.
const hostSeed = `[A note from the app, not the owner: this conversation went on before your history here begins. The earlier messages are not in your history; they are in the conversation's transcript, which the owner can scroll back through. What carries over is in the context block: your notes on each slot and the slot's standing instructions. Pick up from those, and ask the owner if you need something from before.]`

// openHostRunner opens the app's agent on libs agent/host with the app's tools.
func (s *Service) openHostRunner(ctx context.Context, w *wire) (*hostrunner.Runner, error) {
	if s.cfgErr != nil {
		return nil, fmt.Errorf("agent: config: %w", s.cfgErr)
	}
	c := s.cfg
	if c.Model == "" || c.GatewayURL == "" || c.KeyFile == "" {
		return nil, fmt.Errorf("agent: the host runner needs model, gateway_url and key_file in %s", ConfigPath(s.k.AppName()))
	}
	db := s.k.DB()
	dir := c.AgentDir(s.k.AppName())
	// The standing text the fixed system prompt is built with: a later change to it is
	// carried in the context block until the app restarts (standing.go).
	if dir != "" {
		s.noteStandingLoaded(dir)
	}
	r, err := hostrunner.Open(ctx, hostrunner.Options{
		Config: host.Config{
			Model:     c.Model,
			BaseURL:   c.GatewayURL,
			KeyFile:   c.KeyFile,
			System:    systemPrompt(s.k.AppName(), joinNonEmpty(agentDirText(dir, s.log), c.System)),
			MaxTokens: c.MaxTokens,
			MaxSteps:  c.MaxSteps,
			CompactAt: compactAt(c.CompactAt),
		},
		DB:    db,
		Tools: s.hostTools(),
		Ask: func(_ context.Context, call hostrunner.Call) bool {
			return w.asks.has(call.ConversationID, call.Tool)
		},
		Granted: func(ctx context.Context, call hostrunner.Call) bool {
			ok, gerr := granted(ctx, db, w.agent, call.Tool)
			if gerr != nil {
				s.log.Warn("standing approvals not read; asking", "err", gerr)
			}
			return ok
		},
		Seed: func(ctx context.Context, conversation string) string {
			if w.ranBefore(ctx, conversation) {
				return hostSeed
			}
			return ""
		},
		// A compaction is told on the app's bus (agent.compacted), where a reflex reminds the
		// agent to bring its notes and standing up to date (the seeded save-state).
		Compacted: func(conversation string, before, after int) {
			s.k.Events().Publish(TopicCompacted, map[string]any{"conversation_id": conversation, "before": before, "after": after})
		},
		Logger: s.log.With("part", "host"),
	})
	if err != nil {
		return nil, fmt.Errorf("agent: host runner: %w", err)
	}
	cfg := r.Config()
	s.log.Info("the agent runs on libs agent/host", "tools", len(s.engineTools()), "compact_at", cfg.CompactAt,
		"max_steps", cfg.MaxSteps, "max_tokens", cfg.MaxTokens)
	return r, nil
}

// compactAt is the config's compact_at, or Basalisk's 110,000 when it is not set. Negative
// turns compaction off.
func compactAt(n int) int {
	if n == 0 {
		return hostrunner.DefaultCompactAt
	}
	return n
}

// hostTools is the app's tools as the host runner takes them: the same list, levels and
// approver the engine gets, and how long each may run.
func (s *Service) hostTools() []hostrunner.Tool {
	var out []hostrunner.Tool
	for _, t := range s.engineTools() {
		out = append(out, hostrunner.Tool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema, Level: t.Level, Run: t.Run,
			WaitsForOwner: waitsForOwner(t)})
	}
	return out
}

// waitsForOwner reports whether a tool waits for the owner's answer: by the app's
// convention such a tool takes timeout_seconds, how long to wait (card_ask, genui_show,
// design_approve). It may then run for up to 70 minutes; any other tool for 5.
func waitsForOwner(t EngineTool) bool {
	props, _ := t.InputSchema["properties"].(map[string]any)
	_, ok := props["timeout_seconds"]
	return ok
}

// ranBefore reports whether a conversation has an agent's run on its stream already: one
// from before its host thread (the runner it had before). A thread's first turn is asked
// about before its own run is on the stream.
func (w *wire) ranBefore(ctx context.Context, conversation string) bool {
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for rec, err := range w.store.After(rctx, conversation, 0) {
		if err != nil {
			return false
		}
		if rec.Name != string(chatstream.VerbRunStart) {
			continue
		}
		// A turn the app answered itself is recorded in the thread; it is not history from
		// before it.
		var start struct {
			Provider string `json:"provider"`
		}
		if json.Unmarshal(rec.Data, &start) == nil && start.Provider == chat.ProviderHost {
			continue
		}
		return true
	}
	return false
}

// hostHistory is the conversation so far as the reflexes see it, most recent first, read
// from the host thread.
func (w *wire) hostHistory(ctx context.Context, conversation string) []libreflex.MessageSignal {
	msgs, err := w.host.History(ctx, conversation)
	if err != nil {
		return nil
	}
	var prior []libreflex.MessageSignal
	for i := len(msgs) - 1; i >= 0 && len(prior) < 40; i-- {
		m := msgs[i]
		var text, tools []string
		for _, b := range m.Blocks {
			switch b.Type {
			case "text":
				text = append(text, b.Text)
			case "tool_use":
				tools = append(tools, b.Name)
			}
		}
		content := strings.Join(text, "\n")
		if m.Role == "user" {
			var meta struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(m.Meta, &meta)
			if meta.Text == "" {
				continue // tool results only
			}
			content = meta.Text // what was said, without the context block
		}
		prior = append(prior, libreflex.MessageSignal{Role: m.Role, Content: content, CreatedAt: m.CreatedAt,
			ToolCalls: len(tools), ToolNames: tools})
	}
	return prior
}
