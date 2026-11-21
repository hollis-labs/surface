package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"
)

// Plan is a linked Torque plan and its ordered phases. Tasks remain in Rollup.Tasks,
// deduplicated with direct and epic links, and join the same activity reads.
type Plan struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Status     string  `json:"status"`
	Workstream string  `json:"workstream"`
	Phases     []Phase `json:"phases"`
	Via        string  `json:"via,omitempty"` // directly linked task that revealed this plan
	Truncated  bool    `json:"truncated,omitempty"`
}

type Phase struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Order int    `json:"order"`
	State string `json:"state"`
}

type planRead struct {
	plan Plan
	ids  []string
	at   time.Time
}

func (r reader) plan(ctx context.Context, id string) (Plan, []string, error) {
	if r.cache != nil {
		r.cache.mu.Lock()
		cached, ok := r.cache.plans[id]
		r.cache.mu.Unlock()
		if ok && time.Since(cached.at) < taskTTL {
			return cached.plan, cached.ids, nil
		}
	}
	p := Plan{ID: id, Status: "unknown", Phases: []Phase{}}
	raw, err := r.call(ctx, "torque_plan_get", map[string]any{"plan_id": id})
	var metadataErr error
	if err != nil {
		metadataErr = err
	} else {
		var out struct {
			Data struct {
				Task struct{ ID, Title, Status string } `json:"task"`
				Plan struct {
					Phases []Phase `json:"phases"`
				} `json:"plan"`
			} `json:"data"`
		}
		if json.Unmarshal(raw, &out) != nil || out.Data.Task.ID != id {
			metadataErr = errors.New("torque answered, but not with the plan")
		} else {
			p.Title, p.Status = out.Data.Task.Title, out.Data.Task.Status
			if out.Data.Plan.Phases != nil {
				p.Phases = out.Data.Plan.Phases
			}
			sort.SliceStable(p.Phases, func(i, j int) bool { return p.Phases[i].Order < p.Phases[j].Order })
		}
	}
	// Read children even if metadata failed: a partial plan must not hide its tasks.
	ids, truncated, childErr := r.planTasks(ctx, id)
	p.Truncated = truncated
	err = errors.Join(metadataErr, childErr)
	if err == nil && r.cache != nil {
		r.cache.mu.Lock()
		if r.cache.plans == nil {
			r.cache.plans = map[string]planRead{}
		}
		r.cache.plans[id] = planRead{plan: p, ids: ids, at: time.Now()}
		if len(r.cache.plans) > 4*maxTasks {
			for key, value := range r.cache.plans {
				if time.Since(value.at) > taskTTL {
					delete(r.cache.plans, key)
				}
			}
		}
		r.cache.mu.Unlock()
	}
	return p, ids, err
}

// planTasks follows server pages, but never reads more than maxTasks children or pages.
func (r reader) planTasks(ctx context.Context, id string) ([]string, bool, error) {
	ids := []string{}
	seen, cursors := map[string]bool{}, map[string]bool{}
	cursor := ""
	for page := 0; page < maxTasks; page++ {
		args := map[string]any{"plan_id": id, "limit": strconv.Itoa(maxTasks - len(ids))}
		if cursor != "" {
			args["cursor"] = cursor
		}
		raw, err := r.call(ctx, "torque_plan_list_children", args)
		if err != nil {
			return ids, true, err
		}
		var out struct {
			Data struct {
				Items []struct {
					ID string `json:"id"`
				} `json:"items"`
				Meta struct {
					HasMore    bool   `json:"has_more"`
					Via        string `json:"via,omitempty"` // directly linked task that revealed this plan
					Truncated  bool   `json:"truncated"`
					NextCursor string `json:"next_cursor"`
				} `json:"meta"`
			} `json:"data"`
		}
		if json.Unmarshal(raw, &out) != nil || out.Data.Items == nil {
			return ids, true, errors.New("torque answered, but not with plan children")
		}
		for _, child := range out.Data.Items {
			if child.ID == "" || seen[child.ID] {
				continue
			}
			if len(ids) == maxTasks {
				return ids, true, nil
			}
			seen[child.ID] = true
			ids = append(ids, child.ID)
		}
		more := out.Data.Meta.HasMore || out.Data.Meta.Truncated || out.Data.Meta.NextCursor != ""
		if !more {
			return ids, false, nil
		}
		if len(ids) == maxTasks {
			return ids, true, nil
		}
		cursor = out.Data.Meta.NextCursor
		if cursor == "" || cursors[cursor] {
			return ids, true, fmt.Errorf("plan children pagination did not advance")
		}
		cursors[cursor] = true
	}
	return ids, true, errors.New("plan children exceeded the page limit")
}
