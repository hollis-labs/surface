package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
	"github.com/hollis-labs/surface/module/tetherlink"
)

type discoveryTether struct {
	*planTether
	metadata map[string]map[string]string
}

func (f *discoveryTether) Call(ctx context.Context, tool string, args map[string]any) (string, error) {
	raw, err := f.planTether.Call(ctx, tool, args)
	if tool == "torque_task_get" && err == nil {
		var out map[string]any
		if err = json.Unmarshal([]byte(raw), &out); err != nil {
			return "", err
		}
		f.mu.Lock()
		for key, value := range f.metadata[fmt.Sprint(args["id"])] {
			out["data"].(map[string]any)[key] = value
		}
		f.mu.Unlock()
		raw = js(out)
	}
	return raw, err
}
func discoveryFixture() *discoveryTether {
	f := &discoveryTether{planTether: newPlanTether(), metadata: map[string]map[string]string{}}
	for _, id := range []string{"CW-20990101-0001", "CW-20990101-0002", "CW-20990101-0003", "CW-20990101-0100"} {
		f.tasks[id] = [3]string{id, "doing", "2099-01-01T10:00:00Z"}
	}
	f.metadata["CW-20990101-0001"] = map[string]string{"parent_id": "CW-20990101-0100", "epic_id": "EP-20990101-0001"}
	f.metadata["CW-20990101-0100"] = map[string]string{"kind": "plan", "parent_id": "CW-20990101-0001"} // cycle must not recurse
	f.plans["CW-20990101-0100"] = planFixture("CW-20990101-0100")
	f.pages["CW-20990101-0100:"] = childrenFixture([]string{"CW-20990101-0001", "CW-20990101-0002"}, "")
	f.epics["EP-20990101-0001"] = []string{"CW-20990101-0001", "CW-20990101-0003"}
	f.comments["CW-20990101-0002"] = [][2]string{{"2099-01-01T11:00:00Z", "STATUS milestone: discovered plan child"}}
	return f
}

func TestDiscoverPlanAndEpicFromLinkedTask(t *testing.T) {
	f := discoveryFixture()
	m := &mod{caller: f}
	h := moduletest.Start(t, m)
	call(t, h, "ops_create", `{"id":"root","title":"Root"}`)
	call(t, h, "ops_create", `{"id":"child","title":"Child","parent":"root","links":[{"kind":"torque.task","id":"CW-20990101-0001"}]}`)
	got := call(t, h, "ops_get", `{"id":"root"}`)
	if !strings.Contains(js(got["plans"]), `"via":"CW-20990101-0001"`) {
		t.Fatalf("missing provenance: %s", js(got))
	}
	roll, _, _ := m.st.cachedRollup(t.Context(), "root")
	if len(roll.Tasks) != 3 || len(roll.Plans) != 1 || roll.Counts["doing"] != 3 {
		t.Fatalf("roll: %+v", roll)
	}
	if roll.Plans[0].Workstream != "child" {
		t.Fatalf("provenance: %+v", roll.Plans)
	}
	for _, task := range roll.Tasks {
		if task.ID == "CW-20990101-0003" && (task.Via != "EP-20990101-0001" || task.DiscoveredVia != "CW-20990101-0001") {
			t.Fatalf("epic: %+v", task)
		}
	}
	if strings.Count(js(roll.Entries), "STATUS milestone: discovered plan child") != 1 {
		t.Fatalf("activity: %+v", roll.Entries)
	}
	before := f.counts["torque_task_get"]
	call(t, h, "ops_activity", `{"id":"child","refresh":true}`)
	if f.counts["torque_task_get"] != before {
		t.Fatal("parent/task cache not shared")
	}
	// Discovery is read-only, and linking uses the existing owner route on the source child.
	w, _, _ := m.st.Get(t.Context(), "child")
	if len(w.Links) != 1 {
		t.Fatalf("discovery wrote links: %+v", w.Links)
	}
	for _, actor := range []module.Actor{module.ActorOutside, module.ActorAgent, module.ActorOwner} {
		req := httptest.NewRequest("POST", "/api/m/ops/workstreams/child/links", strings.NewReader(`{"kind":"torque.plan","id":"CW-20990101-0100"}`))
		req = req.WithContext(module.WithActor(req.Context(), actor))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		want := 403
		if actor == module.ActorOwner {
			want = 200
		}
		if rec.Code != want {
			t.Fatalf("link as %s: %d %s", actor, rec.Code, rec.Body.String())
		}
	}
	call(t, h, "ops_activity", `{"id":"root","refresh":true}`)
	roll, _, _ = m.st.cachedRollup(t.Context(), "root")
	if len(roll.Plans) != 1 || roll.Plans[0].Via != "" || len(roll.Tasks) != 3 {
		t.Fatalf("explicit duplicate: %+v", roll)
	}
}

