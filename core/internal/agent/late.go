package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hollis-labs/libs/agent/chat"
	"github.com/hollis-labs/surface/module"
)

// Late answers. The agent asks the owner with card_ask and waits; when its run ends first
// (stopped, the app shut down) the card is abandoned, and when its wait runs out it expires.
// The owner can still answer such a card once (cards.Store.Respond). That answer is handed
// to the agent here, as a turn on its conversation, the way a module's errand is: meta
// {auto, errand: "late-answer", title}, kept out of the context trace, run-log origin
// errand:late-answer.
//
// The queue is the cards themselves: a late answer submitted and not yet delivered
// (cards.Store.LateAnswers). It is in the core database, so a restart does not lose one.
// One goroutine delivers, in the order they were given; it marks a card delivered once its
// turn was accepted, so none is sent twice. When the agent is busy it tries again when a run
// ends, and every few seconds while any wait.

// LateErrand is the errand name a late answer's turn carries.
const LateErrand = "late-answer"

// lateRetry is how often waiting late answers are tried again while the agent is busy.
const lateRetry = 2 * time.Second

// lateCards is what the delivery needs of the card store (cards.Store).
type lateCards interface {
	LateAnswers(ctx context.Context) ([]module.Card, error)
	MarkDelivered(ctx context.Context, id string) (bool, error)
}

// lateDelivery runs the delivery of late answers.
type lateDelivery struct {
	wake chan struct{}
	stop func()
	done chan struct{}
	mu   sync.Mutex
	// warned is the cards already logged as not deliverable (no conversation wire).
	warned map[string]bool
}

// startLate starts delivering late answers. Without a card store that keeps them it does
// nothing.
func (s *Service) startLate() {
	store, ok := s.k.Cards().(lateCards)
	if !ok || store == nil {
		return
	}
	d := &lateDelivery{wake: make(chan struct{}, 1), done: make(chan struct{}), warned: map[string]bool{}}
	poke := func() {
		select {
		case d.wake <- struct{}{}:
		default:
		}
	}
	unsubCards := s.k.Events().Subscribe("cards.changed", func(ev module.Event) {
		var c module.Card
		if json.Unmarshal(ev.Data, &c) == nil && c.Late && c.Status == "submitted" && c.Delivered == "" {
			poke()
		}
	})
	unsubRuns := s.k.Events().Subscribe("agent.run.ended", func(module.Event) { poke() })
	ctx, cancel := context.WithCancel(context.Background())
	d.stop = func() { unsubCards(); unsubRuns(); cancel() }
	s.late = d
	go s.lateLoop(ctx, store, d)
	poke() // what was waiting when the app last stopped
}

func (s *Service) stopLate() {
	if s.late != nil {
		s.late.stop()
		<-s.late.done
	}
}

func (s *Service) lateLoop(ctx context.Context, store lateCards, d *lateDelivery) {
	defer close(d.done)
	var retry <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.wake:
		case <-retry:
		}
		retry = nil
		if waiting := s.deliverLate(ctx, store, d); waiting {
			retry = time.After(lateRetry)
		}
	}
}

// deliverLate hands each waiting late answer to the agent. It reports whether any is still
// waiting because the agent was busy.
func (s *Service) deliverLate(ctx context.Context, store lateCards, d *lateDelivery) bool {
	list, err := store.LateAnswers(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Warn("late answers not read", "err", err)
		}
		return true
	}
	for i := 0; i < len(list); {
		c := list[i]
		if s.wire == nil {
			d.mu.Lock()
			if !d.warned[c.ID] {
				d.warned[c.ID] = true
				s.log.Warn("a late answer to the agent's card cannot be delivered: the agent is not on the conversation wire; it is kept on the card",
					"card", c.ID, "title", c.Title)
			}
			d.mu.Unlock()
			i++
			continue
		}
		// One turn per agent: the run of answers meant for the same one.
		target := lateTarget(c, s.wire.agent)
		end := i + 1
		for end < len(list) && lateTarget(list[end], s.wire.agent) == target {
			end++
		}
		batch := list[i:end]
		var run string
		var err error
		if len(batch) > 1 {
			run, err = s.submitLateBatch(ctx, batch, target)
		} else {
			run, err = s.submitLate(ctx, c, target)
		}
		if errors.Is(err, chat.ErrBusy) {
			return true // the rest wait behind it, in order
		}
		var rejected *chat.RejectedError
		if err != nil && !errors.As(err, &rejected) {
			if ctx.Err() == nil {
				s.log.Warn("a late answer was not delivered; it will be tried again", "card", c.ID, "err", err)
			}
			return true
		}
		if rejected != nil {
			// The host's own hook refused the turn: trying again would be refused again.
			s.log.Warn("a late answer's turn was refused by the host", "card", c.ID, "reason", rejected.Message)
		}
		for _, item := range batch {
			if _, merr := store.MarkDelivered(context.WithoutCancel(ctx), item.ID); merr != nil {
				s.log.Warn("a late answer was delivered but not marked so", "card", item.ID, "err", merr)
			}
		}
		if run != "" {
			s.k.Events().Publish("agent.errand", map[string]any{"run_id": run, "module": LateErrand, "title": c.Title, "card": c.ID})
		}
		i += len(batch)
	}
	return false
}

