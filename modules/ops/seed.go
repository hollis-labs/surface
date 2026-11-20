package ops

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// The seed maps the owner's workstream tracker (kept by the Local Orchestrator: threads with a
// stage, phase, owner, summary, phase note and Torque tasks, plus loose follow-ups) into Ops
// workstreams. It is not an import of the tracker: it is read as data and mapped, and always
// as a preview first. The preview is kept here (seed_plans) for the owner to look over and
// edit in the page; only the owner's Apply in the page writes anything to Caster. Proposed
// concept groupings are off until the owner ticks them.

// Tracker is the tracker's JSON.
type Tracker struct {
	Updated   string          `json:"updated"`
	Note      string          `json:"note"`
	Threads   []TrackerThread `json:"threads"`
	Followups []any           `json:"followups"`
}

// TrackerThread is one thread of the tracker.
type TrackerThread struct {
	Name       string  `json:"name"`
	Stage      string  `json:"stage"`
	Phase      string  `json:"phase"`
	PhaseLabel string  `json:"phase_label"`
	Owner      string  `json:"owner"`
	Summary    string  `json:"summary"`
	PhaseNote  string  `json:"phase_note"`
	Tasks      [][]any `json:"tasks"`
}

// SeedPlan is a preview: what applying would write, as the owner edits it.
type SeedPlan struct {
	ID             string         `json:"id"`
	Source         string         `json:"source"`
	TrackerUpdated string         `json:"tracker_updated,omitempty"`
	CreatedAt      string         `json:"created_at"`
	AppliedAt      string         `json:"applied_at,omitempty"`
	Workstreams    []SeedItem     `json:"workstreams"`
	Concepts       []SeedConcept  `json:"concepts"`
	Followups      []SeedFollowup `json:"followups"`
	// Mapping says, in lines, how the tracker's fields became Ops'.
	Mapping []string `json:"mapping"`
}

// SeedItem is one tracker thread as a workstream.
type SeedItem struct {
	Include   bool   `json:"include"`
	ID        string `json:"id"`
	Title     string `json:"title"`
	Stage     string `json:"stage"`
	FromStage string `json:"from_stage"`
	Phase     string `json:"phase,omitempty"`
	Owner     string `json:"owner,omitempty"`
	// Parent is a proposed concept (applied only when that concept is ticked), another seed
	// workstream, or a workstream already in Ops.
	Parent string `json:"parent,omitempty"`
	Notes  string `json:"notes"`
	Links  []Link `json:"links"`
	// Exists marks an id Ops has already: applying skips it and never overwrites.
	Exists   bool     `json:"exists,omitempty"`
	Problems []string `json:"problems,omitempty"`
}

// SeedConcept is a proposed umbrella workstream over several threads.
type SeedConcept struct {
	Accept  bool     `json:"accept"`
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Why     string   `json:"why"`
	Members []string `json:"members"`
	Stage   string   `json:"stage"`
	Exists  bool     `json:"exists,omitempty"`
}

// SeedFollowup is a follow-up line and where it goes: a workstream's notes, or the owner's list.
type SeedFollowup struct {
	Index      int    `json:"index"`
	Text       string `json:"text"`
	Workstream string `json:"workstream,omitempty"`
	Why        string `json:"why,omitempty"`
}

// stageMap is the tracker's stage as Ops'.
var stageMap = map[string]string{
	"flight": StageInFlight, "queued": StageQueued, "review": StageInReview,
	// landed is Adoption, not Active: landed means merged or deployed, and Active is the
	// owner's "active when" checklist met, which the tracker does not attest (its one landed
	// thread is "unverified"). The owner ticks the list and moves it on.
	"landed": StageAdoption,
}

// conceptRules are the proposed groupings: a thread whose name (or, failing that, summary)
// names one of the words belongs to the concept. First rule wins. A concept is proposed only
// when it gathers at least two threads.
var conceptRules = []struct {
	id, title, why string
	words          []string
}{
	{"auth-identity-secrets", "Auth, identity and secrets",
		"one concept landing across the whole mesh: who an agent is, what it may do, the keys it holds and who approves",
		[]string{"warden", "gatecrasher", "identity", "service token", "approval", "credential", "secret", "auth"}},
	{"observability-and-status", "Observability and status",
		"what is happening and what happened, for agents and the owner: status stream, OTel, correlation, transcripts",
		[]string{"otel", "observability", "status stream", "transcript", "correlation"}},
	{"mcp-gateway-and-ops-mcp", "MCP gateway and the Ops MCP",
		"the shared gateway every tool goes through, and what it serves",
		[]string{"mcp"}},
	{"agent-runtime-and-chat", "Agent runtime and chat",
		"how agents run and talk: the chat wire, Nanite and session continuity",
		[]string{"chat", "nanite", "session continuity"}},
	{"fleet-and-placement", "Fleet and placement",
		"where work runs and what runs where",
		[]string{"fleet", "placement"}},
}

