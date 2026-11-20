package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/hollis-labs/libs/agent/chat"
	"github.com/hollis-labs/surface/core/internal/slots"
	"github.com/hollis-labs/surface/module"
)

// What the modules bring the agent besides their slots: the lines they carry with every
// message (Manifest.Around), and the errands they hand it (Host.Agent().Ask).

// aroundTimeout bounds one module's Around: it runs on every message.
const aroundTimeout = 1500 * time.Millisecond

// extra is what goes into the context block beside the slots, before routing and reflexes.
func (s *Service) extra(ctx context.Context, sit module.Situation) slots.Extra {
	return slots.Extra{Instructions: s.layoutInstructions(ctx, sit), Around: s.around(ctx)}
}

// around reads every module's Around.
func (s *Service) around(ctx context.Context) []slots.AroundLine {
	var out []slots.AroundLine
	for _, a := range s.k.Arounds() {
		actx, cancel := context.WithTimeout(ctx, aroundTimeout)
		text, err := a.Read(actx)
		cancel()
		if err != nil {
			s.log.Debug("a module's around was not read", "module", a.Module, "err", err)
			continue
		}
		if text = strings.TrimSpace(text); text == "" {
			continue
		}
		if r := []rune(text); len(r) > slots.BudgetAround {
			text = string(r[:slots.BudgetAround]) + "…"
		}
		out = append(out, slots.AroundLine{Module: a.Module, Text: text})
	}
	return out
}

// errandMeta marks a turn a module asked for: the router does not route it, and the context
// trace (the classifier's training data, which is the owner's messages) does not record it.
type errandMeta struct {
	Auto   bool   `json:"auto"`
	Errand string `json:"errand"`
	Title  string `json:"title,omitempty"`
}

// AskFrom runs a module's errand as a turn on the agent's conversation (kernel.Asker).
func (s *Service) AskFrom(ctx context.Context, mod string, a module.Ask) (string, error) {
	text := strings.TrimSpace(a.Text)
	if text == "" {
		return "", errors.New("an errand needs text")
	}
	if s.wire == nil {
		return "", module.ErrNoAgent
	}
	title := a.Title
	if r := []rune(title); len(r) > 200 {
		title = string(r[:200])
	}
	meta, _ := json.Marshal(errandMeta{Auto: true, Errand: mod, Title: title})
	view, err := s.wire.svc.EnsureConversation(ctx, s.wire.agent)
	if err != nil {
		return "", err
	}
	head := "[An errand from the " + mod + " module, not a message from the owner"
	if title != "" {
		head += ": " + title
	}
	r, err := s.wire.svc.Submit(ctx, view.ConversationID, chat.Turn{Text: head + "]\n\n" + text, Meta: meta})
	if errors.Is(err, chat.ErrBusy) {
		return "", module.ErrAgentBusy
	}
	if err != nil {
		return "", err
	}
	s.k.Events().Publish("agent.errand", map[string]any{"run_id": r.RunID, "module": mod, "title": title})
	return r.RunID, nil
}

// errandOf is the module that asked for a turn, or "".
func errandOf(meta json.RawMessage) string {
	var m errandMeta
	if len(meta) == 0 || json.Unmarshal(meta, &m) != nil {
		return ""
	}
	return m.Errand
}
