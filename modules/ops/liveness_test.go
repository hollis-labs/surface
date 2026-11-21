package ops

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const (
	liveAgent    = "msg://agent/agent-mux/agt_live"
	stoppedAgent = "msg://agent/agent-mux/agt_stopped"
)

func liveSnapshot(warning string, truncated bool) func(context.Context) (SessionsResult, error) {
	return func(context.Context) (SessionsResult, error) {
		return SessionsResult{FetchedAt: "2026-11-21T14:00:00Z", Warning: warning, Truncated: truncated, Sessions: []SessionRow{
			{ID: "session:s-live", AgentID: liveAgent},
			{ID: "seat:run-live", AgentID: "msg://agent/agent-mux/agt_seat"},
		}}, nil
	}
}

func rollupOf(tasks ...Task) Rollup {
	return Rollup{Workstream: "ws", Tasks: tasks, Entries: []Entry{{At: "2026-11-21T13:00:00Z", Source: "torque", Summary: "older"}}}
}

func TestAssignmentReadsTorqueAssignmentThenOwner(t *testing.T) {
	read := func(raw string) *Holder {
		t.Helper()
		var a taskAssignment
		if err := json.Unmarshal([]byte(raw), &a); err != nil {
			t.Fatal(err)
		}
		return a.assignment()
	}
	if read(`{"assignment":null,"metadata":{}}`) != nil {
		t.Fatal("an unassigned task has a holder")
	}
	if read(`{"metadata":{"owner_urn":"Surface lead"}}`) != nil {
		t.Fatal("a free-text owner is a holder")
	}
	if h := read(`{"metadata":{"owner_urn":"` + stoppedAgent + `"}}`); h == nil || h.Agent != stoppedAgent || h.Source != "owner" {
		t.Fatalf("owner fallback: %+v", h)
	}
	// Torque's object (CW-20261121-0148) wins over the metadata.
	active := `{"assignment":{"assignee_urn":"` + liveAgent + `","tether_session_id":"s-9","team_run_id":null,"claim_id":"clm_1","source":"claim","state":"active"},"metadata":{"owner_urn":"` + stoppedAgent + `"}}`
	if h := read(active); h == nil || h.Source != "claim" || h.Agent != liveAgent || h.Session != "s-9" || h.Live != "" {
		t.Fatalf("active assignment: %+v", h)
	}
	expired := `{"assignment":{"assignee_urn":"` + liveAgent + `","source":"doing","state":"expired","end_reason":"session_ended"}}`
	if h := read(expired); h == nil || h.Live != HolderNone || h.Why != "the assignment expired (session ended)" {
		t.Fatalf("expired: %+v", h)
	}
	if h := read(`{"assignment":{"assignee_urn":"` + liveAgent + `","state":"released","end_reason":"done"}}`); h != nil {
		t.Fatalf("released: %+v", h)
	}
}

func TestCheckHoldersFlagsOnlyOpenTasksNobodyLiveHolds(t *testing.T) {
	r := rollupOf(
		Task{ID: "CW-1", Status: "todo", Workstream: "ws", Holder: &Holder{Agent: stoppedAgent, Source: "owner"}},
		Task{ID: "CW-2", Status: "doing", Workstream: "ws", Holder: &Holder{Agent: liveAgent, Source: "owner"}},
		Task{ID: "CW-3", Status: "done", Workstream: "ws", Holder: &Holder{Agent: stoppedAgent, Source: "owner"}},
		Task{ID: "CW-4", Status: "doing", Workstream: "child", Holder: &Holder{Run: "run-live", Source: "claim"}},
		Task{ID: "CW-5", Status: "doing", Workstream: "ws", Holder: &Holder{Agent: liveAgent, Source: "claim", Live: HolderNone, Why: "the assignment expired (lease lapsed)"}},
		Task{ID: "CW-6", Status: "doing", Workstream: "ws"},
		// The agent is live, but not in the session the task is bound to.
		Task{ID: "CW-7", Status: "doing", Workstream: "ws", Holder: &Holder{Agent: liveAgent, Session: "s-dead", Source: "doing"}},
		Task{ID: "CW-8", Status: "doing", Workstream: "ws", Holder: &Holder{Agent: stoppedAgent, Session: "s-live", Source: "doing"}},
	)
	last := r.Entries[0]
	r.Last = &r.Entries[0]
	checkHolders(t.Context(), &r, liveSnapshot("", false))
	if strings.Join(r.NoLiveAgent, ",") != "CW-1,CW-5,CW-7" {
		t.Fatalf("flagged %v", r.NoLiveAgent)
	}
	want := map[string]string{"CW-1": HolderNone, "CW-2": HolderLive, "CW-3": "", "CW-4": HolderLive, "CW-5": HolderNone, "CW-7": HolderNone, "CW-8": HolderLive}
	for _, task := range r.Tasks {
		if task.Holder == nil {
			continue
		}
		if task.Holder.Live != want[task.ID] {
			t.Errorf("%s: %+v", task.ID, task.Holder)
		}
	}
	var rows []Entry
	for _, e := range r.Entries {
		if e.Source == SourceLiveness {
			rows = append(rows, e)
		}
	}
	all := js(rows)
	if len(rows) != 3 || r.Entries[0].Source != SourceLiveness || !strings.Contains(all, "assigned to "+stoppedAgent) ||
		!strings.Contains(all, "lease lapsed") || !strings.Contains(all, "CW-7 is assigned to "+liveAgent) {
		t.Fatalf("feed rows: %+v", r.Entries)
	}
	if r.Last == nil || js(*r.Last) != js(last) {
		t.Fatalf("the newest activity changed: %+v", r.Last)
	}
}

