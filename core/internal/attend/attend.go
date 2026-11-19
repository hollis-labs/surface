// Package attend keeps what needs the owner, or has not been seen by them yet, in one place
// with one vocabulary, so the desk can say "this is waiting for you" the same way wherever it
// comes from (docs/attention.md).
//
// Items are derived from what happens on the app's bus, never claimed by an agent:
//
//   - a window a delegate (the agent, a reflex, an outside client) opened while the owner was
//     elsewhere is unseen until the owner brings it in front;
//   - an open card that asks something (a confirm, a choice, a form, an approval, a plan, or
//     anything blocking) needs the owner until it is answered or closed;
//   - an error card is a failure until the owner looks;
//   - a card that expired with nobody answering is unseen: lapsing is not declining, and the
//     owner should hear that it happened.
//
// Seeing is the owner's alone: bringing the window in front, or saying so (POST
// /api/attend/seen). Nothing the owner does raises an item.
package attend

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hollis-labs/surface/core/internal/storage"
	"github.com/hollis-labs/surface/module"
)

//go:embed migrations/*.sql
var migrations embed.FS

// The states, in the order the owner should see them.
const (
	NeedsYou = "needs_you" // waiting on the owner's answer
	Failed   = "failed"    // something went wrong and the owner has not looked
	Unseen   = "unseen"    // new since the owner last looked
	Working  = "working"   // the agent is on it; nothing to do but know
)

var rank = map[string]int{NeedsYou: 0, Failed: 1, Unseen: 2, Working: 3}

// AgentSubject is the item for the embedded agent's latest run.
const AgentSubject = "agent"

// Item is one thing waiting for the owner.
type Item struct {
	// Subject is what the item is about, and its key: "window:<id>" or "card:<id>".
	Subject string `json:"subject"`
	State   string `json:"state"`
	Title   string `json:"title,omitempty"`
	Reason  string `json:"reason,omitempty"`
	// Window is the window to bring in front to see it, when there is one; Card the card.
	Window string `json:"window,omitempty"`
	Card   string `json:"card,omitempty"`
	// By is who raised it: an actor, or the module or agent that showed a card.
	By    string `json:"by,omitempty"`
	Since string `json:"since"`
}

// Service keeps the items.
type Service struct {
	db      *sql.DB
	bus     module.Events
	windows func(context.Context) []module.Window

	mu    sync.Mutex
	items map[string]Item

	// events hands the bus's events to the worker. It is never closed: the bus may still be
	// delivering an event when Close runs (removing a subscription does not wait for a
	// delivery already under way), and a send on a closed channel would panic. quit ends the
	// worker instead.
	events    chan module.Event
	quit      chan struct{}
	unsubs    []func()
	done      chan struct{}
	closeOnce sync.Once
}

// New opens the store and starts following the bus. windows lists the desk's open windows.
func New(ctx context.Context, db *sql.DB, bus module.Events, windows func(context.Context) []module.Window) (*Service, error) {
	if err := storage.Migrate(ctx, db, "attend", migrations, "migrations"); err != nil {
		return nil, fmt.Errorf("attend: %w", err)
	}
	s := &Service{db: db, bus: bus, windows: windows, items: map[string]Item{}, events: make(chan module.Event, 256), quit: make(chan struct{}), done: make(chan struct{})}
	rows, err := db.QueryContext(ctx, `SELECT item FROM attend_items`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var raw string
		var it Item
		if rows.Scan(&raw) == nil && json.Unmarshal([]byte(raw), &it) == nil {
			s.items[it.Subject] = it
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	// Windows that are gone since the app last ran take their items with them.
	s.prune(ctx, openIDs(windows(ctx)))

	// The bus calls subscribers on the publisher's goroutine: hand the event to a worker,
	// so the desk is never slowed by a write here.
	for _, topic := range []string{"desk", "cards", "agent.run"} {
		s.unsubs = append(s.unsubs, bus.Subscribe(topic, func(ev module.Event) {
			select {
			case <-s.quit: // closing: nobody will read it
			case s.events <- ev:
			default: // full: dropping a notice is better than holding up the desk
			}
		}))
	}
	// The worker outlives New's caller, so it gets its own context, not the start-up one.
	wctx := context.WithoutCancel(ctx)
	go func() {
		defer close(s.done)
		for {
			select {
			case <-s.quit:
				return
			case ev := <-s.events:
				s.on(wctx, ev)
			}
		}
	}()
	return s, nil
}

// Close stops following the bus and waits for the worker. It is safe to call more than once,
// and while events are still being published.
func (s *Service) Close(context.Context) error {
	s.closeOnce.Do(func() {
		for _, u := range s.unsubs {
			u()
		}
		close(s.quit)
		<-s.done
	})
	return nil
}

// List is every item, the most pressing first: what needs the owner, then failures, then
// what is new, each oldest first.
func (s *Service) List() []Item {
	s.mu.Lock()
	out := make([]Item, 0, len(s.items))
	for _, it := range s.items {
		out = append(out, it)
	}
	s.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if rank[out[i].State] != rank[out[j].State] {
			return rank[out[i].State] < rank[out[j].State]
		}
		if out[i].Since != out[j].Since {
			return out[i].Since < out[j].Since
		}
		return out[i].Subject < out[j].Subject
	})
	return out
}

