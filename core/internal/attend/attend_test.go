package attend

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/hollis-labs/surface/core/internal/events"
	"github.com/hollis-labs/surface/core/internal/storage"
	"github.com/hollis-labs/surface/module"
)

func open(t *testing.T, path string, wins *[]module.Window) (*Service, *events.Bus) {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	bus := events.New()
	s, err := New(ctx, db, bus, func(context.Context) []module.Window { return *wins })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	return s, bus
}

func ev(topic string, v any) module.Event {
	raw, _ := json.Marshal(v)
	return module.Event{Topic: topic, Data: raw}
}

func states(s *Service) map[string]string {
	out := map[string]string{}
	for _, it := range s.List() {
		out[it.Subject] = it.State
	}
	return out
}

func TestAWindowTheAgentOpenedElsewhereIsUnseenUntilTheOwnerLooks(t *testing.T) {
	wins := []module.Window{{ID: "notes-1", View: "notes", Title: "ideas.md"}, {ID: "browser-2", View: "browser", Title: "Docs"}}
	s, _ := open(t, filepath.Join(t.TempDir(), "a.db"), &wins)
	ctx := context.Background()

	// Opened by the owner, or by the agent in front of the owner: nothing to say.
	s.on(ctx, ev("desk.opened", map[string]any{"window": "notes-1", "view": "notes", "opened_by": "owner"}))
	s.on(ctx, ev("desk.opened", map[string]any{"window": "x", "view": "notes", "opened_by": "agent", "focused": true}))
	if len(s.List()) != 0 {
		t.Fatalf("raised for nothing: %v", states(s))
	}
	s.on(ctx, ev("desk.opened", map[string]any{"window": "browser-2", "view": "browser", "opened_by": "agent"}))
	items := s.List()
	if len(items) != 1 || items[0].State != Unseen || items[0].Title != "Docs" || items[0].Window != "browser-2" {
		t.Fatalf("after the agent opened a window in the background: %+v", items)
	}
	// The agent bringing it in front is not the owner seeing it.
	s.on(ctx, ev("desk.focus", map[string]any{"window": "browser-2", "by": "agent"}))
	if len(s.List()) != 1 {
		t.Fatal("the agent saw it for the owner")
	}
	if err := s.Seen(module.WithActor(ctx, module.ActorAgent), "window:browser-2"); err == nil {
		t.Error("an agent marked it seen")
	}
	s.on(ctx, ev("desk.focus", map[string]any{"window": "browser-2", "by": "owner"}))
	if len(s.List()) != 0 {
		t.Errorf("still unseen after the owner looked: %v", states(s))
	}
}

func TestCardsThatAskNeedTheOwnerAndAnExpiredOneIsNotADecline(t *testing.T) {
	wins := []module.Window{}
	s, _ := open(t, filepath.Join(t.TempDir(), "a.db"), &wins)
	ctx := context.Background()
	s.on(ctx, ev("cards.changed", module.Card{ID: "c1", Kind: "approval", Title: "Publish the draft?", Source: "agent"}))
	s.on(ctx, ev("cards.changed", module.Card{ID: "c2", Kind: "error", Title: "Build failed", Source: "terminal"}))
	s.on(ctx, ev("cards.changed", module.Card{ID: "c3", Kind: "info", Title: "Saved"}))
	s.on(ctx, ev("cards.changed", module.Card{ID: "c4", Kind: "choice", Title: "Which one?", Source: "agent"}))
	got := states(s)
	if got["card:c1"] != NeedsYou || got["card:c2"] != Failed || got["card:c4"] != NeedsYou || got["card:c3"] != "" {
		t.Fatalf("states: %v", got)
	}
	if l := s.List(); l[0].Subject != "card:c1" || l[2].State != Failed {
		t.Errorf("order: %+v (want what needs you first, oldest first, then failures)", l)
	}
	// Looking at a question does not answer it.
	if err := s.Seen(module.WithActor(ctx, module.ActorOwner), "card:c1"); err != nil {
		t.Fatal(err)
	}
	if states(s)["card:c1"] != NeedsYou {
		t.Error("seeing a question answered it")
	}
	s.on(ctx, ev("cards.changed", module.Card{ID: "c1", Kind: "approval", Status: "submitted"}))
	s.on(ctx, ev("cards.changed", module.Card{ID: "c4", Kind: "choice", Status: "expired"}))
	got = states(s)
	if got["card:c1"] != "" || got["card:c4"] != Unseen {
		t.Fatalf("after one answered and one expired: %v", got)
	}
	if err := s.Seen(module.WithActor(ctx, module.ActorOwner), "card:c4"); err != nil {
		t.Fatal(err)
	}
	if states(s)["card:c4"] != "" {
		t.Error("an expired question stayed after the owner saw it")
	}
}

func TestItemsFollowTheBusSurviveARestartAndGoWithTheirWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.db")
	wins := []module.Window{{ID: "w1", View: "browser", Title: "Docs"}}
	s, bus := open(t, path, &wins)
	changed := make(chan struct{}, 4)
	stop := bus.Subscribe("attend.changed", func(module.Event) { changed <- struct{}{} })
	defer stop()
	bus.Publish("desk.opened", map[string]any{"window": "w1", "view": "browser", "opened_by": "reflex"})
	select {
	case <-changed:
	case <-time.After(2 * time.Second):
		t.Fatal("no attend.changed after a background open")
	}
	_ = s.Close(context.Background())

	again, _ := open(t, path, &wins)
	if got := states(again); got["window:w1"] != Unseen {
		t.Fatalf("after a restart: %v", got)
	}
	// The window is gone by the next start: so is its item.
	wins = nil
	third, _ := open(t, path, &wins)
	if len(third.List()) != 0 {
		t.Errorf("an item outlived its window: %v", states(third))
	}
}
