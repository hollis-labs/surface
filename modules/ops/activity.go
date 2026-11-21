package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hollis-labs/surface/module/tetherlink"
)

// Activity is a read-time merge, per workstream and rolled up from its children: Torque task
// state and STATUS comments (today's live signal), Tesseract status-stream entries where the
// workstream names a status namespace, and Ops' own transitions. Nothing here writes.

// Entry is one row of the merged feed.
type Entry struct {
	At         string   `json:"at"`
	Source     string   `json:"source"` // torque, comment, status, ops
	State      string   `json:"state,omitempty"`
	Summary    string   `json:"summary"`
	SubjectURN string   `json:"subject_urn,omitempty"`
	Task       string   `json:"task,omitempty"`
	Workstream string   `json:"workstream"`
	Refs       []string `json:"refs,omitempty"`
	// Verified is true only for a status entry from a trusted writer. Today no writer is
	// trusted (phase 2), so status rows show as claimed.
	Verified bool `json:"verified"`
}

// Task is one Torque task as the roll-up shows it.
type Task struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	Updated    string `json:"updated,omitempty"`
	Workstream string `json:"workstream"`
	// Via is how the workstream reaches it: a link, epic or plan.
	Via           string `json:"via,omitempty"`
	PlanID        string `json:"plan_id,omitempty"`
	PhaseID       string `json:"phase_id,omitempty"`
	ParentID      string `json:"parent_id,omitempty"`
	EpicID        string `json:"epic_id,omitempty"`
	Kind          string `json:"kind,omitempty"`
	DiscoveredVia string `json:"discovered_via,omitempty"`
}

// Rollup is a workstream's tasks and activity, with its children's.
type Rollup struct {
	Workstream string         `json:"workstream"`
	Counts     map[string]int `json:"counts"`
	Tasks      []Task         `json:"tasks"`
	Plans      []Plan         `json:"plans,omitempty"`
	Entries    []Entry        `json:"entries"`
	// Last is the newest entry, for a card's "last activity" line.
	Last *Entry `json:"last,omitempty"`
	// Suggest is a stage the tasks suggest the workstream is in, when it differs (a hint:
	// moves are the owner's).
	Suggest     string   `json:"suggest,omitempty"`
	SuggestWhy  string   `json:"suggest_why,omitempty"`
	Errors      []string `json:"errors,omitempty"`
	FetchedAt   string   `json:"fetched_at"`
	Unavailable bool     `json:"unavailable,omitempty"`
}

const (
	// maxTasks bounds the tasks one roll-up reads.
	maxTasks = 80
	// readConcurrency bounds the gateway calls one roll-up makes at once.
	readConcurrency = 6
	// commentsPerTask is how many of a task's newest comments are read.
	commentsPerTask = 20
	// maxEntries bounds the feed kept.
	maxEntries = 300
)

// statusLine picks the comments that are status: STATUS lines, and reviewers' verdicts.
var statusLine = regexp.MustCompile(`(?i)^\s*(STATUS\b|REVIEW\b|VERDICT\b|Independent review\b|INCIDENT\b)`)

// statusWord is the state a STATUS line names ("STATUS milestone: ...").
var statusWord = regexp.MustCompile(`(?i)^\s*STATUS\s+([a-z_-]+)`)

// reader reads Torque and Tesseract through the module's link.
type reader struct {
	link func() (tetherlink.Caller, error)
	// cache keeps each task's read for taskTTL, shared by every roll-up (a task under a
	// concept and under its child is read once).
	cache *taskCache
}

// taskTTL is how long a task's read (its state and status comments) is used again.
const taskTTL = 60 * time.Second

// taskCache is the reads of Torque tasks, by id.
type taskCache struct {
	mu      sync.Mutex
	m       map[string]taskRead
	plans   map[string]planRead
	parents map[string]parentRead
}

type taskRead struct {
	task Task
	// err is why the task could not be read; cerr why its comments could not be.
	err      error
	cerr     error
	comments []Entry // the workstream field is filled in by the roll-up
	at       time.Time
}

func newTaskCache() *taskCache { return &taskCache{m: map[string]taskRead{}} }