// Counts says how many items there are in each state.
func (s *Service) Counts() map[string]int {
	out := map[string]int{NeedsYou: 0, Failed: 0, Unseen: 0, Working: 0}
	for _, it := range s.List() {
		out[it.State]++
	}
	return out
}

// Seen marks a subject seen by the owner. A delegate cannot see for the owner.
func (s *Service) Seen(ctx context.Context, subject string) error {
	if module.ActorOf(ctx).Delegate() {
		return fmt.Errorf("attend: only the owner sees")
	}
	s.mu.Lock()
	it, ok := s.items[subject]
	if ok && (it.State == NeedsYou || it.State == Working) {
		// Looking at a question does not answer it, and looking at work does not finish it.
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	if ok {
		s.drop(ctx, subject)
		s.changed()
	}
	return nil
}

// on applies one event from the bus.
func (s *Service) on(ctx context.Context, ev module.Event) {
	changed := false
	switch ev.Topic {
	case "desk.opened":
		var o struct {
			Window   string `json:"window"`
			View     string `json:"view"`
			OpenedBy string `json:"opened_by"`
			Focused  bool   `json:"focused"`
		}
		if json.Unmarshal(ev.Data, &o) != nil || !module.Actor(o.OpenedBy).Delegate() || o.Focused {
			return
		}
		title := o.View
		for _, w := range s.windows(ctx) {
			if w.ID == o.Window {
				title = w.Title
			}
		}
		changed = s.put(ctx, Item{Subject: "window:" + o.Window, State: Unseen, Title: title, Window: o.Window, By: o.OpenedBy,
			Reason: "opened by " + who(o.OpenedBy) + " while you were in another window"})
	case "desk.focus":
		var f struct {
			Window string `json:"window"`
			By     string `json:"by"`
		}
		if json.Unmarshal(ev.Data, &f) != nil || module.Actor(f.By).Delegate() || f.Window == "" {
			return
		}
		// In front, by the owner's own move: what it was waiting to show has been seen.
		for _, it := range s.List() {
			if it.Window == f.Window && it.State != NeedsYou && !strings.HasPrefix(it.Subject, "held:") {
				s.drop(ctx, it.Subject)
				changed = true
			}
		}
	case "desk.held":
		var h struct {
			Window string `json:"window"`
			By     string `json:"by"`
		}
		if json.Unmarshal(ev.Data, &h) != nil || h.Window == "" {
			return
		}
		title := h.Window
		for _, w := range s.windows(ctx) {
			if w.ID == h.Window {
				title = w.Title
			}
		}
		changed = s.put(ctx, Item{Subject: "held:" + h.Window, State: Unseen, Title: title, Window: h.Window, By: h.By,
			Reason: who(h.By) + " has a change for this window, waiting until you are done in it"})
	case "desk.changed":
		var st struct {
			Wins []struct {
				ID   string          `json:"id"`
				Held json.RawMessage `json:"held"`
			} `json:"wins"`
		}
		if json.Unmarshal(ev.Data, &st) != nil {
			return
		}
		open, held := map[string]bool{}, map[string]bool{}
		for _, w := range st.Wins {
			open[w.ID] = true
			held[w.ID] = len(w.Held) > 0 && string(w.Held) != "null"
		}
		changed = s.prune(ctx, open)
		// A held change applied or dropped: nothing waits any more.
		for _, it := range s.List() {
			if strings.HasPrefix(it.Subject, "held:") && open[it.Window] && !held[it.Window] {
				s.drop(ctx, it.Subject)
				changed = true
			}
		}
	case "agent.run.started":
		changed = s.put(ctx, Item{Subject: AgentSubject, State: Working, Title: "The agent", By: "agent", Reason: "working on your message"})
	case "agent.run.ended":
		var e struct {
			Status string `json:"status"`
			Reason string `json:"reason"`
		}
		if json.Unmarshal(ev.Data, &e) != nil {
			return
		}
		switch e.Status {
		case "finished":
			// The answer is in the panel; it is news until the owner has seen the panel.
			changed = s.put(ctx, Item{Subject: AgentSubject, State: Unseen, Title: "The agent", By: "agent", Reason: "answered; not yet read"})
		case "failed":
			reason := "the run failed"
			if e.Reason != "" {
				reason += ": " + e.Reason
			}
			changed = s.put(ctx, Item{Subject: AgentSubject, State: Failed, Title: "The agent", By: "agent", Reason: reason})
		default:
			// Canceled: the owner stopped it, or it was let go. Nothing to tell.
			s.mu.Lock()
			_, had := s.items[AgentSubject]
			s.mu.Unlock()
			if had {
				s.drop(ctx, AgentSubject)
				changed = true
			}
		}
	case "cards.changed":
		var c module.Card
		if json.Unmarshal(ev.Data, &c) != nil || c.ID == "" {
			return
		}
		changed = s.card(ctx, c)
	}
	if changed {
		s.changed()
	}
}

// asking are the card kinds that wait on the owner's answer.
var asking = []string{"confirm", "prompt", "choice", "form", "approval", "plan"}

func (s *Service) card(ctx context.Context, c module.Card) bool {
	subject := "card:" + c.ID
	s.mu.Lock()
	was, had := s.items[subject]
	s.mu.Unlock()
	title := c.Title
	if title == "" {
		title = c.Kind
	}
	switch {
	case c.Status == "" && (c.Blocking || slices.Contains(asking, c.Kind)):
		if had && was.State == NeedsYou {
			return false
		}
		return s.put(ctx, Item{Subject: subject, State: NeedsYou, Title: title, Card: c.ID, By: c.Source, Reason: who(c.Source) + " is waiting on your answer"})
	case c.Status == "" && c.Kind == "error":
		if had {
			return false
		}
		return s.put(ctx, Item{Subject: subject, State: Failed, Title: title, Card: c.ID, By: c.Source, Reason: "an error from " + who(c.Source)})
	case c.Status == "expired" && had && was.State == NeedsYou:
		// It lapsed with nobody answering. That is not a no: say it happened.
		return s.put(ctx, Item{Subject: subject, State: Unseen, Title: title, Card: c.ID, By: c.Source, Reason: "expired before you answered"})
	case c.Status != "" && had && (c.Status != "expired" || was.State != Unseen):
		s.drop(ctx, subject)
		return true
	}
	return false
}

// put keeps an item, keeping the time it was first raised when it was there already.
func (s *Service) put(ctx context.Context, it Item) bool {
	s.mu.Lock()
	if old, ok := s.items[it.Subject]; ok && old.State == it.State {
		it.Since = old.Since
	}
	if it.Since == "" {
		it.Since = time.Now().UTC().Format(time.RFC3339Nano)
	}
	s.items[it.Subject] = it
	s.mu.Unlock()
	raw, _ := json.Marshal(it)
	_, _ = s.db.ExecContext(context.WithoutCancel(ctx), `INSERT INTO attend_items (subject, item) VALUES (?, ?)
		ON CONFLICT(subject) DO UPDATE SET item=excluded.item`, it.Subject, string(raw))
	return true
}

func (s *Service) drop(ctx context.Context, subject string) {
	s.mu.Lock()
	delete(s.items, subject)
	s.mu.Unlock()
	_, _ = s.db.ExecContext(context.WithoutCancel(ctx), `DELETE FROM attend_items WHERE subject=?`, subject)
}

// prune drops the items of windows that are no longer open.
func (s *Service) prune(ctx context.Context, open map[string]bool) bool {
	changed := false
	for _, it := range s.List() {
		if it.Window != "" && !open[it.Window] {
			s.drop(ctx, it.Subject)
			changed = true
		}
	}
	return changed
}

func (s *Service) changed() {
	s.bus.Publish("attend.changed", map[string]any{"items": s.List(), "counts": s.Counts()})
}

func openIDs(ws []module.Window) map[string]bool {
	out := map[string]bool{}
	for _, w := range ws {
		out[w.ID] = true
	}
	return out
}

// who names an actor or a card's source for a reason line.
func who(by string) string {
	switch by {
	case "agent":
		return "the agent"
	case "reflex":
		return "a reflex"
	case "outside":
		return "an outside client"
	case "":
		return "the app"
	}
	return strings.TrimSpace(by)
}
