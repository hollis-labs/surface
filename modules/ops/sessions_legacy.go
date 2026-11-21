package ops

import (
	"context"
	"github.com/hollis-labs/surface/module/tetherlink"
	"sort"
	"strings"
	"sync"
	"time"
)

// Interim adapter for Tether-launched sessions. A unified Tether live-agent read
// can replace this adapter without changing the Agents/Fleet row contract.
type legacySessionReader struct {
	link func() (tetherlink.Caller, error)
}

const legacySessionLimit = 50

func (r legacySessionReader) Read(ctx context.Context) (SessionsResult, error) {
	out := SessionsResult{Sessions: []SessionRow{}, FetchedAt: now(), Warning: "Kit-booted agents appear once Tether's live-agents feed is deployed. Host information is unavailable from this source."}
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
		if err = sessionCall(ctx, c, "tether_session_list", map[string]any{"state": state, "limit": legacySessionLimit / 2}, &page); err != nil {
			return out, err
		}
		out.Truncated = out.Truncated || page.NextCursor != "" || len(page.Sessions) > legacySessionLimit/2
		for i, s := range page.Sessions {
			if i >= legacySessionLimit/2 {
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