func (c *taskCache) get(id string) (taskRead, bool) {
	if c == nil {
		return taskRead{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.m[id]
	if !ok || time.Since(r.at) > taskTTL {
		return taskRead{}, false
	}
	return r, true
}

func (c *taskCache) put(id string, r taskRead) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	r.at = time.Now()
	c.m[id] = r
	if len(c.m) > 4*maxTasks {
		for k, v := range c.m {
			if time.Since(v.at) > taskTTL {
				delete(c.m, k)
			}
		}
	}
}

// read is one task's state and, unless it is finished, its status comments: from the cache
// when it is fresh, else from Torque. A failed comment read is not cached.
func (r reader) read(ctx context.Context, id string) taskRead {
	if got, ok := r.cache.get(id); ok {
		return got
	}
	var got taskRead
	got.task, got.err = r.task(ctx, id)
	if got.err != nil {
		got.task = Task{ID: id, Status: "unknown"}
		// A read cut short by the caller (canceled, out of time) says nothing about the task.
		if ctx.Err() == nil && !errors.Is(got.err, errActivityBudget) && !errors.Is(got.err, context.Canceled) && !errors.Is(got.err, context.DeadlineExceeded) {
			r.cache.put(id, got)
		}
		return got
	}
	if !Finished(got.task.Status) {
		got.comments, got.cerr = r.comments(ctx, id, "")
		if got.cerr != nil {
			return got
		}
	}
	r.cache.put(id, got)
	return got
}

func (r reader) call(ctx context.Context, tool string, args map[string]any) ([]byte, error) {
	if b, ok := ctx.Value(activityBudgetKey{}).(*activityBudget); ok && !b.take() {
		return nil, errActivityBudget
	}
	c, err := r.link()
	if err != nil {
		return nil, err
	}
	out, err := c.Call(ctx, tool, args)
	return []byte(out), err
}

// Roll reads the roll-up of w and its descendants.
func (r reader) Roll(ctx context.Context, w Workstream, descendants []Workstream) Rollup {
	out := Rollup{Workstream: w.ID, Counts: map[string]int{}, Tasks: []Task{}, Entries: []Entry{}, FetchedAt: now()}
	all := append([]Workstream{w}, descendants...)
	// Ops' own transitions first: they need nothing from the network.
	for _, x := range all {
		for _, t := range x.Transitions {
			s := fmt.Sprintf("%s → %s", label(t.From), label(t.To))
			if t.From == "" {
				s = "created in " + label(t.To)
			}
			if t.Why != "" {
				s += ": " + t.Why
			}
			if t.By != "" {
				s += " (" + t.By + ")"
			}
			out.Entries = append(out.Entries, Entry{At: t.At, Source: "ops", State: t.To, Summary: s, Workstream: x.ID})
		}
	}
	if _, err := r.link(); err != nil {
		out.Unavailable = true
		out.Errors = append(out.Errors, err.Error())
		finish(&out, w)
		return out
	}
	var mu sync.Mutex
	fail := func(err error) {
		mu.Lock()
		if len(out.Errors) < 8 {
			out.Errors = append(out.Errors, err.Error())
		}
		mu.Unlock()
	}

	// The tasks: linked ones, and the children of linked epics.
	type want struct{ id, ws, via, plan, discovered string }
	var wants []want
	seen := map[string]bool{}
	add := func(id, ws, via string) {
		if !seen[id] && len(wants) < maxTasks {
			seen[id] = true
			wants = append(wants, want{id: id, ws: ws, via: via})
		}
	}
	for _, x := range all {
		for _, l := range x.Links {
			if l.Kind == "torque.task" {
				add(l.ID, x.ID, "link")
			}
		}
	}
	// Only directly linked tasks seed discovery: never recursively chase a graph.
	// Keep these reads locally too, including failures, so final assembly reads each once.
	initial := make(map[string]taskRead, len(wants))
	semInitial := make(chan struct{}, readConcurrency)
	var initialWG sync.WaitGroup
	for _, t := range wants {
		initialWG.Add(1)
		go func() {
			defer initialWG.Done()
			semInitial <- struct{}{}
			defer func() { <-semInitial }()
			got := r.read(ctx, t.id)
			mu.Lock()
			initial[t.id] = got
			mu.Unlock()
		}()
	}
	initialWG.Wait()
	containers := r.discover(ctx, all, initial, fail)
	seenEpics := map[string]bool{}
	for _, x := range containers {
		for _, l := range x.Links {
			if l.Kind != "torque.epic" || seenEpics[l.ID] {
				continue
			}
			if len(seenEpics) == maxTasks {
				fail(errors.New("epic limit reached; showing the first 80 epics"))
				break
			}
			seenEpics[l.ID] = true
			ids, err := r.epicTasks(ctx, l.ID)
			if err != nil {
				fail(fmt.Errorf("epic %s: %w", l.ID, err))
				continue
			}
			for _, id := range ids {
				add(id, x.ID, l.ID)
				if !seen[id] {
					fail(fmt.Errorf("epic %s: task limit reached", l.ID))
					break
				}
				for i := range wants {
					if wants[i].id == id && wants[i].via == l.ID {
						wants[i].discovered = x.Via
					}
				}
			}
		}
	}
	// Plans add to the same task set; linked task reads above are reused below.
	seenPlans := map[string]bool{}
	for _, x := range containers {
		for _, l := range x.Links {
			if l.Kind != "torque.plan" || seenPlans[l.ID] {
				continue
			}
			if len(seenPlans) == maxTasks {
				fail(errors.New("plan limit reached; showing the first 80 plans"))
				break
			}
			seenPlans[l.ID] = true
			p, ids, err := r.plan(ctx, l.ID)
			p.Workstream = x.ID
			p.Via = x.Via
			if err != nil {
				fail(fmt.Errorf("plan %s: %w", l.ID, err))
			}
			for _, id := range ids {
				add(id, x.ID, l.ID)
				if !seen[id] {
					p.Truncated = true
					continue
				}
				for i := range wants {
					if wants[i].id == id {
						wants[i].plan = l.ID
						break
					}
				}
			}
			if p.Truncated {
				fail(fmt.Errorf("plan %s: task limit reached or children incomplete", l.ID))
			}
			out.Plans = append(out.Plans, p)
		}
	}
	sem := make(chan struct{}, readConcurrency)
	var wg sync.WaitGroup
	for _, t := range wants {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			got, ok := initial[t.id]
			if !ok {
				got = r.read(ctx, t.id)
			}
			if got.err != nil {
				fail(fmt.Errorf("%s: %w", t.id, got.err))
			}
			if got.cerr != nil {
				fail(fmt.Errorf("%s comments: %w", t.id, got.cerr))
			}
			task := got.task
			task.Workstream, task.Via, task.PlanID = t.ws, t.via, t.plan
			task.DiscoveredVia = t.discovered
			mu.Lock()
			defer mu.Unlock()
			out.Tasks = append(out.Tasks, task)
			out.Counts[task.Status]++
			if task.Updated != "" && got.err == nil {
				out.Entries = append(out.Entries, Entry{At: task.Updated, Source: "torque", State: task.Status, Task: t.id, Workstream: t.ws,
					Summary: fmt.Sprintf("%s is %s: %s", t.id, task.Status, task.Title)})
			}
			for _, c := range got.comments {
				c.Workstream = t.ws
				out.Entries = append(out.Entries, c)
			}
		}()
	}
	wg.Wait()

	// The status stream, per namespace, filtered to the tasks.
	byNS := map[string][]string{}
	for _, x := range all {
		if x.StatusNS == "" || !validNS(x.StatusNS) {
			continue
		}
		for _, t := range out.Tasks {
			if t.Workstream == x.ID {
				byNS[x.StatusNS] = append(byNS[x.StatusNS], t.ID)
			}
		}
		if _, ok := byNS[x.StatusNS]; !ok {
			byNS[x.StatusNS] = nil
		}
	}
	nsOwner := map[string]string{}
	for _, x := range all {
		if x.StatusNS != "" && nsOwner[x.StatusNS] == "" {
			nsOwner[x.StatusNS] = x.ID
		}
	}
	for ns, tasks := range byNS {
		es, err := r.status(ctx, ns, tasks, nsOwner[ns])
		if err != nil {
			fail(fmt.Errorf("status %s: %w", ns, err))
			continue
		}
		out.Entries = append(out.Entries, es...)
	}
	finish(&out, w)
	return out
}

