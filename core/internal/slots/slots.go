// Package slots decides what the embedded agent has in mind for a turn.
//
// The owner moves between kinds of work and talks to one agent throughout. A slot is what
// the agent brings to one kind of work: the tools that are top of mind there, standing
// instructions, what the module knows right now, the agent's own note of where things
// stand, and where the rest is kept. Moving to another slot changes those; it does not
// change who the agent is or which tools it has.
//
// Two parts, kept apart so either can be replaced:
//
//   - A Selector picks the active slots from the Situation. Today that is the window in
//     front, the user's mode, and a slot the agent pinned. Later it is a classifier over the
//     owner's input, activity and workspace; it gets the same Situation and returns the same
//     Selection, so nothing downstream changes.
//   - The Assembler turns a Selection into what the agent is told: the <context> block that
//     leads the owner's message, and the list of top-of-mind tools.
package slots

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/hollis-labs/surface/core/internal/kernel"
	"github.com/hollis-labs/surface/module"
)

// General is the slot in use when nothing more specific is: the core's own, with no
// module behind it.
var General = kernel.BoundSlot{Slot: module.Slot{
	Name:  "general",
	About: "whatever is in front of the owner",
	Where: "the open windows (desk_windows) and what each shows (window_context); collaboration marks (marks_list)",
	Tools: []string{"desk_windows", "desk_open", "desk_focus", "window_context", "desk_layout_list", "desk_run_command"},
}}

// Selection is the slots active for a turn, the first being the primary one.
type Selection struct {
	Slots []kernel.BoundSlot
	// Why says how they were chosen, for the panel: "focus", "mode", "pinned", "default".
	Why string
}

// Primary is the first active slot.
func (s Selection) Primary() kernel.BoundSlot {
	if len(s.Slots) == 0 {
		return General
	}
	return s.Slots[0]
}

// Names lists the active slots' names.
func (s Selection) Names() []string {
	out := make([]string, 0, len(s.Slots))
	for _, sl := range s.Slots {
		out = append(out, sl.Name)
	}
	return out
}

// Selector picks the active slots. Implementations must be quick and safe for concurrent use.
type Selector interface {
	Select(ctx context.Context, sit module.Situation, all []kernel.BoundSlot) Selection
}

// FocusSelector is today's selector: the window in front selects a slot; a set mode adds the
// slots that name it; a slot the agent pinned holds until the window in front selects
// another; with none of those, the slot stays where it was, or is General.
type FocusSelector struct {
	mu      sync.Mutex
	current string // the primary slot now
	pinned  bool
	// lastView is the view in front when current was chosen, so a pin holds only until the
	// owner moves to a different window.
	lastView string
}

// NewFocusSelector returns a selector starting in General.
func NewFocusSelector() *FocusSelector { return &FocusSelector{current: General.Name} }

// Pin makes name the primary slot until the window in front selects another. "" unpins.
func (f *FocusSelector) Pin(name, view string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if name == "" {
		f.pinned = false
		return
	}
	f.current, f.pinned, f.lastView = name, true, view
}

// Select implements Selector.
func (f *FocusSelector) Select(_ context.Context, sit module.Situation, all []kernel.BoundSlot) Selection {
	f.mu.Lock()
	defer f.mu.Unlock()
	view := ""
	if sit.Focused != nil {
		view = sit.Focused.View
	}
	why := "kept"
	if byView := forView(all, view); byView != "" {
		if !f.pinned || view != f.lastView {
			why = "focus"
			f.current, f.pinned, f.lastView = byView, false, view
		}
	}
	if f.pinned {
		why = "pinned"
	}
	sel := Selection{Why: why}
	if s, ok := named(all, f.current); ok {
		sel.Slots = append(sel.Slots, s)
	} else {
		f.current = General.Name
		sel.Slots = append(sel.Slots, General)
		if why == "kept" {
			sel.Why = "default"
		}
	}
	if sit.Mode != "" {
		for _, s := range all {
			if slices.Contains(s.Modes, sit.Mode) && s.Name != sel.Slots[0].Name {
				sel.Slots = append(sel.Slots, s)
			}
		}
		if len(sel.Slots) > 1 && sel.Why == "kept" {
			sel.Why = "mode"
		}
	}
	return sel
}

// forView is the slot a view in front selects, or "". A view "<module>.<name>" with no slot
// of its own selects its module's slot.
func forView(all []kernel.BoundSlot, view string) string {
	if view == "" {
		return ""
	}
	for _, s := range all {
		if slices.Contains(s.Views, view) {
			return s.Name
		}
	}
	mod, _, _ := strings.Cut(view, ".")
	for _, s := range all {
		if s.Module == mod && len(s.Views) == 0 {
			return s.Name
		}
	}
	return ""
}

// Named returns the slot called name among all, General included.
func Named(all []kernel.BoundSlot, name string) (kernel.BoundSlot, bool) { return named(all, name) }

func named(all []kernel.BoundSlot, name string) (kernel.BoundSlot, bool) {
	if name == General.Name {
		return General, true
	}
	for _, s := range all {
		if s.Name == name {
			return s, true
		}
	}
	return kernel.BoundSlot{}, false
}
