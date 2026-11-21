package ops

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var briefNow = time.Date(2026, 11, 21, 15, 0, 0, 0, time.UTC)

func ago(d time.Duration) string { return briefNow.Add(-d).Format(time.RFC3339) }

func ids(s BriefSection) []string {
	out := []string{}
	for _, it := range s.Items {
		out = append(out, it.ID)
	}
	return out
}

func has(s BriefSection, id string) bool {
	for _, it := range s.Items {
		if it.ID == id {
			return true
		}
	}
	return false
}

func briefFixture() briefInput {
	ws := []Workstream{
		{ID: "held", Title: "Held", Stage: "queued", Owner: "lo", Held: &Held{Reason: "waiting on Torque 0146", Since: ago(3 * time.Hour)}},
		{ID: "asked", Title: "Asked", Stage: "planned", OrchestrationRequest: &OrchestrationRequest{At: ago(time.Hour), Note: "please sequence", MessageID: "m1"}},
		{ID: "queued-ask", Title: "Sequenced", Stage: "queued", OrchestrationRequest: &OrchestrationRequest{At: ago(time.Hour), MessageID: "m2"}},
		{ID: "quiet", Title: "Quiet", Stage: "in_flight", Owner: "lo", StageSince: ago(5 * time.Hour),
			Transitions: []Transition{{From: "queued", To: "in_flight", At: ago(5 * time.Hour)}}},
		{ID: "busy", Title: "Busy", Stage: "in_flight", Owner: "other", Parent: "quiet",
			Correlation: Correlation{TetherWorkstreams: []string{"workstream:01X"}},
			Transitions: []Transition{{From: "queued", To: "in_flight", At: ago(2 * time.Hour), Why: "go"}}},
		{ID: "landing", Title: "Landing", Stage: "landing"},
		{ID: "unread", Title: "Unread", Stage: "in_flight"},
		{ID: "settled", Title: "Settled", Stage: "complete", SettledAt: ago(time.Hour), Held: &Held{Reason: "old"}},
	}
	rollups := map[string]Rollup{
		"quiet": {Workstream: "quiet", FetchedAt: ago(time.Minute), Last: &Entry{At: ago(46 * time.Minute), Source: "torque", Summary: "CW-1 doing", Task: "CW-1"},
			Counts: map[string]int{"doing": 1}, Tasks: []Task{{ID: "CW-1", Title: "Work", Status: "doing"}}},
		"busy": {Workstream: "busy", FetchedAt: ago(time.Minute), Last: &Entry{At: ago(5 * time.Minute), Source: "status", Summary: "milestone", Refs: []string{"tesseract:1"}},
			Counts: map[string]int{"blocked": 1, "doing": 1}, Suggest: "landing", SuggestWhy: "most tasks done",
			Tasks:   []Task{{ID: "CW-2", Status: "blocked"}, {ID: "CW-3", Title: "Three", Status: "doing"}},
			Entries: []Entry{{At: ago(5 * time.Minute), Source: "status", Summary: "milestone", Refs: []string{"tesseract:1"}}, {At: ago(30 * time.Hour), Source: "torque", Summary: "old"}, {At: ago(time.Hour), Source: "ops", Summary: "mirror of a move"}}},
		"landing": {Workstream: "landing", FetchedAt: ago(time.Minute)},
	}
	return briefInput{Now: briefNow, Since: briefNow.Add(-24 * time.Hour), Scope: "all", Max: 40,
		Board:    Board{Workstreams: ws},
		Rollups:  rollups,
		Sessions: &SessionsResult{Sessions: []SessionRow{{AgentName: "w1", Host: "box", State: "awake", WorkstreamID: "workstream:01X"}, {AgentName: "w2", State: "sleeping"}, {AgentName: "w3", Host: "box", State: "awake", TaskID: "CW-1"}}},
		Hosts:    &FleetResult{Hosts: []FleetHost{{Name: "box", State: "online"}, {Name: "laptop", State: "offline"}}},
		Usage:    &UsageResult{Cards: []UsageCard{{ID: "tether-ai", Title: "Tether AI router", Status: usageWarning, Gap: "budget broken"}, {ID: "x", Status: usageOK}, {ID: "y", Status: usageGap}}},
		Sources:  []BriefSource{{Name: "ops items", AsOf: ago(10 * time.Minute), Stale: true}, {Name: "agents", Problem: "offline"}},
	}
}

