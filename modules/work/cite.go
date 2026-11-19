package work

import (
	"context"
	"regexp"
	"strings"

	"github.com/hollis-labs/surface/module"
)

// Cite: a Planner, Content or Writer item named in text as work:<workspace>/<id> (the ref a Work
// window reports) is a reference of kind "work.item", with the id "<workspace>/<id>". It is read
// from the module's working copy only: resolving never reads Caster again, since that refreshes
// the copy, and never writes. An item of a workspace no window has opened since the app started
// may not be in the copy yet; it is then not found until one does.

const itemKind = "work.item"

var itemRef = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}/[a-z0-9]{8}$`)

// appCommands open each app's window on a workspace and an item.
var appCommands = map[string]string{"planner": "work.open", "content": "work.content", "writer": "work.writer"}

func (m *mod) refs() []module.RefKind {
	return []module.RefKind{{
		Kind: itemKind, Label: "Work item", ID: itemRef,
		Resolve: func(ctx context.Context, _ module.Host, ids []string) (map[string]module.RefEntity, error) {
			out := map[string]module.RefEntity{}
			for _, id := range ids {
				if e, ok := m.citeItem(ctx, id, false); ok {
					out[id] = e
				}
			}
			return out, nil
		},
		Record: func(ctx context.Context, _ module.Host, id string) (module.RefEntity, error) {
			if e, ok := m.citeItem(ctx, id, true); ok {
				return e, nil
			}
			return module.RefEntity{}, module.ErrRefNotFound
		},
	}}
}

func (m *mod) citeItem(ctx context.Context, ref string, full bool) (module.RefEntity, bool) {
	ws, id, _ := strings.Cut(ref, "/")
	d, ok, err := m.st.Detail(ctx, ws, id)
	if err != nil || !ok {
		return module.RefEntity{}, false
	}
	w, _ := m.st.workspace(ctx, ws)
	app := w.App
	if appCommands[app] == "" {
		app = "planner"
	}
	kind := d.Kind
	if kind == "" {
		kind = "item"
	}
	e := module.RefEntity{
		Title: d.Title, Subtitle: strings.ToUpper(app[:1]) + app[1:] + " · " + w.Title,
		Description: d.Excerpt, UpdatedAt: d.Updated, ChipLabel: strings.ToUpper(kind[:1]) + kind[1:] + ": " + d.Title,
		Open: &module.RefOpen{Command: appCommands[app], Args: map[string]any{"workspace": ws, "item": id}},
	}
	if d.Stage != "" {
		e.Status = &module.RefStatus{Label: d.Stage, Tone: module.StatusTone(d.Stage)}
	}
	e.Fields = append(e.Fields, module.RefField{Label: "Kind", Value: kind}, module.RefField{Label: "Workspace", Value: ws, Mono: true})
	if d.Task != "" {
		e.Fields = append(e.Fields, module.RefField{Label: "Task", Value: d.Task, Mono: true})
	}
	if e.Description == "" {
		e.Description = excerptOf(d.Lead, 320)
	}
	if full {
		var b strings.Builder
		if d.Lead != "" {
			b.WriteString(d.Lead + "\n")
		}
		for _, sec := range d.Order {
			if t := strings.TrimSpace(d.Sections[sec]); t != "" {
				b.WriteString("\n## " + sec + "\n\n" + t + "\n")
			}
		}
		e.Body = strings.TrimSpace(b.String())
	}
	return e, true
}

func excerptOf(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