// statusPrefix is the head of a STATUS comment ("STATUS milestone: "), dropped to compare it
// with the status-stream entry it mirrors.
var statusPrefix = regexp.MustCompile(`(?i)^\s*STATUS\s+[a-z_-]+\s*:\s*`)

func mirrorKey(task, summary string) string {
	return task + "\x00" + strings.ToLower(strings.Join(strings.Fields(statusPrefix.ReplaceAllString(summary, "")), " "))
}

// When reads an entry's time (RFC 3339, with or without fractions, or Torque's "2006-01-02
// 15:04:05"). The zero time when it cannot.
func When(s string) time.Time {
	for _, f := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(f, strings.TrimSpace(s)); err == nil {
			return t
		}
	}
	return time.Time{}
}

// finish drops STATUS comments that mirror a status-stream entry, sorts the feed (by time,
// newest first) and the tasks, and works out the suggestion.
func finish(out *Rollup, w Workstream) {
	mirrored := map[string]bool{}
	for _, e := range out.Entries {
		if e.Source == "status" && e.Task != "" {
			mirrored[mirrorKey(e.Task, e.Summary)] = true
		}
	}
	if len(mirrored) > 0 {
		out.Entries = slices.DeleteFunc(out.Entries, func(e Entry) bool {
			return e.Source == "comment" && mirrored[mirrorKey(e.Task, e.Summary)]
		})
	}
	sort.SliceStable(out.Entries, func(i, j int) bool { return When(out.Entries[i].At).After(When(out.Entries[j].At)) })
	if len(out.Entries) > maxEntries {
		out.Entries = out.Entries[:maxEntries]
	}
	for i := range out.Entries {
		if out.Entries[i].Source != "ops" {
			out.Last = &out.Entries[i]
			break
		}
	}
	if out.Last == nil && len(out.Entries) > 0 {
		out.Last = &out.Entries[0]
	}
	sort.Slice(out.Tasks, func(i, j int) bool {
		if a, b := openRank(out.Tasks[i].Status), openRank(out.Tasks[j].Status); a != b {
			return a < b
		}
		return out.Tasks[i].ID < out.Tasks[j].ID
	})
	out.Suggest, out.SuggestWhy = suggest(w.Stage, out.Counts)
}