// submitLateBatch submits multiple waiting late/held answers together as one consolidated turn.
// lateTarget is the agent a late answer goes to: the seat that asked, or the embedded agent.
func lateTarget(c module.Card, embedded string) string {
	if strings.HasPrefix(c.ReplyTo, SeatPrefix) {
		return c.ReplyTo
	}
	return embedded
}

func (s *Service) submitLateBatch(ctx context.Context, batch []module.Card, target string) (string, error) {
	title := fmt.Sprintf("Answers to %d cards", len(batch))
	meta, _ := json.Marshal(errandMeta{Auto: true, Errand: LateErrand, Title: title})
	view, err := s.wire.svc.EnsureConversation(ctx, target)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[The owner's answers to %d of your cards, submitted from the Cards window:]\n", len(batch))
	for _, c := range batch {
		t := cardTitle(c)
		fmt.Fprintf(&b, "\n### %s (%s, id %s)\n%s\n", t, c.Kind, c.ID, answerText(c.Outcome))
	}
	r, err := s.wire.svc.Submit(ctx, view.ConversationID, chat.Turn{Text: strings.TrimSpace(b.String()), Meta: meta})
	if err != nil {
		return "", err
	}
	return r.RunID, nil
}

// submitLate submits one late answer as a turn on the agent's conversation.
func (s *Service) submitLate(ctx context.Context, c module.Card, target string) (string, error) {
	title := cardTitle(c)
	if r := []rune(title); len(r) > 200 {
		title = string(r[:200])
	}
	meta, _ := json.Marshal(errandMeta{Auto: true, Errand: LateErrand, Title: title})
	view, err := s.wire.svc.EnsureConversation(ctx, target)
	if err != nil {
		return "", err
	}
	r, err := s.wire.svc.Submit(ctx, view.ConversationID, chat.Turn{Text: lateText(c, title), Meta: meta})
	if err != nil {
		return "", err
	}
	return r.RunID, nil
}

// cardTitle is what a card is called in the late answer's message.
func cardTitle(c module.Card) string {
	if t := strings.TrimSpace(c.Title); t != "" {
		return t
	}
	var p map[string]any
	_ = json.Unmarshal(c.Payload, &p)
	for _, k := range []string{"question", "summary", "body"} {
		if v, ok := p[k].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return c.Kind + " card " + c.ID
}

// lateText is the late answer's turn: what it is, then the answer in readable form.
func lateText(c module.Card, title string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[The owner's answer to your card %q (%s, id %s), given after the run that asked it ended. Not a reply in the current exchange: act on it if it still matters.]\n\n",
		title, c.Kind, c.ID)
	b.WriteString(answerText(c.Outcome))
	return b.String()
}

// answerText writes an outcome compactly: a form's data as JSON, answers and decisions one
// per line.
func answerText(o *module.CardOutcome) string {
	if o == nil {
		return "(no answer)"
	}
	var lines []string
	if len(o.Data) > 0 {
		raw, err := json.Marshal(o.Data)
		if err == nil {
			lines = append(lines, "data: "+string(raw))
		}
	}
	for _, a := range o.Answers {
		v := fmt.Sprint(a.Value)
		if raw, err := json.Marshal(a.Value); err == nil {
			if _, isString := a.Value.(string); !isString {
				v = string(raw)
			}
		}
		line := a.QuestionID + ": " + v
		if a.Note != "" {
			line += " (" + a.Note + ")"
		}
		lines = append(lines, line)
	}
	for _, d := range o.Decisions {
		line := d.ItemID + ": " + d.Action
		if d.Note != "" {
			line += " (" + d.Note + ")"
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "submitted, with nothing filled in"
	}
	return strings.Join(lines, "\n")
}
