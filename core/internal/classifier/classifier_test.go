package classifier

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hollis-labs/surface/core/internal/kernel"
	"github.com/hollis-labs/surface/core/internal/slots"
	"github.com/hollis-labs/surface/module"
)

var testSlots = []kernel.BoundSlot{
	{Slot: module.Slot{Name: "notes", About: "notes", Views: []string{"notes"}, Modes: []string{"writing"}}, Module: "notes"},
	{Slot: module.Slot{Name: "scratch", About: "quick capture"}, Module: "scratch"},
}

func sitIn(view, mod string) module.Situation {
	return module.Situation{Focused: &module.WindowContext{View: view, Module: mod}}
}

func TestActOnPlainMessages(t *testing.T) {
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	for text, want := range map[string]string{
		"/layout focus":                     "command",
		"why is the build red?":             "question",
		"how do I add a reflex":             "question",
		"add a note about friday's release": "task",
		"could you tidy the layout":         "task",
		"no, I meant the other window":      "correction",
		"thanks!":                           "chat",
		"yes, go ahead":                     "approval",
		"hmm":                               "", // nothing to go on
	} {
		res, err := c.Classify(context.Background(), sitIn("notes", "notes"), testSlots, "", text)
		if err != nil {
			t.Fatal(err)
		}
		if got := res.Label(Act); got != want {
			fr := res.Facets[Act]
			t.Errorf("%q: act %q (top %q p=%.2f), want %q", text, got, fr.Top, fr.P, want)
		}
	}
}

func TestSlotFollowsWhatIsInFrontAndWhatIsNamed(t *testing.T) {
	c, _ := New()
	ctx := context.Background()
	res, _ := c.Classify(ctx, sitIn("notes", "notes"), testSlots, "", "tighten this paragraph")
	if res.Label(Slot) != "notes" {
		t.Errorf("view prior: %+v", res.Facets[Slot])
	}
	res, _ = c.Classify(ctx, sitIn("scratch.main", "scratch"), testSlots, "", "list what I captured")
	if res.Label(Slot) != "scratch" {
		t.Errorf("module prior: %+v", res.Facets[Slot])
	}
	res, _ = c.Classify(ctx, module.Situation{}, testSlots, "", "put that in my scratch list")
	if res.Facets[Slot].Top != "scratch" {
		t.Errorf("named slot: %+v", res.Facets[Slot])
	}
}

func TestNoSlotsNoSlotFacet(t *testing.T) {
	c, _ := New()
	res, err := c.Classify(context.Background(), module.Situation{}, nil, "", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.Facets[Slot]; ok {
		t.Fatal("a slot facet with nothing to choose")
	}
	if _, ok := res.Facets[Act]; !ok {
		t.Fatal("no act facet")
	}
}

func TestSlotLabelPrefersTheOwnersPin(t *testing.T) {
	pin, _ := json.Marshal(map[string]string{"slot": "scratch"})
	turn := slots.TraceTurn{Text: "x", Slot: "notes", Signals: []slots.TraceSignal{
		{Kind: "pin", Actor: "agent", Detail: pin},
	}}
	if l, w, corrected := SlotLabel(turn); l != "notes" || corrected || w != weightChosen {
		t.Errorf("agent pin: %s %v %v", l, w, corrected)
	}
	turn.Signals = append(turn.Signals, slots.TraceSignal{Kind: "pin", Actor: "owner", Detail: pin})
	if l, w, corrected := SlotLabel(turn); l != "scratch" || !corrected || w != weightCorrected {
		t.Errorf("owner pin: %s %v %v", l, w, corrected)
	}
}

func TestTheSlotModelLearnsCorrections(t *testing.T) {
	c, _ := New()
	ctx := context.Background()
	for i := 0; i < 30; i++ {
		c.Learn(slots.TraceTurn{Text: "capture this idea quickly", Slot: "notes"})
		c.Correct("capture this idea quickly", "scratch")
		c.Learn(slots.TraceTurn{Text: "rewrite the intro paragraph", Slot: "notes"})
	}
	// Nothing in front, nothing named: only the model can tell.
	res, _ := c.Classify(ctx, module.Situation{}, testSlots, "", "capture this idea")
	if res.Facets[Slot].Top != "scratch" {
		t.Errorf("corrections not learned: %+v", res.Facets[Slot].Probabilities)
	}
}

func TestReplayScoresCorrectionsBeforeLearningThem(t *testing.T) {
	pin, _ := json.Marshal(map[string]string{"slot": "scratch"})
	var turns []slots.TraceTurn
	for i := 0; i < 40; i++ {
		// The selector keeps giving notes; the owner keeps moving these to scratch.
		turns = append(turns, slots.TraceTurn{Text: "capture this idea for later", Slot: "notes",
			Signals: []slots.TraceSignal{{Kind: "pin", Actor: "owner", Detail: pin}}})
		turns = append(turns, slots.TraceTurn{Text: "tighten the intro paragraph", Slot: "notes", View: "notes", Module: "notes"})
	}
	rep, err := Replay(context.Background(), turns, testSlots)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Turns != 80 || rep.Corrected != 40 {
		t.Fatalf("report %+v", rep)
	}
	// The first corrections cannot be predicted; once learned, they are.
	if rep.CorrectedHits == 0 || rep.CorrectedHits == 40 {
		t.Errorf("corrected hits %d of 40", rep.CorrectedHits)
	}
}
