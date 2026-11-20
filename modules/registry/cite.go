package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/hollis-labs/surface/module"
)

// Cite: a Registry record named in any text as `registry:<id>`, where <id> is the record's
// canonical id as the Registry gives it (`<kind>:<native id>`: project:surface,
// tool:mcp/tether/torque_task_get, document:guide/ops). The prefix keeps an ordinary
// "project:x" in prose from turning into a chip. The chip's canonical form is the bare id.
//
// Resolving reads the record with registry_record_get (the working copy when nothing is
// published), and nothing else.

const (
	citeKind   = "registry.record"
	citePrefix = "registry:"
	// citeConcurrency bounds the gateway calls one batch makes at once.
	citeConcurrency = 6
)

func (m *mod) refs() []module.RefKind {
	return []module.RefKind{{
		Kind: citeKind, Label: "Registry record", ID: idRe,
		Ready: func(context.Context, module.Host) bool { return m.ready() },
		Resolve: func(ctx context.Context, _ module.Host, ids []string) (map[string]module.RefEntity, error) {
			return m.resolve(ctx, ids)
		},
		Record: func(ctx context.Context, _ module.Host, id string) (module.RefEntity, error) {
			e, err := m.entity(ctx, id, true)
			if errors.Is(err, errMissing) {
				return e, module.ErrRefNotFound
			}
			return e, err
		},
	}}
}

// resolve looks the ids up a few at a time. One that is not there is left out; when every
// lookup failed for another reason, that is the batch's error.
func (m *mod) resolve(ctx context.Context, ids []string) (map[string]module.RefEntity, error) {
	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		out    = map[string]module.RefEntity{}
		failed error
		ok     int
		sem    = make(chan struct{}, citeConcurrency)
	)
	for _, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			e, err := m.entity(ctx, id, false)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				out[id] = e
				ok++
			case errors.Is(err, errMissing):
				ok++
			default:
				failed = err
			}
		}()
	}
	wg.Wait()
	if failed != nil && ok == 0 {
		return nil, failed
	}
	return out, nil
}

// entity reads one record as a reference: its card, and with full its fields as the body.
func (m *mod) entity(ctx context.Context, id string, full bool) (module.RefEntity, error) {
	raw, err := m.call(ctx, "registry_record_get", map[string]any{"id": id, "publication": "published"})
	pub := "published"
	if errors.Is(err, errMissing) {
		pub = "draft"
		raw, err = m.call(ctx, "registry_record_get", map[string]any{"id": id, "publication": pub})
	}
	if err != nil {
		return module.RefEntity{}, err
	}
	var rec rawRecord
	if err := json.Unmarshal(raw, &rec); err != nil || rec.ID == "" {
		return module.RefEntity{}, errors.New("the Registry's record could not be read")
	}
	b := rec.summary()
	e := module.RefEntity{
		Title:       b.Name,
		Subtitle:    fmt.Sprintf("%s · %s", b.Kind, b.Schema),
		Description: clip(b.Summary, 300),
		UpdatedAt:   b.UpdatedAt,
		ChipLabel:   clip(b.Kind+": "+b.Name, 80),
		Open:        &module.RefOpen{Command: "registry.open", Args: map[string]any{"id": b.ID}},
	}
	if b.Lifecycle != "" {
		e.Status = &module.RefStatus{Label: b.Lifecycle, Tone: module.StatusTone(b.Lifecycle)}
	}
	e.Fields = append(e.Fields, module.RefField{Label: "ID", Value: b.ID, Mono: true})
	if b.Owner != "" {
		e.Fields = append(e.Fields, module.RefField{Label: "Owner", Value: b.Owner, Mono: true})
	}
	e.Fields = append(e.Fields, module.RefField{Label: "Publication", Value: pubWord(pub)})
	if b.Review != nil && b.Review.State != "" {
		e.Fields = append(e.Fields, module.RefField{Label: "Review", Value: b.Review.State})
	}
	if b.Edges > 0 {
		e.Fields = append(e.Fields, module.RefField{Label: "Relations", Value: fmt.Sprint(b.Edges)})
	}
	if full {
		e.Body = string(raw)
	}
	return e, nil
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
