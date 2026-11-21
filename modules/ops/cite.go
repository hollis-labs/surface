package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/hollis-labs/surface/module"
)

// Cite: a workstream named in any text as ops:<id> is a reference of kind "ops.workstream".
// It is read from the working copy only: resolving never reaches Caster and never writes.
// Opening it runs ops.open, which opens the workstream's drill-in.

const citeKind = "ops.workstream"

func (m *mod) refs() []module.RefKind {
	return []module.RefKind{{
		Kind: citeKind, Label: "Workstream", ID: idRe,
		Resolve: func(ctx context.Context, _ module.Host, ids []string) (map[string]module.RefEntity, error) {
			out := map[string]module.RefEntity{}
			b, err := m.st.All(ctx)
			if err != nil {
				return nil, err
			}
			for _, id := range ids {
				if w, ok := b.Find(id); ok {
					out[id] = m.entity(ctx, w, false)
				}
			}
			return out, nil
		},
		Record: func(ctx context.Context, _ module.Host, id string) (module.RefEntity, error) {
			b, err := m.st.All(ctx)
			if err != nil {
				return module.RefEntity{}, err
			}
			w, ok := b.Find(id)
			if !ok {
				return module.RefEntity{}, module.ErrRefNotFound
			}
			return m.entity(ctx, w, true), nil
		},
	}}
}

func (m *mod) entity(ctx context.Context, w Workstream, full bool) module.RefEntity {
	e := module.RefEntity{
		Title: w.Title, Subtitle: "Operations · " + label(w.Stage), ChipLabel: "Workstream: " + w.Title,
		Description: oneLine(firstNonEmpty(w.Purpose, w.Notes), 320), UpdatedAt: w.Updated,
		Status: &module.RefStatus{Label: label(w.Stage), Tone: stageTone(w.Stage)},
		Open:   &module.RefOpen{Command: "ops.open", Args: map[string]any{"id": w.ID}},
	}
	e.Fields = append(e.Fields, module.RefField{Label: "Id", Value: Ref(w.ID), Mono: true})
	if w.Owner != "" {
		e.Fields = append(e.Fields, module.RefField{Label: "Owner", Value: w.Owner})
	}
	if w.Parent != "" {
		e.Fields = append(e.Fields, module.RefField{Label: "Inside", Value: Ref(w.Parent), Mono: true})
	}
	if len(w.Children) > 0 {
		e.Fields = append(e.Fields, module.RefField{Label: "Workstreams inside", Value: fmt.Sprint(len(w.Children))})
	}
	if w.Held != nil {
		e.Fields = append(e.Fields, module.RefField{Label: "Held", Value: w.Held.Reason})
	}
	if r, _, ok := m.st.cachedRollup(ctx, w.ID); ok {
		if t := CountsText(r.Counts); t != "" {
			e.Fields = append(e.Fields, module.RefField{Label: "Tasks", Value: t})
		}
	}
	if full {
		var sb strings.Builder
		for _, s := range []struct{ h, t string }{{"Purpose", w.Purpose}, {"Complete means", w.ActiveMeans}, {"Notes", w.Notes}} {
			if strings.TrimSpace(s.t) != "" {
				sb.WriteString("## " + s.h + "\n\n" + strings.TrimSpace(s.t) + "\n\n")
			}
		}
		if len(w.ActiveWhen) > 0 {
			sb.WriteString("## Complete when\n\n")
			for _, c := range w.ActiveWhen {
				x := " "
				if c.Done {
					x = "x"
				}
				fmt.Fprintf(&sb, "- [%s] %s\n", x, c.Text)
			}
		}
		e.Body = strings.TrimSpace(sb.String())
	}
	return e
}

func stageTone(s string) string {
	switch s {
	case StageInFlight:
		return "info"
	case StageQueued, StagePlanned:
		return "neutral"
	case StageLanding:
		return "warning"
	case StageComplete:
		return "success"
	case StageDropped:
		return "danger"
	}
	return "neutral"
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
