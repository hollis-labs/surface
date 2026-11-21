package ops

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hollis-labs/surface/module"
)

// ops_brief is the Lead Orchestrator's running brief (caster://ops/spec.orchestrator-brief@1):
// one read that says what needs a decision, what is stalled, what changed, what is in flight,
// the fleet and budgets, built only from data Ops already holds. Nothing here is hand-kept.
//
// The seam: buildBrief is a pure function of briefInput. Today gatherBrief fills it from the
// ops/v1 copy, the cached roll-ups, the Agents/Fleet observations and Usage; when ops/v2
// lands (derived, suggestions, holds with until, intake.needs_triage) only the gatherer
// changes. Sections v1 cannot fill are named in Gaps, never silently empty.

const (
	briefDefaultSince = 24 * time.Hour
	briefDefaultMax   = 40
	briefMaxMax       = 200
	// briefQuiet is how long an in-flight item may go without activity before it is stalled.
	briefQuiet = 45 * time.Minute
	// briefStale is the age past which a source is reported stale in the header.
	briefStale = 5 * time.Minute
)

// BriefItem is one line of a section: an item, why it is there, and refs to its sources.
type BriefItem struct {
	ID     string   `json:"id"`
	Ref    string   `json:"ref"`
	Title  string   `json:"title"`
	Stage  string   `json:"stage"`
	Owner  string   `json:"owner,omitempty"`
	Why    string   `json:"why,omitempty"`
	Latest string   `json:"latest,omitempty"`
	At     string   `json:"at,omitempty"`
	Age    string   `json:"age,omitempty"`
	Refs   []string `json:"refs,omitempty"`
	// In flight only.
	Counts map[string]int `json:"counts,omitempty"`
	Doing  []string       `json:"doing,omitempty"`
	Agents []BriefAgent   `json:"agents,omitempty"`
	// Changed only: the moves and activity since the window opened, newest first.
	Changes []BriefChange `json:"changes,omitempty"`
}

type BriefAgent struct {
	Name  string `json:"name"`
	Host  string `json:"host,omitempty"`
	State string `json:"state"`
}

type BriefChange struct {
	At      string `json:"at"`
	Kind    string `json:"kind"` // transition or the activity source (torque, comment, status)
	Summary string `json:"summary"`
	Ref     string `json:"ref,omitempty"`
}

type BriefSection struct {
	Items     []BriefItem `json:"items"`
	Truncated int         `json:"truncated,omitempty"` // items left out by max_items
}

type BriefFleet struct {
	Agents   map[string]int `json:"agents"` // by state
	ByHost   map[string]int `json:"by_host"`
	Down     []string       `json:"down"` // machines not online, agents not awake
	Warnings []string       `json:"warnings,omitempty"`
}

