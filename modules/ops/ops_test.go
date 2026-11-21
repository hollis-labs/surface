package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/collab/collabtest"
	"github.com/hollis-labs/surface/module/moduletest"
	"github.com/hollis-labs/surface/module/tetherlink"
)

// tether is Tether as Ops sees it: collabtest's fake Caster, and Torque and Tesseract reads
// answered from fixtures in the real tools' shapes. Any other tool is an error.
type tether struct {
	*collabtest.Fake
	mu       sync.Mutex
	tasks    map[string][3]string   // id -> title, status, updated_at
	epics    map[string][]string    // epic -> task ids
	comments map[string][][2]string // task -> (created_at, excerpt)
	events   []map[string]any
	other    []string
	counts   map[string]int // non-Caster tool calls
	// beforeLatest, when set, runs before a read of a document's latest revision (a
	// caster_get with no @rev), with its key: a test writes there as someone else would.
	beforeLatest func(key string)
}

func newTether() *tether {
	return &tether{Fake: collabtest.NewFake(), tasks: map[string][3]string{}, epics: map[string][]string{}, comments: map[string][][2]string{}, counts: map[string]int{}}
}

func js(v any) string { b, _ := json.Marshal(v); return string(b) }

func (f *tether) Call(ctx context.Context, tool string, args map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !strings.HasPrefix(tool, "caster_") {
		f.counts[tool]++
		if err := ctx.Err(); err != nil {
			return "", err
		}
	}
	switch tool {
	case "torque_task_get":
		id, _ := args["id"].(string)
		t, ok := f.tasks[id]
		if !ok {
			return "", fmt.Errorf("no task %s", id)
		}
		return js(map[string]any{"ok": true, "data": map[string]any{"id": id, "title": t[0], "status": t[1], "updated_at": t[2]}}), nil
	case "torque_task_list":
		items := []map[string]any{}
		for _, id := range f.epics[fmt.Sprint(args["epic_id"])] {
			items = append(items, map[string]any{"id": id})
		}
		return js(map[string]any{"ok": true, "data": map[string]any{"items": items}}), nil
	case "torque_comment_list":
		items := []map[string]any{}
		for _, c := range f.comments[fmt.Sprint(args["entity_id"])] {
			items = append(items, map[string]any{"created_at": c[0], "excerpt": c[1]})
		}
		return js(map[string]any{"ok": true, "data": map[string]any{"items": items}}), nil
	case "event_list":
		return js(map[string]any{"entries": f.events}), nil
	}
	if strings.HasPrefix(tool, "caster_") {
		hook := f.beforeLatest
		f.mu.Unlock()
		defer f.mu.Lock()
		if ref, _ := args["ref"].(string); tool == "caster_get" && hook != nil && !strings.Contains(ref, "@") {
			hook(strings.TrimPrefix(ref, "caster://"))
		}
		return f.Fake.Call(ctx, tool, args)
	}
	f.other = append(f.other, tool)
	return "", fmt.Errorf("the fake has no tool %s", tool)
}

func start(t *testing.T, f *tether) (*moduletest.Host, *mod) {
	t.Helper()
	m := &mod{caller: f}
	return moduletest.Start(t, m), m
}

func call(t *testing.T, h *moduletest.Host, tool, input string) map[string]any {
	t.Helper()
	out, err := h.Tool(tool, input)
	if err != nil {
		t.Fatalf("%s %s: %v", tool, input, err)
	}
	var m map[string]any
	b, _ := json.Marshal(out)
	_ = json.Unmarshal(b, &m)
	return m
}

func queue(t *testing.T, m *mod) []string {
	t.Helper()
	b, err := m.st.All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return b.Queue
}

func TestAllowlistNeverLaunches(t *testing.T) {
	for _, tool := range forbidden {
		if allowed[tool] {
			t.Errorf("%s is on Ops' allowlist", tool)
		}
	}
	for tool := range allowed {
		for _, w := range []string{"launch", "dispatch", "claim", "team_", "_start"} {
			if strings.Contains(tool, w) {
				t.Errorf("%s looks like a launch tool", tool)
			}
		}
	}
}

func TestCreateWritesOpsV1AndQueueFollowsStage(t *testing.T) {
	f := newTether()
	h, m := start(t, f)
	call(t, h, "ops_create", `{"id":"alpha","title":"Alpha","owner":"owner","stage":"queued","links":[{"kind":"torque.task","id":"CW-20990101-0101","role":"task"}]}`)
	call(t, h, "ops_create", `{"id":"beta","title":"Beta","stage":"queued"}`)
	call(t, h, "ops_create", `{"id":"gamma","title":"Gamma"}`)

	doc, rev := f.Content("ops/ws.alpha")
	if rev != 1 || !strings.Contains(doc, "ops: v1") || !strings.Contains(doc, "stage: queued") || !strings.Contains(doc, "kind: torque.task") {
		t.Fatalf("alpha's document:\n%s", doc)
	}
	if q, _ := f.Content("ops/queue"); !strings.Contains(q, "order: [alpha, beta]") {
		t.Fatalf("queue: %s", q)
	}
	if got := queue(t, m); !slices.Equal(got, []string{"alpha", "beta"}) {
		t.Fatalf("queue = %v", got)
	}
	// Into the queue goes to the end; out of it leaves.
	call(t, h, "ops_move", `{"id":"gamma","stage":"queued","why":"ready"}`)
	call(t, h, "ops_move", `{"id":"alpha","stage":"in_flight","why":"launched"}`)
	if got := queue(t, m); !slices.Equal(got, []string{"beta", "gamma"}) {
		t.Fatalf("queue after moves = %v", got)
	}
	w, _, _ := m.st.Get(context.Background(), "alpha")
	if w.Stage != StageInFlight || len(w.Transitions) != 2 || w.Transitions[1].Why != "launched" || w.Transitions[1].From != StageQueued {
		t.Fatalf("alpha = %+v", w)
	}
	if _, err := h.Tool("ops_create", `{"id":"alpha","title":"Again"}`); err == nil {
		t.Fatal("a taken id was created again")
	}
	if _, err := h.Tool("ops_create", `{"id":"Bad Id","title":"x"}`); err == nil {
		t.Fatal("a bad id was taken")
	}
	if !slices.Contains(h.Bus.Topics(), "ops.changed") {
		t.Fatal("no ops.changed")
	}
}

