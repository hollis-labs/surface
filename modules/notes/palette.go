package notes

import (
	"context"
	"strings"

	"github.com/hollis-labs/surface/module"
)

func (m *mod) palette(ctx context.Context, _ module.Host, q module.PaletteQuery) ([]module.PaletteResult, error) {
	notes, err := m.st.listContext(ctx, 2000)
	if err != nil {
		return nil, err
	}
	out := []module.PaletteResult{}
	for _, n := range notes {
		if !strings.Contains(strings.ToLower(n.Title+" "+n.Path), strings.ToLower(q.Text)) {
			continue
		}
		out = append(out, module.PaletteResult{ID: n.Path, Title: n.Title, Description: n.Path, Icon: "notebook-pen", Command: &module.RefOpen{Command: "notes.open", Args: map[string]any{"path": n.Path}}})
		if len(out) >= q.Limit {
			break
		}
	}
	return out, nil
}
