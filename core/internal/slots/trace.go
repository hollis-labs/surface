package slots

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/hollis-labs/surface/core/internal/storage"
)

// Trace keeps a record of every turn: what was in front of the owner, which slots were
// chosen and why, what the router and the reflexes did, and what the budget cut. Beside it
// are signals: what the owner (or the agent) did after a turn that says whether the choice
// was right. A pin to another slot is a correction; moving to another window is a hint.
//
// It is the training set for a classifier-driven Selector, and it only grows from the day
// it is switched on, so it is kept from the start. It lives in the app's own database, on
// the owner's machine, and the setting surface.agent_trace turns it off.
type Trace struct {
	db *sql.DB

	mu   sync.Mutex
	last int64     // the newest turn's id
	at   time.Time // when it was recorded
	n    int       // turns recorded since the last prune
}

// TraceTurn is one turn as the trace keeps it.
type TraceTurn struct {
	ID         int64    `json:"id"`
	At         string   `json:"at"`
	RunID      string   `json:"run_id,omitempty"`
	Text       string   `json:"text"`
	View       string   `json:"view,omitempty"`
	Module     string   `json:"module,omitempty"`
	Mode       string   `json:"mode,omitempty"`
	Layout     string   `json:"layout,omitempty"`
	OpenViews  []string `json:"open_views"`
	Slot       string   `json:"slot"`
	Slots      []string `json:"slots"`
	Why        string   `json:"why,omitempty"`
	TopOfMind  []string `json:"top_of_mind"`
	Routed     string   `json:"routed,omitempty"`
	Fired      []string `json:"fired"`
	Halted     bool     `json:"halted,omitempty"`
	BlockChars int      `json:"block_chars"`
	Trimmed    []string `json:"trimmed"`
	// Classified is what the classifier made of the turn (a classify.Result), when it ran.
	Classified json.RawMessage `json:"classified,omitempty"`
	// ClassifiedAsync is the async tier's result, when it ran.
	ClassifiedAsync json.RawMessage `json:"classified_async,omitempty"`
	// Signals are what followed the turn, oldest first.
	Signals []TraceSignal `json:"signals,omitempty"`
}

// TraceSignal is something that happened after a turn that says how the turn's context fared.
type TraceSignal struct {
	At     string          `json:"at"`
	Kind   string          `json:"kind"`
	Actor  string          `json:"actor,omitempty"`
	Detail json.RawMessage `json:"detail,omitempty"`
}

const (
	// signalWindow is how long after a turn a signal still counts as following it.
	signalWindow = 15 * time.Minute
	// traceKeep is how many turns are kept; older ones go, with their signals.
	traceKeep = 50000
)

// OpenTrace migrates the trace's tables into db (the slots' migration set).
func OpenTrace(ctx context.Context, db *sql.DB) (*Trace, error) {
	if err := storage.Migrate(ctx, db, "slots", migrations, "migrations"); err != nil {
		return nil, err
	}
	t := &Trace{db: db}
	var at sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT id, at FROM context_turns ORDER BY id DESC LIMIT 1`).Scan(&t.last, &at); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if at.Valid {
		t.at, _ = time.Parse(time.RFC3339Nano, at.String)
	}
	return t, nil
}

// TurnFrom is what a turn's record is made of: the situation's parts and the assembly.
func TurnFrom(text, view, module, mode, layout string, openViews []string, a Assembly, routed string, fired []string, halted bool) TraceTurn {
	names := make([]string, 0, len(a.Slots))
	for _, s := range a.Slots {
		names = append(names, s.Name)
	}
	return TraceTurn{Text: text, View: view, Module: module, Mode: mode, Layout: layout, OpenViews: openViews,
		Slot: a.Slot, Slots: names, Why: a.Why, TopOfMind: a.TopOfMind, Routed: routed, Fired: fired,
		Halted: halted, BlockChars: len([]rune(a.Block)), Trimmed: a.Trimmed}
}

// Record keeps a turn and returns its id.
func (t *Trace) Record(ctx context.Context, turn TraceTurn) (int64, error) {
	now := time.Now().UTC()
	j := func(v []string) string {
		if v == nil {
			v = []string{}
		}
		b, _ := json.Marshal(v)
		return string(b)
	}
	res, err := t.db.ExecContext(context.WithoutCancel(ctx), `INSERT INTO context_turns
		(at, run_id, text, view, module, mode, layout, open_views, slot, slots, why, top_of_mind, routed, fired, halted, block_chars, trimmed)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		now.Format(time.RFC3339Nano), turn.RunID, turn.Text, turn.View, turn.Module, turn.Mode, turn.Layout, j(turn.OpenViews),
		turn.Slot, j(turn.Slots), turn.Why, j(turn.TopOfMind), turn.Routed, j(turn.Fired), turn.Halted, turn.BlockChars, j(turn.Trimmed))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	t.mu.Lock()
	t.last, t.at = id, now
	t.n++
	prune := t.n >= 500
	if prune {
		t.n = 0
	}
	t.mu.Unlock()
	if prune {
		t.prune(ctx)
	}
	return id, nil
}

