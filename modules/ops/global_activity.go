package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"sync"
	"time"
)

// A refresh round has a total network budget, in addition to Roll's concurrency bound.
const globalCalls = 240

var errActivityBudget = errors.New("activity refresh call limit reached; remaining workstreams refresh next round")

type activityBudgetKey struct{}
type activityBudget struct {
	mu     sync.Mutex
	calls  int
	denied bool
}

func (b *activityBudget) take() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.calls >= globalCalls {
		b.denied = true
		return false
	}
	b.calls++
	return true
}

type activityFilter struct {
	Workstream, Stage, Source, Since, Task string
	Limit                                  int
}

func (f activityFilter) validate() error {
	if f.Workstream != "" && !ValidID(f.Workstream) {
		return &InputError{Msg: "invalid workstream"}
	}
	if f.Stage != "" {
		if _, ok := StageLabel[f.Stage]; !ok {
			return &InputError{Msg: "invalid stage"}
		}
	}
	if f.Source != "" && !slices.Contains([]string{"torque", "comment", "status", "ops", SourceLiveness}, f.Source) {
		return &InputError{Msg: "invalid source"}
	}
	if f.Since != "" && When(f.Since).IsZero() {
		return &InputError{Msg: "since must be a time (RFC 3339)"}
	}
	return nil
}

type globalFeed struct {
	Entries   []Entry  `json:"entries"`
	Errors    []string `json:"errors,omitempty"`
	Pending   int      `json:"pending"`
	Truncated bool     `json:"truncated,omitempty"`
	Calls     int      `json:"calls"`
	FetchedAt string   `json:"fetched_at"`
}

// globalActivity shares the drill-in caches and builder. A cycle visits every workstream,
// in-flight first, in bounded batches; its queue prevents a busy board starving later lanes.
func (m *mod) globalActivity(ctx context.Context, f activityFilter, force bool) (globalFeed, error) {
	if err := f.validate(); err != nil {
		return globalFeed{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	m.activityMu.Lock()
	refreshed := 0
	defer func() {
		m.activityMu.Unlock()
		// Every entry point (page, tool and background) updates other open views once.
		if refreshed > 0 && m.h != nil {
			m.h.Events().Publish("ops.activity", map[string]any{})
		}
	}()
	b, err := m.st.All(ctx)
	if err != nil {
		return globalFeed{}, err
	}
	cached := m.st.Cached(ctx)
	canRefresh := force || !time.Now().Before(m.activityNext)
	if canRefresh && len(m.activityPending) == 0 {
		ws := slices.Clone(b.Workstreams)
		sort.SliceStable(ws, func(i, j int) bool {
			if (ws[i].Stage == StageInFlight) != (ws[j].Stage == StageInFlight) {
				return ws[i].Stage == StageInFlight
			}
			return ws[i].ID < ws[j].ID
		})
		for _, w := range ws {
			r, ok := cached[w.ID]
			if force || !ok || time.Since(When(r.FetchedAt)) >= activityFresh {
				m.activityPending = append(m.activityPending, w.ID)
			}
		}
	}
	budget := &activityBudget{}
	ctx = context.WithValue(ctx, activityBudgetKey{}, budget)
	out := globalFeed{Entries: []Entry{}, FetchedAt: now()}
	if canRefresh && len(m.activityPending) > 0 {
		m.activityNext = time.Now().Add(activityFresh)
	}
	for n := 0; canRefresh && n < rollAtOnce && len(m.activityPending) > 0 && ctx.Err() == nil; n++ {
		id := m.activityPending[0]
		w, ok := b.Find(id)
		if !ok {
			m.activityPending = m.activityPending[1:]
			continue
		}
		r := m.roll(ctx, w, b.Descendants(id))
		if budget.denied || ctx.Err() != nil {
			// Never replace a complete cache with a budget-truncated roll-up.
			out.Errors = append(out.Errors, "Refresh incomplete; cached activity is retained.")
			m.activityPending = append(m.activityPending[1:], id)
			break
		}
		refreshed++
		m.st.keepRollup(ctx, r)
		cached[id] = r
		m.activityPending = m.activityPending[1:]
	}
	out.Calls, out.Pending = budget.calls, len(m.activityPending)
	out.Entries, out.Truncated = mergedActivity(b, cached, f)
	for _, w := range b.Workstreams {
		if f.Workstream != "" && w.ID != f.Workstream || f.Stage != "" && w.Stage != f.Stage {
			continue
		}
		if r, ok := cached[w.ID]; ok {
			for _, e := range r.Errors {
				if len(out.Errors) < 8 {
					out.Errors = append(out.Errors, Ref(w.ID)+": "+e)
				}
			}
		}
	}
	return out, nil
}

// Merge incrementally so the retained rows are globally bounded, even with many caches.
// The workstream stays part of identity: a shared task legitimately belongs to two streams.
func mergedActivity(b Board, cached map[string]Rollup, f activityFilter) ([]Entry, bool) {
	es := []Entry{}
	truncated := false
	limit := f.Limit
	if limit <= 0 || limit > maxEntries {
		limit = maxEntries
	}
	add := func(rows []Entry) {
		seen := map[string]bool{}
		for _, e := range es {
			raw, _ := json.Marshal(e)
			seen[string(raw)] = true
		}
		for _, e := range rows {
			w, ok := b.Find(e.Workstream)
			if !ok {
				continue
			}
			if f.Workstream != "" && e.Workstream != f.Workstream || f.Stage != "" && w.Stage != f.Stage || f.Source != "" && e.Source != f.Source || f.Task != "" && e.Task != f.Task || f.Since != "" && When(e.At).Before(When(f.Since)) {
				continue
			}
			raw, _ := json.Marshal(e)
			key := string(raw)
			if !seen[key] {
				seen[key] = true
				es = append(es, e)
			}
		}
		sort.Slice(es, func(i, j int) bool {
			a, c := When(es[i].At), When(es[j].At)
			if !a.Equal(c) {
				return a.After(c)
			}
			return jsEntry(es[i]) < jsEntry(es[j])
		})
		if len(es) > limit {
			truncated = true
			es = es[:limit]
		}
	}
	for _, w := range b.Workstreams {
		if r, ok := cached[w.ID]; ok {
			add(r.Entries)
		}
	}
	return es, truncated
}
func jsEntry(e Entry) string { b, _ := json.Marshal(e); return string(b) }

func (m *mod) apiGlobalActivity(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := activityFilter{Workstream: q.Get("workstream"), Stage: q.Get("stage"), Source: q.Get("source"), Since: q.Get("since")}
	if err := f.validate(); err != nil {
		failErr(w, err)
		return
	}
	syncErr := m.sync(r.Context(), false)
	got, err := m.globalActivity(r.Context(), f, q.Get("refresh") == "1")
	if err != nil {
		failErr(w, err)
		return
	}
	if syncErr != nil {
		got.Errors = append(got.Errors, syncErr.Error())
	}
	reply(w, http.StatusOK, got)
}

func latestActivityLine(b Board, cached map[string]Rollup) string {
	es, _ := mergedActivity(b, cached, activityFilter{Limit: 1})
	if len(es) == 0 {
		return "Latest across workstreams: no cached activity yet."
	}
	e := es[0]
	return fmt.Sprintf("Latest across workstreams: %s · %s · %s", e.At, Ref(e.Workstream), e.Summary)
}