type BriefBudget struct {
	Source string `json:"source"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Line   string `json:"line"`
}

type BriefSource struct {
	Name    string `json:"name"`
	AsOf    string `json:"as_of,omitempty"`
	Stale   bool   `json:"stale"`
	Problem string `json:"problem,omitempty"`
}

type Brief struct {
	GeneratedAt  string        `json:"generated_at"`
	Since        string        `json:"since"`
	Scope        string        `json:"scope"`
	Sources      []BriefSource `json:"sources"`
	Warnings     []string      `json:"warnings,omitempty"`
	NeedsMe      BriefSection  `json:"needs_me"`
	Stalled      BriefSection  `json:"stalled"`
	ChangedSince BriefSection  `json:"changed_since"`
	Suggestions  BriefSection  `json:"suggestions"`
	InFlight     BriefSection  `json:"in_flight"`
	Untracked    BriefSection  `json:"untracked"`
	Fleet        BriefFleet    `json:"fleet"`
	Budgets      []BriefBudget `json:"budgets"`
	Gaps         []string      `json:"gaps"`
	Markdown     string        `json:"markdown"`
}

// briefInput is everything the brief is built from. Nil observations are unknown.
type briefInput struct {
	Now      time.Time
	Since    time.Time
	Scope    string
	Max      int
	Board    Board
	Rollups  map[string]Rollup
	Sessions *SessionsResult
	Hosts    *FleetResult
	Usage    *UsageResult
	Sources  []BriefSource
}

// briefGaps are the spec's lines ops/v1 data cannot answer yet, with what will answer them.
var briefGaps = []string{
	"needs_me: owner decisions awaiting the owner, and restart or cutover requests, are not recorded in Ops yet (ops/v2 owner gate and ops_ask).",
	"needs_me: expired holds need held.until (ops/v2); v1 holds have no expiry.",
	"stalled: tasks assigned to an agent that is not live need task assignees in Ops (G6, derived.orphaned_assignments).",
	"stalled: merged but not deployed needs deploy events with the item id (Cerberus 0155) and PR state (watcher 0149).",
	"stalled: unit runs flagged by the stall monitor are not read yet.",
	"changed_since: merges, releases and deploys need the watcher (0149); only stage moves and task and status activity are shown.",
	"suggestions: v1 suggestions are the task-count hints; watcher suggestions with their signals arrive with ops/v2.",
	"untracked: tasks, PRs and deploys without a workstream need ops_untracked (G1 triage); not computed yet.",
	"budgets: item budgets need ops/v2 budget and derived.spend; provider rate-limit events are not read yet.",
}

func buildBrief(in briefInput) Brief {
	b := Brief{GeneratedAt: in.Now.UTC().Format(time.RFC3339), Since: in.Since.UTC().Format(time.RFC3339), Scope: in.Scope,
		Sources: in.Sources, Budgets: []BriefBudget{}, Gaps: briefGaps}
	for _, s := range in.Sources {
		if s.Problem != "" {
			b.Warnings = append(b.Warnings, fmt.Sprintf("%s unavailable: %s", s.Name, s.Problem))
		} else if s.Stale {
			b.Warnings = append(b.Warnings, fmt.Sprintf("%s is stale (as of %s)", s.Name, s.AsOf))
		}
	}
	items := scoped(in.Board.Workstreams, in.Scope)
	var needs, stalled, changed, suggest, flight []BriefItem
	for _, w := range items {
		if w.SettledAt != "" || w.Stage == "dropped" {
			continue
		}
		r, hasRoll := in.Rollups[w.ID]
		base := briefBase(w, r, in.Now)
		if w.Held != nil {
			it := base
			it.Why = "held: " + w.Held.Reason
			it.At, it.Age = w.Held.Since, age(in.Now, w.Held.Since)
			needs = append(needs, it)
		}
		if o := w.OrchestrationRequest; o != nil && (w.Stage == "planned" || w.Stage == "parked") {
			it := base
			it.Why = "sent to orchestration, not yet sequenced: " + oneLine(o.Note, 160)
			it.At, it.Age = o.At, age(in.Now, o.At)
			it.Refs = append(it.Refs, "tether.message:"+o.MessageID)
			needs = append(needs, it)
		}
		if hasRoll {
			var blocked []string
			for _, t := range r.Tasks {
				if t.Status == "blocked" {
					blocked = append(blocked, t.ID)
				}
			}
			if len(blocked) > 0 {
				it := base
				it.Why = fmt.Sprintf("%d blocked task(s): %s", len(blocked), strings.Join(blocked, ", "))
				it.Refs = append(it.Refs, blocked...)
				needs = append(needs, it)
			}
			if r.Suggest != "" {
				it := base
				it.Why = fmt.Sprintf("tasks suggest %s: %s", r.Suggest, r.SuggestWhy)
				suggest = append(suggest, it)
			}
		}
		if w.Stage == "in_flight" {
			last := lastSeen(w, r)
			if last.IsZero() || in.Now.Sub(last) >= briefQuiet {
				it := base
				if !hasRoll {
					it.Why = "in flight; activity has not been read yet"
				} else if last.IsZero() {
					it.Why = "in flight with no recorded activity"
				} else {
					it.Why = fmt.Sprintf("in flight with no activity for %s", age(in.Now, last.Format(time.RFC3339)))
				}
				stalled = append(stalled, it)
			}
		}
		if ch := changes(w, r, in.Since); len(ch) > 0 {
			it := base
			it.Changes = ch
			it.Why = fmt.Sprintf("%d change(s) since %s", len(ch), b.Since)
			changed = append(changed, it)
		}
		if w.Stage == "in_flight" || w.Stage == "landing" {
			it := base
			if hasRoll {
				it.Counts = r.Counts
				for _, t := range r.Tasks {
					if t.Status == "doing" || t.Status == "review" {
						it.Doing = append(it.Doing, t.ID+" "+t.Status+": "+oneLine(t.Title, 80))
					}
				}
			}
			it.Agents = agentsFor(w, r, in.Sessions)
			flight = append(flight, it)
		}
	}
	byAge := func(s []BriefItem) []BriefItem {
		sort.SliceStable(s, func(i, j int) bool { return s[i].At < s[j].At })
		return s
	}
	b.NeedsMe = bounded(byAge(needs), in.Max)
	b.Stalled = bounded(byAge(stalled), in.Max)
	sort.SliceStable(changed, func(i, j int) bool { return changed[i].Changes[0].At > changed[j].Changes[0].At })
	b.ChangedSince = bounded(changed, in.Max)
	b.Suggestions = bounded(suggest, in.Max)
	b.InFlight = bounded(flight, in.Max)
	b.Untracked = BriefSection{Items: []BriefItem{}}
	b.Fleet = fleetSummary(in.Sessions, in.Hosts)
	if in.Usage != nil {
		for _, c := range in.Usage.Cards {
			switch c.Status {
			case usageWarning, usageExhausted, usageError:
				line := c.Summary
				if c.Gap != "" {
					line = c.Gap
				}
				b.Budgets = append(b.Budgets, BriefBudget{Source: c.ID, Title: c.Title, Status: c.Status, Line: oneLine(line, 240)})
			}
		}
	}
	b.Markdown = briefMarkdown(b)
	return b
}

// scoped is every item, an owner's, or one item and its descendants.
func scoped(all []Workstream, scope string) []Workstream {
	switch {
	case strings.HasPrefix(scope, "owner:"):
		owner := strings.TrimPrefix(scope, "owner:")
		var out []Workstream
		for _, w := range all {
			if w.Owner == owner {
				out = append(out, w)
			}
		}
		return out
	case strings.HasPrefix(scope, "workstream:"):
		root := strings.TrimPrefix(scope, "workstream:")
		keep := map[string]bool{root: true}
		for changed := true; changed; {
			changed = false
			for _, w := range all {
				if !keep[w.ID] && keep[w.Parent] {
					keep[w.ID], changed = true, true
				}
			}
		}
		var out []Workstream
		for _, w := range all {
			if keep[w.ID] {
				out = append(out, w)
			}
		}
		return out
	}
	return all
}

func briefBase(w Workstream, r Rollup, now time.Time) BriefItem {
	it := BriefItem{ID: w.ID, Ref: Ref(w.ID), Title: w.Title, Stage: w.Stage, Owner: w.Owner}
	if r.Last != nil {
		it.Latest = oneLine(r.Last.Summary, 160)
		it.At, it.Age = r.Last.At, age(now, r.Last.At)
		if r.Last.Task != "" {
			it.Refs = append(it.Refs, r.Last.Task)
		}
		it.Refs = append(it.Refs, r.Last.Refs...)
	} else if w.StageSince != "" {
		it.At, it.Age = w.StageSince, age(now, w.StageSince)
	}
	return it
}

// lastSeen is the newest of the item's activity and its own moves.
func lastSeen(w Workstream, r Rollup) time.Time {
	var last time.Time
	if r.Last != nil {
		last = When(r.Last.At)
	}
	for _, t := range w.Transitions {
		if at := When(t.At); at.After(last) {
			last = at
		}
	}
	return last
}

func changes(w Workstream, r Rollup, since time.Time) []BriefChange {
	var out []BriefChange
	for _, t := range w.Transitions {
		if When(t.At).After(since) {
			s := fmt.Sprintf("%s → %s", label(t.From), label(t.To))
			if t.From == "" {
				s = "created in " + label(t.To)
			}
			if t.Why != "" {
				s += ": " + oneLine(t.Why, 120)
			}
			out = append(out, BriefChange{At: t.At, Kind: "transition", Summary: s, Ref: Ref(w.ID)})
		}
	}
	for _, e := range r.Entries {
		if e.Source == "ops" || !When(e.At).After(since) {
			continue // Ops' own moves are the transitions above
		}
		ref := e.Task
		if ref == "" && len(e.Refs) > 0 {
			ref = e.Refs[0]
		}
		out = append(out, BriefChange{At: e.At, Kind: e.Source, Summary: oneLine(e.Summary, 160), Ref: ref})
	}
	sort.SliceStable(out, func(i, j int) bool { return When(out[i].At).After(When(out[j].At)) })
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

// agentsFor joins live sessions to an item by the item's id, its correlation, or a task in its
// roll-up; placement is never guessed.
func agentsFor(w Workstream, r Rollup, s *SessionsResult) []BriefAgent {
	if s == nil {
		return nil
	}
	ids := map[string]bool{w.ID: true}
	for _, t := range w.Correlation.TetherWorkstreams {
		ids[t] = true
	}
	for _, l := range w.Correlation.Labels {
		ids[l] = true
	}
	for _, l := range w.Links {
		if l.Kind == "registry.record" && l.Role == "identity" {
			ids[l.ID] = true
		}
	}
	tasks := map[string]bool{}
	for _, t := range r.Tasks {
		tasks[t.ID] = true
	}
	var out []BriefAgent
	for _, row := range s.Sessions {
		if row.WorkstreamID != "" && ids[row.WorkstreamID] || row.TaskID != "" && tasks[row.TaskID] {
			name := row.AgentName
			if name == "" {
				name = row.AgentID
			}
			out = append(out, BriefAgent{Name: name, Host: row.Host, State: row.State})
		}
	}
	return out
}

func fleetSummary(s *SessionsResult, h *FleetResult) BriefFleet {
	f := BriefFleet{Agents: map[string]int{}, ByHost: map[string]int{}, Down: []string{}}
	if s == nil {
		f.Warnings = append(f.Warnings, "Agent observations unavailable.")
	} else {
		for _, r := range s.Sessions {
			f.Agents[r.State]++
			host := r.Host
			if host == "" {
				host = "unknown"
			}
			f.ByHost[host]++
		}
		if s.Warning != "" {
			f.Warnings = append(f.Warnings, s.Warning)
		}
		if s.Truncated {
			f.Warnings = append(f.Warnings, "Agent observations are truncated.")
		}
	}
	if h == nil {
		f.Warnings = append(f.Warnings, "Machine health unavailable.")
	} else {
		for _, m := range h.Hosts {
			if m.State != "online" || m.Stale {
				state := m.State
				if m.Stale {
					state += ", stale"
				}
				f.Down = append(f.Down, fmt.Sprintf("machine %s: %s", m.Name, state))
			}
		}
	}
	f.Warnings = append(f.Warnings, "Unit worker pools and hand-started seats (G7) are not observed yet.")
	return f
}

func bounded(items []BriefItem, limit int) BriefSection {
	if items == nil {
		items = []BriefItem{}
	}
	if len(items) > limit {
		return BriefSection{Items: items[:limit], Truncated: len(items) - limit}
	}
	return BriefSection{Items: items}
}

func age(now time.Time, at string) string {
	t := When(at)
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func briefMarkdown(b Brief) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Ops brief, %s (since %s, scope %s)\n\n", b.GeneratedAt, b.Since, b.Scope)
	for _, w := range b.Warnings {
		fmt.Fprintf(&sb, "> WARNING: %s\n", w)
	}
	if len(b.Warnings) > 0 {
		sb.WriteString("\n")
	}
	section := func(title string, s BriefSection, detail func(BriefItem)) {
		fmt.Fprintf(&sb, "## %s (%d)\n", title, len(s.Items)+s.Truncated)
		if len(s.Items) == 0 {
			sb.WriteString("- none\n")
		}
		for _, it := range s.Items {
			fmt.Fprintf(&sb, "- %s %s [%s]", it.Ref, it.Title, label(it.Stage))
			if it.Why != "" {
				fmt.Fprintf(&sb, ": %s", it.Why)
			}
			switch it.Age {
			case "":
			case "just now":
				sb.WriteString(" (just now)")
			default:
				fmt.Fprintf(&sb, " (%s ago)", it.Age)
			}
			sb.WriteString("\n")
			if detail != nil {
				detail(it)
			}
		}
		if s.Truncated > 0 {
			fmt.Fprintf(&sb, "- … %d more (raise max_items or open the item)\n", s.Truncated)
		}
		sb.WriteString("\n")
	}
	section("Needs me", b.NeedsMe, nil)
	section("Stalled", b.Stalled, nil)
	section("Changed since", b.ChangedSince, func(it BriefItem) {
		for _, c := range it.Changes {
			fmt.Fprintf(&sb, "  - %s %s: %s", c.At, c.Kind, c.Summary)
			if c.Ref != "" {
				fmt.Fprintf(&sb, " (%s)", c.Ref)
			}
			sb.WriteString("\n")
		}
	})
	section("Suggestions", b.Suggestions, nil)
	section("In flight", b.InFlight, func(it BriefItem) {
		if it.Latest != "" {
			fmt.Fprintf(&sb, "  - latest: %s\n", it.Latest)
		}
		if len(it.Counts) > 0 {
			fmt.Fprintf(&sb, "  - tasks: %s\n", CountsText(it.Counts))
		}
		for _, d := range it.Doing {
			fmt.Fprintf(&sb, "  - %s\n", d)
		}
		for _, a := range it.Agents {
			fmt.Fprintf(&sb, "  - agent %s on %s: %s\n", a.Name, orUnknown(a.Host), a.State)
		}
	})
	section("Untracked", b.Untracked, nil)
	sb.WriteString("## Fleet\n")
	fmt.Fprintf(&sb, "- agents: %s\n", countsLine(b.Fleet.Agents))
	fmt.Fprintf(&sb, "- by host: %s\n", countsLine(b.Fleet.ByHost))
	for _, d := range b.Fleet.Down {
		fmt.Fprintf(&sb, "- DOWN %s\n", d)
	}
	for _, w := range b.Fleet.Warnings {
		fmt.Fprintf(&sb, "- note: %s\n", w)
	}
	sb.WriteString("\n## Budgets\n")
	if len(b.Budgets) == 0 {
		sb.WriteString("- nothing past its warning line\n")
	}
	for _, x := range b.Budgets {
		fmt.Fprintf(&sb, "- %s [%s]: %s\n", x.Title, x.Status, x.Line)
	}
	sb.WriteString("\n## Not covered yet\n")
	for _, g := range b.Gaps {
		fmt.Fprintf(&sb, "- %s\n", g)
	}
	return sb.String()
}

func countsLine(m map[string]int) string {
	if len(m) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

// ---- Gathering (ops/v1) ----

type briefOptions struct {
	Since    string
	Scope    string
	MaxItems int
}

// briefReads remembers each caller's last read, so a brief without since shows what changed
// since that caller last looked. Callers are told apart only by actor kind for now.
type briefReads struct {
	mu   sync.Mutex
	last map[module.Actor]time.Time
}

func (r *briefReads) swap(a module.Actor, now time.Time) (time.Time, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.last == nil {
		r.last = map[module.Actor]time.Time{}
	}
	prev, ok := r.last[a]
	r.last[a] = now
	return prev, ok
}

func (m *mod) brief(ctx context.Context, o briefOptions) (Brief, error) {
	now := time.Now().UTC()
	in := briefInput{Now: now, Scope: o.Scope, Max: o.MaxItems}
	if in.Scope == "" {
		in.Scope = "all"
	}
	if in.Scope != "all" && !strings.HasPrefix(in.Scope, "owner:") && !strings.HasPrefix(in.Scope, "workstream:") {
		return Brief{}, bad("scope is all, owner:<urn> or workstream:<id>")
	}
	if in.Max <= 0 {
		in.Max = briefDefaultMax
	}
	in.Max = min(in.Max, briefMaxMax)
	prev, seen := m.briefReads.swap(module.ActorOf(ctx), now)
	switch {
	case o.Since != "":
		t := When(o.Since)
		if t.IsZero() {
			return Brief{}, bad("since must be an RFC 3339 time")
		}
		in.Since = t
	case seen:
		in.Since = prev
	default:
		in.Since = now.Add(-briefDefaultSince)
	}

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	src := BriefSource{Name: "ops items (Caster ops/v1)"}
	if _, err := m.st.Refresh(ctx, false); err != nil {
		src.Problem = err.Error()
	}
	board, err := m.st.All(ctx)
	if err != nil {
		return Brief{}, err
	}
	src.AsOf = m.st.Refreshed().UTC().Format(time.RFC3339)
	src.Stale = src.Problem == "" && now.Sub(m.st.Refreshed()) > briefStale
	in.Board = board
	in.Sources = append(in.Sources, src)

	// Advance the shared activity refresh (bounded, in the background) so a brief read with
	// no Operations window open does not go stale; this read uses what is cached now.
	m.kickRoll()
	in.Rollups = m.st.Cached(ctx)
	act := BriefSource{Name: "activity (Torque, status stream)"}
	var oldest time.Time
	missing := 0
	for _, w := range board.Workstreams {
		if w.SettledAt != "" || w.Stage == "dropped" || w.Stage == "complete" {
			continue
		}
		r, ok := in.Rollups[w.ID]
		if !ok {
			missing++
			continue
		}
		if at := When(r.FetchedAt); oldest.IsZero() || at.Before(oldest) {
			oldest = at
		}
	}
	if !oldest.IsZero() {
		act.AsOf = oldest.Format(time.RFC3339)
		act.Stale = now.Sub(oldest) > briefStale
	}
	if missing > 0 {
		act.Problem = fmt.Sprintf("activity not read yet for %d open item(s)", missing)
	}
	in.Sources = append(in.Sources, act)

	sess := BriefSource{Name: "agents (Tether)"}
	if s, err := m.sessions(ctx); err != nil {
		sess.Problem = err.Error()
	} else {
		in.Sessions = &s
		sess.AsOf = s.FetchedAt
	}
	in.Sources = append(in.Sources, sess)

	hosts := BriefSource{Name: "machines (Cerberus)"}
	if link, err := m.tether(); err != nil {
		hosts.Problem = err.Error()
	} else if f, err := m.liveFleet.read(ctx, link, ""); err != nil {
		hosts.Problem = err.Error()
	} else {
		in.Hosts = &f
		hosts.AsOf = f.FetchedAt
	}
	in.Sources = append(in.Sources, hosts)

	use := BriefSource{Name: "usage"}
	if u, err := m.usage(ctx, ""); err != nil {
		use.Problem = err.Error()
	} else {
		in.Usage = &u
		use.AsOf = u.FetchedAt
	}
	in.Sources = append(in.Sources, use)
	return buildBrief(in), nil
}
