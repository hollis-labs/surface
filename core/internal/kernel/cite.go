package kernel

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hollis-labs/surface/module"
)

// Cite: references in any text, resolved by the module that owns the records. The page finds
// them (web/src/cite) and asks here; each kind is a module's (Manifest.Refs), and the core
// batches, bounds and caches the lookups so a page full of chips costs one call per kind.

const (
	// MaxCiteBatch is the most ids one resolve takes.
	MaxCiteBatch = 50
	citeFound    = 45 * time.Second
	citeMissing  = 15 * time.Second
	citeCacheMax = 2000
	citeResolveT = 12 * time.Second
	citeRecordT  = 15 * time.Second
)

// ErrUnknownRefKind means no module resolves the kind (or it cannot now).
var ErrUnknownRefKind = errors.New("no module resolves that kind of reference here")

// boundRef is a module's reference kind bound to its Host.
type boundRef struct {
	module.RefKind
	Module string
	h      module.Host
}

// RefKindInfo is a kind as the page is told it.
type RefKindInfo struct {
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Module string `json:"module"`
	// Record: the kind has more to show than its preview.
	Record bool `json:"record,omitempty"`
}

type citeEntry struct {
	entity  *module.RefEntity // nil: not found
	expires time.Time
}

type citeCache struct {
	mu sync.Mutex
	m  map[string]citeEntry
}

func (a *App) addRefs(h *host) error {
	for _, r := range h.man.Refs {
		if !module.ValidRefKind(r.Kind) {
			return fmt.Errorf("surface: module %q declares reference kind %q: not a valid kind", h.id, r.Kind)
		}
		if r.Resolve == nil {
			return fmt.Errorf("surface: reference kind %q of module %q has no Resolve", r.Kind, h.id)
		}
		if by, dup := a.refs[r.Kind]; dup {
			return fmt.Errorf("surface: reference kind %q is declared by both %s and %s", r.Kind, by.Module, h.id)
		}
		if r.Label == "" {
			r.Label = r.Kind
		}
		a.refs[r.Kind] = &boundRef{RefKind: r, Module: h.id, h: h}
	}
	return nil
}

func (a *App) ref(ctx context.Context, kind string) (*boundRef, error) {
	r, ok := a.refs[kind]
	if !ok || (r.Ready != nil && !r.Ready(ctx, r.h)) {
		return nil, fmt.Errorf("%w: %q", ErrUnknownRefKind, kind)
	}
	return r, nil
}

// RefKinds lists the kinds the app can resolve now, by kind.
func (a *App) RefKinds(ctx context.Context) []RefKindInfo {
	out := []RefKindInfo{}
	for _, r := range a.refs {
		if r.Ready != nil && !r.Ready(ctx, r.h) {
			continue
		}
		out = append(out, RefKindInfo{Kind: r.Kind, Label: r.Label, Module: r.Module, Record: r.Record != nil})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

// ResolveRefs looks up ids of one kind: what each stands for, nil for one not found. Answers
// are cached briefly (found 45 s, not found 15 s); the rest go to the module in one batch. An
// error is returned only when the module failed and nothing could be answered.
func (a *App) ResolveRefs(ctx context.Context, kind string, ids []string) (map[string]*module.RefEntity, error) {
	r, err := a.ref(ctx, kind)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 || len(ids) > MaxCiteBatch {
		return nil, fmt.Errorf("give 1 to %d ids", MaxCiteBatch)
	}
	out := make(map[string]*module.RefEntity, len(ids))
	var ask []string
	seen := map[string]bool{}
	now := time.Now()
	a.cite.mu.Lock()
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if len(id) > 2048 || (r.ID != nil && !r.ID.MatchString(id)) {
			out[id] = nil
			continue
		}
		if e, ok := a.cite.m[kind+"\x00"+id]; ok && now.Before(e.expires) {
			out[id] = e.entity
			continue
		}
		ask = append(ask, id)
	}
	a.cite.mu.Unlock()
	if len(ask) == 0 {
		return out, nil
	}
	cctx, cancel := context.WithTimeout(ctx, citeResolveT)
	defer cancel()
	found, err := r.Resolve(cctx, r.h, ask)
	if err != nil {
		cached := 0
		for _, id := range ids {
			if _, ok := out[id]; ok {
				cached++
			}
		}
		if cached == 0 {
			return nil, err
		}
		return out, nil // what was cached; the rest are left out, to be asked again
	}
	now = time.Now()
	a.cite.mu.Lock()
	if len(a.cite.m) > citeCacheMax {
		a.cite.m = map[string]citeEntry{}
	}
	for _, id := range ask {
		e := citeEntry{expires: now.Add(citeMissing)}
		if f, ok := found[id]; ok {
			f.Body = "" // a preview never carries the record
			e = citeEntry{entity: &f, expires: now.Add(citeFound)}
		}
		a.cite.m[kind+"\x00"+id] = e
		out[id] = e.entity
	}
	a.cite.mu.Unlock()
	return out, nil
}

// RecordRef reads one reference whole, for the record window. Never cached: the owner opened
// it to read it as it is now.
func (a *App) RecordRef(ctx context.Context, kind, id string) (module.RefEntity, error) {
	r, err := a.ref(ctx, kind)
	if err != nil {
		return module.RefEntity{}, err
	}
	if id == "" || len(id) > 2048 || (r.ID != nil && !r.ID.MatchString(id)) {
		return module.RefEntity{}, module.ErrRefNotFound
	}
	cctx, cancel := context.WithTimeout(ctx, citeRecordT)
	defer cancel()
	if r.Record != nil {
		return r.Record(cctx, r.h, id)
	}
	found, err := r.Resolve(cctx, r.h, []string{id})
	if err != nil {
		return module.RefEntity{}, err
	}
	e, ok := found[id]
	if !ok {
		return module.RefEntity{}, module.ErrRefNotFound
	}
	return e, nil
}
