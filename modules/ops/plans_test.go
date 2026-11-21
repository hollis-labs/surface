package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
	"github.com/hollis-labs/surface/module/tetherlink"
)

// planTether adds the plan tools' actual legacy envelope to the existing fake Torque.
// task_get remains typed; plan_list_children's brief records need only their IDs.
type planTether struct {
	*tether
	plans  map[string]string
	pages  map[string]string
	phases map[string]string
}

func newPlanTether() *planTether {
	return &planTether{tether: newTether(), plans: map[string]string{}, pages: map[string]string{}, phases: map[string]string{}}
}
func (f *planTether) Call(ctx context.Context, tool string, args map[string]any) (string, error) {
	if tool == "torque_plan_get" || tool == "torque_plan_list_children" {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.counts[tool]++
		if err := ctx.Err(); err != nil {
			return "", err
		}
		id := fmt.Sprint(args["plan_id"])
		raw := f.plans[id]
		if tool == "torque_plan_list_children" {
			cursor, _ := args["cursor"].(string)
			raw = f.pages[id+":"+cursor]
		}
		if raw == "" {
			return "", errors.New("fixture unavailable")
		}
		return raw, nil
	}
	raw, err := f.tether.Call(ctx, tool, args)
	if tool == "torque_task_get" && err == nil {
		var out map[string]any
		if err = json.Unmarshal([]byte(raw), &out); err != nil {
			return "", err
		}
		f.mu.Lock()
		out["data"].(map[string]any)["phase_id"] = f.phases[fmt.Sprint(args["id"])]
		f.mu.Unlock()
		raw = js(out)
	}
	return raw, err
}
func planFixture(id string) string {
	return js(map[string]any{"ok": true, "data": map[string]any{
		"task": map[string]any{"ID": id, "Title": "Ship the garden", "Status": "doing"},
		"plan": map[string]any{"phases": []Phase{{ID: "build", Name: "Build", Order: 2, State: "active"}, {ID: "design", Name: "Design", Order: 1, State: "done"}}},
	}})
}
func childrenFixture(ids []string, next string) string {
	items := []map[string]string{}
	for _, id := range ids {
		items = append(items, map[string]string{"id": id})
	}
	return js(map[string]any{"ok": true, "data": map[string]any{"items": items, "meta": map[string]any{"has_more": next != "", "next_cursor": next}}})
}

func TestPlanTasksJoinRollupActivityAndSlot(t *testing.T) {
	f := newPlanTether()
	const plan = "CW-20990101-0200"
	ids := []string{"CW-20990101-0201", "CW-20990101-0202", "CW-20990101-0203"}
	f.plans[plan] = planFixture(plan)
	f.pages[plan+":"] = childrenFixture(ids[:1], "page2")
	f.pages[plan+":page2"] = childrenFixture(ids[1:], "")
	f.tasks[ids[0]] = [3]string{"Designed", "done", "2099-01-01T10:00:00Z"}
	f.tasks[ids[1]] = [3]string{"Building", "doing", "2099-01-01T11:00:00Z"}
	f.tasks[ids[2]] = [3]string{"Unassigned", "todo", "2099-01-01T09:00:00Z"}
	f.phases[ids[0]], f.phases[ids[1]] = "design", "build"
	f.epics["EP-20990101-0001"] = []string{ids[1]}
	f.comments[ids[1]] = [][2]string{{"2099-01-01T12:00:00Z", "STATUS milestone: Garden built"}}
	m := &mod{caller: f}
	h := moduletest.Start(t, m)
	call(t, h, "ops_create", `{"id":"garden","title":"Garden","stage":"in_flight","links":[{"kind":"torque.task","id":"`+ids[0]+`"},{"kind":"torque.epic","id":"EP-20990101-0001"}]}`)
	call(t, h, "ops_create", `{"id":"garden-build","title":"Build","parent":"garden","links":[{"kind":"torque.plan","id":"`+plan+`"},{"kind":"torque.plan","id":"`+plan+`"}]}`)
	out := call(t, h, "ops_get", `{"id":"garden"}`)
	if out["tasks"].(map[string]any)["text"] != "1 doing · 1 todo · 1 done" {
		t.Fatalf("counts: %s", js(out))
	}
	r, _, ok := m.st.cachedRollup(t.Context(), "garden")
	if !ok || len(r.Tasks) != 3 || len(r.Plans) != 1 || r.Plans[0].Phases[0].ID != "design" {
		t.Fatalf("plan rollup: %+v", r)
	}
	if r.Plans[0].Status != "doing" || r.Plans[0].Phases[1].State != "active" {
		t.Fatalf("plan states: %+v", r.Plans)
	}
	for _, task := range r.Tasks {
		if task.PlanID != plan || task.PhaseID != f.phases[task.ID] {
			t.Fatalf("task membership: %+v", task)
		}
	}
	if !strings.Contains(js(out["plans"]), "Ship the garden") || !strings.Contains(js(out["activity"]), "STATUS milestone: Garden built") {
		t.Fatalf("ops_get: %s", js(out))
	}
	activity := call(t, h, "ops_activity", `{"id":"garden"}`)
	if strings.Count(js(activity["entries"]), "STATUS milestone: Garden built") != 1 {
		t.Fatalf("activity: %s", js(activity))
	}
	knowledge, err := m.knowledge(t.Context(), h, module.Situation{Focused: &module.WindowContext{Ref: "ops:garden"}})
	if err != nil || !strings.Contains(knowledge, "Ship the garden") || !strings.Contains(knowledge, "1 doing") {
		t.Fatalf("slot: %s %v", knowledge, err)
	}
	// Parent and child share both the plan expansion and individual task/comment reads.
	call(t, h, "ops_activity", `{"id":"garden-build","refresh":true}`)
	if f.counts["torque_plan_get"] != 1 || f.counts["torque_plan_list_children"] != 2 || f.counts["torque_task_get"] != 3 || f.counts["torque_comment_list"] != 2 {
		t.Fatalf("shared reads: %v", f.counts)
	}
	// After the minute's cache expires, both plan and task states are read again.
	m.tasks.mu.Lock()
	for id, v := range m.tasks.m {
		v.at = time.Now().Add(-2 * taskTTL)
		m.tasks.m[id] = v
	}
	for id, v := range m.tasks.plans {
		v.at = time.Now().Add(-2 * taskTTL)
		m.tasks.plans[id] = v
	}
	m.tasks.mu.Unlock()
	f.mu.Lock()
	f.tasks[ids[1]] = [3]string{"Built", "review", "2099-01-01T13:00:00Z"}
	f.plans[plan] = strings.ReplaceAll(f.plans[plan], `"active"`, `"done"`)
	f.mu.Unlock()
	fresh, err := m.rollup(t.Context(), "garden", true)
	if err != nil || fresh.Counts["review"] != 1 || fresh.Plans[0].Phases[1].State != "done" {
		t.Fatalf("live states: %+v %v", fresh, err)
	}
}

