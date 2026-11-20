package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/hollis-labs/libs/agent/steer"
	nanite "github.com/hollis-labs/nanite/agent"
	"github.com/hollis-labs/surface/core/internal/reflex"
)

// naniteSteering reads live reflex definitions; Nanite owns matching, pending reminders,
// and per-conversation cooldowns. Surface retains the editable definitions and actions.
func (s *Service) naniteSteering() *nanite.Steering {
	registry, _ := steer.NewRegistry(steer.Preset{Name: "surface"})
	vocab := steer.NewVocabulary()
	for _, kind := range []string{reflex.ActionRunCommand, reflex.ActionCallTool} {
		_ = vocab.Register(kind, nil)
	}
	return &nanite.Steering{Registry: registry, Vocabulary: vocab, Reflexes: func() []steer.Reflex {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		sit := s.k.Situation(ctx)
		var out []steer.Reflex
		for _, r := range s.reflexes.Active(ctx, s.layoutReflexes(ctx, sit)) {
			trigger := r.Trigger
			// Nanite raises this event itself. Do not fire it a second time from the app bus.
			if r.TriggerKind == reflex.TriggerEvent {
				var v map[string]any
				if json.Unmarshal(trigger, &v) == nil && v["topic"] == TopicCompacted {
					v["topic"] = nanite.TopicCompacted
					trigger, _ = json.Marshal(v)
				}
			}
			out = append(out, steer.Reflex{Name: r.Name, About: r.About, TriggerKind: r.TriggerKind,
				Trigger: trigger, ActionKind: r.ActionKind, Action: r.Action, Cooldown: r.Cooldown, Enabled: true})
		}
		return out
	}}
}

func (s *Service) steerSituation(ctx context.Context, text string) steer.Situation {
	sit := s.k.Situation(ctx)
	out := steer.Situation{Input: text, Layout: sit.Layout, Source: "user", Signals: map[string]string{"mode": sit.Mode}}
	if f := sit.Focused; f != nil {
		out.Focused = &steer.Focus{View: f.View, Module: f.Module, Selection: f.Selection}
	}
	return out
}

func (s *Service) applySteered(fired []string, actions []steer.Action) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.reflexes.RecordFired(ctx, fired)
	for _, action := range actions {
		s.reflexes.ApplyAction(ctx, action.Kind, action.Spec)
	}
}

func (s *Service) steerHostEvent(ctx context.Context, topic, focus string) {
	w := s.wire
	view, err := w.svc.EnsureConversation(ctx, w.agent)
	if err != nil {
		return
	}
	id, err := w.naniteChatID(ctx, view.ConversationID)
	if err != nil {
		return
	}
	if topic != TopicCompacted {
		fired, err := w.nanite.SteerEvent(ctx, id, topic)
		if err != nil {
			s.log.Warn("event steering failed", "err", err)
		} else {
			s.applySteered(fired.Fired, fired.Actions)
		}
	}
	if focus != "" {
		fired, err := w.nanite.SteerFocus(ctx, id, focus)
		if err != nil {
			s.log.Warn("focus steering failed", "err", err)
		} else {
			s.applySteered(fired.Fired, fired.Actions)
		}
	}
}
