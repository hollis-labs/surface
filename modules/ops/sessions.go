package ops

import (
	"context"
	"encoding/json"
	"errors"
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
	TaskID         string `json:"task_id,omitempty"`
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

// Tether owns liveness and includes both launched sessions and kit seats. Keep
// the source boundary here so Agents and Fleet consume the same observation.
type tetherSessionReader struct {
	link func() (tetherlink.Caller, error)
}

const sessionLimit = 500

func (r tetherSessionReader) Read(ctx context.Context) (SessionsResult, error) {
	out := SessionsResult{Sessions: []SessionRow{}}
	c, err := r.link()
	if err != nil {
		return out, err
	}
	var snapshot struct {
		Agents *[]struct {
			AgentURN    string `json:"agent_urn"`
			DisplayName string `json:"display_name"`
			// Agent is the current snapshot's identity; agent_urn and display_name are
			// the earlier flat shape, still accepted.
			Agent *struct {
				URN  string `json:"urn"`
				Name string `json:"name"`
			} `json:"agent"`
			Kind           string `json:"kind"`
			Host           string `json:"host"`
			State          string `json:"state"`
			RawState       string `json:"raw_state"`
			LastActivityAt string `json:"last_activity_at"`
			Summary        string `json:"summary"`
			SessionID      string `json:"session_id"`
			RunID          string `json:"run_id"`
			WorkstreamID   string `json:"workstream_id"`
			TaskID         string `json:"task_id"`
			Stale          bool   `json:"stale"`
		} `json:"agents"`
		ObservedAt string `json:"observed_at"`
		Count      int    `json:"count"`
	}
	if err = sessionCall(ctx, c, "tether_agents_live", map[string]any{"include_stale": false}, &snapshot); err != nil {
		if errors.Is(err, errSessionToolMissing) {
			return (legacySessionReader{link: func() (tetherlink.Caller, error) { return c, nil }}).Read(ctx)
		}
		return out, fmt.Errorf("live agent observations unavailable")
	}
	if snapshot.Agents == nil || snapshot.ObservedAt == "" {
		return out, fmt.Errorf("live agent observation is incomplete")
	}
	out.FetchedAt = snapshot.ObservedAt
	out.Truncated = snapshot.Count > len(*snapshot.Agents) || len(*snapshot.Agents) > sessionLimit
	seen := map[string]bool{}
	for i, s := range *snapshot.Agents {
		if i >= sessionLimit {
			break
		}
		// The view promises live agents. Do not reintroduce stale registrations if
		// a source accidentally includes them despite include_stale=false.
		if s.Stale {
			continue
		}
		if s.Agent != nil {
			if s.AgentURN == "" {
				s.AgentURN = s.Agent.URN
			}
			if s.DisplayName == "" {
				s.DisplayName = s.Agent.Name
			}
		}
		id := s.SessionID
		kind := "session"
		if s.Kind == "kit_seat" {
			kind = "seat"
			id = s.RunID
		}
		if id == "" {
			id = s.AgentURN
		}
		id = kind + ":" + id
		if s.AgentURN == "" || seen[id] {
			continue
		}
		seen[id] = true
		raw := s.State
		if s.RawState != "" {
			raw = s.RawState
		}
		row := SessionRow{ID: id, Kind: kind, AgentID: s.AgentURN, AgentName: s.DisplayName, Host: s.Host,
			State: "unknown", RawState: raw, LastActivityAt: s.LastActivityAt, LastActivity: s.Summary, WorkstreamID: s.WorkstreamID, TaskID: s.TaskID}
		if row.AgentName == "" {
			row.AgentName = s.AgentURN
		}
		if strings.HasPrefix(s.AgentURN, "msg://") {
			row.AgentRef = s.AgentURN
		}
		// The current snapshot states liveness itself (awake, sleeping); the earlier one
		// gave the runtime state (busy, idle).
		switch s.State {
		case "awake", "busy":
			row.State = "awake"
		case "sleeping", "idle":
			row.State = "sleeping"
		default:
			row.Warning = "Live state unavailable"
		}
		out.Sessions = append(out.Sessions, row)
	}
	sort.Slice(out.Sessions, func(i, j int) bool {
		if out.Sessions[i].AgentName == out.Sessions[j].AgentName {
			return out.Sessions[i].ID < out.Sessions[j].ID
		}
		return out.Sessions[i].AgentName < out.Sessions[j].AgentName
	})
	return out, nil
}

var errSessionToolMissing = errors.New("live agents tool is not deployed")

// Recognize explicit missing-tool refusals only. Authentication, timeout and
// source failures must stay visible instead of silently narrowing the fleet.
func sessionToolMissing(detail, tool string) bool {
	text := strings.ToLower(detail)
	var problem struct {
		Code  string          `json:"code"`
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal([]byte(detail), &problem) == nil {
		if problem.Code == "tool_not_found" || problem.Code == "unknown_tool" {
			return true
		}
		if len(problem.Error) > 0 && string(problem.Error) != "null" {
			var message string
			if json.Unmarshal(problem.Error, &message) == nil {
				return sessionToolMissing(message, tool)
			}
			return sessionToolMissing(string(problem.Error), tool)
		}
	}
	return strings.Contains(text, strings.ToLower(tool)) &&
		(strings.Contains(text, "unknown tool") || strings.Contains(text, "tool not found") ||
			strings.Contains(text, "tool '"+strings.ToLower(tool)+"' not found"))
}

func sessionCall(ctx context.Context, c tetherlink.Caller, tool string, args map[string]any, into any) error {
	raw, err := c.Call(ctx, tool, args)
	if err != nil {
		var refused *tetherlink.RefusedError
		if errors.As(err, &refused) && sessionToolMissing(refused.Detail, tool) {
			return errSessionToolMissing
		}
		return err
	}
	var envelope struct {
		OK *bool `json:"ok"`
	}
	if err = json.Unmarshal([]byte(raw), &envelope); err != nil {
		return err
	}
	if envelope.OK != nil && !*envelope.OK {
		if sessionToolMissing(raw, tool) {
			return errSessionToolMissing
		}
		return fmt.Errorf("%s unavailable", tool)
	}
	return json.Unmarshal([]byte(raw), into)
}