// openRank orders tasks: in progress first, then review, then waiting, then finished.
func openRank(s string) int {
	switch s {
	case "doing", "in_progress":
		return 0
	case "review", "in_review":
		return 1
	case "blocked":
		return 2
	case "todo", "backlog", "queued":
		return 3
	case "done", "canceled", "cancel" + "led": // the British spelling, as some services write it
		return 5
	}
	return 4
}

// Finished says whether a task status is an end.
func Finished(s string) bool {
	switch s {
	case "done", "canceled", "cancel" + "led", "archived":
		return true
	}
	return false
}

// suggest is the stage the tasks point to, when it is not the stage the workstream is at.
// Moves stay explicit: this is shown as a hint with a button.
func suggest(stage string, counts map[string]int) (string, string) {
	total, finished, doing, review := 0, 0, counts["doing"]+counts["in_progress"], counts["review"]+counts["in_review"]
	for s, n := range counts {
		total += n
		if Finished(s) {
			finished += n
		}
	}
	if total == 0 {
		return "", ""
	}
	switch stage {
	case StagePlanned, StageParked, StageQueued:
		if doing > 0 {
			return StageInFlight, fmt.Sprintf("%d of its tasks are in progress", doing)
		}
	case StageInFlight:
		if doing == 0 && finished+review == total && review > 0 {
			return StageLanding, "no task is in progress, and the rest are in review or done"
		}
		if finished == total {
			return StageLanding, "all its tasks are done"
		}
	}
	return "", ""
}

func label(stage string) string {
	if l, ok := StageLabel[stage]; ok {
		return l
	}
	return stage
}

