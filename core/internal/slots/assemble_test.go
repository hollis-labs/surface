package slots

import (
	"context"
	"strings"
	"testing"

	"github.com/hollis-labs/surface/core/internal/kernel"
	"github.com/hollis-labs/surface/module"
)

func TestBudgets(t *testing.T) {
	big := func(n int) func(context.Context, module.Situation) (string, error) {
		return func(context.Context, module.Situation) (string, error) { return strings.Repeat("x", n), nil }
	}
	sel := Selection{Slots: []kernel.BoundSlot{
		{Slot: module.Slot{Name: "a", About: "a"}, Knowledge: big(50_000)},
		{Slot: module.Slot{Name: "b", About: "b"}, Knowledge: big(50_000)},
	}}
	sit := module.Situation{Focused: &module.WindowContext{Title: "t", View: "a", Window: "w1", Selection: strings.Repeat("s", 20_000)}}
	a := Assemble(context.Background(), sit, sel, nil, nil, Extra{Reminders: []string{"keep me"}})
	if n := len([]rune(a.Block)); n > budgetBlock+200 {
		t.Fatalf("block is %d characters", n)
	}
	if len([]rune(a.Slots[0].Knowledge)) > budgetPrimary+1 || len([]rune(a.Slots[1].Knowledge)) > budgetSecondary+1 {
		t.Fatalf("knowledge %d / %d", len(a.Slots[0].Knowledge), len(a.Slots[1].Knowledge))
	}
	for _, want := range []string{"Slot: a", "Also in play: b", "Reminder: keep me", "In front:"} {
		if !strings.Contains(a.Block, want) {
			t.Errorf("block lacks %q", want)
		}
	}
}
