package slots

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

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
	// What the budget cut is said, for the context trace.
	if len(a.Trimmed) < 2 || a.Trimmed[0] != "a:clipped" || a.Trimmed[1] != "b:clipped" {
		t.Errorf("trimmed: %v, want both slots clipped", a.Trimmed)
	}
	for _, want := range []string{"Slot: a", "Also in play: b", "Reminder: keep me", "In front:"} {
		if !strings.Contains(a.Block, want) {
			t.Errorf("block lacks %q", want)
		}
	}
}

// A runner with a fixed room for the context drops a block that does not fit, whole. Under
// a hard Limit the block always fits, in bytes: knowledge goes first, then the modules'
// Around lines, then the notes, and as a last resort the block is cut with a marker.
func TestAssembleHardLimitAlwaysFits(t *testing.T) {
	big := strings.Repeat("é", 3000) // two bytes a character: a rune count would let it through
	sel := Selection{Slots: []kernel.BoundSlot{
		{Slot: module.Slot{Name: "notes", About: "notes"}, Knowledge: func(context.Context, module.Situation) (string, error) { return big, nil }},
		{Slot: module.Slot{Name: "drafts", About: "drafts"}, Knowledge: func(context.Context, module.Situation) (string, error) { return big, nil }},
	}}
	around := []AroundLine{{Module: "assist", Text: strings.Repeat("a", 1400)}, {Module: "frag", Text: strings.Repeat("f", 1400)}}
	for _, limit := range []int{7800, 3000, 1200} {
		a := Assemble(context.Background(), module.Situation{}, sel, nil, nil, Extra{Around: around, Reminders: []string{"remember"}, Limit: limit})
		if len(a.Block) > limit {
			t.Fatalf("limit %d: block is %d bytes", limit, len(a.Block))
		}
		if !strings.HasPrefix(a.Block, "<context>") || !strings.HasSuffix(strings.TrimSpace(a.Block), "</context>") {
			t.Fatalf("limit %d: the block lost its frame: %q…%q", limit, a.Block[:20], a.Block[len(a.Block)-20:])
		}
		if !utf8.ValidString(a.Block) {
			t.Fatalf("limit %d: cut inside a character", limit)
		}
		if len(a.Trimmed) == 0 {
			t.Fatalf("limit %d: nothing recorded as trimmed", limit)
		}
	}
	// What is in front is never trimmed by the soft cap; under a Limit a huge selection still fits, with what is in front kept.
	sit := module.Situation{Focused: &module.WindowContext{Title: "t", View: "a", Window: "w1", Selection: strings.Repeat("s", 20_000)}}
	if a := Assemble(context.Background(), sit, sel, nil, nil, Extra{Limit: 7800}); len(a.Block) > 7800 || !strings.Contains(a.Block, "In front") {
		t.Fatalf("selection: %d bytes, trimmed %v", len(a.Block), a.Trimmed)
	}
	// Without a Limit the soft cap still counts characters and only trims knowledge.
	a := Assemble(context.Background(), module.Situation{}, sel, nil, nil, Extra{Around: around})
	if len([]rune(a.Block)) > budgetBlock {
		t.Fatalf("soft cap: %d characters", len([]rune(a.Block)))
	}
}
