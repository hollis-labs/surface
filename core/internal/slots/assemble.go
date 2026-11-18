package slots

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hollis-labs/surface/core/internal/kernel"
	"github.com/hollis-labs/surface/core/internal/storage"
	"github.com/hollis-labs/surface/module"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Notes are the agent's own notes, one per slot: where that kind of work stands. They are
// kept in the core database and put in front of the agent when the slot is active.
type Notes struct{ db *sql.DB }

// OpenNotes migrates the notes table into db.
func OpenNotes(ctx context.Context, db *sql.DB) (*Notes, error) {
	if err := storage.Migrate(ctx, db, "slots", migrations, "migrations"); err != nil {
		return nil, err
	}
	return &Notes{db: db}, nil
}

// Get returns the note on a slot and when it was written; "" when there is none.
func (n *Notes) Get(ctx context.Context, slot string) (note, updated string) {
	if n == nil {
		return "", ""
	}
	err := n.db.QueryRowContext(ctx, `SELECT note, updated_at FROM slot_notes WHERE slot = ?`, slot).Scan(&note, &updated)
	if err != nil {
		return "", ""
	}
	return note, updated
}

// Set replaces the note on a slot. An empty note removes it.
func (n *Notes) Set(ctx context.Context, slot, note string) error {
	note = strings.TrimSpace(note)
	if note == "" {
		_, err := n.db.ExecContext(ctx, `DELETE FROM slot_notes WHERE slot = ?`, slot)
		return err
	}
	if len(note) > 4000 {
		return errors.New("the note is longer than 4000 characters; keep it to where things stand")
	}
	_, err := n.db.ExecContext(ctx, `INSERT INTO slot_notes (slot, note, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (slot) DO UPDATE SET note = excluded.note, updated_at = excluded.updated_at`,
		slot, note, time.Now().UTC().Format(time.RFC3339))
	return err
}

// Assembly is what the agent is told for one turn, and what the panel shows of it.
type Assembly struct {
	// Slot is the primary slot; Slots are all the active ones, the primary first.
	Slot  string     `json:"slot"`
	Slots []SlotView `json:"slots"`
	// Why is how the slots were chosen.
	Why string `json:"why"`
	// TopOfMind are the tools named by the active slots that the app has, in order.
	TopOfMind []string `json:"top_of_mind"`
	// Focused is the window in front, as its module describes it.
	Focused *module.WindowContext `json:"focused,omitempty"`
	Mode    string                `json:"mode,omitempty"`
	Layout  string                `json:"layout,omitempty"`
	// Reminders are what reflexes asked to tell the agent with this message.
	Reminders []string `json:"reminders,omitempty"`
	// Block is the <context> block that leads the owner's message.
	Block string `json:"block"`
}

// SlotView is one active slot as the panel shows it.
type SlotView struct {
	Name         string `json:"name"`
	Module       string `json:"module,omitempty"`
	About        string `json:"about"`
	Instructions string `json:"instructions,omitempty"`
	Where        string `json:"where,omitempty"`
	Note         string `json:"note,omitempty"`
	NoteUpdated  string `json:"note_updated,omitempty"`
	Knowledge    string `json:"knowledge,omitempty"`
}

// Extra is what goes into the block beside the slots: reflex reminders, a routed call, the
// layout's standing instructions.
type Extra struct {
	Reminders    []string
	Instructions string
	Routed       string
}

// knowledgeTimeout bounds one slot's Knowledge call: it runs on every message.
const knowledgeTimeout = 1500 * time.Millisecond

// Size budgets, in characters, for what the slots put in front of every message. The
// primary slot gets the most; a slot also in play (a mode) gets a glance; the whole block
// is capped so a module cannot crowd out the conversation.
const (
	budgetPrimary   = 6000
	budgetSecondary = 1500
	budgetBlock     = 12000
	budgetNote      = 2000
)