func (r reader) task(ctx context.Context, id string) (Task, error) {
	raw, err := r.call(ctx, "torque_task_get", map[string]any{"id": id, "format": "typed", "comments": "false"})
	if err != nil {
		return Task{}, err
	}
	var out struct {
		Data struct {
			ID        string `json:"id"`
			Title     string `json:"title"`
			Status    string `json:"status"`
			UpdatedAt string `json:"updated_at"`
			PhaseID   string `json:"phase_id"`
			ParentID  string `json:"parent_id"`
			EpicID    string `json:"epic_id"`
			Kind      string `json:"kind"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Data.ID != id {
		return Task{}, errors.New("torque answered, but not with the task")
	}
	return Task{ID: out.Data.ID, Title: out.Data.Title, Status: out.Data.Status, Updated: out.Data.UpdatedAt, PhaseID: out.Data.PhaseID, ParentID: out.Data.ParentID, EpicID: out.Data.EpicID, Kind: out.Data.Kind}, nil
}

func (r reader) epicTasks(ctx context.Context, epic string) ([]string, error) {
	raw, err := r.call(ctx, "torque_task_list", map[string]any{"epic_id": epic, "limit": maxTasks})
	if err != nil {
		return nil, err
	}
	var out struct {
		Data struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.New("torque answered, but not with a task list")
	}
	ids := make([]string, 0, len(out.Data.Items))
	for _, it := range out.Data.Items {
		if it.ID != "" {
			ids = append(ids, it.ID)
		}
	}
	return ids, nil
}

func (r reader) comments(ctx context.Context, task, ws string) ([]Entry, error) {
	raw, err := r.call(ctx, "torque_comment_list", map[string]any{
		"entity_type": "task", "entity_id": task, "limit": commentsPerTask, "sort_dir": "desc",
	})
	if err != nil {
		return nil, err
	}
	var out struct {
		Data struct {
			Items []struct {
				CreatedAt string `json:"created_at"`
				Excerpt   string `json:"excerpt"`
				Body      string `json:"body"`
				Author    string `json:"author"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.New("torque answered, but not with comments")
	}
	var es []Entry
	for _, c := range out.Data.Items {
		text := c.Excerpt
		if text == "" {
			text = c.Body
		}
		if !statusLine.MatchString(text) {
			continue
		}
		e := Entry{At: c.CreatedAt, Source: "comment", Summary: oneLine(text, 280), Task: task, Workstream: ws}
		if m := statusWord.FindStringSubmatch(text); m != nil {
			e.State = strings.ToLower(m[1])
		}
		es = append(es, e)
	}
	return es, nil
}

// status reads a status namespace, filtered to tasks when there are any.
func (r reader) status(ctx context.Context, ns string, tasks []string, ws string) ([]Entry, error) {
	if !validNS(ns) {
		return nil, fmt.Errorf("%q is not a status namespace", ns)
	}
	nsJSON, _ := json.Marshal([]string{ns})
	args := map[string]any{"namespaces": string(nsJSON), "limit": 50, "payload_mode": "full"}
	if len(tasks) > 0 {
		f, _ := json.Marshal([]map[string]any{{"field": "task", "values": tasks}})
		args["state_filters"] = string(f)
	}
	raw, err := r.call(ctx, "event_list", args)
	if err != nil {
		return nil, err
	}
	var out struct {
		Entries []struct {
			CreatedAt     string         `json:"created_at"`
			ConsumerState map[string]any `json:"consumer_state"`
			Payload       struct {
				Summary string `json:"summary"`
				Body    string `json:"body"`
			} `json:"payload"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.New("tesseract answered, but not with entries")
	}
	var es []Entry
	for _, e := range out.Entries {
		s := e.Payload.Summary
		if s == "" {
			s = e.Payload.Body
		}
		st, _ := e.ConsumerState["state"].(string)
		task, _ := e.ConsumerState["task"].(string)
		subj, _ := e.ConsumerState["subject_urn"].(string)
		es = append(es, Entry{At: e.CreatedAt, Source: "status", State: st, Summary: oneLine(s, 280), Task: task, SubjectURN: subj, Workstream: ws})
	}
	return es, nil
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// CountsText is a roll-up as a card says it: "2 doing · 1 review · 5 todo".
func CountsText(c map[string]int) string {
	order := []string{"doing", "in_progress", "review", "in_review", "blocked", "todo", "backlog", "queued", "done"}
	var parts []string
	for _, s := range order {
		if c[s] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c[s], s))
		}
	}
	var rest []string
	for s, n := range c {
		if n > 0 && !slices.Contains(order, s) {
			rest = append(rest, fmt.Sprintf("%d %s", n, s))
		}
	}
	sort.Strings(rest)
	return strings.Join(append(parts, rest...), " · ")
}

// ---- The cache ----

// activityFresh is how long a cached roll-up is used before it is read again.
const activityFresh = 60 * time.Second

func (s *Store) cachedRollup(ctx context.Context, id string) (Rollup, time.Time, bool) {
	var body, at string
	if s.db.QueryRowContext(ctx, `SELECT body, fetched_at FROM activity WHERE ws=?`, id).Scan(&body, &at) != nil {
		return Rollup{}, time.Time{}, false
	}
	var r Rollup
	if json.Unmarshal([]byte(body), &r) != nil {
		return Rollup{}, time.Time{}, false
	}
	t, _ := time.Parse(time.RFC3339, at)
	return r, t, true
}

func (s *Store) keepRollup(ctx context.Context, r Rollup) {
	b, err := json.Marshal(r)
	if err != nil {
		return
	}
	_, _ = s.db.ExecContext(ctx, `INSERT INTO activity(ws, body, fetched_at) VALUES(?,?,?)
		ON CONFLICT(ws) DO UPDATE SET body=excluded.body, fetched_at=excluded.fetched_at`, r.Workstream, string(b), now())
}

// Cached are the cached roll-ups of every workstream, for the board (no network).
func (s *Store) Cached(ctx context.Context) map[string]Rollup {
	out := map[string]Rollup{}
	rows, err := s.db.QueryContext(ctx, `SELECT ws, body FROM activity`)
	if err != nil {
		return out
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var ws, body string
		if rows.Scan(&ws, &body) != nil {
			continue
		}
		var r Rollup
		if json.Unmarshal([]byte(body), &r) == nil {
			out[ws] = r
		}
	}
	return out
}