func TestCheckHoldersNeverGuesses(t *testing.T) {
	held := func() Rollup {
		return rollupOf(Task{ID: "CW-1", Status: "doing", Workstream: "ws", Holder: &Holder{Agent: stoppedAgent, Source: "owner"}})
	}
	for name, live := range map[string]func(context.Context) (SessionsResult, error){
		"unreadable": func(context.Context) (SessionsResult, error) { return SessionsResult{}, errors.New("tether down") },
		"truncated":  liveSnapshot("", true),
	} {
		r := held()
		checkHolders(t.Context(), &r, live)
		if len(r.NoLiveAgent) != 0 || r.Tasks[0].Holder.Live != HolderUnknown || len(r.Entries) != 1 {
			t.Fatalf("%s: %+v", name, r)
		}
	}
	// The fallback source cannot see kit seats: flagged, but unconfirmed.
	r := held()
	checkHolders(t.Context(), &r, liveSnapshot("Kit-booted agents appear once ...", false))
	if len(r.NoLiveAgent) != 1 || !r.Tasks[0].Holder.Unconfirmed || !strings.Contains(r.Tasks[0].Holder.Why, "fallback") {
		t.Fatalf("fallback: %+v", r.Tasks[0].Holder)
	}
	// Nothing held: the live agents are not read at all.
	r = rollupOf(Task{ID: "CW-1", Status: "doing"})
	checkHolders(t.Context(), &r, func(context.Context) (SessionsResult, error) {
		t.Fatal("read the live agents for a roll-up with nothing held")
		return SessionsResult{}, nil
	})
}

// Through the module: the drill-in (ops_get), the board card, ops_list, the workstream feed
// and the global feed all carry the flag, read from Torque and Tether as the real tools answer.
func TestAssignedNoLiveAgentReachesBoardToolsAndFeeds(t *testing.T) {
	f := newTether()
	f.tasks["CW-20990101-0201"] = [3]string{"Given to a stopped worker", "todo", "2026-11-21T07:35:06Z"}
	f.tasks["CW-20990101-0202"] = [3]string{"Held by a live worker", "doing", "2026-11-21T08:00:00Z"}
	f.meta = map[string]map[string]any{
		"CW-20990101-0201": {"metadata": map[string]any{"owner_urn": stoppedAgent, "owner_name": "Worker 4"}},
		"CW-20990101-0202": {"assignment": map[string]any{"assignee_urn": liveAgent, "tether_session_id": "s1", "source": "doing", "state": "active"}},
	}
	f.live = js(map[string]any{"observed_at": "2026-11-21T14:00:00Z", "count": 1, "agents": []map[string]any{
		{"agent_urn": liveAgent, "kind": "tether_session", "session_id": "s1", "state": "busy"}}})
	h, _ := start(t, f)
	call(t, h, "ops_create", `{"id":"orphaned","title":"Orphaned","stage":"in_flight","links":[{"kind":"torque.task","id":"CW-20990101-0201"},{"kind":"torque.task","id":"CW-20990101-0202"}]}`)

	got := call(t, h, "ops_get", `{"id":"orphaned"}`)
	if js(got["no_live_agent"]) != `["CW-20990101-0201"]` {
		t.Fatalf("ops_get no_live_agent = %v", got["no_live_agent"])
	}
	list := js(got["tasks"].(map[string]any)["list"])
	if !strings.Contains(list, `"live":"none"`) || !strings.Contains(list, `"live":"live"`) || !strings.Contains(list, stoppedAgent) {
		t.Fatalf("drill-in tasks lack holders: %s", list)
	}
	if code, body := h.Do("GET", "/board", ""); code != 200 || !strings.Contains(body, `"no_live_agent":["CW-20990101-0201"]`) {
		t.Fatalf("board: %d %s", code, body)
	}
	if lanes := js(call(t, h, "ops_list", `{}`)); !strings.Contains(lanes, `"no_live_agent":["CW-20990101-0201"]`) {
		t.Fatalf("ops_list: %s", lanes)
	}
	act := js(call(t, h, "ops_activity", `{"id":"orphaned","source":"liveness"}`))
	if !strings.Contains(act, "CW-20990101-0201 is assigned to "+stoppedAgent) || strings.Contains(act, "CW-20990101-0202 is") {
		t.Fatalf("workstream feed: %s", act)
	}
	global := js(call(t, h, "ops_activity", `{"source":"liveness"}`))
	if !strings.Contains(global, `"source":"liveness"`) || !strings.Contains(global, "CW-20990101-0201") {
		t.Fatalf("global feed: %s", global)
	}
	if len(f.other) > 0 {
		t.Fatalf("called tools the fake does not have: %v", f.other)
	}
}