// SetRun names the run a recorded turn started, once the engine has said.
func (t *Trace) SetRun(ctx context.Context, id int64, runID string) {
	_, _ = t.db.ExecContext(context.WithoutCancel(ctx), `UPDATE context_turns SET run_id=? WHERE id=?`, runID, id)
}

// SetClassified keeps what the classifier made of a recorded turn.
func (t *Trace) SetClassified(ctx context.Context, id int64, result json.RawMessage) {
	_, _ = t.db.ExecContext(context.WithoutCancel(ctx), `UPDATE context_turns SET classified=? WHERE id=?`, string(result), id)
}

// SetClassifiedAsync keeps the async tier's result for a recorded turn.
func (t *Trace) SetClassifiedAsync(ctx context.Context, id int64, result json.RawMessage) {
	_, _ = t.db.ExecContext(context.WithoutCancel(ctx), `UPDATE context_turns SET classified_async=? WHERE id=?`, string(result), id)
}

// Feedback is the owner's answer to a classifier hint on a draft.
type Feedback struct {
	At       string `json:"at"`
	Text     string `json:"text"`
	Facet    string `json:"facet"`
	Label    string `json:"label"`
	Accepted bool   `json:"accepted"`
	Actor    string `json:"actor,omitempty"`
}

// AddFeedback keeps one answer to a hint.
func (t *Trace) AddFeedback(ctx context.Context, f Feedback) error {
	_, err := t.db.ExecContext(context.WithoutCancel(ctx), `INSERT INTO classify_feedback (at, text, facet, label, accepted, actor) VALUES (?, ?, ?, ?, ?, ?)`,
		time.Now().UTC().Format(time.RFC3339Nano), f.Text, f.Facet, f.Label, f.Accepted, f.Actor)
	return err
}

