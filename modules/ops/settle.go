package ops

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var errAlreadySettled = errors.New("already settled")

// Settle preserves the final tracking in the revisioned document. It is not a stage:
// the workstream remains complete and addressable by its original Ops/Caster references.
func (s *Store) Settle(ctx context.Context, id string, baseRev int, tracking Rollup, by string) (Workstream, error) {
	if baseRev <= 0 {
		return Workstream{}, bad("give base_rev, the workstream revision you reviewed")
	}
	got, err := s.change(ctx, id, baseRev, func(d *Doc, w Workstream) error {
		if w.SettledAt != "" {
			return errAlreadySettled
		}
		if w.Rev != baseRev {
			return &Conflict{Rev: w.Rev}
		}
		if w.Stage != "complete" {
			return bad("only a complete workstream can be settled")
		}
		at := now()
		counts := CountsText(tracking.Counts)
		if counts == "" {
			counts = "no linked tasks observed"
		}
		checked := 0
		for _, c := range w.ActiveWhen {
			if c.Done {
				checked++
			}
		}
		summary := fmt.Sprintf("%s; stage complete; revision %d; %d/%d completion checks; %s. Tracking read at %s.", strings.Join(strings.Fields(w.Title), " "), w.Rev, checked, len(w.ActiveWhen), counts, tracking.FetchedAt)
		if len(tracking.Errors) > 0 {
			summary += " Source warnings: " + strings.Join(strings.Fields(strings.Join(tracking.Errors, "; ")), " ") + "."
		}
		if err := d.Set("settled_summary", summary); err != nil {
			return err
		}

		if err := d.Set("settled_at", at); err != nil {
			return err
		}
		if err := d.Set("settled_by", by); err != nil {
			return err
		}
		return addTransition(d, Transition{From: w.Stage, To: w.Stage, At: at, By: by, Why: "settled: final tracking recorded"})
	})
	if errors.Is(err, errAlreadySettled) {
		got, _, err = s.Get(ctx, id)
	}
	return got, err
}

func (m *mod) settle(ctx context.Context, id string, baseRev int) (Workstream, error) {
	if err := m.sync(ctx, true); err != nil {
		return Workstream{}, err
	}
	w, ok, err := m.st.Get(ctx, id)
	if err != nil || !ok {
		return Workstream{}, errOr(err, ErrNotFound)
	}
	if w.SettledAt != "" {
		return w, nil
	}
	if w.Rev != baseRev {
		return Workstream{}, &Conflict{Rev: w.Rev}
	}
	tracking, err := m.rollup(ctx, id, true)
	if err != nil {
		return Workstream{}, err
	}
	got, err := m.st.Settle(ctx, id, baseRev, tracking, actor(ctx))
	if err == nil {
		m.actionChanged(ctx, id, "settled")
	}
	return got, err
}

func (m *mod) apiSettle(w http.ResponseWriter, r *http.Request) {
	var in struct {
		BaseRev int `json:"base_rev"`
	}
	if !decode(w, r, &in, 4096) {
		return
	}
	got, err := m.settle(r.Context(), r.PathValue("id"), in.BaseRev)
	if err != nil {
		failErr(w, err)
		return
	}
	reply(w, http.StatusOK, got)
}

func (s *Store) Unsettle(ctx context.Context, id string, baseRev int, by string) (Workstream, error) {
	if baseRev <= 0 {
		return Workstream{}, bad("give base_rev, the workstream revision you reviewed")
	}
	return s.change(ctx, id, baseRev, func(d *Doc, w Workstream) error {
		if w.Rev != baseRev {
			return &Conflict{Rev: w.Rev}
		}
		d.Del("settled_at")
		d.Del("settled_by")
		d.Del("settled_summary")
		return addTransition(d, Transition{From: w.Stage, To: w.Stage, At: now(), By: by, Why: "unsettled"})
	})
}
func (m *mod) apiUnsettle(w http.ResponseWriter, r *http.Request) {
	var in struct {
		BaseRev int `json:"base_rev"`
	}
	if !decode(w, r, &in, 4096) {
		return
	}
	got, err := m.st.Unsettle(r.Context(), r.PathValue("id"), in.BaseRev, actor(r.Context()))
	if err != nil {
		failErr(w, err)
		return
	}
	m.actionChanged(r.Context(), got.ID, "unsettled")
	reply(w, http.StatusOK, got)
}

// unsettled is a board projection only. Children of a hidden settled concept are
// promoted to its nearest visible ancestor so unfinished work does not disappear.
func (b Board) unsettled() Board {
	byID := map[string]Workstream{}
	for _, w := range b.Workstreams {
		byID[w.ID] = w
	}
	out := Board{Workstreams: []Workstream{}}
	for _, w := range b.Workstreams {
		if w.SettledAt != "" {
			continue
		}
		for n := 0; n < maxDepth; n++ {
			p, ok := byID[w.Parent]
			if !ok || p.SettledAt == "" {
				break
			}
			w.Parent = p.Parent
		}
		w.Children = []string{}
		out.Workstreams = append(out.Workstreams, w)
	}
	for i := range out.Workstreams {
		for _, w := range out.Workstreams {
			if w.Parent == out.Workstreams[i].ID {
				out.Workstreams[i].Children = append(out.Workstreams[i].Children, w.ID)
			}
		}
	}
	out.Queue = effectiveQueue(b.Queue, out.Workstreams)
	return out
}

// Refresh this workstream and ancestor roll-ups after an action adds activity.
func (m *mod) actionChanged(ctx context.Context, id, what string) {
	b, err := m.st.All(ctx)
	if err == nil {
		for next, n := id, 0; next != "" && n <= maxDepth; n++ {
			_, _ = m.st.db.ExecContext(ctx, "DELETE FROM activity WHERE ws = ?", next)
			w, ok := b.Find(next)
			if !ok {
				break
			}
			next = w.Parent
		}
	}
	m.changed(id, what)
	m.kickRoll()
}
