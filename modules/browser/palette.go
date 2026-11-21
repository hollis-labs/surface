package browser

import (
	"context"
	"strings"

	"github.com/hollis-labs/surface/module"
)

func (m *mod) palette(ctx context.Context, h module.Host, q module.PaletteQuery) ([]module.PaletteResult, error) {
	out := []module.PaletteResult{}
	for _, w := range h.Desk().Windows(ctx) {
		if w.View != "browser" || q.Scope == "focused" && w.ID != q.FocusedWindow {
			continue
		}
		u, _ := w.Args["url"].(string)
		if !strings.Contains(strings.ToLower(w.Title+" "+u), strings.ToLower(q.Text)) {
			continue
		}
		out = append(out, module.PaletteResult{ID: w.ID, Title: w.Title, Description: u, Icon: "globe", Window: w.ID, Close: true})
		if len(out) >= q.Limit {
			break
		}
	}
	return out, nil
}
