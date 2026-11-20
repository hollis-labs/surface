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
	"unicode/utf8"

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
	// Around is what the modules carry with every message (Manifest.Around).
	Around []AroundLine `json:"around,omitempty"`
	// Block is the <context> block that leads the owner's message.
	Block string `json:"block"`
	// Trimmed names the slots whose knowledge a budget cut: "<slot>:clipped" (over the
	// slot's own budget), then "<slot>:dropped" or "<slot>:trimmed" (to fit the block).
	Trimmed []string `json:"trimmed,omitempty"`
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
	// Around is what the modules' Around said, already read and capped.
	Around []AroundLine
	// Limit, when above zero, is a hard cap on the block in bytes: the runner the block goes
	// to has a fixed room for it and drops a block that does not fit, whole. Zero keeps the
	// soft cap (budgetBlock characters), which only trims what the modules know.
	Limit int
}

// AroundLine is one module's Around for this message.
type AroundLine struct {
	Module string `json:"module"`
	Text   string `json:"text"`
}

// BudgetAround caps one module's Around, in characters.
const BudgetAround = 1500

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
	a := Assembly{Slot: sel.Primary().Name, Why: sel.Why, Focused: sit.Focused, Mode: sit.Mode, Layout: sit.Layout, Reminders: extra.Reminders, Around: extra.Around}
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
			k = strings.TrimSpace(k)
			if len([]rune(k)) > budget {
				a.Trimmed = append(a.Trimmed, s.Name+":clipped")
			}
			v.Knowledge = clip(k, budget)
		}
		a.Slots = append(a.Slots, v)
		for _, t := range s.Tools {
			if have[t] && !slices.Contains(a.TopOfMind, t) {
				a.TopOfMind = append(a.TopOfMind, t)
			}
		}
	}
	a.Block = block(sit, a, extra)
	over := func() int {
		if extra.Limit > 0 {
			return len(a.Block) - extra.Limit
		}
		return len([]rune(a.Block)) - budgetBlock
	}
	// Over the cap: drop what the modules know, the secondary slots' first, then trim the
	// primary's. Under the soft cap, what is in front, the slots, the note and the reminders
	// always stay; under a hard Limit they give way too, in fitHard.
	for i := len(a.Slots) - 1; i >= 0 && over() > 0; i-- {
		over := over()
		k := []rune(a.Slots[i].Knowledge)
		if len(k) == 0 {
			continue
		}
		if i > 0 || over >= len(k) {
			a.Slots[i].Knowledge = ""
			a.Trimmed = append(a.Trimmed, a.Slots[i].Name+":dropped")
		} else {
			a.Slots[i].Knowledge = clip(string(k), len(k)-over-40)
			a.Trimmed = append(a.Trimmed, a.Slots[i].Name+":trimmed")
		}
		a.Block = block(sit, a, extra)
	}
	if extra.Limit > 0 && over() > 0 {
		fitHard(sit, &a, extra, over)
	}
	return a
}

// fitHard makes the block fit a hard Limit once the modules' knowledge is gone: the
// modules' Around lines go (the last first), then the secondary slots' notes, then the
// primary's note is clipped, and as a last resort the block is cut, with a marker, so it
// is never dropped whole. Each step is recorded in Trimmed.
func fitHard(sit module.Situation, a *Assembly, extra Extra, over func() int) {
	for len(extra.Around) > 0 && over() > 0 {
		last := extra.Around[len(extra.Around)-1]
		extra.Around = extra.Around[:len(extra.Around)-1]
		a.Around = extra.Around
		a.Trimmed = append(a.Trimmed, "around:"+last.Module+":dropped")
		a.Block = block(sit, *a, extra)
	}
	for i := len(a.Slots) - 1; i >= 0 && over() > 0; i-- {
		n := []rune(a.Slots[i].Note)
		if len(n) == 0 {
			continue
		}
		if i > 0 || over() >= len(string(n)) {
			a.Slots[i].Note = ""
			a.Trimmed = append(a.Trimmed, a.Slots[i].Name+":note:dropped")
		} else {
			a.Slots[i].Note = clipBytes(string(n), len(string(n))-over()-8)
			a.Trimmed = append(a.Trimmed, a.Slots[i].Name+":note:trimmed")
		}
		a.Block = block(sit, *a, extra)
	}
	if over() > 0 {
		const tail = "\n…(context cut to fit)\n</context>"
		a.Block = clipBytes(a.Block, extra.Limit-len(tail)) + tail
		a.Trimmed = append(a.Trimmed, "block:cut")
	}
}

// clipBytes cuts s to at most n bytes, on a character boundary.
func clipBytes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
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
	for _, x := range extra.Around {
		fmt.Fprintf(&b, "\nAround (%s):\n%s\n", x.Module, x.Text)
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
