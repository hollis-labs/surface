package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hollis-labs/surface/module"
)

func TestTheClassifierOverrulesFocusOnlyWhenSwitchedOnAndSure(t *testing.T) {
	s, k, _, _ := setup(t, Options{})
	k.cfg = mapConfig{}
	ctx := context.Background()
	sit := k.Situation(ctx) // notes in front
	sit.Input = "add this to my scratch quick capture list"

	// Off (the default): the window in front picks.
	if sel := s.selector.Select(ctx, sit, k.Slots()); sel.Primary().Name != "notes" || sel.Why == "classified" {
		t.Fatalf("focus mode: %s (%s)", sel.Primary().Name, sel.Why)
	}

	// On, but the classifier is not sure enough yet: focus stands.
	k.cfg["agent_selector"] = selectorClassifier
	weak := s.selector.Select(ctx, sit, k.Slots())

	// Teach it: the owner keeps moving this kind of message to scratch.
	for i := 0; i < 40; i++ {
		s.cls.Correct("add this to my scratch quick capture list", "scratch")
		s.cls.Correct("tighten the wording of this paragraph", "notes")
	}
	sel := s.selector.Select(ctx, sit, k.Slots())
	if sel.Primary().Name != "scratch" || sel.Why != "classified" {
		t.Fatalf("classifier mode, after learning: %s (%s); before learning: %s (%s)", sel.Primary().Name, sel.Why, weak.Primary().Name, weak.Why)
	}
	if len(sel.Slots) != 2 || sel.Slots[1].Name != "notes" {
		t.Errorf("the focus slot should follow: %v", sel.Names())
	}

	// Nothing typed (the preview): focus.
	empty := k.Situation(ctx)
	if got := s.selector.Select(ctx, empty, k.Slots()); got.Primary().Name != "notes" {
		t.Errorf("empty input: %s", got.Primary().Name)
	}

	// An owner's pin wins over the classifier.
	raw, _ := json.Marshal(map[string]string{"slot": "notes"})
	if _, err := s.callOwnCtx(module.WithActor(ctx, module.ActorOwner), "context_switch", raw); err != nil {
		t.Fatal(err)
	}
	if got := s.selector.Select(ctx, sit, k.Slots()); got.Primary().Name != "notes" || got.Why != "pinned" {
		t.Errorf("pinned: %s (%s)", got.Primary().Name, got.Why)
	}

	// Switched back: focus again.
	k.cfg["agent_selector"] = selectorFocus
	s.pin.Pin("", "")
	if got := s.selector.Select(ctx, sit, k.Slots()); got.Primary().Name != "notes" {
		t.Errorf("back to focus: %s", got.Primary().Name)
	}
}

func TestSendUsesTheClassifiedSlot(t *testing.T) {
	s, k, _, _ := setup(t, Options{})
	k.cfg = mapConfig{"agent_selector": selectorClassifier}
	for i := 0; i < 40; i++ {
		s.cls.Correct("jot down quick capture idea", "scratch")
		s.cls.Correct("tighten the wording of this paragraph", "notes")
	}
	res, err := s.Send(context.Background(), "jot down quick capture idea", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Assembly.Slot != "scratch" || res.Assembly.Why != "classified" {
		t.Fatalf("assembly slot %q why %q", res.Assembly.Slot, res.Assembly.Why)
	}
}
