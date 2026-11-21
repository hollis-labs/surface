package files

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/hollis-labs/surface/module"
)

// Search local place names/paths without reading contents or following links. Remote
// folders keep their explicit Files search: typing in the palette never starts SSH.
func (m *mod) palette(ctx context.Context, h module.Host, q module.PaletteQuery) ([]module.PaletteResult, error) {
	ps, err := m.st.list(ctx)
	if err != nil {
		return nil, err
	}
	out := []module.PaletteResult{}
	visited := 0
	seen := map[string]bool{}
	for _, p := range ps {
		l, locErr := locate(p, "", true)
		if locErr != nil {
			continue
		}
		err = filepath.WalkDir(l.Abs, func(full string, d fs.DirEntry, walkErr error) error {
			if e := ctx.Err(); e != nil {
				return e
			}
			if walkErr != nil {
				return nil //nolint:nilerr // inaccessible entries do not hide matches from other folders
			}
			visited++
			if visited > grepMaxFiles || len(out) >= q.Limit {
				return filepath.SkipAll
			}
			rel, _ := filepath.Rel(l.Abs, full)
			rel = filepath.ToSlash(rel)
			if d.IsDir() {
				if full != l.Abs && (grepSkip[d.Name()] || sensitive(full) || strings.Count(rel, "/") >= grepMaxDepth || !h.Config().Bool("show_hidden") && strings.HasPrefix(d.Name(), ".")) {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() || sensitive(full) || !h.Config().Bool("show_hidden") && strings.HasPrefix(d.Name(), ".") {
				return nil
			}
			if !strings.Contains(strings.ToLower(rel), strings.ToLower(q.Text)) || seen[full] {
				return nil
			}
			seen[full] = true
			out = append(out, module.PaletteResult{ID: full, Title: d.Name(), Description: p.Name + " / " + rel, Icon: "file", Ref: &module.PaletteRef{Kind: "path", Canonical: full}})
			return nil
		})
		if err != nil {
			return nil, err
		}
		if visited >= grepMaxFiles || len(out) >= q.Limit {
			break
		}
	}
	return out, nil
}
