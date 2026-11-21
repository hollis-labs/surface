package kernel

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hollis-labs/surface/module"
)

var ErrPaletteProvider = errors.New("unknown palette provider")

type PaletteInfo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Mode  string `json:"mode"`
}
type boundPalette struct {
	info   PaletteInfo
	search func(context.Context, module.Host, module.PaletteQuery) ([]module.PaletteResult, error)
	host   *host
}

func (a *App) addPalette(h *host) error {
	for _, p := range h.man.Palette {
		if !module.ValidID(p.ID) || p.Search == nil || p.Title == "" {
			return fmt.Errorf("surface: invalid palette provider %q in %s", p.ID, h.id)
		}
		mode := p.Mode
		if mode == "" {
			mode = "commands"
		}
		if mode != "commands" && mode != "tabs" {
			return fmt.Errorf("surface: invalid palette mode %q", mode)
		}
		id := h.id + "/" + p.ID
		if _, ok := a.palette[id]; ok {
			return fmt.Errorf("surface: repeated palette provider %q", id)
		}
		a.palette[id] = boundPalette{info: PaletteInfo{ID: id, Title: p.Title, Mode: mode}, search: p.Search, host: h}
	}
	return nil
}
func (a *App) PaletteProviders() []PaletteInfo {
	out := []PaletteInfo{{ID: "surface/cites", Title: "Cited records", Mode: "commands"}}
	for _, p := range a.palette {
		out = append(out, p.info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// SearchPalette isolates each provider: the page queries them independently, so a slow
// source never holds up commands or other providers. Six searches may execute at once.
func (a *App) SearchPalette(ctx context.Context, id string, q module.PaletteQuery) ([]module.PaletteResult, error) {
	q.Text = strings.TrimSpace(q.Text)
	if len(q.Text) > 512 {
		return nil, errors.New("palette query exceeds 512 bytes")
	}
	if q.Scope != "" && q.Scope != "all" && q.Scope != "focused" {
		return nil, errors.New("invalid palette scope")
	}
	if q.Scope == "" {
		q.Scope = "all"
	}
	q.Limit = 20
	if w, ok := a.Desk().Focused(ctx); ok {
		q.FocusedWindow = w.ID
	}
	if id == "surface/cites" {
		return a.paletteCites(ctx, q), nil
	}
	p, ok := a.palette[id]
	if !ok {
		return nil, ErrPaletteProvider
	}
	if p.info.Mode != "tabs" && q.Text == "" {
		return []module.PaletteResult{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	select {
	case a.paletteSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	type result struct {
		rows []module.PaletteResult
		err  error
	}
	done := make(chan result, 1)
	go func() {
		defer func() { <-a.paletteSlots }()
		rows, err := p.search(ctx, p.host, q)
		done <- result{rows, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case got := <-done:
		if got.err != nil {
			return nil, got.err
		}
		out := []module.PaletteResult{}
		seen := map[string]bool{}
		for _, r := range got.rows {
			if r.ID == "" || r.Title == "" || seen[r.ID] {
				continue
			}
			actions := 0
			if r.Ref != nil {
				if !module.ValidRefKind(r.Ref.Kind) || r.Ref.Canonical == "" {
					continue
				}
				actions++
			}
			if r.Command != nil {
				if r.Command.Command == "" {
					continue
				}
				actions++
			}
			if r.Window != "" {
				actions++
			}
			if actions != 1 {
				continue
			}
			if r.Window == "" {
				r.Close = false
			}
			seen[r.ID] = true
			out = append(out, r)
			if len(out) == q.Limit {
				break
			}
		}
		return out, nil
	}
}
func (a *App) paletteCites(ctx context.Context, q module.PaletteQuery) []module.PaletteResult {
	out := []module.PaletteResult{}
	if q.Text == "" {
		return out
	}
	a.cite.mu.Lock()
	for key, c := range a.cite.m {
		if c.entity == nil || time.Now().After(c.expires) {
			continue
		}
		kind, id, ok := strings.Cut(key, "\x00")
		if !ok {
			continue
		}
		e := c.entity
		if strings.Contains(strings.ToLower(e.Title+" "+id), strings.ToLower(q.Text)) {
			out = append(out, module.PaletteResult{ID: key, Title: e.Title, Description: id, Ref: &module.PaletteRef{Kind: kind, Canonical: id}})
		}
	}
	a.cite.mu.Unlock()
	// Check live readiness outside the cache lock.
	filtered := out[:0]
	for _, r := range out {
		if _, err := a.ref(ctx, r.Ref.Kind); err == nil {
			filtered = append(filtered, r)
		}
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].ID < filtered[j].ID })
	if len(filtered) > q.Limit {
		filtered = filtered[:q.Limit]
	}
	return filtered
}
