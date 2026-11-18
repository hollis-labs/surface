package cards

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/hollis-labs/surface/core/internal/storage"
	"github.com/hollis-labs/surface/module"
)

type recorder struct {
	mu   sync.Mutex
	seen []module.Card
}

func (r *recorder) Publish(topic string, data any) {
	if c, ok := data.(module.Card); ok && topic == "cards.changed" {
		r.mu.Lock()
		r.seen = append(r.seen, c)
		r.mu.Unlock()
	}
}

func open(t *testing.T) (*Store, *recorder) {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = storage.MigrateCore(ctx, db); err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	return New(db, rec), rec
}

func payload(s string) json.RawMessage { return json.RawMessage(s) }

func TestACardIsOpenUntilItIsAnsweredAndThenStaysClosed(t *testing.T) {
	ctx := context.Background()
	s, rec := open(t)
	c, err := s.Show(ctx, module.Card{Kind: "choice", Source: "agent", Title: "Which one?",
		Payload: payload(`{"question":"Pick","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	if c.ID == "" || c.Status != "" || c.Revision != 1 || c.Version != "1" {
		t.Fatalf("shown: %+v", c)
	}
	if open, _ := s.List(ctx, false); len(open) != 1 {
		t.Fatalf("%d open cards, want 1", len(open))
	}

	done, err := s.Respond(ctx, c.ID, module.CardOutcome{Status: "submitted", Answers: []module.CardAnswer{{QuestionID: "choice", Value: "b"}}})
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != "submitted" || done.Outcome == nil || done.Outcome.Answers[0].Value != "b" || done.Revision != 2 || done.Responded == "" {
		t.Errorf("answered: %+v", done)
	}
	// A second answer (another tab, a double click) is refused and changes nothing.
	again, err := s.Respond(ctx, c.ID, module.CardOutcome{Status: "canceled"})
	if !errors.Is(err, ErrClosed) || again.Status != "submitted" {
		t.Errorf("a second answer: %v, card %+v; want ErrClosed and the first answer kept", err, again)
	}
	if _, err := s.Respond(ctx, c.ID, module.CardOutcome{Status: "partial"}); err == nil {
		t.Error("an answer that is neither submitted nor canceled was accepted")
	}
	if open, _ := s.List(ctx, false); len(open) != 0 {
		t.Errorf("%d open cards after the answer, want 0", len(open))
	}
	if all, _ := s.List(ctx, true); len(all) != 1 {
		t.Errorf("%d cards in all, want 1", len(all))
	}
	if n := len(rec.seen); n != 2 {
		t.Errorf("%d announcements, want 2 (shown, answered)", n)
	}
}

func TestAKnownKindWithoutWhatItNeedsIsRefused(t *testing.T) {
	ctx := context.Background()
	s, _ := open(t)
	for name, c := range map[string]module.Card{
		"no kind":              {},
		"prompt, no question":  {Kind: "prompt", Payload: payload(`{}`)},
		"choice, no options":   {Kind: "choice", Payload: payload(`{"question":"q"}`)},
		"choice, repeated ids": {Kind: "choice", Payload: payload(`{"question":"q","options":[{"id":"a","label":"A"},{"id":"a","label":"B"}]}`)},
		"form, no schema":      {Kind: "form", Payload: payload(`{"body":"fill in"}`)},
		"approval, odd risk":   {Kind: "approval", Risk: "extreme", Payload: payload(`{"summary":"s"}`)},
		"plan, no steps":       {Kind: "plan", Payload: payload(`{"steps":[]}`)},
		"payload not json":     {Kind: "info", Payload: payload(`{`)},
	} {
		if _, err := s.Show(ctx, c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// A kind of a module's own is kept as given.
	if _, err := s.Show(ctx, module.Card{Kind: "notes.conflict", Source: "notes", Payload: payload(`{"anything":true}`)}); err != nil {
		t.Errorf("a module's own kind: %v", err)
	}
}

func TestProgressIsUpdatedInPlaceAndASameSubjectCardReplacesTheOpenOne(t *testing.T) {
	ctx := context.Background()
	s, _ := open(t)
	p, _ := s.Show(ctx, module.Card{Kind: "progress", Source: "agent", Subject: "import", Payload: payload(`{"label":"Importing","value":1,"max":4}`)})
	up, err := s.Update(ctx, p.ID, "", payload(`{"label":"Importing","value":3,"max":4}`))
	if err != nil || up.Revision != 2 || string(up.Payload) != `{"label":"Importing","value":3,"max":4}` {
		t.Fatalf("updated: %+v, %v", up, err)
	}

	next, _ := s.Show(ctx, module.Card{Kind: "progress", Source: "agent", Subject: "import", Payload: payload(`{"label":"Again"}`)})
	old, _, _ := s.Get(ctx, p.ID)
	if old.Status != "superseded" || old.SupersededBy != next.ID {
		t.Errorf("the first card: status %q superseded_by %q; want superseded by %s", old.Status, old.SupersededBy, next.ID)
	}
	if _, err = s.Update(ctx, p.ID, "", payload(`{"label":"late"}`)); !errors.Is(err, ErrClosed) {
		t.Errorf("updating a closed card: %v, want ErrClosed", err)
	}
	// The same subject from another sender is another card.
	other, _ := s.Show(ctx, module.Card{Kind: "progress", Source: "files", Subject: "import", Payload: payload(`{}`)})
	if still, _, _ := s.Get(ctx, next.ID); still.Status != "" || other.ID == next.ID {
		t.Errorf("a card from another sender closed this one: %+v", still)
	}
	ended, err := s.End(ctx, next.ID, "canceled")
	if err != nil || ended.Status != "canceled" {
		t.Errorf("ended: %+v, %v", ended, err)
	}
	if _, err := s.End(ctx, next.ID, "submitted"); err == nil {
		t.Error("the sender ended a card as submitted, which only the user's answer can")
	}
}

func TestAwaitReturnsTheAnswerAndACardLapsesByTheServersClock(t *testing.T) {
	ctx := context.Background()
	s, _ := open(t)
	c, _ := s.Show(ctx, module.Card{Kind: "confirm", Source: "agent", Payload: payload(`{"body":"Go ahead?"}`)})
	got := make(chan module.Card, 1)
	go func() {
		done, _ := s.Await(ctx, c.ID)
		got <- done
	}()
	time.Sleep(30 * time.Millisecond)
	if _, err := s.Respond(ctx, c.ID, module.CardOutcome{Status: "canceled"}); err != nil {
		t.Fatal(err)
	}
	select {
	case done := <-got:
		if done.Status != "canceled" {
			t.Errorf("awaited %+v, want canceled", done)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Await did not return after the answer")
	}

	// A waiter gives up when its context ends, and the card stays open.
	open2, _ := s.Show(ctx, module.Card{Kind: "confirm", Source: "agent", Payload: payload(`{"body":"Still there?"}`)})
	short, cancel := context.WithTimeout(ctx, 40*time.Millisecond)
	defer cancel()
	if _, err := s.Await(short, open2.ID); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Await past its context: %v", err)
	}

	// An answer after the card's time is refused: the server's clock decides, not the page's.
	clock := time.Now()
	s.now = func() time.Time { return clock }
	exp, _ := s.Show(ctx, module.Card{Kind: "confirm", Source: "agent", Payload: payload(`{"body":"Quick"}`),
		ExpiresAt: clock.Add(time.Minute).UTC().Format(time.RFC3339)})
	clock = clock.Add(2 * time.Minute)
	late, err := s.Respond(ctx, exp.ID, module.CardOutcome{Status: "submitted"})
	if !errors.Is(err, ErrClosed) || late.Status != "expired" {
		t.Errorf("a late answer: %v, card %+v; want ErrClosed and expired", err, late)
	}
	done, err := s.Await(ctx, exp.ID)
	if err != nil || done.Status != "expired" {
		t.Errorf("awaiting a lapsed card: %+v, %v", done, err)
	}
}
