package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hollis-labs/surface/module"
)

func TestGlobalActivityRoutesToolsFiltersAndSlot(t *testing.T) {
	f := newTether()
	f.tasks["CW-20990101-0101"] = [3]string{"First task", "doing", "2099-01-01T10:00:00Z"}
	f.tasks["CW-20990101-0102"] = [3]string{"Second task", "review", "2099-01-01T11:00:00Z"}
	f.comments["CW-20990101-0102"] = [][2]string{{"2099-01-01T12:00:00Z", "STATUS milestone: Ready"}}
	h, m := start(t, f)
	call(t, h, "ops_create", `{"id":"parent","title":"Parent","stage":"in_flight","links":[{"kind":"torque.task","id":"CW-20990101-0101"}]}`)
	call(t, h, "ops_create", `{"id":"child","title":"Child","parent":"parent","stage":"in_review","links":[{"kind":"torque.task","id":"CW-20990101-0102"}]}`)
	code, body := h.Do("GET", "/activity", "")
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	var feed globalFeed
	if err := json.Unmarshal([]byte(body), &feed); err != nil {
		t.Fatal(err)
	}
	if len(feed.Entries) != 5 || feed.Entries[0].Summary != "STATUS milestone: Ready" {
		t.Fatalf("feed=%+v", feed)
	}
	seen := map[string]bool{}
	for i, e := range feed.Entries {
		if e.Workstream == "" || seen[jsEntry(e)] {
			t.Fatalf("unlabelled/duplicate: %+v", e)
		}
		seen[jsEntry(e)] = true
		if i > 0 && When(e.At).After(When(feed.Entries[i-1].At)) {
			t.Fatal("not newest first")
		}
	}
	for _, tc := range []struct {
		query string
		count int
	}{
		{"workstream=child", 3}, {"stage=in_flight", 2}, {"source=comment", 1},
		{"since=2099-01-01T11:30:00Z", 1}, {"workstream=child&stage=in_review&source=torque", 1},
	} {
		code, body = h.Do("GET", "/activity?"+tc.query, "")
		if code != 200 {
			t.Fatalf("%s: %d %s", tc.query, code, body)
		}
		_ = json.Unmarshal([]byte(body), &feed)
		if len(feed.Entries) != tc.count {
			t.Fatalf("%s: %+v", tc.query, feed)
		}
	}
	for _, q := range []string{"since=nonsense", "stage=missing", "source=missing", "workstream=!!"} {
		code, _ = h.Do("GET", "/activity?"+q, "")
		if code != 400 {
			t.Fatalf("%s: %d", q, code)
		}
	}
	out := call(t, h, "ops_activity", `{"source":"comment"}`)
	if len(out["entries"].([]any)) != 1 {
		t.Fatalf("tool=%v", out)
	}
	call(t, h, "ops_activity", `{"id":"parent"}`)
	if f.counts["torque_task_get"] != 2 || f.counts["torque_comment_list"] != 2 {
		t.Fatalf("cache not shared: %v", f.counts)
	}
	slot, err := m.knowledge(context.Background(), h, module.Situation{})
	if err != nil || !strings.Contains(slot, "Latest across workstreams: 2099-01-01T12:00:00Z · ops:child · STATUS milestone: Ready") {
		t.Fatalf("slot=%s err=%v", slot, err)
	}
	wc, _ := m.describeBoard(context.Background(), h, module.Window{Args: map[string]any{"layout": "activity", "parent": "parent", "source": "comment"}})
	if wc.Ref != "ops" || wc.Extra["source"] != "comment" {
		t.Fatalf("context=%+v", wc)
	}
}

func TestGlobalActivityRefreshOrderAndProgress(t *testing.T) {
	f := newTether()
	h, m := start(t, f)
	// Put the in-flight stream after the first batch alphabetically.
	for i := 0; i < rollAtOnce+2; i++ {
		id := fmt.Sprintf("stream-%02d", i)
		stage := StagePlanning
		if i == rollAtOnce+1 {
			stage = StageInFlight
		}
		call(t, h, "ops_create", js(map[string]any{"id": id, "title": id, "stage": stage}))
	}
	got, err := m.globalActivity(context.Background(), activityFilter{}, false)
	if err != nil || got.Pending != 2 {
		t.Fatalf("first=%+v %v", got, err)
	}
	cached := m.st.Cached(context.Background())
	if _, ok := cached[fmt.Sprintf("stream-%02d", rollAtOnce+1)]; !ok {
		t.Fatal("in-flight did not refresh first")
	}
	if _, ok := cached[fmt.Sprintf("stream-%02d", rollAtOnce-1)]; ok {
		t.Fatal("refresh exceeded batch limit")
	}
	// Repeated viewer reads must not turn bounded batches into an unbounded event loop.
	before := len(h.Bus.Topics())
	m.rollActivity(context.Background())
	if len(h.Bus.Topics()) != before || len(m.st.Cached(context.Background())) != rollAtOnce {
		t.Fatal("fresh viewer read triggered another automatic batch or event")
	}
	// The background refresh used while a window is open continues the same cycle.
	m.activityNext = time.Time{}
	m.rollActivity(context.Background())
	if len(m.st.Cached(context.Background())) != rollAtOnce+2 {
		t.Fatal("later lanes did not progress")
	}
	got, err = m.globalActivity(context.Background(), activityFilter{}, false)
	if err != nil || got.Pending != 0 || got.Calls != 0 {
		t.Fatalf("fresh=%+v %v", got, err)
	}
}

