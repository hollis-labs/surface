package files

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/hollis-labs/surface/module"
)

// Cite: a path in some text ("~/dev/app/main.go", "/srv/notes/plan.md", "remote:box/src/a.go")
// as a reference (kind "path"). It is looked up through the same checks as every other access:
// a path outside every place is not read (the card says so), a file that looks like a
// credential is refused, and nothing is written. Opening one shows it in Files.

const pathKind = "path"

// citePath is what a path reference may look like: absolute, from home, or on a remote place.
var citePath = regexp.MustCompile(`^(/|~/|remote:[a-z0-9][a-z0-9-]{0,39}/)[^\x00\n]*$`)

// citeText bounds the text a path's record shows.
const citeText = 64 << 10

func (m *mod) refs() []module.RefKind {
	return []module.RefKind{{
		Kind: pathKind, Label: "File path", ID: citePath,
		Resolve: func(ctx context.Context, _ module.Host, ids []string) (map[string]module.RefEntity, error) {
			out := map[string]module.RefEntity{}
			for _, id := range ids {
				e, err := m.citePath(ctx, id, false)
				if err == nil {
					out[id] = e
				}
			}
			return out, nil
		},
		Record: func(ctx context.Context, _ module.Host, id string) (module.RefEntity, error) {
			return m.citePath(ctx, id, true)
		},
	}}
}

// placeOf finds the place a path on this machine lies in (the deepest one) and the path
// within it.
func (m *mod) placeOf(ctx context.Context, abs string) (Place, string, bool) {
	ps, err := m.st.list(ctx)
	if err != nil {
		return Place{}, "", false
	}
	var best Place
	var rel string
	found := false
	for _, p := range ps {
		root := filepath.Clean(p.Path)
		r, err := filepath.Rel(root, abs)
		if err != nil || !filepath.IsLocal(r) && r != "." {
			continue
		}
		if !found || len(root) > len(filepath.Clean(best.Path)) {
			best, rel, found = p, filepath.ToSlash(r), true
		}
	}
	if rel == "." {
		rel = ""
	}
	return best, rel, found
}

func (m *mod) citePath(ctx context.Context, id string, full bool) (module.RefEntity, error) {
	if strings.HasPrefix(id, remotePrefix) {
		return m.citeRemote(ctx, id, full)
	}
	abs := id
	if strings.HasPrefix(abs, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return module.RefEntity{}, module.ErrRefNotFound
		}
		abs = filepath.Join(home, abs[2:])
	}
	abs = filepath.Clean(abs)
	name := filepath.Base(abs)
	p, rel, ok := m.placeOf(ctx, abs)
	if !ok {
		e := module.RefEntity{Title: name, Subtitle: "Not in a Files place", ChipLabel: name,
			Description: "Files reaches only its places. Add a place that holds this path to preview and open it.",
			Fields:      []module.RefField{{Label: "Path", Value: id, Mono: true}}}
		if full {
			e.Body = e.Description
		}
		return e, nil
	}
	l, err := m.at(ctx, p.ID, rel, true)
	if err != nil {
		return module.RefEntity{}, module.ErrRefNotFound // outside, or refused as a credential
	}
	fi, err := os.Stat(l.Abs)
	if err != nil {
		return module.RefEntity{}, module.ErrRefNotFound
	}
	e := fileEntity(name, p, l.Rel, fi.IsDir(), fi.Size(), fi.ModTime())
	if full && !fi.IsDir() {
		if t, err := readText(l, 0, 0, min(previewLimit(m.h), citeText)); err == nil {
			e.Body = fence(l.Rel, t.Text, t.Truncated)
		} else {
			e.Body = "This file cannot be shown as text here. Open it in Files."
		}
	}
	return e, nil
}

func (m *mod) citeRemote(ctx context.Context, id string, full bool) (module.RefEntity, error) {
	place, rel, _ := strings.Cut(id, "/")
	r, err := m.remote(place)
	if err != nil {
		return module.RefEntity{}, module.ErrRefNotFound
	}
	st, err := r.stat(ctx, rel, true)
	if errors.Is(err, errNotFound) || err == nil && st.Kind == 'm' {
		return module.RefEntity{}, module.ErrRefNotFound
	}
	if err != nil {
		if errors.Is(err, errOutside) || errors.Is(err, errSensitive) {
			return module.RefEntity{}, module.ErrRefNotFound
		}
		return module.RefEntity{}, err // the host is not reachable: not the same as missing
	}
	e := fileEntity(path.Base(st.Rel), r.cfg.place(), st.Rel, st.Kind == 'd', st.Size, time.Time{})
	if full && st.Kind == 'f' {
		if t, err := r.readText(ctx, st.Rel, 0, 0, min(previewLimit(m.h), citeText)); err == nil {
			e.Body = fence(st.Rel, t.Text, t.Truncated)
		} else {
			e.Body = "This file cannot be shown as text here. Open it in Files."
		}
	}
	return e, nil
}

func fileEntity(name string, p Place, rel string, dir bool, size int64, mod time.Time) module.RefEntity {
	what := "File"
	if dir {
		what = "Folder"
	}
	e := module.RefEntity{
		Title: name, Subtitle: what + " in " + p.Name, ChipLabel: name,
		Open: &module.RefOpen{Command: "files.open", Args: map[string]any{"place": p.ID, "path": rel}},
	}
	e.Fields = append(e.Fields, module.RefField{Label: "Place", Value: p.Name}, module.RefField{Label: "Path", Value: rel, Mono: true})
	if !dir {
		e.Fields = append(e.Fields, module.RefField{Label: "Size", Value: humanSize(size)})
	}
	if !mod.IsZero() {
		e.UpdatedAt = mod.UTC().Format(time.RFC3339)
	}
	return e
}

// fence shows a file's text: Markdown as it is, anything else in a code block.
func fence(rel, text string, cut bool) string {
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(rel)), ".")
	out := text
	if ext != "md" && ext != "markdown" {
		ticks := "```"
		for strings.Contains(text, ticks) {
			ticks += "`"
		}
		out = ticks + ext + "\n" + strings.TrimRight(text, "\n") + "\n" + ticks
	}
	if cut {
		out += "\n\n…(the rest is in Files)"
	}
	return out
}

func humanSize(n int64) string {
	switch {
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	case n < 1<<30:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
}