func TestBriefSections(t *testing.T) {
	b := buildBrief(briefFixture())
	// needs_me: hold, an unsequenced request, blocked tasks; settled items never show.
	if !has(b.NeedsMe, "held") || !has(b.NeedsMe, "asked") || !has(b.NeedsMe, "busy") || has(b.NeedsMe, "queued-ask") || has(b.NeedsMe, "settled") {
		t.Fatalf("needs_me: %v", ids(b.NeedsMe))
	}
	for _, it := range b.NeedsMe.Items {
		switch it.ID {
		case "held":
			if !strings.Contains(it.Why, "waiting on Torque 0146") || it.Age != "3h00m" {
				t.Errorf("hold line: %+v", it)
			}
		case "asked":
			if !strings.Contains(it.Why, "please sequence") || it.Refs[len(it.Refs)-1] != "tether.message:m1" {
				t.Errorf("request line: %+v", it)
			}
		case "busy":
			if !strings.Contains(it.Why, "CW-2") {
				t.Errorf("blocked line: %+v", it)
			}
		}
	}
	// stalled: quiet for 46 minutes, and in flight with activity never read; busy is not.
	if got := ids(b.Stalled); len(got) != 2 || !has(b.Stalled, "quiet") || !has(b.Stalled, "unread") {
		t.Fatalf("stalled: %v", got)
	}
	// changed_since: busy's move and status entry, newest item first, and quiet's move; the
	// 30h-old entry and Ops' mirror rows are not.
	if got := ids(b.ChangedSince); len(got) != 2 || got[0] != "busy" || got[1] != "quiet" {
		t.Fatalf("changed: %v", got)
	}
	ch := b.ChangedSince.Items[0].Changes
	if len(ch) != 2 || ch[0].Kind != "status" || ch[0].Ref != "tesseract:1" || ch[1].Kind != "transition" || !strings.Contains(ch[1].Summary, "go") {
		t.Fatalf("changes: %+v", ch)
	}
	if got := ids(b.Suggestions); len(got) != 1 || got[0] != "busy" {
		t.Fatalf("suggestions: %v", got)
	}
	// in_flight: in flight and landing, with doing tasks and agents joined by correlation.
	if got := ids(b.InFlight); len(got) != 4 {
		t.Fatalf("in flight: %v", got)
	}
	for _, it := range b.InFlight.Items {
		if it.ID == "busy" && (len(it.Agents) != 1 || it.Agents[0].Host != "box" || len(it.Doing) != 1 || !strings.HasPrefix(it.Doing[0], "CW-3 doing")) {
			t.Errorf("busy in flight: %+v", it)
		}
	}
	for _, it := range b.InFlight.Items {
		if it.ID == "quiet" && (len(it.Agents) != 1 || it.Agents[0].Name != "w3") {
			t.Errorf("agents joined by task: %+v", it)
		}
	}
	if b.Fleet.Agents["awake"] != 2 || b.Fleet.ByHost["unknown"] != 1 || len(b.Fleet.Down) != 1 || !strings.Contains(b.Fleet.Down[0], "laptop") {
		t.Fatalf("fleet: %+v", b.Fleet)
	}
	if len(b.Budgets) != 1 || b.Budgets[0].Line != "budget broken" {
		t.Fatalf("budgets: %+v", b.Budgets)
	}
	// Stale and missing sources lead the brief; gaps are named, never silent.
	if len(b.Warnings) != 2 || !strings.HasPrefix(b.Markdown, "# Ops brief") || !strings.Contains(b.Markdown, "WARNING: agents unavailable: offline") {
		t.Fatalf("warnings: %v\n%s", b.Warnings, b.Markdown)
	}
	if len(b.Gaps) == 0 || !strings.Contains(b.Markdown, "## Not covered yet") || b.Untracked.Items == nil {
		t.Fatal("gaps must be listed")
	}
	if _, err := json.Marshal(b); err != nil {
		t.Fatal(err)
	}
}

func TestBriefScopeAndBounds(t *testing.T) {
	in := briefFixture()
	in.Scope = "workstream:quiet"
	b := buildBrief(in)
	if got := ids(b.InFlight); len(got) != 2 || !has(b.InFlight, "busy") {
		t.Fatalf("workstream scope keeps descendants: %v", got)
	}
	in.Scope = "owner:lo"
	if got := ids(buildBrief(in).NeedsMe); len(got) != 1 || got[0] != "held" {
		t.Fatalf("owner scope: %v", got)
	}
	in = briefFixture()
	in.Max = 1
	b = buildBrief(in)
	if len(b.NeedsMe.Items) != 1 || b.NeedsMe.Truncated != 2 || !strings.Contains(b.Markdown, "… 2 more") {
		t.Fatalf("bounds: %+v", b.NeedsMe)
	}
	// The oldest need comes first.
	if b.NeedsMe.Items[0].ID != "held" {
		t.Fatalf("order: %v", ids(b.NeedsMe))
	}
}

func TestBriefUnknownObservations(t *testing.T) {
	in := briefFixture()
	in.Sessions, in.Hosts, in.Usage = nil, nil, nil
	b := buildBrief(in)
	if len(b.Fleet.Warnings) < 2 || b.Budgets == nil {
		t.Fatalf("unknown fleet must be said: %+v", b.Fleet)
	}
}

func TestBriefThroughTheTool(t *testing.T) {
	f := newTether()
	h, _ := start(t, f)
	call(t, h, "ops_create", `{"id":"one","title":"One","stage":"in_flight"}`)
	raw, err := h.Tool("ops_brief", `{"max_items":5}`)
	if err != nil {
		t.Fatal(err)
	}
	b, ok := raw.(Brief)
	if !ok {
		t.Fatalf("result %T", raw)
	}
	// Just created: in flight, changed, not yet stalled; unread sources are warned about.
	if !has(b.InFlight, "one") || has(b.Stalled, "one") || !has(b.ChangedSince, "one") || len(b.Sources) != 5 || len(b.Warnings) == 0 {
		t.Fatalf("brief: %+v", b)
	}
	first := When(b.Since)
	if briefNow := When(b.GeneratedAt); briefNow.Sub(first) < 23*time.Hour {
		t.Fatalf("first read covers 24h: %s", b.Since)
	}
	raw, _ = h.Tool("ops_brief", `{}`)
	b = raw.(Brief)
	if When(b.GeneratedAt).Sub(When(b.Since)) > time.Minute {
		t.Fatalf("second read starts at the first: %s", b.Since)
	}
	if _, err := h.Tool("ops_brief", `{"scope":"bogus"}`); err == nil {
		t.Fatal("bad scope accepted")
	}
}
