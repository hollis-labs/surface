package ops

import (
	"context"
	"fmt"
	"strings"
)

// Assigned, no live agent (gap G6 of the Ops system-of-record brief, Torque CW-20261121-0152).
//
// A workstream's open task that is assigned or claimed is checked against the live agents (the
// Agents view's reader: Tether's tether_agents_live, or the tether_session_list fallback). When
// nobody live holds it, the task is flagged in the roll-up, the board's card counts it, and the
// feed carries a row for it (source "liveness"). Work given to a worker that has stopped then
// shows at once instead of sitting unseen (CW-20261121-0132/0133 sat 6.5 h).
//
// The check never guesses: when the live agents cannot be read, or the read was truncated, a
// task is marked unknown and not flagged. On the fallback source, which cannot see kit-booted
// agents, a flag is marked unconfirmed.

// Holder says who a task is assigned to or claimed by, and whether they are live.
type Holder struct {
	// Agent is the assignee or claimant: an agent urn (msg://agent/...).
	Agent string `json:"agent,omitempty"`
	// Session and Run name a claim's holder when Torque records one.
	Session string `json:"session,omitempty"`
	Run     string `json:"run,omitempty"`
	// Source is how it was assigned: Torque's owner, doing, claim or dispatch.
	Source string `json:"source"`
	// Live is "live", "none" (nobody live holds it) or "unknown" (the check could not tell).
	Live string `json:"live"`
	// Why says what was found, for the drill-in and the agent.
	Why string `json:"why,omitempty"`
	// Unconfirmed is set when the fallback source, which cannot see kit-booted agents, found none.
	Unconfirmed bool `json:"unconfirmed,omitempty"`
}

// Liveness of a task's holder.
const (
	HolderLive    = "live"
	HolderNone    = "none"
	HolderUnknown = "unknown"
)

// SourceLiveness is the feed source for "assigned, no live agent" rows.
const SourceLiveness = "liveness"

// taskAssignment is what the typed task record says about who holds the task. The field names
// are Torque's (CW-20261121-0148, from the Torque Architect) and are read only here.
type taskAssignment struct {
	// Assignment is Torque's first-class assignment, null when the task was never assigned.
	// It stays after expiry (state expired, with an end reason).
	Assignment *struct {
		AssigneeURN     string `json:"assignee_urn"`
		AssigneeName    string `json:"assignee_name"`
		TetherSessionID string `json:"tether_session_id"`
		TeamRunID       string `json:"team_run_id"`
		ClaimID         string `json:"claim_id"`
		Source          string `json:"source"` // owner | doing | claim | dispatch
		State           string `json:"state"`  // active | expired | released
		EndReason       string `json:"end_reason"`
	} `json:"assignment"`
	// Metadata.owner_urn is the plain assignment until 0148 ships; after it, Torque projects
	// it into Assignment (source owner).
	Metadata struct {
		OwnerURN  string `json:"owner_urn"`
		OwnerName string `json:"owner_name"`
	} `json:"metadata"`
}

// assignment is a task's holder from its typed record, nil when it is not assigned (or the
// assignment was released). An expired assignment is a holder nobody live holds.
func (a taskAssignment) assignment() *Holder {
	if x := a.Assignment; x != nil {
		if strings.EqualFold(x.State, "released") {
			return nil
		}
		source := x.Source
		if source == "" {
			source = "owner"
		}
		h := &Holder{Agent: firstAgent(x.AssigneeURN), Session: strings.TrimSpace(x.TetherSessionID), Run: strings.TrimSpace(x.TeamRunID), Source: source}
		if strings.EqualFold(x.State, "expired") {
			h.Live, h.Why = HolderNone, "the assignment expired"
			if x.EndReason != "" {
				h.Why += " (" + strings.ReplaceAll(x.EndReason, "_", " ") + ")"
			}
		}
		if h.Agent != "" || h.Session != "" || h.Run != "" {
			return h
		}
	}
	if u := strings.TrimSpace(a.Metadata.OwnerURN); strings.HasPrefix(u, "msg://agent/") {
		return &Holder{Agent: u, Source: "owner"}
	}
	return nil
}

func firstAgent(urns ...string) string {
	for _, u := range urns {
		if u = strings.TrimSpace(u); strings.HasPrefix(u, "msg://agent/") {
			return u
		}
	}
	return ""
}

// checkHolders marks the open, held tasks of a roll-up against the live agents and adds a feed
// row for each that nobody live holds. live is read only when a task needs it.
func checkHolders(ctx context.Context, r *Rollup, live func(context.Context) (SessionsResult, error)) {
	var held []int
	for i, t := range r.Tasks {
		if t.Holder != nil && !Finished(t.Status) {
			held = append(held, i)
		}
	}
	if len(held) == 0 {
		return
	}
	snap, err := live(ctx)
	unknown := ""
	switch {
	case err != nil:
		unknown = "the live agents could not be read"
	case snap.Truncated:
		unknown = "the live agents list was truncated"
	}
	agents, sessions := map[string]bool{}, map[string]bool{}
	for _, s := range snap.Sessions {
		agents[s.AgentID] = true
		sessions[s.ID] = true
	}
	fallback := snap.Warning != ""
	at := snap.FetchedAt
	if at == "" {
		at = now()
	}
	// The newest entry stays what it was: a liveness row is a standing flag, not activity.
	if r.Last != nil {
		last := *r.Last
		r.Last = &last
	}
	for _, i := range held {
		t := &r.Tasks[i]
		h := *t.Holder
		switch {
		case h.Live == HolderNone: // the record already says so (an expired claim)
		case holderLive(h, agents, sessions):
			h.Live, h.Why = HolderLive, ""
		case unknown != "":
			h.Live, h.Why = HolderUnknown, unknown
		default:
			h.Live, h.Why, h.Unconfirmed = HolderNone, "no live agent holds it", fallback
			if fallback {
				h.Why += " (fallback source: kit-booted agents are not visible)"
			}
		}
		t.Holder = &h
		if h.Live != HolderNone {
			continue
		}
		r.NoLiveAgent = append(r.NoLiveAgent, t.ID)
		who := h.Agent
		if who == "" {
			who = firstNonEmpty(h.Session, h.Run)
		}
		r.Entries = append(r.Entries, Entry{At: at, Source: SourceLiveness, State: "no_live_agent", Task: t.ID, Workstream: t.Workstream,
			Summary: fmt.Sprintf("%s is %s %s, but %s", t.ID, verb(h.Source), who, h.Why),
			Refs:    []string{t.ID}, SubjectURN: h.Agent})
	}
	if len(r.NoLiveAgent) > 0 {
		r.Entries = sortEntries(r.Entries)
	}
}

// holderLive matches a holder against the live agents. A bound session must itself be live (the
// agent may have another session); without one, the agent or its team run must be.
func holderLive(h Holder, agents, sessions map[string]bool) bool {
	if h.Session != "" {
		return sessions["session:"+h.Session]
	}
	return h.Agent != "" && agents[h.Agent] || h.Run != "" && sessions["seat:"+h.Run]
}

func verb(source string) string {
	if source == "claim" {
		return "claimed by"
	}
	return "assigned to"
}