func TestQueueReorder(t *testing.T) {
	f := newTether()
	h, m := start(t, f)
	for _, id := range []string{"one", "two", "three"} {
		call(t, h, "ops_create", fmt.Sprintf(`{"id":%q,"title":%q,"stage":"queued"}`, id, id))
	}
	call(t, h, "ops_queue_reorder", `{"id":"three","before":"one"}`)
	if got := queue(t, m); !slices.Equal(got, []string{"three", "one", "two"}) {
		t.Fatalf("before: %v", got)
	}
	call(t, h, "ops_queue_reorder", `{"id":"three","dir":"down"}`)
	if got := queue(t, m); !slices.Equal(got, []string{"one", "three", "two"}) {
		t.Fatalf("down: %v", got)
	}
	call(t, h, "ops_queue_reorder", `{"id":"one","after":"two"}`)
	if got := queue(t, m); !slices.Equal(got, []string{"three", "two", "one"}) {
		t.Fatalf("after: %v", got)
	}
	// The page's route does the same.
	code, body := h.Do("POST", "/queue/reorder", `{"id":"one","dir":"up"}`)
	if code != 200 || !strings.Contains(body, `["three","one","two"]`) {
		t.Fatalf("route: %d %s", code, body)
	}
	call(t, h, "ops_create", `{"id":"idle","title":"Idle"}`)
	if _, err := h.Tool("ops_queue_reorder", `{"id":"idle","before":"one"}`); err == nil {
		t.Fatal("a workstream not queued was ordered")
	}
}