// Assemble builds the Assembly for a Selection.
func Assemble(ctx context.Context, sit module.Situation, sel Selection, tools []kernel.BoundTool, notes *Notes, extra Extra) Assembly {
	have := map[string]bool{}
	for _, t := range tools {
		have[t.Name] = true
	}
	a := Assembly{Slot: sel.Primary().Name, Why: sel.Why, Focused: sit.Focused, Mode: sit.Mode, Layout: sit.Layout, Reminders: extra.Reminders}
	slots := sel.Slots
	if len(slots) == 0 {
		slots = []kernel.BoundSlot{General}
	}
	for i, s := range slots {
		v := SlotView{Name: s.Name, Module: s.Module, About: s.About, Instructions: s.Instructions, Where: s.Where}
		v.Note, v.NoteUpdated = notes.Get(ctx, s.Name)
		v.Note = clip(v.Note, budgetNote)
		budget := budgetPrimary
		if i > 0 {
			budget = budgetSecondary
		}
		if s.Knowledge != nil {
			kctx, cancel := context.WithTimeout(ctx, knowledgeTimeout)
			k, err := s.Knowledge(kctx, sit)
			cancel()
			if err != nil {
				k = "(could not be read: " + err.Error() + ")"
			}
			v.Knowledge = clip(strings.TrimSpace(k), budget)
		}
		a.Slots = append(a.Slots, v)
		for _, t := range s.Tools {
			if have[t] && !slices.Contains(a.TopOfMind, t) {
				a.TopOfMind = append(a.TopOfMind, t)
			}
		}
	}
	a.Block = block(sit, a, extra)
	// Over the cap: drop what the modules know, the secondary slots' first, then trim the
	// primary's. What is in front, the slots, the note and the reminders always stay.
	for i := len(a.Slots) - 1; i >= 0 && len([]rune(a.Block)) > budgetBlock; i-- {
		over := len([]rune(a.Block)) - budgetBlock
		k := []rune(a.Slots[i].Knowledge)
		if i > 0 || over >= len(k) {
			a.Slots[i].Knowledge = ""
		} else {
			a.Slots[i].Knowledge = clip(string(k), len(k)-over-40)
		}
		a.Block = block(sit, a, extra)
	}
	return a
}

func block(sit module.Situation, a Assembly, extra Extra) string {
	var b strings.Builder
	b.WriteString("<context>\n")
	if f := sit.Focused; f != nil {
		fmt.Fprintf(&b, "In front: %q (view %s", f.Title, f.View)
		if f.Kind != "" {
			b.WriteString(", " + f.Kind)
		}
		if f.Ref != "" {
			b.WriteString(", ref " + f.Ref)
		}
		if f.URL != "" {
			b.WriteString(", " + f.URL)
		}
		fmt.Fprintf(&b, ", window %s)\n", f.Window)
		if s := strings.TrimSpace(f.Selection); s != "" {
			fmt.Fprintf(&b, "Selected: %q\n", clip(s, 1500))
		}
	} else {
		b.WriteString("In front: nothing (the desk is empty)\n")
	}
	if others := otherWindows(sit); others != "" {
		b.WriteString("Also open: " + others + "\n")
	}
	if sit.Layout != "" {
		b.WriteString("Layout: " + sit.Layout + "\n")
	}
	if sit.Mode != "" {
		b.WriteString("Mode: " + sit.Mode + "\n")
	}
	for i, s := range a.Slots {
		if i == 0 {
			fmt.Fprintf(&b, "\nSlot: %s (%s)\n", s.Name, s.About)
		} else {
			fmt.Fprintf(&b, "\nAlso in play: %s (%s)\n", s.Name, s.About)
		}
		if s.Instructions != "" {
			b.WriteString("How to work here: " + s.Instructions + "\n")
		}
		if s.Note != "" {
			fmt.Fprintf(&b, "Your note on this (%s): %s\n", s.NoteUpdated, s.Note)
		} else if i == 0 {
			b.WriteString("Your note on this: none yet (context_note keeps one)\n")
		}
		if s.Where != "" {
			b.WriteString("Where things are kept: " + s.Where + "\n")
		}
		if s.Knowledge != "" {
			b.WriteString("Now:\n" + s.Knowledge + "\n")
		}
	}
	if len(a.TopOfMind) > 0 {
		b.WriteString("\nTop of mind: " + strings.Join(a.TopOfMind, ", ") + "\n")
	}
	if extra.Instructions != "" {
		b.WriteString("\nFor this layout: " + extra.Instructions + "\n")
	}
	if extra.Routed != "" {
		b.WriteString("\n" + extra.Routed + "\n")
	}
	for _, r := range extra.Reminders {
		b.WriteString("\nReminder: " + r + "\n")
	}
	b.WriteString("</context>")
	return b.String()
}

func otherWindows(sit module.Situation) string {
	var parts []string
	for _, w := range sit.Windows {
		if sit.Focused != nil && w.ID == sit.Focused.Window {
			continue
		}
		parts = append(parts, fmt.Sprintf("%q (%s, %s)", w.Title, w.View, w.ID))
		if len(parts) == 12 {
			parts = append(parts, "…")
			break
		}
	}
	return strings.Join(parts, "; ")
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