func TestDiscoveryFailuresBoundsAndRefresh(t *testing.T) {
	for _, mode := range []string{"ordinary", "missing", "self", "epic-parent", "bounded", "refresh"} {
		t.Run(mode, func(t *testing.T) {
			f := discoveryFixture()
			w := Workstream{ID: "w", Links: []Link{{Kind: "torque.task", ID: "CW-20990101-0001"}}}
			switch mode {
			case "ordinary":
				f.metadata["CW-20990101-0100"]["kind"] = "agent"
			case "missing":
				delete(f.tasks, "CW-20990101-0100")
			case "self":
				f.metadata["CW-20990101-0001"]["parent_id"] = "CW-20990101-0001"
			case "epic-parent":
				f.metadata["CW-20990101-0001"] = map[string]string{"parent_id": "EP-20990101-0001"}
			case "bounded":
				ids := []string{"CW-20990101-0001"}
				for i := 0; i < 100; i++ {
					id := fmt.Sprintf("CW-20990101-%04d", 200+i)
					ids = append(ids, id)
					f.tasks[id] = [3]string{id, "todo", ""}
				}
				f.pages["CW-20990101-0100:"] = childrenFixture(ids, "")
			}
			cache := newTaskCache()
			r := reader{link: func() (tetherlink.Caller, error) { return f, nil }, cache: cache}
			roll := r.Roll(t.Context(), w, nil)
			switch mode {
			case "ordinary", "missing", "self", "epic-parent":
				if len(roll.Plans) != 0 || len(roll.Tasks) != 2 {
					t.Fatalf("unexpected expansion: %+v", roll)
				}
				if mode == "missing" && len(roll.Errors) == 0 {
					t.Fatal("missing parent hidden")
				}
			case "bounded":
				if len(roll.Tasks) != maxTasks || !roll.Plans[0].Truncated || len(roll.Errors) == 0 {
					t.Fatalf("bounds: %+v", roll)
				}
			case "refresh":
				cache.mu.Lock()
				p := cache.parents["CW-20990101-0100"]
				p.at = time.Now().Add(-2 * taskTTL)
				cache.parents["CW-20990101-0100"] = p
				cache.mu.Unlock()
				f.metadata["CW-20990101-0100"]["kind"] = "agent"
				if next := r.Roll(t.Context(), w, nil); len(next.Plans) != 0 {
					t.Fatal("stale parent kind retained")
				}
			}
		})
	}
}

func TestDiscoveredPlanGlobalFeedAndBudget(t *testing.T) {
	f := discoveryFixture()
	m := &mod{caller: f}
	h := moduletest.Start(t, m)
	call(t, h, "ops_create", `{"id":"garden","title":"Garden","links":[{"kind":"torque.task","id":"CW-20990101-0001"}]}`)
	got := call(t, h, "ops_activity", `{"source":"comment"}`)
	if strings.Count(js(got["entries"]), "discovered plan child") != 1 {
		t.Fatalf("global feed: %s", js(got))
	}
	// The global coordinator's budget applies to parent metadata too, without poisoning it.
	cache := newTaskCache()
	r := reader{link: m.tether, cache: cache}
	budget := &activityBudget{calls: globalCalls}
	ctx := context.WithValue(t.Context(), activityBudgetKey{}, budget)
	if _, err := r.parent(ctx, "CW-20990101-0100"); !errors.Is(err, errActivityBudget) || len(cache.parents) != 0 {
		t.Fatalf("budget cached: %v %+v", err, cache.parents)
	}
	if p, err := r.parent(t.Context(), "CW-20990101-0100"); err != nil || p.Kind != "plan" {
		t.Fatalf("retry failed: %+v %v", p, err)
	}
}