func TestQueueHealsAHalfMove(t *testing.T) {
	f := newTether()
	h, m := start(t, f)
	call(t, h, "ops_create", `{"id":"one","title":"One","stage":"queued"}`)
	// Someone wrote a queued workstream straight to Caster, without the queue.
	f.Put("ops/ws.two", "---\nops: v1\nid: two\ntitle: Two\nstage: queued\nstage_since: 2026-11-20T00:00:00Z\n---\n", "Two", "")
	if _, err := m.st.Refresh(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if got := queue(t, m); !slices.Equal(got, []string{"one", "two"}) {
		t.Fatalf("queue = %v", got)
	}
}

func TestNesting(t *testing.T) {
	f := newTether()
	h, m := start(t, f)
	call(t, h, "ops_create", `{"id":"auth-identity-secrets","title":"Auth, identity and secrets","stage":"in_flight"}`)
	call(t, h, "ops_create", `{"id":"warden","title":"Warden","parent":"auth-identity-secrets","stage":"queued"}`)
	call(t, h, "ops_create", `{"id":"gatecrasher","title":"Gatecrasher","parent":"auth-identity-secrets","stage":"queued"}`)
	call(t, h, "ops_create", `{"id":"vault","title":"Vault","parent":"warden"}`)
	call(t, h, "ops_create", `{"id":"top-queued","title":"Top","stage":"queued"}`)

	b, _ := m.st.All(context.Background())
	concept, _ := b.Find("auth-identity-secrets")
	if !slices.Equal(concept.Children, []string{"gatecrasher", "warden"}) {
		t.Fatalf("children = %v", concept.Children)
	}
	var ds []string
	for _, d := range b.Descendants("auth-identity-secrets") {
		ds = append(ds, d.ID)
	}
	if !slices.Equal(ds, []string{"gatecrasher", "warden", "vault"}) {
		t.Fatalf("descendants = %v", ds)
	}
	// Loops are refused.
	if _, err := h.Tool("ops_update", `{"id":"auth-identity-secrets","parent":"vault"}`); err == nil || !strings.Contains(err.Error(), "loop") {
		t.Fatalf("a loop was made: %v", err)
	}
	if _, err := h.Tool("ops_create", `{"id":"orphan","title":"x","parent":"nope"}`); err == nil {
		t.Fatal("a missing parent was taken")
	}
	// The list shows the top level by default, and a concept's children on request.
	top := call(t, h, "ops_list", `{}`)
	if s := js(top["lanes"]); strings.Contains(s, `"id":"warden"`) || !strings.Contains(s, "auth-identity-secrets") {
		t.Fatalf("top level: %s", s)
	}
	inside := call(t, h, "ops_list", `{"parent":"auth-identity-secrets"}`)
	if q := js(inside["queue"]); q != `["warden","gatecrasher"]` {
		t.Fatalf("inside queue: %s", q)
	}
	// Reorder among siblings only.
	call(t, h, "ops_queue_reorder", `{"id":"gatecrasher","dir":"up"}`)
	inside = call(t, h, "ops_list", `{"parent":"auth-identity-secrets"}`)
	if q := js(inside["queue"]); q != `["gatecrasher","warden"]` {
		t.Fatalf("after reorder: %s", q)
	}
	if _, err := h.Tool("ops_queue_reorder", `{"id":"warden","before":"top-queued"}`); err == nil || !strings.Contains(err.Error(), "siblings") {
		t.Fatalf("reordered across levels: %v", err)
	}
	// Making a child top level.
	call(t, h, "ops_update", `{"id":"vault","parent":""}`)
	if w, _, _ := m.st.Get(context.Background(), "vault"); w.Parent != "" {
		t.Fatal("vault is still nested")
	}
}

func TestUpdateConflictAndKeepsUnknownKeys(t *testing.T) {
	f := newTether()
	h, m := start(t, f)
	f.Put("ops/ws.keep", "---\nops: v1\nid: keep\ntitle: Keep\nstage: planning\nextra_key: kept\nactive_when:\n  - {text: one, done: false}\n---\n## Purpose\nWhy.\n\n## Custom\nMine.\n", "Keep", "")
	_, _ = m.st.Refresh(context.Background(), true)
	call(t, h, "ops_update", `{"id":"keep","base_rev":1,"tick":{"index":0,"done":true},"held":"waiting on review","append_note":"first note"}`)
	doc, rev := f.Content("ops/ws.keep")
	for _, want := range []string{"extra_key: kept", "## Custom\nMine.", "done: true", "reason: waiting on review", "first note", "## Purpose\nWhy."} {
		if !strings.Contains(doc, want) {
			t.Errorf("missing %q in\n%s", want, doc)
		}
	}
	if rev != 2 {
		t.Fatalf("rev = %d", rev)
	}
	_, err := h.Tool("ops_update", `{"id":"keep","base_rev":1,"title":"Stale"}`)
	var c *Conflict
	if !errors.As(err, &c) || c.Rev != 2 {
		t.Fatalf("a stale update: %v", err)
	}
	code, _ := h.Do("PUT", "/workstreams/keep", `{"base_rev":1,"title":"Stale"}`)
	if code != 409 {
		t.Fatalf("route answered %d, not 409", code)
	}
	call(t, h, "ops_update", `{"id":"keep","held":""}`)
	if w, _, _ := m.st.Get(context.Background(), "keep"); w.Held != nil {
		t.Fatal("held was not cleared")
	}
}

func TestLinks(t *testing.T) {
	f := newTether()
	h, m := start(t, f)
	call(t, h, "ops_create", `{"id":"links","title":"Links"}`)
	call(t, h, "ops_link", `{"id":"links","link":{"kind":"registry.record","id":"workstream:01J0000000000000000000000","role":"identity"}}`)
	call(t, h, "ops_link", `{"id":"links","link":{"kind":"work.item","id":"garden/ab12cd34","role":"origin"}}`)
	if _, err := h.Tool("ops_link", `{"id":"links","link":{"kind":"torque.task","id":"not-a-task"}}`); err == nil {
		t.Fatal("a bad task id was linked")
	}
	if _, err := h.Tool("ops_link", `{"id":"links","link":{"kind":"tether.session.launch","id":"x"}}`); err == nil {
		t.Fatal("an unknown kind was linked")
	}
	call(t, h, "ops_unlink", `{"id":"links","kind":"work.item","ref":"garden/ab12cd34"}`)
	w, _, _ := m.st.Get(context.Background(), "links")
	if len(w.Links) != 1 || w.Links[0].Role != "identity" {
		t.Fatalf("links = %+v", w.Links)
	}
}

func TestRollupMergesAndRollsUpChildren(t *testing.T) {
	f := newTether()
	f.tasks["CW-20990101-0101"] = [3]string{"Parent task", "doing", "2026-11-20T10:00:00Z"}
	f.tasks["CW-20990101-0102"] = [3]string{"Child task", "review", "2026-11-20T11:00:00Z"}
	f.tasks["CW-20990101-0103"] = [3]string{"Epic child", "todo", "2026-11-20T09:00:00Z"}
	f.epics["EP-20990101-0001"] = []string{"CW-20990101-0103"}
	f.comments["CW-20990101-0102"] = [][2]string{
		{"2026-11-20T12:00:00Z", "STATUS milestone: PR opened"},
		{"2026-11-20T11:30:00Z", "a chatty comment that is not status"},
	}
	f.events = []map[string]any{{"created_at": "2026-11-20T12:30:00Z", "consumer_state": map[string]any{"state": "deployed", "task": "CW-20990101-0101"},
		"payload": map[string]any{"summary": "Deployed it"}}}
	h, m := start(t, f)
	call(t, h, "ops_create", `{"id":"concept","title":"Concept","stage":"planned","status_ns":"project/x/event/status","links":[{"kind":"torque.task","id":"CW-20990101-0101"},{"kind":"torque.epic","id":"EP-20990101-0001"}]}`)
	call(t, h, "ops_create", `{"id":"child","title":"Child","parent":"concept","links":[{"kind":"torque.task","id":"CW-20990101-0102"}]}`)

	out := call(t, h, "ops_get", `{"id":"concept"}`)
	tasks := out["tasks"].(map[string]any)
	if tasks["text"] != "1 doing · 1 review · 1 todo" {
		t.Fatalf("roll-up = %v", tasks["text"])
	}
	if out["suggested_stage"] == nil || !strings.HasPrefix(out["suggested_stage"].(string), "in_flight") {
		t.Fatalf("suggestion = %v", out["suggested_stage"])
	}
	act := call(t, h, "ops_activity", `{"id":"concept"}`)
	s := js(act["entries"])
	for _, want := range []string{"STATUS milestone: PR opened", "Deployed it", `"source":"ops"`, `"workstream":"child"`} {
		if !strings.Contains(s, want) {
			t.Errorf("activity lacks %s: %s", want, s)
		}
	}
	if strings.Contains(s, "chatty") {
		t.Error("a plain comment is in the feed")
	}
	var outside []string
	for _, e := range act["entries"].([]any) {
		if e := e.(map[string]any); e["source"] != "ops" {
			outside = append(outside, e["summary"].(string))
		}
	}
	if len(outside) < 4 || outside[0] != "Deployed it" || outside[1] != "STATUS milestone: PR opened" {
		t.Fatalf("not newest first: %v", outside)
	}
	// The board reads the cache only.
	code, body := h.Do("GET", "/board", "")
	if code != 200 || !strings.Contains(body, "1 doing · 1 review · 1 todo") {
		t.Fatalf("board: %d %s", code, body)
	}
	if len(f.other) > 0 {
		t.Fatalf("called tools the fake does not have: %v", f.other)
	}
	_ = m
}

func TestCite(t *testing.T) {
	f := newTether()
	h, m := start(t, f)
	call(t, h, "ops_create", `{"id":"cited","title":"Cited","stage":"in_flight","purpose":"Because."}`)
	var kind module.RefKind
	for _, k := range h.Manifest().Refs {
		if k.Kind == "ops.workstream" {
			kind = k
		}
	}
	got, err := kind.Resolve(context.Background(), h, []string{"cited", "missing"})
	if err != nil || len(got) != 1 {
		t.Fatalf("resolve: %v %v", got, err)
	}
	e := got["cited"]
	if e.Status.Label != "In Flight" || e.Open.Command != "ops.open" || e.Description != "Because." {
		t.Fatalf("entity = %+v", e)
	}
	if _, err := kind.Record(context.Background(), h, "missing"); !errors.Is(err, module.ErrRefNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if !kind.ID.MatchString("cited") || kind.ID.MatchString("Not An Id") {
		t.Fatal("id pattern")
	}
	_ = m
}

func TestReadBackPicksUpOthersWrites(t *testing.T) {
	f := newTether()
	_, m := start(t, f)
	f.Put("ops/ws.from-task-ops", "---\nops: v1\nid: from-task-ops\ntitle: Written by Task Ops\nstage: planned\n---\n", "x", "")
	n, err := m.st.Refresh(context.Background(), true)
	if err != nil || n != 1 {
		t.Fatalf("refresh: %d %v", n, err)
	}
	if w, ok, _ := m.st.Get(context.Background(), "from-task-ops"); !ok || w.Stage != StagePlanned {
		t.Fatal("not in the copy")
	}
	// Within 15 s a refresh does nothing unless forced.
	f.Put("ops/ws.later", "---\nops: v1\nid: later\ntitle: Later\n---\n", "x", "")
	if n, _ := m.st.Refresh(context.Background(), false); n != 0 {
		t.Fatal("refreshed inside the window")
	}
}

func TestSeedPreviewMapsTheTracker(t *testing.T) {
	raw, err := os.ReadFile("testdata/tracker.json")
	if err != nil {
		t.Fatal(err)
	}
	tr, err := ParseTracker(raw)
	if err != nil {
		t.Fatal(err)
	}
	p := Preview(tr, "test", map[string]bool{"secret-shed": true})
	if len(p.Workstreams) != len(tr.Threads) {
		t.Fatalf("%d workstreams for %d threads", len(p.Workstreams), len(tr.Threads))
	}
	byName := map[string]SeedItem{}
	for _, w := range p.Workstreams {
		byName[w.Title] = w
		if !ValidID(w.ID) {
			t.Errorf("bad id %q", w.ID)
		}
	}
	want := map[string]string{"flight": StageInFlight, "queued": StageQueued, "review": StageLanding, "landed": StageLanding}
	for i, th := range tr.Threads {
		if got := p.Workstreams[i].Stage; got != want[th.Stage] {
			t.Errorf("%s: %s -> %s", th.Name, th.Stage, got)
		}
		ids := map[string]bool{}
		for _, row := range th.Tasks {
			for _, id := range expandTaskIDs(row[0].(string)) {
				ids[id] = true
			}
		}
		if len(p.Workstreams[i].Links) != len(ids) {
			t.Errorf("%s: %d links for %d tasks", th.Name, len(p.Workstreams[i].Links), len(ids))
		}
	}
	if got := expandTaskIDs("CW-20990101-0021..0025"); len(got) != 5 || got[4] != "CW-20990101-0025" {
		t.Fatalf("range: %v", got)
	}
	if n := len(byName["Seed catalog as source of truth"].Links); n != 6 {
		t.Fatalf("the range and a repeated id: %d links, want 6", n)
	}
	if w := byName["Secret shed"]; !w.Exists || w.Include {
		t.Fatalf("an existing id is not skipped: %+v", w)
	}
	if w := byName["Identity badges for garden robots"]; len(w.Problems) == 0 {
		t.Fatal("a thread with no owner is not flagged")
	}
	concepts := map[string][]string{}
	for _, c := range p.Concepts {
		if c.Accept {
			t.Errorf("concept %s is ticked by default", c.ID)
		}
		concepts[c.ID] = c.Members
	}
	auth := concepts["auth-identity-secrets"]
	for _, id := range []string{"approval-flow-for-the-demo-garden", "gatepost-auth-proxy", "identity-badges-for-garden-robots", "secret-shed"} {
		if !slices.Contains(auth, id) {
			t.Errorf("auth concept lacks %s: %v", id, auth)
		}
	}
	if chat := concepts["agent-runtime-and-chat"]; !slices.Contains(chat, "chat-wire-for-the-garden") {
		t.Errorf("chat concept: %v", chat)
	}
	if len(p.Followups) != len(tr.Followups) {
		t.Fatalf("%d follow-ups for %d", len(p.Followups), len(tr.Followups))
	}
	placed := map[int]string{}
	for _, f := range p.Followups {
		placed[f.Index] = f.Workstream
	}
	if placed[0] != "approval-flow-for-the-demo-garden" || placed[1] != "seed-catalog-as-source-of-truth" || placed[3] != "chat-wire-for-the-garden" {
		t.Fatalf("follow-ups naming tasks: %v", placed)
	}
	if placed[2] != "" || placed[5] != "" {
		t.Fatalf("a follow-up naming nothing was placed: %v", placed)
	}
}

func TestSlugs(t *testing.T) {
	if id, ok := slugOf("Chat wire / Chatstream -- "); !ok || id != "chat-wire-chatstream" {
		t.Fatalf("slug: %q %v", id, ok)
	}
	id, ok := slugOf("工作流")
	if ok || !ValidID(id) {
		t.Fatalf("a name with no ASCII: %q %v", id, ok)
	}
	long, _ := slugOf(strings.Repeat("abcdefgh-", 10))
	if !ValidID(long) || strings.HasSuffix(long, "-") || len(long) > 48 {
		t.Fatalf("long: %q", long)
	}
	if ValidID("ends-with-") || ValidID("ab") || !ValidID("abc") {
		t.Fatal("id pattern")
	}
	p := Preview(Tracker{Threads: []TrackerThread{{Name: "工作流", Stage: "flight", Owner: "x"}}}, "t", nil)
	if len(p.Workstreams[0].Problems) == 0 {
		t.Fatal("a made-up id is not flagged")
	}
}

func seedPlan(t *testing.T, h *moduletest.Host, m *mod) SeedPlan {
	t.Helper()
	raw, _ := os.ReadFile("testdata/tracker.json")
	out := call(t, h, "ops_seed", js(map[string]any{"json": string(raw)}))
	p, ok, err := m.st.Plan(context.Background(), out["preview"].(string))
	if err != nil || !ok {
		t.Fatal("no plan kept")
	}
	return p
}

func TestSeedPreviewWritesNothingAndApplyNests(t *testing.T) {
	f := newTether()
	h, m := start(t, f)
	p := seedPlan(t, h, m)
	for _, c := range f.Calls {
		if c == "caster_put" {
			t.Fatal("the preview wrote to Caster")
		}
	}
	if ws := h.DeskState.Windows(context.Background()); len(ws) != 1 || ws[0].View != viewSeed {
		t.Fatalf("the preview did not open: %+v", ws)
	}
	// The owner ticks the auth concept only and drops the last thread, and saves.
	for i := range p.Concepts {
		p.Concepts[i].Accept = p.Concepts[i].ID == "auth-identity-secrets"
	}
	p.Workstreams[len(p.Workstreams)-1].Include = false
	if code, body := h.Do("PUT", "/seed/"+p.ID, js(p)); code != 200 {
		t.Fatalf("save: %d %s", code, body)
	}
	// The apply takes no plan from the request: a body naming another thread is ignored.
	code, body := h.Do("POST", "/seed/"+p.ID+"/apply", "")
	if code != 200 {
		t.Fatalf("apply: %d %s", code, body)
	}
	var res SeedResult
	_ = json.Unmarshal([]byte(body), &res)
	if len(res.Failed) > 0 || len(res.Created) != len(p.Workstreams) /* minus one dropped, plus one concept */ {
		t.Fatalf("result: %+v", res)
	}
	b, _ := m.st.All(context.Background())
	concept, ok := b.Find("auth-identity-secrets")
	if !ok || len(concept.Children) != 4 {
		t.Fatalf("concept: %+v", concept)
	}
	if chat, _ := b.Find("chat-wire-for-the-garden"); chat.Parent != "" {
		t.Fatalf("chat nested under an unticked concept: %s", chat.Parent)
	}
	if w, _ := b.Find("approval-flow-for-the-demo-garden"); !strings.Contains(w.Notes, "CW-20990101-0001 tomorrow") {
		t.Fatalf("the follow-up is not on its workstream: %q", w.Notes)
	}
	if res.Followups == 0 || len(m.st.Followups(context.Background())) != res.Followups {
		t.Fatalf("owner's follow-ups: %d / %v", res.Followups, m.st.Followups(context.Background()))
	}
	// Applying twice is refused, and so is editing an applied preview.
	if code, _ := h.Do("POST", "/seed/"+p.ID+"/apply", ""); code != 409 {
		t.Fatalf("second apply: %d", code)
	}
	if code, _ := h.Do("PUT", "/seed/"+p.ID, js(p)); code != 409 {
		t.Fatalf("edit after apply: %d", code)
	}
}

func TestSeedClaimIsAtomic(t *testing.T) {
	f := newTether()
	h, m := start(t, f)
	p := seedPlan(t, h, m)
	ctx := context.Background()
	if err := m.st.ClaimPlan(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.st.ClaimPlan(ctx, p.ID); !errors.Is(err, ErrApplied) {
		t.Fatalf("a second claim: %v", err)
	}
	if err := m.st.SavePlan(ctx, p); !errors.Is(err, ErrApplied) {
		t.Fatalf("a save over a claim: %v", err)
	}
	// An apply that writes nothing gives the claim back.
	_ = m.st.ReleasePlan(ctx, p.ID)
	f.Down = true
	if code, _ := h.Do("POST", "/seed/"+p.ID+"/apply", ""); code == 200 {
		t.Fatal("applied with Caster down")
	}
	f.Down = false
	if code, body := h.Do("POST", "/seed/"+p.ID+"/apply", ""); code != 200 {
		t.Fatalf("the claim was not given back: %d %s", code, body)
	}
}

func TestSeedFollowupsReachExistingWorkstreams(t *testing.T) {
	f := newTether()
	h, m := start(t, f)
	call(t, h, "ops_create", `{"id":"chat-wire-for-the-garden","title":"Chat wire, made earlier"}`)
	p := seedPlan(t, h, m)
	if code, body := h.Do("POST", "/seed/"+p.ID+"/apply", ""); code != 200 {
		t.Fatalf("apply: %d %s", code, body)
	}
	w, _, _ := m.st.Get(context.Background(), "chat-wire-for-the-garden")
	if w.Title != "Chat wire, made earlier" || !strings.Contains(w.Notes, "retries too often") {
		t.Fatalf("the existing workstream: %q / %q", w.Title, w.Notes)
	}
}

func TestReadTrackerPath(t *testing.T) {
	dir := t.TempDir()
	good := dir + "/tracker.json"
	raw, _ := os.ReadFile("testdata/tracker.json")
	if err := os.WriteFile(good, raw, 0o600); err != nil { //nolint:gosec // the test's own temp dir
		t.Fatal(err)
	}
	if b, err := ReadTracker(good); err != nil || len(b) != len(raw) {
		t.Fatalf("a good path: %v", err)
	}
	link := dir + "/link.json"
	_ = os.Symlink(good, link)
	big := dir + "/big.json"
	_ = os.WriteFile(big, make([]byte, maxTracker+1), 0o600)
	_ = os.Mkdir(dir+"/adir.json", 0o700)
	for _, p := range []string{link, big, dir + "/missing.json", dir + "/adir.json", "relative.json", dir + "/tracker.txt"} {
		_, err := ReadTracker(p)
		if err == nil || err.Error() != errTrackerPath.Error() {
			t.Errorf("%s: %v", p, err)
		}
	}
}

func TestOutsideClientsCannotChangeThroughRoutes(t *testing.T) {
	f := newTether()
	h, m := start(t, f)
	call(t, h, "ops_create", `{"id":"one","title":"One","stage":"queued"}`)
	p := seedPlan(t, h, m)
	do := func(a module.Actor, method, path, body string) int {
		req := httptest.NewRequest(method, "/api/m/ops"+path, strings.NewReader(body))
		req = req.WithContext(module.WithActor(req.Context(), a))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	for _, r := range [][3]string{
		{"POST", "/workstreams", `{"id":"two","title":"Two"}`},
		{"PUT", "/workstreams/one", `{"held":"x"}`},
		{"POST", "/workstreams/one/move", `{"stage":"planned","why":"x"}`},
		{"POST", "/workstreams/one/links", `{"kind":"torque.task","id":"CW-20990101-0001"}`},
		{"DELETE", "/workstreams/one/links?kind=torque.task&ref=CW-20990101-0001", ""},
		{"POST", "/queue/reorder", `{"id":"one","dir":"up"}`},
		{"PUT", "/seed/" + p.ID, js(p)},
		{"POST", "/seed/" + p.ID + "/apply", ""},
		{"POST", "/seed/preview", `{"path":"/tmp/tracker.json"}`},
	} {
		for _, a := range []module.Actor{module.ActorOutside, module.ActorAgent} {
			if code := do(a, r[0], r[1], r[2]); code != 403 {
				t.Errorf("%s %s %s as %s: %d, want 403", r[0], r[1], r[2], a, code)
			}
		}
	}
	// Reads, and a preview from pasted JSON, stay open.
	if code := do(module.ActorOutside, "GET", "/board", ""); code != 200 {
		t.Errorf("board: %d", code)
	}
	raw, _ := os.ReadFile("testdata/tracker.json")
	if code := do(module.ActorOutside, "POST", "/seed/preview", js(map[string]string{"json": string(raw)})); code != 200 {
		t.Errorf("json preview: %d", code)
	}
	// The owner at the page may, and is recorded as such.
	if code := do(module.ActorOwner, "POST", "/workstreams/one/move", `{"stage":"planned","why":"x"}`); code != 200 {
		t.Fatalf("owner move: %d", code)
	}
	w, _, _ := m.st.Get(context.Background(), "one")
	if w.Transitions[len(w.Transitions)-1].By != "owner" {
		t.Fatalf("by: %+v", w.Transitions)
	}
	// The agent's seed tool takes no path.
	if _, err := h.Tool("ops_seed", `{"path":"/tmp/tracker.json"}`); err == nil {
		t.Fatal("ops_seed read a path")
	}
}

// theirs makes the next put of key land after someone else's write of content.
func theirs(f *tether, key, content string) {
	done := false
	f.BeforePut = func(k string) {
		if k == key && !done {
			done = true
			f.Put(key, content, "theirs", "")
		}
	}
}

func TestWritesSurviveARace(t *testing.T) {
	f := newTether()
	h, m := start(t, f)
	ctx := context.Background()
	call(t, h, "ops_create", `{"id":"race","title":"Race","active_when":[{"text":"one"}]}`)

	// A hold while someone renames it: both stand.
	cur, _ := f.Content("ops/ws.race")
	theirs(f, "ops/ws.race", strings.Replace(cur, "title: Race", "title: Renamed by them", 1))
	call(t, h, "ops_update", `{"id":"race","held":"waiting"}`)
	doc, _ := f.Content("ops/ws.race")
	if !strings.Contains(doc, "Renamed by them") || !strings.Contains(doc, "reason: waiting") {
		t.Fatalf("a write was lost:\n%s", doc)
	}

	// A replace (notes) over theirs is refused, and theirs is the latest again.
	w, _, _ := m.st.Get(ctx, "race")
	cur, _ = f.Content("ops/ws.race")
	mine := strings.Replace(cur, "title: Renamed by them", "title: Renamed again", 1)
	theirs(f, "ops/ws.race", mine)
	_, err := h.Tool("ops_update", js(map[string]any{"id": "race", "base_rev": w.Rev, "notes": "replaced"}))
	var c *Conflict
	if !errors.As(err, &c) {
		t.Fatalf("a replace over theirs: %v", err)
	}
	if doc, _ := f.Content("ops/ws.race"); doc != mine {
		t.Fatalf("theirs is not the latest:\n%s", doc)
	}

	// The queue: their append and my reorder both stand.
	f.BeforePut = nil
	call(t, h, "ops_create", `{"id":"q-one","title":"One","stage":"queued"}`)
	call(t, h, "ops_create", `{"id":"q-two","title":"Two","stage":"queued"}`)
	f.Put("ops/ws.q-three", "---\nops: v1\nid: q-three\ntitle: Three\nstage: queued\n---\n", "Three", "")
	q, _ := f.Content("ops/queue")
	theirs(f, "ops/queue", strings.Replace(q, "order: [q-one, q-two]", "order: [q-one, q-two, q-three]", 1))
	call(t, h, "ops_queue_reorder", `{"id":"q-two","before":"q-one"}`)
	if q, _ := f.Content("ops/queue"); !strings.Contains(q, "order: [q-two, q-one, q-three]") {
		t.Fatalf("queue: %s", q)
	}

	// A create that loses the race says the id is taken and leaves theirs.
	theirs(f, "ops/ws.taken", "---\nops: v1\nid: taken\ntitle: Theirs\n---\n")
	_, err = h.Tool("ops_create", `{"id":"taken","title":"Mine"}`)
	var tk *Taken
	if !errors.As(err, &tk) {
		t.Fatalf("create race: %v", err)
	}
	if doc, _ := f.Content("ops/ws.taken"); !strings.Contains(doc, "title: Theirs") {
		t.Fatalf("theirs was not put back:\n%s", doc)
	}

	// The owner's follow-up list: their line and mine.
	f.BeforePut = nil
	if err := m.st.addFollowups(ctx, []string{"first"}, "t"); err != nil {
		t.Fatal(err)
	}
	cur, _ = f.Content("ops/followups")
	theirs(f, "ops/followups", cur+"- theirs\n")
	if err := m.st.addFollowups(ctx, []string{"second"}, "t"); err != nil {
		t.Fatal(err)
	}
	if got := m.st.Followups(ctx); !slices.Equal(got, []string{"first", "theirs", "second"}) {
		t.Fatalf("follow-ups: %v", got)
	}
}

func TestReplaceNeedsBaseRev(t *testing.T) {
	f := newTether()
	h, _ := start(t, f)
	call(t, h, "ops_create", `{"id":"base","title":"Base","active_when":[{"text":"one"}]}`)
	for _, in := range []string{`{"id":"base","notes":"x"}`, `{"id":"base","purpose":"x"}`, `{"id":"base","active_when":[]}`,
		`{"id":"base","labels":["x"]}`, `{"id":"base","tick":{"index":0,"done":true}}`} {
		if _, err := h.Tool("ops_update", in); err == nil || !strings.Contains(err.Error(), "base_rev") {
			t.Errorf("%s: %v", in, err)
		}
	}
	call(t, h, "ops_update", `{"id":"base","held":"x","append_note":"y","owner":"z"}`)
	if _, err := h.Tool("ops_update", `{"id":"base","status_ns":"project/*/event/status"}`); err == nil {
		t.Error("a wildcard status namespace was taken")
	}
}

func TestOrphansLoopsAndDepthStayOnTheBoard(t *testing.T) {
	f := newTether()
	h, m := start(t, f)
	ctx := context.Background()
	put := func(id, parent string) {
		f.Put("ops/ws."+id, fmt.Sprintf("---\nops: v1\nid: %s\ntitle: %s\nparent: %s\nstage: planning\n---\n", id, id, parent), id, "")
	}
	put("orphan", "gone-away")
	put("loop-a", "loop-b")
	put("loop-b", "loop-a")
	_, _ = m.st.Refresh(ctx, true)
	b, _ := m.st.All(ctx)
	for _, id := range []string{"orphan", "loop-a"} {
		w, ok := b.Find(id)
		if !ok || w.Parent != "" || w.Problem == "" {
			t.Errorf("%s: %+v", id, w)
		}
	}
	// Lifting one of a loop breaks it: the other stays under it.
	if w, _ := b.Find("loop-b"); w.Parent != "loop-a" {
		t.Errorf("loop-b: %+v", w)
	}
	top := js(call(t, h, "ops_list", `{}`)["lanes"])
	for _, id := range []string{"orphan", "loop-a"} {
		if !strings.Contains(top, `"id":"`+id+`"`) {
			t.Errorf("%s is not listed at the top", id)
		}
	}
	if inside := js(call(t, h, "ops_list", `{"parent":"loop-a"}`)["lanes"]); !strings.Contains(inside, `"id":"loop-b"`) {
		t.Errorf("loop-b is not listed under loop-a: %s", inside)
	}
	// Depth counts the moved subtree: a chain of 4 under the end of a chain of 4 would sit 7
	// below the top.
	chain := func(prefix string, n int) {
		for i := 0; i < n; i++ {
			p := ""
			if i > 0 {
				p = fmt.Sprintf("%s%d", prefix, i-1)
			}
			call(t, h, "ops_create", js(map[string]any{"id": fmt.Sprintf("%s%d", prefix, i), "title": "x", "parent": p}))
		}
	}
	chain("deep-a", 4)
	chain("deep-b", 4)
	if _, err := h.Tool("ops_update", `{"id":"deep-b0","parent":"deep-a3"}`); err == nil || !strings.Contains(err.Error(), "nest at most") {
		t.Fatalf("too deep: %v", err)
	}
	call(t, h, "ops_update", `{"id":"deep-b1","parent":"deep-a3"}`) // 3 + 3 = 6
	b, _ = m.st.All(ctx)
	if n := len(b.Descendants("deep-a0")); n != 6 {
		t.Fatalf("descendants: %d", n)
	}
}

func TestRefreshKeepsTheCopyOnAnEmptyListing(t *testing.T) {
	f := newTether()
	h, m := start(t, f)
	call(t, h, "ops_create", `{"id":"kept","title":"Kept"}`)
	empty := newTether() // a gateway that lists nothing
	m.st.caster = GatewayCaster{Link: func() (tetherlink.Caller, error) { return empty, nil }}
	if _, err := m.st.Refresh(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := m.st.Get(context.Background(), "kept"); !ok {
		t.Fatal("an empty listing emptied the copy")
	}
}

func TestTaskReadsAreShared(t *testing.T) {
	f := newTether()
	f.tasks["CW-20990101-0001"] = [3]string{"Open", "doing", "2099-01-01T10:00:00Z"}
	f.tasks["CW-20990101-0002"] = [3]string{"Finished", "done", "2099-01-01 09:00:00"}
	f.comments["CW-20990101-0001"] = [][2]string{{"2099-01-01T11:00:00.5Z", "STATUS milestone: Deployed it"}}
	f.events = []map[string]any{{"created_at": "2099-01-01T11:00:01Z", "consumer_state": map[string]any{"state": "milestone", "task": "CW-20990101-0001"},
		"payload": map[string]any{"summary": "Deployed it"}}}
	h, _ := start(t, f)
	call(t, h, "ops_create", `{"id":"parent-ws","title":"P","status_ns":"project/garden/event/status","links":[{"kind":"torque.task","id":"CW-20990101-0002"}]}`)
	call(t, h, "ops_create", `{"id":"child-ws","title":"C","parent":"parent-ws","links":[{"kind":"torque.task","id":"CW-20990101-0001"}]}`)
	call(t, h, "ops_activity", `{"id":"child-ws","refresh":true}`)
	act := call(t, h, "ops_activity", `{"id":"parent-ws","refresh":true}`)
	if f.counts["torque_task_get"] != 2 {
		t.Fatalf("task reads: %d, want 2 (one each, shared)", f.counts["torque_task_get"])
	}
	if f.counts["torque_comment_list"] != 1 {
		t.Fatalf("comment reads: %d, want 1 (none for a finished task)", f.counts["torque_comment_list"])
	}
	var sources []string
	for _, e := range act["entries"].([]any) {
		sources = append(sources, e.(map[string]any)["source"].(string))
	}
	if slices.Contains(sources, "comment") {
		t.Fatalf("a STATUS comment mirroring a status entry was kept: %v", sources)
	}
	if sources[0] != "status" {
		t.Fatalf("not newest first by time: %v", sources)
	}
	since := call(t, h, "ops_activity", `{"id":"parent-ws","since":"2099-01-01T10:30:00Z","source":"torque"}`)
	if n := len(since["entries"].([]any)); n != 0 {
		t.Fatalf("since: %d torque entries after 10:30", n)
	}
}

// foreignSeq numbers the foreign writes, so no two write the same text (Caster keeps no new
// revision for a put of the text it has).
var foreignSeq atomic.Int64

// foreign makes the next n puts of key each land after someone else's write, built on the
// latest revision there, setting owner to f<seq>. It returns the texts they wrote.
func foreign(f *tether, key string, n int) *[]string {
	wrote := &[]string{}
	f.BeforePut = func(k string) {
		if k != key || len(*wrote) >= n {
			return
		}
		cur, _ := f.Content(key)
		d, err := ParseDoc(cur)
		if err != nil {
			return
		}
		_ = d.Set("owner", fmt.Sprintf("f%d", foreignSeq.Add(1)))
		*wrote = append(*wrote, d.String())
		f.Put(key, d.String(), "theirs", "")
	}
	return wrote
}

func TestRacingWritesReportWhatStands(t *testing.T) {
	f := newTether()
	h, m := start(t, f)
	ctx := context.Background()
	key := "ops/ws.multi"
	call(t, h, "ops_create", `{"id":"multi","title":"Multi","active_when":[{"text":"one"}]}`)
	held := func(r string) error {
		_, err := m.st.Update(ctx, "multi", 0, Patch{Held: &r})
		return err
	}

	// Two foreign writes before two of my puts: re-applied, mine stands on the last of theirs.
	wrote := foreign(f, key, 2)
	if err := held("first"); err != nil || len(*wrote) != 2 {
		t.Fatalf("re-applied twice: %v (%d foreign)", err, len(*wrote))
	}
	doc, _ := f.Content(key)
	lastOwner := regexp.MustCompile(`owner: f\d+`).FindString((*wrote)[1])
	if !strings.Contains(doc, "reason: first") || !strings.Contains(doc, lastOwner) {
		t.Fatalf("not both:\n%s", doc)
	}

	// Foreign writes before every one of my puts: I give up, the latest foreign revision is put
	// back, and the answer is a conflict naming it. (Those writers built on my earlier
	// revisions, as a real one would, so what they wrote may carry my change: that is the race
	// Caster's put cannot close, and Caster's history keeps every revision.)
	wrote = foreign(f, key, maxRetries+1)
	err := held("second")
	var c *Conflict
	if !errors.As(err, &c) {
		t.Fatalf("exhausted: %v", err)
	}
	doc, rev := f.Content(key)
	if doc != (*wrote)[len(*wrote)-1] || c.Rev != rev {
		t.Fatalf("after giving up, Caster holds (rev %d, reported %d):\n%s", rev, c.Rev, doc)
	}

	// A write that lands on the restore itself: theirs is put back on top.
	wrote = foreign(f, key, maxRetries+2)
	if err = held("third"); !errors.As(err, &c) {
		t.Fatalf("restore raced: %v", err)
	}
	doc, rev = f.Content(key)
	if doc != (*wrote)[len(*wrote)-1] || c.Rev != rev {
		t.Fatalf("after the raced restore (rev %d, reported %d):\n%s", rev, c.Rev, doc)
	}

	// Someone writes after my last put and before the restore: theirs is left alone.
	foreign(f, key, maxRetries+1)
	late := ""
	f.beforeLatest = func(k string) {
		if k == key && late == "" && f.Revs(key) > 0 {
			cur, _ := f.Content(key)
			if strings.Contains(cur, "reason: fourth") {
				late = strings.Replace(cur, "title: Multi", "title: Late writer", 1)
				f.Put(key, late, "late", "")
			}
		}
	}
	if err = held("fourth"); !errors.As(err, &c) || !strings.Contains(err.Error(), "someone else wrote after") {
		t.Fatalf("late writer: %v", err)
	}
	f.beforeLatest = nil
	if doc, _ = f.Content(key); doc != late {
		t.Fatalf("the late writer's revision was overwritten:\n%s", doc)
	}

	// A replace refused on re-apply, with a write landing on the restore: theirs on top.
	f.BeforePut = nil
	w, _, _ := m.st.Get(ctx, "multi")
	wrote = foreign(f, key, 2)
	notes := "mine"
	_, err = m.st.Update(ctx, "multi", w.Rev, Patch{Notes: &notes})
	if !errors.As(err, &c) {
		t.Fatalf("refused replace: %v", err)
	}
	doc, rev = f.Content(key)
	if doc != (*wrote)[1] || c.Rev != rev {
		t.Fatalf("after the refused replace (rev %d, reported %d):\n%s", rev, c.Rev, doc)
	}
	f.BeforePut = nil
}

func TestApplyThatWroteNothingCanRunAgain(t *testing.T) {
	f := newTether()
	h, m := start(t, f)
	p := seedPlan(t, h, m)
	f.Fail = func(tool string, _ map[string]any) bool { return tool == "caster_put" }
	if code, _ := h.Do("POST", "/seed/"+p.ID+"/apply", ""); code == 200 {
		t.Fatal("an apply whose every put failed answered 200")
	}
	f.Fail = nil
	code, body := h.Do("POST", "/seed/"+p.ID+"/apply", "")
	if code != 200 || !strings.Contains(body, `"created":["`) {
		t.Fatalf("second apply: %d %s", code, body)
	}
}

func TestCanceledReadsAreNotCached(t *testing.T) {
	f := newTether()
	f.tasks["CW-20990101-0001"] = [3]string{"Open", "doing", "2099-01-01T10:00:00Z"}
	r := reader{link: func() (tetherlink.Caller, error) { return f, nil }, cache: newTaskCache()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := r.read(ctx, "CW-20990101-0001"); got.err == nil {
		t.Fatal("a canceled read succeeded")
	}
	if _, ok := r.cache.get("CW-20990101-0001"); ok {
		t.Fatal("a canceled read was cached")
	}
	if got := r.read(context.Background(), "CW-20990101-0001"); got.err != nil || got.task.Status != "doing" {
		t.Fatalf("read after: %+v", got)
	}
}

func TestOnlyTheTopmostOrphanIsLifted(t *testing.T) {
	f := newTether()
	_, m := start(t, f)
	ctx := context.Background()
	put := func(id, parent string) {
		f.Put("ops/ws."+id, fmt.Sprintf("---\nops: v1\nid: %s\ntitle: %s\nparent: %s\nstage: planning\n---\n", id, id, parent), id, "")
	}
	put("mid", "gone-away")
	put("leaf", "mid")
	for i := 0; i <= maxDepth+2; i++ { // lvl-0 … lvl-8: lvl-7 sits 7 deep
		p := ""
		if i > 0 {
			p = fmt.Sprintf("lvl-%d", i-1)
		}
		put(fmt.Sprintf("lvl-%d", i), p)
	}
	_, _ = m.st.Refresh(ctx, true)
	b, _ := m.st.All(ctx)
	mid, _ := b.Find("mid")
	leaf, _ := b.Find("leaf")
	if mid.Parent != "" || !strings.Contains(mid.Problem, "ops:gone-away is not in Ops") {
		t.Fatalf("mid: %+v", mid)
	}
	if leaf.Parent != "mid" || leaf.Problem != "" {
		t.Fatalf("leaf left its parent: %+v", leaf)
	}
	d7, _ := b.Find("lvl-7")
	d8, _ := b.Find("lvl-8")
	d6, _ := b.Find("lvl-6")
	if d7.Parent != "" || !strings.Contains(d7.Problem, "7 deep") || d8.Parent != "lvl-7" || d6.Parent != "lvl-5" {
		t.Fatalf("depth: d6 %q d7 %q (%s) d8 %q", d6.Parent, d7.Parent, d7.Problem, d8.Parent)
	}
}