func TestPlanExpansionBoundsAndPartialFailures(t *testing.T) {
	for _, mode := range []string{"bounded", "combined", "empty", "partial", "metadata", "cursor"} {
		t.Run(mode, func(t *testing.T) {
			f := newPlanTether()
			const plan = "CW-20990101-0300"
			f.plans[plan] = planFixture(plan)
			ids := []string{}
			for i := 0; i < maxTasks+5; i++ {
				id := fmt.Sprintf("CW-20990102-%04d", i)
				ids = append(ids, id)
				f.tasks[id] = [3]string{"Child", "done", ""}
			}
			f.pages[plan+":"] = childrenFixture(ids, "")
			links := []Link{{Kind: "torque.plan", ID: plan}}
			switch mode {
			case "combined":
				for _, id := range ids[:maxTasks] {
					links = append(links, Link{Kind: "torque.task", ID: id})
				}
				f.pages[plan+":"] = childrenFixture(ids[maxTasks:], "")
			case "empty":
				f.pages[plan+":"] = childrenFixture(nil, "")
			case "partial":
				f.pages[plan+":"] = childrenFixture(ids[:1], "missing")
			case "metadata":
				delete(f.plans, plan)
				f.pages[plan+":"] = childrenFixture(ids[:1], "")
			case "cursor":
				f.pages[plan+":"] = childrenFixture(ids[:1], "again")
				f.pages[plan+":again"] = childrenFixture(ids[:1], "again")
			}
			r := reader{link: func() (tetherlink.Caller, error) { return f, nil }, cache: newTaskCache()}.Roll(t.Context(), Workstream{ID: "bounded", Links: links}, nil)
			want := 1
			if mode == "bounded" || mode == "combined" {
				want = maxTasks
			}
			if mode == "empty" {
				want = 0
			}
			if len(r.Tasks) != want || len(r.Plans) != 1 || (len(r.Errors) == 0) != (mode == "empty") {
				t.Fatalf("partial rollup: %+v", r)
			}
			if mode != "metadata" && mode != "empty" && !r.Plans[0].Truncated {
				t.Fatal("incomplete plan not marked")
			}
			if f.counts["torque_task_get"] > maxTasks {
				t.Fatalf("task bound: %v", f.counts)
			}
			if !allowed["torque_plan_get"] || !allowed["torque_plan_list_children"] {
				t.Fatal("plan read tools not allowed")
			}
		})
	}
}