func TestGlobalActivityCallBudgetRetainsCompleteCache(t *testing.T) {
	f := newTether()
	h, m := start(t, f)
	for i := 0; i < 3; i++ {
		links := []Link{}
		for j := 0; j < maxTasks; j++ {
			id := fmt.Sprintf("CW-20990101-%04d", i*maxTasks+j)
			f.tasks[id] = [3]string{id, "doing", "2099-01-01T12:00:00Z"}
			links = append(links, Link{Kind: "torque.task", ID: id})
		}
		id := fmt.Sprintf("stream-%d", i)
		call(t, h, "ops_create", js(map[string]any{"id": id, "title": id, "links": links}))
	}
	old := Rollup{Workstream: "stream-1", FetchedAt: "2000-01-01T00:00:00Z", Entries: []Entry{{At: "2000-01-01T00:00:00Z", Source: "ops", Workstream: "stream-1", Summary: "previous complete read"}}}
	m.st.keepRollup(context.Background(), old)
	got, err := m.globalActivity(context.Background(), activityFilter{}, false)
	if err != nil || got.Calls != globalCalls || got.Pending != 2 {
		t.Fatalf("budget=%+v %v", got, err)
	}
	if n := f.counts["torque_task_get"] + f.counts["torque_comment_list"]; n != globalCalls {
		t.Fatalf("network calls=%d", n)
	}
	saved, _, _ := m.st.cachedRollup(context.Background(), "stream-1")
	if saved.FetchedAt != old.FetchedAt {
		t.Fatal("partial refresh replaced complete cache")
	}
	for _, r := range m.tasks.m {
		if errors.Is(r.err, errActivityBudget) {
			t.Fatal("budget refusal poisoned task cache")
		}
	}
	for i := 0; i < 4 && got.Pending > 0; i++ {
		got, err = m.globalActivity(context.Background(), activityFilter{}, true)
		if err != nil || got.Calls > globalCalls {
			t.Fatalf("next=%+v %v", got, err)
		}
	}
	if got.Pending != 0 {
		t.Fatal("bounded rounds never completed")
	}
}

func TestGlobalActivityRowsBoundAndFilterBeforeLimit(t *testing.T) {
	b := Board{Workstreams: []Workstream{{ID: "first", Stage: StageActive}, {ID: "second", Stage: StagePlanning}}}
	cached := map[string]Rollup{}
	for _, w := range b.Workstreams {
		r := Rollup{}
		for i := 0; i < maxEntries; i++ {
			r.Entries = append(r.Entries, Entry{At: time.Date(2099, 1, 1, 0, i, 0, 0, time.UTC).Format(time.RFC3339), Source: "torque", Summary: fmt.Sprint(i), Workstream: w.ID})
		}
		cached[w.ID] = r
	}
	es, truncated := mergedActivity(b, cached, activityFilter{})
	if len(es) != maxEntries || !truncated {
		t.Fatalf("len=%d truncated=%v", len(es), truncated)
	}
	es, truncated = mergedActivity(b, cached, activityFilter{Workstream: "second"})
	if len(es) != maxEntries || truncated {
		t.Fatalf("filtered len=%d truncated=%v", len(es), truncated)
	}
	for _, e := range es {
		if e.Workstream != "second" {
			t.Fatal("filter leaked")
		}
	}
}

func TestGlobalActivityConcurrentViewersShareBatch(t *testing.T) {
	f := newTether()
	h, m := start(t, f)
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("stream-%02d", i)
		task := fmt.Sprintf("CW-20990101-%04d", i)
		f.tasks[task] = [3]string{task, "doing", "2099-01-01T12:00:00Z"}
		call(t, h, "ops_create", js(map[string]any{"id": id, "title": id, "links": []Link{{Kind: "torque.task", ID: task}}}))
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := m.globalActivity(context.Background(), activityFilter{}, false)
			if err != nil || got.Pending != 8 {
				t.Errorf("viewer: pending=%d err=%v", got.Pending, err)
			}
		}()
	}
	wg.Wait()
	if f.counts["torque_task_get"] != rollAtOnce || f.counts["torque_comment_list"] != rollAtOnce {
		t.Fatalf("multiple batches: %v", f.counts)
	}
}
