package cards

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
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
		"approval, odd risk":         {Kind: "approval", Risk: "extreme", Payload: payload(`{"summary":"s"}`)},
		"approval, bad view_mode":    {Kind: "approval", Payload: payload(`{"summary":"s","view_mode":"unknown"}`)},
		"approval, bad change op":    {Kind: "approval", Payload: payload(`{"summary":"s","changes":[{"label":"theme","op":"deleted"}]}`)},
		"approval, callout no title": {Kind: "approval", Payload: payload(`{"summary":"s","callouts":[{"tone":"warning"}]}`)},
		"approval, bad callout tone": {Kind: "approval", Payload: payload(`{"summary":"s","callouts":[{"tone":"critical","title":"Watch out"}]}`)},
		"plan, no steps":             {Kind: "plan", Payload: payload(`{"steps":[]}`)},
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
	// An entity card without a ref, data, or title is refused; with a ref or data it is accepted.
	if _, err := s.Show(ctx, module.Card{Kind: "entity", Source: "agent", Payload: payload(`{}`)}); err == nil {
		t.Error("empty entity card was accepted")
	}
	if _, err := s.Show(ctx, module.Card{Kind: "entity", Source: "agent", Payload: payload(`{"ref":"ops:phase-1","options":[{"id":"a","label":"A"},{"id":"a","label":"Dup"}]}`)}); err == nil {
		t.Error("entity card with duplicate option ids was accepted")
	}
	entCard, err := s.Show(ctx, module.Card{Kind: "entity", Source: "agent", Payload: payload(`{"ref":"ops:phase-1","entity_kind":"ops.workstream","view":"card","question":"Approve phase 1?","options":[{"id":"approve","label":"Approve"}]}`)})
	if err != nil {
		t.Fatalf("entity card with question and option: %v", err)
	}
	if !AnswerableCard(entCard) {
		t.Errorf("entity card with question/options should be answerable: %+v", entCard)
	}
	displayOnly, err := s.Show(ctx, module.Card{Kind: "ops.workstream", Source: "agent", Payload: payload(`{"ref":"ops:phase-1","view":"compact"}`)})
	if err != nil {
		t.Fatalf("display-only entity card: %v", err)
	}
	if AnswerableCard(displayOnly) {
		t.Errorf("display-only entity card should not be answerable: %+v", displayOnly)
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
	exp, _ := s.Show(ctx, module.Card{Kind: "confirm", Source: "notes", Payload: payload(`{"body":"Quick"}`),
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

// An agent's question the agent stopped waiting for (its run ended: abandoned; its wait ran
// out: expired) takes one late answer, marked late and queued for the agent. Anything else
// that is closed still refuses an answer.
func TestALateAnswerIsTakenOnceForAnAgentsQuestionOnly(t *testing.T) {
	ctx := context.Background()
	s, _ := open(t)
	ask := func(source, kind, body string) module.Card {
		t.Helper()
		c, err := s.Show(ctx, module.Card{Kind: kind, Source: source, Title: "Ship?", Payload: payload(body)})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	form := `{"schema":{"type":"object","properties":{"name":{"type":"string"}}}}`

	// End takes abandoned.
	ab := ask("agent", "form", form)
	ended, err := s.End(ctx, ab.ID, Abandoned)
	if err != nil || ended.Status != Abandoned || !LateAnswerable(ended) {
		t.Fatalf("abandoned: %+v, %v", ended, err)
	}
	late, err := s.Respond(ctx, ab.ID, module.CardOutcome{Status: "submitted", Data: map[string]any{"name": "Ada"}})
	if err != nil || late.Status != "submitted" || !late.Late || late.Outcome == nil || late.Outcome.Data["name"] != "Ada" || late.Delivered != "" {
		t.Fatalf("a late answer to an abandoned card: %+v, %v", late, err)
	}
	// Once only: a second late answer is refused and changes nothing.
	again, err := s.Respond(ctx, ab.ID, module.CardOutcome{Status: "submitted", Data: map[string]any{"name": "Bob"}})
	if !errors.Is(err, ErrClosed) || again.Outcome.Data["name"] != "Ada" {
		t.Fatalf("a second late answer: %v, %+v", err, again)
	}
	if raw, _ := json.Marshal(late); !strings.Contains(string(raw), `"late":true`) {
		t.Errorf("the card's JSON does not say it was answered late: %s", raw)
	}

	// An agent's question that expired takes one too.
	clock := time.Now()
	s.now = func() time.Time { return clock }
	exp, _ := s.Show(ctx, module.Card{Kind: "confirm", Source: "agent", Payload: payload(`{"body":"Go?"}`),
		ExpiresAt: clock.Add(time.Minute).UTC().Format(time.RFC3339)})
	clock = clock.Add(2 * time.Minute)
	if got, _, _ := s.Get(ctx, exp.ID); got.Status != "expired" {
		t.Fatalf("not expired: %+v", got)
	}
	expLate, err := s.Respond(ctx, exp.ID, module.CardOutcome{Status: "submitted"})
	if err != nil || !expLate.Late || expLate.Status != "submitted" {
		t.Fatalf("a late answer to an expired agent card: %+v, %v", expLate, err)
	}

	// A canceled late answer only dismisses the card: it is not queued for the agent.
	dis := ask("agent", "prompt", `{"question":"Name?"}`)
	_, _ = s.End(ctx, dis.ID, Abandoned)
	gone, err := s.Respond(ctx, dis.ID, module.CardOutcome{Status: "canceled"})
	if err != nil || gone.Status != "canceled" || !gone.Late {
		t.Fatalf("dismissed: %+v, %v", gone, err)
	}

	// Refused: a module's abandoned question, an agent's notice, an agent's question the
	// owner declined or the agent closed.
	mod := ask("notes", "confirm", `{"body":"x"}`)
	_, _ = s.End(ctx, mod.ID, Abandoned)
	notice := ask("agent", "info", `{"body":"x"}`)
	_, _ = s.End(ctx, notice.ID, "expired")
	declined := ask("agent", "confirm", `{"body":"x"}`)
	_, _ = s.Respond(ctx, declined.ID, module.CardOutcome{Status: "canceled"})
	closed := ask("agent", "confirm", `{"body":"x"}`)
	_, _ = s.End(ctx, closed.ID, "canceled")
	for name, c := range map[string]module.Card{"module's": mod, "notice": notice, "declined": declined, "closed by the agent": closed} {
		if _, refused := s.Respond(ctx, c.ID, module.CardOutcome{Status: "submitted"}); !errors.Is(refused, ErrClosed) {
			t.Errorf("%s: a late answer was not refused: %v", name, refused)
		}
	}

	// The queue: submitted late answers not yet delivered, oldest first; marked once.
	queue, err := s.LateAnswers(ctx)
	if err != nil || len(queue) != 2 || queue[0].ID != ab.ID || queue[1].ID != exp.ID {
		t.Fatalf("the late answers waiting: %+v, %v", queue, err)
	}
	if first, err := s.MarkDelivered(ctx, ab.ID); !first || err != nil {
		t.Fatalf("marking delivered: %v %v", first, err)
	}
	if second, _ := s.MarkDelivered(ctx, ab.ID); second {
		t.Error("a late answer was marked delivered twice")
	}
	if queue, _ = s.LateAnswers(ctx); len(queue) != 1 || queue[0].ID != exp.ID {
		t.Errorf("after one delivery: %+v", queue)
	}
	if got, _, _ := s.Get(ctx, ab.ID); got.Delivered == "" {
		t.Errorf("the card does not say when it was delivered: %+v", got)
	}
}

// After a start, the agent's questions left open by an app that was killed are abandoned,
// and can be answered late. Its notices and a module's questions stay open.
func TestOpenAgentQuestionsAreAbandonedAtStart(t *testing.T) {
	ctx := context.Background()
	s, _ := open(t)
	q, _ := s.Show(ctx, module.Card{Kind: "choice", Source: "agent", Payload: payload(`{"question":"Which?","options":[{"id":"a","label":"A"}]}`)})
	n, _ := s.Show(ctx, module.Card{Kind: "progress", Source: "agent", Payload: payload(`{}`)})
	m, _ := s.Show(ctx, module.Card{Kind: "confirm", Source: "notes", Payload: payload(`{"body":"x"}`)})
	if count, err := s.AbandonOrphans(ctx); err != nil || count != 1 {
		t.Fatalf("abandoned %d, %v; want 1", count, err)
	}
	for id, want := range map[string]string{q.ID: Abandoned, n.ID: "", m.ID: ""} {
		if c, _, _ := s.Get(ctx, id); c.Status != want {
			t.Errorf("%s: %q, want %q", id, c.Status, want)
		}
	}
	// It is offered for a late answer until it has one, and for LateWindow at most.
	if late, err := s.LateOpen(ctx); err != nil || len(late) != 1 || late[0].ID != q.ID {
		t.Errorf("offered late: %+v, %v", late, err)
	}
	s.now = func() time.Time { return time.Now().Add(LateWindow + time.Hour) }
	if late, _ := s.LateOpen(ctx); len(late) != 0 {
		t.Errorf("offered past the window: %+v", late)
	}
}
