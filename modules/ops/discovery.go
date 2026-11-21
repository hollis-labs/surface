package ops

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// containerLinks keeps explicit links ahead of discovered ones, so explicit ownership
// wins when descendants and siblings mention the same plan. Via is read-time provenance.
type containerLinks struct {
	ID    string
	Links []Link
	Via   string
}

type parentRead struct {
	task Task
	at   time.Time
}

// parent reads only metadata, not comments. Successful parent lookups share the task TTL;
// failed and interrupted reads must remain retryable by the next rollup.
func (r reader) parent(ctx context.Context, id string) (Task, error) {
	if got, ok := r.cache.get(id); ok && got.err == nil {
		return got.task, nil
	}
	if r.cache != nil {
		r.cache.mu.Lock()
		got, ok := r.cache.parents[id]
		r.cache.mu.Unlock()
		if ok && time.Since(got.at) < taskTTL {
			return got.task, nil
		}
	}
	t, err := r.task(ctx, id)
	if err == nil && r.cache != nil {
		r.cache.mu.Lock()
		if r.cache.parents == nil {
			r.cache.parents = map[string]parentRead{}
		}
		r.cache.parents[id] = parentRead{task: t, at: time.Now()}
		if len(r.cache.parents) > 4*maxTasks {
			for key, value := range r.cache.parents {
				if time.Since(value.at) > taskTTL {
					delete(r.cache.parents, key)
				}
			}
		}
		r.cache.mu.Unlock()
	}
	return t, err
}

func (r reader) discover(ctx context.Context, all []Workstream, initial map[string]taskRead, fail func(error)) []containerLinks {
	out := make([]containerLinks, 0, len(all))
	for _, w := range all {
		out = append(out, containerLinks{ID: w.ID, Links: w.Links})
	}
	// At most 80 distinct linked tasks and parents; no recursion even for cyclic parents.
	parents := map[string]Task{}
	checked := map[string]bool{}
	for _, w := range all {
		for _, l := range w.Links {
			if l.Kind != "torque.task" {
				continue
			}
			got, ok := initial[l.ID]
			if !ok || got.err != nil {
				continue
			}
			t := got.task
			links := []Link{}
			epic := t.EpicID
			if epic == "" && strings.HasPrefix(t.ParentID, "EP-") {
				epic = t.ParentID
			}
			if epic != "" {
				links = append(links, Link{Kind: "torque.epic", ID: epic})
			}
			if t.ParentID != "" && t.ParentID != t.ID && !strings.HasPrefix(t.ParentID, "EP-") {
				if !checked[t.ParentID] {
					checked[t.ParentID] = true
					parent, err := r.parent(ctx, t.ParentID)
					if err != nil {
						fail(fmt.Errorf("parent %s via %s: %w", t.ParentID, t.ID, err))
					} else {
						parents[t.ParentID] = parent
					}
				}
				if p := parents[t.ParentID]; p.Kind == "plan" {
					links = append(links, Link{Kind: "torque.plan", ID: p.ID})
				}
			}
			if len(links) > 0 {
				out = append(out, containerLinks{ID: w.ID, Links: links, Via: t.ID})
			}
		}
	}
	return out
}
