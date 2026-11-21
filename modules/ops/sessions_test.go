package ops

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/hollis-labs/surface/module/tetherlink"
)

type sessionFake struct {
	mu       sync.Mutex
	calls    map[string]int
	snapshot string
}

func (f *sessionFake) Call(_ context.Context, tool string, args map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[tool]++
	if tool == "tether_agents_live" && args["include_stale"] == false {
		return f.snapshot, nil
	}
	return "", fmt.Errorf("unexpected tool: %s", tool)
}
func sessionFixture() *sessionFake {
	return &sessionFake{calls: map[string]int{}, snapshot: `{"observed_at":"2026-11-21T02:01:00Z","count":4,"agents":[
 {"agent_urn":"msg://agent/test/a","display_name":"Agent A","kind":"tether_session","host":"host-a","session_id":"a","state":"busy","last_activity_at":"2026-11-21T02:00:00Z","summary":"Running checks","workstream_id":"ws"},
 {"agent_urn":"msg://agent/test/b","kind":"kit_seat","host":"host-b","run_id":"b","state":"idle","last_activity_at":null},
 {"agent_urn":"msg://agent/test/c","kind":"kit_seat","run_id":"c","state":"unknown","last_activity_at":null},
 {"agent_urn":"msg://agent/test/dead","kind":"kit_seat","run_id":"dead","state":"idle","stale":true}]}`}
}
func TestSessionsObserveActivityWithoutInferringIdle(t *testing.T) {
	f := sessionFixture()
	r := tetherSessionReader{link: func() (tetherlink.Caller, error) { return f, nil }}
	out, err := r.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Sessions) != 3 || out.FetchedAt != "2026-11-21T02:01:00Z" {
		t.Fatalf("%+v", out)
	}
	byID := map[string]SessionRow{}
	for _, row := range out.Sessions {
		byID[row.ID] = row
	}
	if a := byID["session:a"]; a.State != "awake" || a.AgentName != "Agent A" || a.Host != "host-a" || a.AgentRef != "msg://agent/test/a" || a.LastActivityAt != "2026-11-21T02:00:00Z" || a.LastActivity != "Running checks" || a.WorkstreamID != "ws" {
		t.Fatalf("awake=%+v", a)
	}
	if b := byID["seat:b"]; b.State != "sleeping" || b.Host != "host-b" || b.LastActivityAt != "" {
		t.Fatalf("idle=%+v", b)
	}
	if c := byID["seat:c"]; c.State != "unknown" || c.Host != "" || c.LastActivityAt != "" || c.Warning == "" {
		t.Fatalf("unknown=%+v", c)
	}
}
func TestSessionReadsAreBoundedAndShared(t *testing.T) {
	f := sessionFixture()
	var rows []map[string]any
	for i := range sessionLimit + 1 {
		rows = append(rows, map[string]any{"agent_urn": fmt.Sprintf("msg://agent/test/%d", i), "state": "busy"})
	}
	f.snapshot = js(map[string]any{"agents": rows, "observed_at": "2026-11-21T02:01:00Z"})
	m := &mod{caller: f}
	first, err := m.sessions(t.Context())
	if err != nil || !first.Truncated || len(first.Sessions) != sessionLimit {
		t.Fatalf("%+v %v", first, err)
	}
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if _, err := m.sessions(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if f.calls["tether_agents_live"] != 1 {
		t.Fatalf("unbounded reads: %v", f.calls)
	}
}
func TestSessionListFailureIsNotAnEmptyHealthyFleet(t *testing.T) {
	for _, raw := range []string{`{"ok":false,"error":"unavailable"}`, `{}`, `{"agents":[],"observed_at":""}`, `not json`} {
		t.Run(raw, func(t *testing.T) {
			f := sessionFixture()
			f.snapshot = raw
			m := &mod{caller: f}
			for range 2 {
				if _, err := m.sessions(t.Context()); err == nil {
					t.Fatal("hidden source failure")
				}
			}
			if f.calls["tether_agents_live"] != 1 {
				t.Fatalf("failure not cached: %v", f.calls)
			}
		})
	}
}
func TestEmptyLiveAgentsIsAHealthyObservation(t *testing.T) {
	f := sessionFixture()
	f.snapshot = `{"agents":[],"count":0,"observed_at":"2026-11-21T02:01:00Z"}`
	m := &mod{caller: f}
	result, err := m.sessions(t.Context())
	if err != nil || result.Sessions == nil || len(result.Sessions) != 0 {
		t.Fatalf("%+v %v", result, err)
	}
}