var slugBad = regexp.MustCompile(`[^a-z0-9]+`)

// slugOf makes an Ops id from a thread's name: lower case, dashes, at most 48 characters on a
// word boundary, never ending in a dash. A name with too few ASCII letters or digits gets an
// id made from a hash of it (ok false), which the preview flags for the owner to change.
func slugOf(name string) (id string, ok bool) {
	s := strings.Trim(slugBad.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 48 {
		s = s[:48]
		if i := strings.LastIndex(s, "-"); i > 16 {
			s = s[:i]
		}
		s = strings.TrimRight(s, "-")
	}
	if ValidID(s) {
		return s, true
	}
	sum := sha256.Sum256([]byte(name))
	return "workstream-" + hex.EncodeToString(sum[:4]), false
}

// ParseTracker reads the tracker's JSON.
func ParseTracker(raw []byte) (Tracker, error) {
	var t Tracker
	if err := json.Unmarshal(raw, &t); err != nil {
		return t, bad("the tracker could not be read as JSON: %v", err)
	}
	if len(t.Threads) == 0 {
		return t, bad("the tracker has no threads")
	}
	return t, nil
}

// maxTracker is the largest tracker read.
const maxTracker = 4 << 20

// errTrackerPath is the one answer for a path that cannot be read, whatever the reason, so the
// route says nothing about what is or is not on the disk.
var errTrackerPath = &InputError{Msg: "the tracker could not be read from that path: give an absolute path to a regular .json file of at most 4 MB"}

// ReadTracker reads a tracker from a path on this machine: an absolute path to a regular .json
// file (not a link), opened once and read to at most maxTracker bytes. Only the owner's page
// gives a path.
func ReadTracker(path string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if !filepath.IsAbs(path) || filepath.Ext(path) != ".json" {
		return nil, errTrackerPath
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() {
		return nil, errTrackerPath
	}
	f, err := os.Open(path) //nolint:gosec // the owner's own file, named in the owner's page
	if err != nil {
		return nil, errTrackerPath
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return nil, errTrackerPath
	}
	b, err := io.ReadAll(io.LimitReader(f, maxTracker+1))
	if err != nil || len(b) > maxTracker {
		return nil, errTrackerPath
	}
	return b, nil
}

var (
	taskID    = regexp.MustCompile(`\bCW-\d{8}-\d{4}\b`)
	taskRange = regexp.MustCompile(`^(CW-\d{8}-)(\d{4})\.\.(\d{4})$`)
)

// expandTaskIDs reads a tracker's task id: one id, or a run written CW-20990101-0097..0105
// (at most 50). Anything else is no task.
func expandTaskIDs(s string) []string {
	s = strings.TrimSpace(s)
	if linkKinds["torque.task"].MatchString(s) {
		return []string{s}
	}
	m := taskRange.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	from, _ := strconv.Atoi(m[2])
	to, _ := strconv.Atoi(m[3])
	if to < from || to-from >= 50 {
		return nil
	}
	out := make([]string, 0, to-from+1)
	for n := from; n <= to; n++ {
		out = append(out, fmt.Sprintf("%s%04d", m[1], n))
	}
	return out
}

// Preview maps a tracker. existing are the ids Ops has already.
func Preview(t Tracker, source string, existing map[string]bool) SeedPlan {
	p := SeedPlan{Source: source, TrackerUpdated: t.Updated, CreatedAt: now(),
		Workstreams: []SeedItem{}, Concepts: []SeedConcept{}, Followups: []SeedFollowup{}}
	p.Mapping = []string{
		"stage: flight → In Flight, queued → Queued, review → In Review, landed → Adoption (landed is merged or deployed; Active means its active-when list is met, which the tracker does not say: tick it and move it on).",
		"id: from the thread's name (a slug); edit it before applying, since an Ops id is never re-keyed and is the Registry workstream key.",
		"tasks: each [Torque id, title, state] becomes a torque.task link (role task, its title as the label); the state is read live from Torque, not copied.",
		"summary and phase note: the workstream's notes, with the tracker's phase.",
		"owner: as the tracker says it; a thread without one is left for you to set.",
		"follow-ups: onto a workstream's notes when they name one of its tasks or its name; the rest are your list (caster://ops/followups).",
		"concepts: proposed groupings, off until you tick them. A ticked concept is made as a workstream and its threads nest under it.",
	}
	used := map[string]bool{}
	taskOwner := map[string]string{}
	for _, th := range t.Threads {
		slug, named := slugOf(th.Name)
		id := slug
		for n := 2; used[id]; n++ {
			id = fmt.Sprintf("%s-%d", slug, n)
		}
		used[id] = true
		it := SeedItem{Include: true, ID: id, Title: strings.TrimSpace(th.Name), FromStage: th.Stage, Phase: th.Phase,
			Owner: strings.TrimSpace(th.Owner), Links: []Link{}, Exists: existing[id]}
		if !named {
			it.Problems = append(it.Problems, "the name has too few plain letters for an id: "+id+" was made up; give it a better one")
		}
		st, ok := stageMap[th.Stage]
		if !ok {
			st = StagePlanning
			it.Problems = append(it.Problems, fmt.Sprintf("stage %q is not one the seed knows: put in Planning", th.Stage))
		}
		it.Stage = st
		if it.Owner == "" {
			it.Problems = append(it.Problems, "no owner in the tracker")
		}
		if it.Exists {
			it.Include = false
			it.Problems = append(it.Problems, "Ops has "+Ref(id)+" already: skipped, never overwritten")
		}
		for _, row := range th.Tasks {
			if len(row) == 0 {
				continue
			}
			first, _ := row[0].(string)
			title := ""
			if len(row) > 1 {
				title, _ = row[1].(string)
			}
			for _, tid := range expandTaskIDs(first) {
				if slices.ContainsFunc(it.Links, func(l Link) bool { return l.ID == tid }) {
					continue
				}
				it.Links = append(it.Links, Link{Kind: "torque.task", ID: tid, Role: "task", Label: oneLine(title, 200)})
				if _, taken := taskOwner[tid]; !taken {
					taskOwner[tid] = id
				}
			}
		}
		var notes []string
		if s := strings.TrimSpace(th.Summary); s != "" {
			notes = append(notes, s)
		}
		phase := th.PhaseLabel
		if phase == "" {
			phase = th.Phase
		}
		if s := strings.TrimSpace(th.PhaseNote); s != "" {
			notes = append(notes, fmt.Sprintf("Tracker, %s (phase: %s): %s", orDash(t.Updated), orDash(phase), s))
		}
		it.Notes = strings.Join(notes, "\n\n")
		p.Workstreams = append(p.Workstreams, it)
	}

	// Concepts.
	members := map[string][]string{}
	for i, it := range p.Workstreams {
		th := t.Threads[i]
		for _, r := range conceptRules {
			if names(th.Name, r.words) {
				members[r.id] = append(members[r.id], it.ID)
				break
			}
		}
	}
	for _, r := range conceptRules {
		ms := members[r.id]
		if len(ms) < 2 {
			continue
		}
		c := SeedConcept{ID: r.id, Title: r.title, Why: r.why, Members: ms, Exists: existing[r.id]}
		c.Stage = conceptStage(p.Workstreams, ms)
		p.Concepts = append(p.Concepts, c)
		for i := range p.Workstreams {
			if slices.Contains(ms, p.Workstreams[i].ID) {
				p.Workstreams[i].Parent = r.id
			}
		}
	}

	// Follow-ups.
	for i, f := range t.Followups {
		text := followupText(f)
		if text == "" {
			continue
		}
		fu := SeedFollowup{Index: i, Text: text}
		for _, tid := range taskID.FindAllString(text, -1) {
			if ws, ok := taskOwner[tid]; ok {
				fu.Workstream, fu.Why = ws, "names "+tid
				break
			}
		}
		if fu.Workstream == "" {
			// By name: the thread whose distinctive words it names most, when that is two of
			// them (or the only one a thread has), and no other thread ties.
			best, bestScore, tie := "", 0, false
			for j, it := range p.Workstreams {
				kw := keywords(t.Threads[j].Name)
				score := 0
				for _, w := range kw {
					if names(text, []string{w}) {
						score++
					}
				}
				if score < 2 && (score == 0 || len(kw) > 1) {
					continue
				}
				switch {
				case score > bestScore:
					best, bestScore, tie = it.ID, score, false
				case score == bestScore:
					tie = true
				}
			}
			if best != "" && !tie {
				fu.Workstream, fu.Why = best, "names the thread"
			}
		}
		p.Followups = append(p.Followups, fu)
	}
	return p
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func followupText(f any) string {
	switch v := f.(type) {
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		for _, k := range []string{"text", "note", "summary", "title"} {
			if s, ok := v[k].(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	b, _ := json.Marshal(f)
	return string(b)
}

// names reports whether text names any of the words (whole words, any case).
func names(text string, words []string) bool {
	low := " " + strings.Join(strings.Fields(slugBad.ReplaceAllString(strings.ToLower(text), " ")), " ") + " "
	for _, w := range words {
		if w == "" {
			continue
		}
		if strings.Contains(low, " "+w+" ") || strings.Contains(low, " "+w+"s ") {
			return true
		}
	}
	return false
}

// keywords are the distinctive words of a thread's name, for matching follow-ups to it.
var stop = map[string]bool{
	"and": true, "the": true, "of": true, "for": true, "with": true, "flow": true, "service": true, "services": true,
	"stream": true, "gaps": true, "landing": true, "priority": true, "critical": true, "path": true, "integration": true,
	"adoption": true, "stacks": true, "pilot": true, "tooling": true, "spec": true, "provenance": true, "unit": true,
	"work": true, "everywhere": true, "metrics": true, "logs": true, "server": true, "shared": true, "over": true,
	"source": true, "truth": true, "dev": true, "mode": true, "friction": true, "agent": true, "gui": true, "ui": true,
	"tokens": true, "approvals": true, "pr": true, "wire": true, "libs": true, "host": true, "terminal": true,
	// Names of agents and services that come up everywhere: naming one is not naming the thread.
	"cerberus": true, "tether": true, "nanite": true, "substrate": true, "torque": true, "tesseract": true,
	"chief": true, "tachyon": true, "surface": true, "folio": true,
}

func keywords(name string) []string {
	var out []string
	for _, w := range strings.Fields(slugBad.ReplaceAllString(strings.ToLower(name), " ")) {
		if len(w) >= 4 && !stop[w] {
			out = append(out, w)
		}
	}
	return out
}

// conceptStage is a concept's stage from its threads: In Flight when any is, else the
// earliest stage among them.
func conceptStage(items []SeedItem, ms []string) string {
	best := ""
	for _, it := range items {
		if !slices.Contains(ms, it.ID) {
			continue
		}
		if it.Stage == StageInFlight {
			return StageInFlight
		}
		if best == "" || stageRank(it.Stage) < stageRank(best) {
			best = it.Stage
		}
	}
	if best == "" {
		best = StagePlanning
	}
	return best
}

func newPlanID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ErrApplied says a preview was applied already (or is being applied): make a new one.
var ErrApplied = errors.New("this preview was applied already; make a new one")

// NewPlan keeps a new preview.
func (s *Store) NewPlan(ctx context.Context, p SeedPlan) (SeedPlan, error) {
	p.ID, p.AppliedAt = newPlanID(), ""
	b, err := json.Marshal(p)
	if err != nil {
		return p, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO seed_plans(id, created_at, source, plan, applied_at) VALUES(?,?,?,?,'')`,
		p.ID, p.CreatedAt, p.Source, string(b))
	return p, err
}

// SavePlan keeps the owner's edits to a preview that has not been applied (ErrApplied when it
// has, or is being).
func (s *Store) SavePlan(ctx context.Context, p SeedPlan) error {
	p.AppliedAt = ""
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	r, err := s.db.ExecContext(ctx, `UPDATE seed_plans SET plan=? WHERE id=? AND applied_at=''`, string(b), p.ID)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return ErrApplied
	}
	return nil
}

// ClaimPlan marks a preview applied, atomically: only one apply can claim it. ErrApplied when
// it was claimed before.
func (s *Store) ClaimPlan(ctx context.Context, id string) error {
	r, err := s.db.ExecContext(ctx, `UPDATE seed_plans SET applied_at=? WHERE id=? AND applied_at=''`, now(), id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return ErrApplied
	}
	return nil
}

// ReleasePlan gives a claimed preview back, for an apply that wrote nothing.
func (s *Store) ReleasePlan(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE seed_plans SET applied_at='' WHERE id=?`, id)
	return err
}

// Plan is a kept preview, with when it was applied.
func (s *Store) Plan(ctx context.Context, id string) (SeedPlan, bool, error) {
	var raw, applied string
	err := s.db.QueryRowContext(ctx, `SELECT plan, applied_at FROM seed_plans WHERE id=?`, id).Scan(&raw, &applied)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SeedPlan{}, false, nil
		}
		return SeedPlan{}, false, err
	}
	var p SeedPlan
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return SeedPlan{}, false, err
	}
	p.ID, p.AppliedAt = id, applied
	return p, true, nil
}

// SeedResult is what an apply did.
type SeedResult struct {
	Created   []string `json:"created"`
	Skipped   []string `json:"skipped"`
	Failed    []string `json:"failed"`
	Followups int      `json:"followups_for_owner"`
	// Noted are the workstreams Ops had already that got follow-ups on their notes.
	Noted []string `json:"noted"`
}

// FollowupsName is the document of the owner's follow-ups the seed could not place.
const FollowupsName = "followups"

// Apply writes a preview as the owner left it (the caller has claimed it): the ticked
// concepts, then the included workstreams (a parent before its children), each with the
// follow-ups placed on it. Follow-ups for a workstream Ops had already go onto its notes; any
// whose workstream was not made go to the owner's list with the unplaced ones. An id Ops has
// already is skipped. Errors on one workstream do not stop the others.
func (s *Store) Apply(ctx context.Context, p SeedPlan, by string) (SeedResult, error) {
	res := SeedResult{Created: []string{}, Skipped: []string{}, Failed: []string{}, Noted: []string{}}
	if s.caster == nil {
		return res, ErrNoCaster
	}
	if _, err := s.Refresh(ctx, true); err != nil {
		return res, err
	}
	b, err := s.All(ctx)
	if err != nil {
		return res, err
	}
	existed := map[string]bool{}
	for _, w := range b.Workstreams {
		existed[w.ID] = true
	}
	accepted := map[string]bool{}
	for _, c := range p.Concepts {
		if c.Accept {
			accepted[c.ID] = true
		}
	}
	included := map[string]bool{}
	for _, it := range p.Workstreams {
		if it.Include {
			included[it.ID] = true
		}
	}
	notes := map[string][]string{}
	var owner []string
	for _, f := range p.Followups {
		if w := f.Workstream; w != "" && (included[w] || accepted[w] || existed[w]) {
			notes[w] = append(notes[w], "- Follow-up from the tracker: "+f.Text)
		} else {
			owner = append(owner, f.Text)
		}
	}
	made := map[string]bool{}
	create := func(in NewWorkstream) {
		if existed[in.ID] {
			res.Skipped = append(res.Skipped, in.ID)
			return
		}
		if _, err := s.Create(ctx, in, by); err != nil {
			res.Failed = append(res.Failed, fmt.Sprintf("%s: %v", in.ID, err))
			return
		}
		made[in.ID] = true
		res.Created = append(res.Created, in.ID)
	}
	there := func(id string) bool { return existed[id] || made[id] }
	for _, c := range p.Concepts {
		if !c.Accept {
			continue
		}
		create(NewWorkstream{ID: c.ID, Title: c.Title, Owner: "owner", Stage: orStage(c.Stage), Why: "seeded: a concept over the tracker's threads",
			Purpose: c.Why, Notes: joinNotes("Proposed by the seed over: "+refsOf(c.Members)+".", notes[c.ID])})
	}
	// Workstreams in rounds: one whose parent is not made yet waits for it.
	pending := []SeedItem{}
	for _, it := range p.Workstreams {
		if it.Include {
			pending = append(pending, it)
		}
	}
	const rounds = maxDepth + 2
	for round := 0; len(pending) > 0 && round < rounds; round++ {
		var later []SeedItem
		for _, it := range pending {
			parent := it.Parent
			if parent != "" && !accepted[parent] && !included[parent] && !existed[parent] {
				parent = "" // a concept left unticked: the thread stays top level
			}
			if parent != "" && !there(parent) {
				later = append(later, it)
				continue
			}
			links := slices.DeleteFunc(slices.Clone(it.Links), func(x Link) bool { return checkLink(x) != nil })
			create(NewWorkstream{ID: it.ID, Title: it.Title, Owner: it.Owner, Stage: orStage(it.Stage), Parent: parent,
				Links: links, Why: "seeded from the tracker (" + orDash(it.FromStage) + ")",
				Correlation: Correlation{Labels: []string{it.ID}}, Notes: joinNotes(it.Notes, notes[it.ID])})
		}
		if len(later) == len(pending) {
			for _, it := range later {
				res.Failed = append(res.Failed, fmt.Sprintf("%s: its parent %s was not made", it.ID, it.Parent))
			}
			pending = nil
			break
		}
		pending = later
	}
	for _, it := range pending {
		res.Failed = append(res.Failed, fmt.Sprintf("%s: left unmade after %d rounds (its parents nest too deep)", it.ID, rounds))
	}
	// Follow-ups: onto the notes of a workstream Ops had already; to the owner's list when
	// their workstream was not made.
	ids := make([]string, 0, len(notes))
	for id := range notes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		switch {
		case made[id]:
			// written with it
		case existed[id]:
			add := strings.TrimPrefix(strings.Join(notes[id], "\n"), "- ")
			if _, err := s.Update(ctx, id, 0, Patch{AppendNote: &add}); err != nil {
				res.Failed = append(res.Failed, fmt.Sprintf("%s follow-up: %v", id, err))
				for _, n := range notes[id] {
					owner = append(owner, strings.TrimPrefix(n, "- Follow-up from the tracker: "))
				}
			} else {
				res.Noted = append(res.Noted, id)
			}
		default:
			for _, n := range notes[id] {
				owner = append(owner, strings.TrimPrefix(n, "- Follow-up from the tracker: "))
			}
		}
	}
	if len(owner) > 0 {
		if err := s.addFollowups(ctx, owner, p.TrackerUpdated); err != nil {
			res.Failed = append(res.Failed, "the owner's follow-ups: "+err.Error())
		} else {
			res.Followups = len(owner)
		}
	}
	sort.Strings(res.Created)
	return res, nil
}

func orStage(s string) string {
	if ValidStage(s) {
		return s
	}
	return StagePlanning
}

func joinNotes(lead string, more []string) string {
	parts := []string{}
	if strings.TrimSpace(lead) != "" {
		parts = append(parts, strings.TrimSpace(lead))
	}
	if len(more) > 0 {
		parts = append(parts, strings.Join(more, "\n"))
	}
	return strings.Join(parts, "\n\n")
}

func refsOf(ids []string) string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = Ref(id)
	}
	return strings.Join(out, ", ")
}

