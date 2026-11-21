package ops

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/hollis-labs/surface/module/tetherlink"
)

type sessionFake struct {
	mu     sync.Mutex
	calls  map[string]int
	pages  map[string]string
	health map[string]string
}

func (f *sessionFake) Call(_ context.Context, tool string, args map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[tool]++
	switch tool {
	case "tether_session_list":
		return f.pages[fmt.Sprint(args["state"])], nil
	case "tether_session_health":
		if raw, ok := f.health[fmt.Sprint(args["session_id"])]; ok {
			return raw, nil
		}
		return "", fmt.Errorf("unavailable")
	}
	return "", fmt.Errorf("unexpected tool: %s", tool)
}
func sessionFixture() *sessionFake {
	return &sessionFake{calls: map[string]int{}, pages: map[string]string{
		"running":  `{"ok":true,"sessions":[{"id":"a","logical_agent_id":"msg://agent/test/a","state":"running","updated_at":"2026-11-21T01:00:00Z","workstream_id":"ws"},{"id":"b","state":"running"},{"id":"dead","state":"running"},{"id":"finished","state":"completed"}]}`,
		"detached": `{"ok":true,"sessions":[{"id":"c","state":"detached"}]}`,
	}, health: map[string]string{
		"a":    `{"ok":true,"alive":true,"live_state":"processing","last_activity_at":"2026-11-21T02:00:00Z"}`,
		"b":    `{"ok":true,"alive":true,"live_state":"idle"}`,
		"dead": `{"ok":true,"alive":false}`,
	}}
}
func TestSessionsObserveActivityWithoutInferringIdle(t *testing.T) {
	f := sessionFixture()
	r := tetherSessionReader{link: func() (tetherlink.Caller, error) { return f, nil }}
	out, err := r.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Sessions) != 3 {
		t.Fatalf("%+v", out)
	}
	byID := map[string]SessionRow{}
	for _, row := range out.Sessions {
		byID[row.ID] = row
	}
	if a := byID["a"]; a.State != "awake" || a.Stale || a.AgentRef != "msg://agent/test/a" || a.LastActivityAt != "2026-11-21T02:00:00Z" || a.WorkstreamID != "ws" {
		t.Fatalf("awake=%+v", a)
	}
	if b := byID["b"]; b.State != "sleeping" || b.Stale || b.LastActivityAt != "" {
		t.Fatalf("idle=%+v", b)
	}
	if c := byID["c"]; c.State != "unknown" || !c.Stale || c.Host != "" || c.LastActivityAt != "" {
		t.Fatalf("unknown=%+v", c)
	}
}
func TestSessionReadsAreBoundedAndShared(t *testing.T) {
	f := sessionFixture()
	var rows []map[string]any
	for i := range 100 {
		rows = append(rows, map[string]any{"id": fmt.Sprint(i), "state": "running"})
	}
	f.pages["running"] = js(map[string]any{"sessions": rows, "next_cursor": "next"})
	m := &mod{caller: f}
	first, err := m.sessions(t.Context())
	if err != nil || !first.Truncated {
		t.Fatalf("%+v %v", first, err)
	}
	for range 3 {
		if _, err = m.sessions(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if f.calls["tether_session_list"] != 2 || f.calls["tether_session_health"] != 26 {
		t.Fatalf("unbounded reads: %v", f.calls)
	}
}
func TestSessionListFailureIsNotAnEmptyHealthyFleet(t *testing.T) {
	f := sessionFixture()
	f.pages["running"] = `{"ok":false,"error":"unavailable"}`
	m := &mod{caller: f}
	for range 2 {
		if _, err := m.sessions(t.Context()); err == nil {
			t.Fatal("hidden source failure")
		}
	}
	if f.calls["tether_session_list"] != 1 {
		t.Fatalf("failure not cached: %v", f.calls)
	}
}