// EachFeedback calls fn for every answer to a hint, oldest first.
func (t *Trace) EachFeedback(ctx context.Context, fn func(Feedback) error) error {
	rows, err := t.db.QueryContext(ctx, `SELECT at, text, facet, label, accepted, actor FROM classify_feedback ORDER BY id`)
	if err != nil {
		return err
	}
	var all []Feedback
	for rows.Next() {
		var f Feedback
		if err := rows.Scan(&f.At, &f.Text, &f.Facet, &f.Label, &f.Accepted, &f.Actor); err != nil {
			_ = rows.Close()
			return err
		}
		all = append(all, f)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, f := range all {
		if err := fn(f); err != nil {
			return err
		}
	}
	return nil
}

// Each calls fn for every turn, oldest first, each with its signals.
func (t *Trace) Each(ctx context.Context, fn func(TraceTurn) error) error { return t.each(ctx, 0, fn) }

// Signal keeps something that followed the newest turn. It is dropped when no turn was
// recorded in the last signalWindow: with nothing to follow, it says nothing about a choice.
func (t *Trace) Signal(ctx context.Context, kind, actor string, detail any) {
	t.mu.Lock()
	turn, at := t.last, t.at
	t.mu.Unlock()
	if turn == 0 || time.Since(at) > signalWindow {
		return
	}
	raw, err := json.Marshal(detail)
	if err != nil || detail == nil {
		raw = []byte("{}")
	}
	_, _ = t.db.ExecContext(context.WithoutCancel(ctx), `INSERT INTO context_signals (at, turn_id, kind, actor, detail) VALUES (?, ?, ?, ?, ?)`,
		time.Now().UTC().Format(time.RFC3339Nano), turn, kind, actor, string(raw))
}

// Recent lists the newest turns, newest first, each with its signals.
func (t *Trace) Recent(ctx context.Context, limit int) ([]TraceTurn, error) {
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	var out []TraceTurn
	err := t.each(ctx, limit, func(tt TraceTurn) error {
		out = append(out, tt)
		return nil
	})
	return out, err
}

// Export writes every turn, oldest first, one JSON object a line, each with its signals.
func (t *Trace) Export(ctx context.Context, w io.Writer) error {
	enc := json.NewEncoder(w)
	return t.each(ctx, 0, func(tt TraceTurn) error { return enc.Encode(tt) })
}

const traceColumns = `SELECT id, at, run_id, text, view, module, mode, layout, open_views, slot, slots, why,
	top_of_mind, routed, fired, halted, block_chars, trimmed, classified, classified_async FROM context_turns`

// each calls fn for turns with their signals: the newest limit of them, newest first, or
// with limit 0 every turn, oldest first.
func (t *Trace) each(ctx context.Context, limit int, fn func(TraceTurn) error) error {
	var rows *sql.Rows
	var err error
	if limit > 0 {
		rows, err = t.db.QueryContext(ctx, traceColumns+` ORDER BY id DESC LIMIT ?`, limit)
	} else {
		rows, err = t.db.QueryContext(ctx, traceColumns+` ORDER BY id ASC`)
	}
	if err != nil {
		return err
	}
	var turns []TraceTurn
	for rows.Next() {
		var tt TraceTurn
		var open, sl, top, fired, trimmed, classified, classifiedAsync string
		if err := rows.Scan(&tt.ID, &tt.At, &tt.RunID, &tt.Text, &tt.View, &tt.Module, &tt.Mode, &tt.Layout, &open, &tt.Slot, &sl,
			&tt.Why, &top, &tt.Routed, &fired, &tt.Halted, &tt.BlockChars, &trimmed, &classified, &classifiedAsync); err != nil {
			_ = rows.Close()
			return err
		}
		for _, p := range []struct {
			raw string
			to  *[]string
		}{{open, &tt.OpenViews}, {sl, &tt.Slots}, {top, &tt.TopOfMind}, {fired, &tt.Fired}, {trimmed, &tt.Trimmed}} {
			_ = json.Unmarshal([]byte(p.raw), p.to)
		}
		if classified != "" {
			tt.Classified = json.RawMessage(classified)
		}
		if classifiedAsync != "" {
			tt.ClassifiedAsync = json.RawMessage(classifiedAsync)
		}
		turns = append(turns, tt)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range turns {
		sigs, err := t.signals(ctx, turns[i].ID)
		if err != nil {
			return err
		}
		turns[i].Signals = sigs
		if err := fn(turns[i]); err != nil {
			return err
		}
	}
	return nil
}

func (t *Trace) signals(ctx context.Context, turn int64) ([]TraceSignal, error) {
	rows, err := t.db.QueryContext(ctx, `SELECT at, kind, actor, detail FROM context_signals WHERE turn_id=? ORDER BY id`, turn)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []TraceSignal
	for rows.Next() {
		var s TraceSignal
		var detail string
		if err := rows.Scan(&s.At, &s.Kind, &s.Actor, &detail); err != nil {
			return nil, err
		}
		if strings.TrimSpace(detail) != "" && detail != "{}" {
			s.Detail = json.RawMessage(detail)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// prune drops all but the newest traceKeep turns, with their signals.
func (t *Trace) prune(ctx context.Context) {
	ctx = context.WithoutCancel(ctx)
	var cut int64
	if err := t.db.QueryRowContext(ctx, `SELECT id FROM context_turns ORDER BY id DESC LIMIT 1 OFFSET ?`, traceKeep).Scan(&cut); err != nil {
		return
	}
	_, _ = t.db.ExecContext(ctx, `DELETE FROM context_signals WHERE turn_id <= ?`, cut)
	_, _ = t.db.ExecContext(ctx, `DELETE FROM context_turns WHERE id <= ?`, cut)
}
