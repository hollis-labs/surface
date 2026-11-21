package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hollis-labs/surface/module/tetherlink"
)

// SessionRow is the shared Agents/Fleet view of an authoritative Tether observation.
// Unknown observations stay unknown: lifecycle state and age do not imply idleness.
type SessionRow struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	AgentID        string `json:"agent_id"`
	AgentName      string `json:"agent_name"`
	AgentRef       string `json:"agent_ref,omitempty"`
	Host           string `json:"host"`
	State          string `json:"state"`
	RawState       string `json:"raw_state"`
	LastActivityAt string `json:"last_activity_at,omitempty"`
	LastActivity   string `json:"last_activity,omitempty"`
	WorkstreamID   string `json:"workstream_id,omitempty"`
	Stale          bool   `json:"stale"`
	Warning        string `json:"warning,omitempty"`
}

type SessionsResult struct {
	Sessions  []SessionRow `json:"sessions"`
	FetchedAt string       `json:"fetched_at"`
	Truncated bool         `json:"truncated"`
	Warning   string       `json:"warning,omitempty"`
}

type liveSessionReader interface {
	Read(context.Context) (SessionsResult, error)
}

type sessionCache struct {
	mu     sync.Mutex
	result SessionsResult
	err    error
	until  time.Time
}

// sessions shares one bounded refresh across viewers, Fleet and the agent tool.
func (m *mod) sessions(ctx context.Context) (SessionsResult, error) {
	m.liveSessions.mu.Lock()
	defer m.liveSessions.mu.Unlock()
	if time.Now().Before(m.liveSessions.until) {
		return m.liveSessions.result, m.liveSessions.err
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	var source liveSessionReader = tetherSessionReader{link: m.tether}
	result, err := source.Read(ctx)
	// Cache failures too: unavailable Tether must not cause a request storm.
	m.liveSessions.result, m.liveSessions.err = result, err
	m.liveSessions.until = time.Now().Add(readBack)
	return result, err
}

// Interim adapter for Tether-launched sessions. A unified Tether live-agent read
// can replace this adapter without changing the Agents/Fleet row contract.
type tetherSessionReader struct {
	link func() (tetherlink.Caller, error)
}

const sessionLimit = 50

func (r tetherSessionReader) Read(ctx context.Context) (SessionsResult, error) {
	out := SessionsResult{Sessions: []SessionRow{}, FetchedAt: now(), Warning: "Tether-launched sessions only; kit-booted seats are not included yet. Host information is unavailable from this source."}
	c, err := r.link()
	if err != nil {
		return out, err
	}
	seen := map[string]bool{}
	for _, state := range []string{"running", "detached"} {
		var page struct {
			Sessions []struct {
				ID           string `json:"id"`
				AgentID      string `json:"logical_agent_id"`
				State        string `json:"state"`
				WorkstreamID string `json:"workstream_id"`
			} `json:"sessions"`
			NextCursor string `json:"next_cursor"`
		}
		if err = sessionCall(ctx, c, "tether_session_list", map[string]any{"state": state, "limit": sessionLimit / 2}, &page); err != nil {
			return out, err
		}
		out.Truncated = out.Truncated || page.NextCursor != "" || len(page.Sessions) > sessionLimit/2
		for i, s := range page.Sessions {
			if i >= sessionLimit/2 {
				break
			}
			if s.ID == "" || seen[s.ID] || (s.State != "running" && s.State != "detached") {
				continue
			}
			seen[s.ID] = true
			row := SessionRow{ID: s.ID, Kind: "session", AgentID: s.AgentID, AgentName: s.AgentID, State: "unknown", RawState: s.State, WorkstreamID: s.WorkstreamID, Stale: true}
			if row.AgentName == "" {
				row.AgentName = s.ID
			}
			if strings.HasPrefix(s.AgentID, "msg://") {
				row.AgentRef = s.AgentID
			}
			out.Sessions = append(out.Sessions, row)
		}
	}
	// At most two list calls and 50 health reads, four at once. No output peeks,
	// workspace paths or process metadata leave the adapter.
	var wg sync.WaitGroup
	jobs := make(chan int)
	for range 4 {
		wg.Go(func() {
			for i := range jobs {
				row := &out.Sessions[i]
				var health struct {
					Alive          *bool  `json:"alive"`
					LiveState      string `json:"live_state"`
					LastActivityAt string `json:"last_activity_at"`
				}
				callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				healthErr := sessionCall(callCtx, c, "tether_session_health", map[string]any{"session_id": row.ID}, &health)
				cancel()
				if healthErr != nil {
					row.Warning = "Live observation unavailable"
					continue
				}
				if health.Alive != nil && !*health.Alive {
					row.ID = ""
					continue
				}
				if health.LiveState != "" {
					row.RawState += " / " + health.LiveState
				}
				row.LastActivityAt = health.LastActivityAt
				if health.Alive != nil && *health.Alive {
					switch health.LiveState {
					case "idle":
						row.State, row.Stale = "sleeping", false
					case "busy", "processing":
						row.State, row.Stale = "awake", false
					}
				}
			}
		})
	}
	for i := range out.Sessions {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	rows := out.Sessions[:0]
	for _, row := range out.Sessions {
		if row.ID != "" {
			rows = append(rows, row)
		}
	}
	out.Sessions = rows
	sort.Slice(out.Sessions, func(i, j int) bool {
		if out.Sessions[i].AgentName == out.Sessions[j].AgentName {
			return out.Sessions[i].ID < out.Sessions[j].ID
		}
		return out.Sessions[i].AgentName < out.Sessions[j].AgentName
	})
	return out, nil
}

func sessionCall(ctx context.Context, c tetherlink.Caller, tool string, args map[string]any, into any) error {
	raw, err := c.Call(ctx, tool, args)
	if err != nil {
		return err
	}
	var envelope struct {
		OK *bool `json:"ok"`
	}
	if err = json.Unmarshal([]byte(raw), &envelope); err != nil {
		return err
	}
	if envelope.OK != nil && !*envelope.OK {
		return fmt.Errorf("%s unavailable", tool)
	}
	return json.Unmarshal([]byte(raw), into)
}
