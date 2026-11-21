package ops

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hollis-labs/surface/module/tetherlink"
)

type sessionFake struct {
	mu       sync.Mutex
	calls    map[string]int
	snapshot string
	failure  error
	pages    map[string]string
	health   map[string]string
}

func (f *sessionFake) Call(_ context.Context, tool string, args map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[tool]++
	if tool == "tether_agents_live" && args["include_stale"] == false {
		return f.snapshot, f.failure
	}
	if tool == "tether_session_list" {
		return f.pages[fmt.Sprint(args["state"])], nil
	}
	if tool == "tether_session_health" {
		return f.health[fmt.Sprint(args["session_id"])], nil
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

func TestMissingLiveToolFallsBackThenSwitchesAutomatically(t *testing.T) {
	f := sessionFixture()
	unified := f.snapshot
	f.failure = &tetherlink.RefusedError{Detail: "unknown tool: tether_agents_live"}
	f.pages = map[string]string{
		"running":  `{"sessions":[{"id":"a","logical_agent_id":"msg://agent/test/a","state":"running"},{"id":"b","state":"running"},{"id":"dead","state":"running"}]}`,
		"detached": `{"sessions":[{"id":"unknown","state":"detached"}]}`,
	}
	f.health = map[string]string{
		"a":    `{"alive":true,"live_state":"processing","last_activity_at":"2026-11-21T02:00:00Z"}`,
		"b":    `{"alive":true,"live_state":"idle"}`,
		"dead": `{"alive":false}`,
	}
	m := &mod{caller: f}
	out, err := m.sessions(t.Context())
	if err != nil || len(out.Sessions) != 3 || !strings.Contains(out.Warning, "Kit-booted agents appear once") {
		t.Fatalf("%+v %v", out, err)
	}
	byID := map[string]SessionRow{}
	for _, row := range out.Sessions {
		byID[row.ID] = row
	}
	if byID["a"].State != "awake" || byID["b"].State != "sleeping" || byID["unknown"].State != "unknown" {
		t.Fatalf("%+v", out)
	}
	if _, err = m.sessions(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.calls["tether_agents_live"] != 1 || f.calls["tether_session_list"] != 2 || f.calls["tether_session_health"] != 4 {
		t.Fatalf("%v", f.calls)
	}
	// A tool deployed between refreshes needs no config change or module restart.
	m.liveSessions.until = time.Time{}
	f.failure = nil
	f.snapshot = unified
	out, err = m.sessions(t.Context())
	if err != nil || out.Warning != "" || out.Sessions[0].Host != "host-a" {
		t.Fatalf("%+v %v", out, err)
	}
	if f.calls["tether_agents_live"] != 2 || f.calls["tether_session_list"] != 2 {
		t.Fatalf("%v", f.calls)
	}
}

func TestLiveToolFailureDoesNotHideBehindFallback(t *testing.T) {
	for _, err := range []error{context.DeadlineExceeded, &tetherlink.RefusedError{Detail: "permission denied for tether_agents_live"}, &tetherlink.RefusedError{Detail: "backend unavailable"}} {
		f := sessionFixture()
		f.failure = err
		m := &mod{caller: f}
		if _, got := m.sessions(t.Context()); got == nil {
			t.Fatal("hidden failure")
		}
		if f.calls["tether_session_list"] != 0 {
			t.Fatalf("fallback on %v", err)
		}
	}
}

func TestSessionMissingToolRecognition(t *testing.T) {
	for _, detail := range []string{`unknown tool: tether_agents_live`, `Tool not found: tether_agents_live`, `{"ok":false,"error":{"code":"tool_not_found"}}`, `{"ok":false,"error":"unknown tool: tether_agents_live"}`} {
		if !sessionToolMissing(detail, "tether_agents_live") {
			t.Fatalf("missing refusal: %s", detail)
		}
	}
	for _, detail := range []string{`tool not found: another_tool`, `permission denied`, `{"ok":false,"error":{"code":"not_found","message":"agent not found"}}`} {
		if sessionToolMissing(detail, "tether_agents_live") {
			t.Fatalf("misclassified: %s", detail)
		}
	}
}

func TestLegacySessionCallsAreBounded(t *testing.T) {
	f := sessionFixture()
	f.failure = &tetherlink.RefusedError{Detail: "unknown tool: tether_agents_live"}
	var rows []map[string]any
	for i := range 100 {
		rows = append(rows, map[string]any{"id": fmt.Sprint(i), "state": "running"})
	}
	page := js(map[string]any{"sessions": rows, "next_cursor": "next"})
	f.pages = map[string]string{"running": page, "detached": page}
	m := &mod{caller: f}
	out, err := m.sessions(t.Context())
	if err != nil || !out.Truncated || len(out.Sessions) != 25 {
		t.Fatalf("%+v %v", out, err)
	}
	if f.calls["tether_session_list"] != 2 || f.calls["tether_session_health"] != 25 {
		t.Fatalf("unbounded: %v", f.calls)
	}
}