// addFollowups appends lines to the owner's follow-up list (caster://ops/followups), to the
// list as it is in Caster at the write (and to someone else's, should theirs land first).
func (s *Store) addFollowups(ctx context.Context, lines []string, asOf string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok, err := s.fresh(ctx, FollowupsName)
	if err != nil {
		return err
	}
	err = s.putOver(ctx, FollowupsName, "text/markdown", it, ok, func(cur Item, there bool) (string, string, error) {
		body := "# Follow-ups for the owner\n\nLines the seed could not place on a workstream. Delete one when it is handled.\n"
		if there {
			body = strings.TrimRight(cur.Content, "\n") + "\n"
		}
		body += fmt.Sprintf("\n## From the tracker (%s)\n\n", orDash(asOf))
		for _, l := range lines {
			body += "- " + strings.Join(strings.Fields(l), " ") + "\n"
		}
		return body, "Ops: follow-ups for the owner", nil
	})
	return err
}

// Followups is the owner's follow-up list from the copy.
func (s *Store) Followups(ctx context.Context) []string {
	var content string
	if s.db.QueryRowContext(ctx, `SELECT content FROM docs WHERE name=?`, FollowupsName).Scan(&content) != nil {
		return []string{}
	}
	out := []string{}
	for _, l := range strings.Split(content, "\n") {
		if t, ok := strings.CutPrefix(strings.TrimSpace(l), "- "); ok {
			out = append(out, t)
		}
	}
	return out
}
