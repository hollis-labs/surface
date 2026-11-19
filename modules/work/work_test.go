package work

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
	"github.com/hollis-labs/surface/module/tetherlink"
)

// tether is Tether as the module sees it through tetherlink: Caster's tools over named,
// revisioned items, and Torque's task tools. It answers with the shapes the real tools do.
type tether struct {
	mu    sync.Mutex
	revs  map[string][]string // ns/name -> contents, revision i+1 at i
	calls []string
	puts  int
	tasks int
}

func newTether() *tether { return &tether{revs: map[string][]string{}} }

func (f *tether) seed(ns, name, content string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revs[ns+"/"+name] = append(f.revs[ns+"/"+name], content)
}

func (f *tether) latest(ns, name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	v := f.revs[ns+"/"+name]
	if len(v) == 0 {
		return ""
	}
	return v[len(v)-1]
}

func js(v any) string { b, _ := json.Marshal(v); return string(b) }

func (f *tether) Call(_ context.Context, tool string, args map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, tool)
	switch tool {
	case "caster_ls":
		ns := args["ns"].(string)
		items := []map[string]any{}
		for k, v := range f.revs {
			if n, ok := strings.CutPrefix(k, ns+"/"); ok {
				items = append(items, map[string]any{"ref": "caster://" + ns + "/" + n, "rev": len(v), "updated": "2026-11-15T20:55:23Z"})
			}
		}
		sort.Slice(items, func(i, j int) bool { return items[i]["ref"].(string) < items[j]["ref"].(string) })
		return js(map[string]any{"items": items}), nil
	case "caster_get":
		ref := strings.TrimPrefix(args["ref"].(string), "caster://")
		key, at, _ := strings.Cut(ref, "@")
		v := f.revs[key]
		rev := len(v)
		if at != "" {
			rev, _ = strconv.Atoi(at)
		}
		if rev == 0 || rev > len(v) {
			return js(map[string]any{"state": "not_found"}), nil
		}
		return js(map[string]any{"state": "resolved", "revision": strconv.Itoa(rev), "content": v[rev-1]}), nil
	case "caster_put":
		key := args["name"].(string)
		v := f.revs[key]
		if len(v) == 0 || v[len(v)-1] != args["content"] {
			f.revs[key] = append(v, args["content"].(string))
			f.puts++
		}
		return js(map[string]any{"ref": fmt.Sprintf("caster://%s@%d", key, len(f.revs[key]))}), nil
	case "caster_log":
		v := f.revs[args["name"].(string)]
		out := []map[string]any{}
		for i := len(v); i >= 1; i-- {
			out = append(out, map[string]any{"rev": i, "at": "2026-11-15T20:55:23Z"})
		}
		return js(map[string]any{"revisions": out}), nil
	case "torque_task_create":
		f.tasks++
		return js(map[string]any{"data": map[string]any{"ID": fmt.Sprintf("CW-TEST-%04d", f.tasks)}}), nil
	case "torque_task_get":
		return js(map[string]any{"data": map[string]any{"id": args["id"], "title": "t", "status": "todo"}}), nil
	}
	return "", tetherlink.ErrRefused
}

func (f *tether) called(tool string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == tool {
			n++
		}
	}
	return n
}

// The owner's Planner data as Basalisk wrote it: an index, a kind and two items.
const (
	portfolioIndex = "---\nwork: v1\nworkspace: portfolio\ntitle: Portfolio\napp: planner\npreset: planner\nns: w-portfolio\nowners: []\ncreated: \"2026-11-15T20:55:23Z\"\n---\nThe owner's portfolio plan.\n"
	taskKind       = "kind: task\nlabel: Task\nplural: Tasks\nicon: check\nstages: [proposed, planned, active, done, dropped]\nfields:\n  - key: due\n    label: Due\n    type: date\nsections: [What, Done when, Notes]\nchildren: []\n"
	planKind       = "kind: plan\nlabel: Plan\nstages: [proposed, planned, active, done, dropped]\nfields: []\nsections: [Summary, Notes]\nchildren: [task]\n"
	planItem       = "---\nwork: v1\nid: plan0001\nkind: plan\ntitle: The plan\nstage: active\nfields: {}\nparent: \"\"\nchildren: [task0001]\nlinks: []\nmaterial: []\npublished: []\ntags: []\nreviewer: someone # a key the engine does not know\n---\n## Summary\nWhat we are doing.\n"
	taskItem       = "---\nwork: v1\nid: task0001\nkind: task\ntitle: Write the port\nstage: planned\nfields: {due: 2026-11-20}\nparent: plan0001\nchildren: []\nlinks: [{rel: relates, to: \"torque://task/CW-20261119-0064\"}]\nmaterial: []\npublished: []\ntags: [surface]\n---\n## What\nPort the engine.\n\n## Done when\nThe tools work.\n"
)

func seeded() *tether {
	f := newTether()
	f.seed("work", "portfolio", portfolioIndex)
	f.seed("w-portfolio", "kind.task", taskKind)
	f.seed("w-portfolio", "kind.plan", planKind)
	f.seed("w-portfolio", "i.plan0001", planItem)
	f.seed("w-portfolio", "i.task0001", taskItem)
	return f
}

func start(t *testing.T, f *tether) *moduletest.Host {
	t.Helper()
	m := &mod{}
	if f != nil {
		m.caller = f
	}
	return moduletest.Start(t, m)
}

func call(t *testing.T, h *moduletest.Host, tool, input string) map[string]any {
	t.Helper()
	out, err := h.Tool(tool, input)
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(js(out)), &v); err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	return v
}

// Every tool Basalisk's page offered is here, with Basalisk's level, and every schema has
// properties (the Anthropic adapter drops a schema without).
func TestToolsAndLevels(t *testing.T) {
	want := map[string]module.Level{
		"work_list": module.LevelRead, "work_get": module.LevelRead, "work_history": module.LevelRead,
		"work_note": module.LevelWrite, "work_space_create": module.LevelWrite, "work_space_update": module.LevelWrite,
		"work_item_create": module.LevelWrite, "work_item_update": module.LevelWrite, "work_item_place": module.LevelWrite,
		"work_item_stage": module.LevelWrite, "work_item_restore": module.LevelWrite, "work_note_update": module.LevelWrite,
		"work_view_save":   module.LevelWrite,
		"work_item_delete": module.LevelConfirm, "work_space_delete": module.LevelConfirm, "work_note_delete": module.LevelConfirm,
	}
	tools := (&mod{}).Manifest().Tools
	if len(tools) != len(want) {
		t.Fatalf("%d tools, want %d", len(tools), len(want))
	}
	for _, tl := range tools {
		if lv, ok := want[tl.Name]; !ok || lv != tl.Level {
			t.Errorf("%s: level %s, want %s", tl.Name, tl.Level, lv)
		}
		var s map[string]any
		if err := json.Unmarshal(tl.Input, &s); err != nil {
			t.Fatalf("%s: schema: %v", tl.Name, err)
		}
		if _, ok := s["properties"].(map[string]any); !ok || s["type"] != "object" {
			t.Errorf("%s: schema without properties", tl.Name)
		}
		for _, r := range s["required"].([]any) {
			if _, ok := s["properties"].(map[string]any)[r.(string)]; !ok {
				t.Errorf("%s: requires %s, which it does not describe", tl.Name, r)
			}
		}
	}
}

// work_list and work_get read the owner's data in Caster as it is, and write nothing.
func TestReadsTheOwnersDataAndWriteNothing(t *testing.T) {
	f := seeded()
	h := start(t, f)
	out, err := h.Tool("work_list", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	list := out.([]map[string]any)
	if len(list) != 1 || list[0]["workspace"] != "portfolio" || list[0]["app"] != "planner" || list[0]["title"] != "Portfolio" {
		t.Fatalf("list %v", list)
	}
	sp := call(t, h, "work_list", `{"workspace":"portfolio"}`)
	items := sp["items"].([]any)
	if len(items) != 2 || len(sp["kinds"].([]any)) != 2 {
		t.Fatalf("space %v", sp)
	}
	sp = call(t, h, "work_list", `{"workspace":"portfolio","kind":"task"}`)
	task := sp["items"].([]any)[0].(map[string]any)
	if task["id"] != "task0001" || task["parent"] != "plan0001" || task["stage"] != "planned" || task["excerpt"] != "Port the engine." {
		t.Fatalf("task %v", task)
	}
	got := call(t, h, "work_get", `{"workspace":"portfolio","id":"task0001"}`)
	if got["rev"] != float64(1) || got["name"] != "i.task0001" || got["ref"] != "caster://w-portfolio/i.task0001" {
		t.Fatalf("get %v", got)
	}
	if got["text"] != "## What\n\nPort the engine.\n\n## Done when\n\nThe tools work.\n\n## Notes\n\n" {
		t.Errorf("text %q", got["text"])
	}
	if got["last_round"] != nil || got["signed_off"] != nil || len(got["draft_notes"].([]any)) != 0 {
		t.Errorf("rounds %v", got)
	}
	if links := got["links"].([]any); len(links) != 1 || links[0].(map[string]any)["to"] != "torque://task/CW-20261119-0064" {
		t.Errorf("links %v", got["links"])
	}
	hist := call(t, h, "work_history", `{"workspace":"portfolio","name":"i.task0001"}`)
	if len(hist["revisions"].([]any)) != 1 {
		t.Errorf("history %v", hist)
	}
	at := call(t, h, "work_history", `{"workspace":"portfolio","name":"i.task0001","rev":1}`)
	if at["title"] != "Write the port" || at["stage"] != "planned" {
		t.Errorf("at %v", at)
	}
	if f.called("caster_put") != 0 || f.puts != 0 {
		t.Fatalf("a read wrote to Caster: %v", f.calls)
	}
}

func TestWritesThroughToCaster(t *testing.T) {
	f := seeded()
	h := start(t, f)
	made := call(t, h, "work_item_create", `{"workspace":"portfolio","kind":"task","title":"Second","parent":"plan0001","sections":{"What":"More."},"tags":["x"]}`)
	id := made["id"].(string)
	if !ValidID(id) || !strings.Contains(f.latest("w-portfolio", "i.plan0001"), "children: [task0001, "+id+"]") {
		t.Fatalf("made %s; parent:\n%s", id, f.latest("w-portfolio", "i.plan0001"))
	}
	// The parent's unknown key survived the write.
	if !strings.Contains(f.latest("w-portfolio", "i.plan0001"), "reviewer: someone") {
		t.Errorf("an unknown key was lost:\n%s", f.latest("w-portfolio", "i.plan0001"))
	}
	if _, err := h.Tool("work_item_stage", `{"workspace":"portfolio","id":"`+id+`","stage":"published"}`); err == nil {
		t.Error("the agent published")
	}
	if _, err := h.Tool("work_item_create", `{"workspace":"portfolio","kind":"task","title":"x","stage":"published"}`); err == nil {
		t.Error("the agent made a published item")
	}
	st := call(t, h, "work_item_stage", `{"workspace":"portfolio","id":"task0001","stage":"active"}`)
	if st["rev"] != float64(2) || !strings.Contains(f.latest("w-portfolio", "i.task0001"), "stage: active") {
		t.Fatalf("stage %v\n%s", st, f.latest("w-portfolio", "i.task0001"))
	}
	// An edit on an older revision is not written over the newer one: it becomes a draft
	// suggestion.
	if _, err := h.Tool("work_item_update", `{"workspace":"portfolio","id":"task0001","base_rev":1,"sections":{"What":"Mine."}}`); err == nil || !strings.Contains(err.Error(), "changed in Caster meanwhile") {
		t.Fatalf("stale edit: %v", err)
	}
	got := call(t, h, "work_get", `{"workspace":"portfolio","id":"task0001"}`)
	if notes := got["draft_notes"].([]any); len(notes) != 1 || notes[0].(map[string]any)["replace"] != "Mine." {
		t.Fatalf("notes %v", got["draft_notes"])
	}
	up := call(t, h, "work_item_update", `{"workspace":"portfolio","id":"task0001","base_rev":2,"title":"Port it","links":[{"rel":"depends-on","to":"plan0001"}]}`)
	if up["rev"] != float64(3) || !strings.Contains(f.latest("w-portfolio", "i.task0001"), "title: Port it") {
		t.Fatalf("update %v", up)
	}
	note := call(t, h, "work_note", `{"workspace":"portfolio","id":"task0001","kind":"suggestion","section":"What","quote":"Port the engine.","replace":"Port all of it."}`)
	if !strings.HasPrefix(note["drafted"].(string), "n") {
		t.Fatalf("note %v", note)
	}
	if _, err := h.Tool("work_note", `{"workspace":"portfolio","id":"task0001","kind":"comment"}`); err == nil {
		t.Error("an empty comment was drafted")
	}
	call(t, h, "work_item_place", `{"workspace":"portfolio","id":"`+id+`","parent":""}`)
	if strings.Contains(f.latest("w-portfolio", "i.plan0001"), id) {
		t.Error("place did not take it out of its parent")
	}
	call(t, h, "work_item_delete", `{"workspace":"portfolio","id":"task0001"}`)
	if !strings.Contains(f.latest("w-portfolio", "i.task0001"), "cut: true") {
		t.Error("not cut")
	}
	if sp := call(t, h, "work_list", `{"workspace":"portfolio","include_cut":true,"kind":"task"}`); len(sp["items"].([]any)) != 2 {
		t.Errorf("include_cut %v", sp["items"])
	}
	call(t, h, "work_item_restore", `{"workspace":"portfolio","id":"task0001"}`)
	v := call(t, h, "work_view_save", `{"workspace":"portfolio","view":"board","title":"Tasks","kinds":["task"],"group_by":"stage"}`)
	if !strings.Contains(f.latest("w-portfolio", "v."+v["id"].(string)), "group_by: stage") {
		t.Errorf("view %v", v)
	}
	call(t, h, "work_space_update", `{"workspace":"portfolio","archived":true}`)
	if !strings.Contains(f.latest("work", "portfolio"), "archived: true") {
		t.Error("not archived")
	}
	sp := call(t, h, "work_space_create", `{"app":"writer","slug":"novel","title":"A novel"}`)
	if sp["workspace"] != "novel" || f.latest("work", "novel") == "" || f.latest("w-novel", "kind.scene") == "" {
		t.Fatalf("create %v", sp)
	}
	if n := len(h.Bus.Topics()); n == 0 || h.Bus.Topics()[0] != "work.changed" {
		t.Errorf("events %v", h.Bus.Topics())
	}
}

func TestHTTP(t *testing.T) {
	f := seeded()
	h := start(t, f)
	code, body := h.Do("GET", "/work?app=planner", "")
	var list struct {
		Workspaces []Workspace `json:"workspaces"`
		Presets    []Preset    `json:"presets"`
	}
	if code != 200 || json.Unmarshal([]byte(body), &list) != nil || len(list.Workspaces) != 1 || len(list.Presets) != 3 {
		t.Fatalf("%d %s", code, body)
	}
	code, body = h.Do("GET", "/work/portfolio", "")
	var open struct {
		Space     Space    `json:"space"`
		Relations []string `json:"relations"`
	}
	if code != 200 || json.Unmarshal([]byte(body), &open) != nil || len(open.Space.Items) != 2 || len(open.Relations) == 0 {
		t.Fatalf("%d %s", code, body)
	}
	code, body = h.Do("GET", "/work/portfolio/items/task0001", "")
	var d Detail
	if code != 200 || json.Unmarshal([]byte(body), &d) != nil || d.Sections["What"] != "Port the engine." {
		t.Fatalf("%d %s", code, body)
	}
	if code, body = h.Do("POST", "/work/portfolio/items", `{"kind":"task","title":"Via HTTP"}`); code != http.StatusCreated {
		t.Fatalf("%d %s", code, body)
	}
	if code, body = h.Do("PUT", "/work/portfolio/items/task0001", `{"base_rev":1,"title":"Mine"}`); code != 200 || !strings.Contains(body, `"rev":2`) {
		t.Fatalf("%d %s", code, body)
	}
	code, body = h.Do("PUT", "/work/portfolio/items/task0001", `{"base_rev":1,"title":"Again"}`)
	if code != http.StatusConflict || !strings.Contains(body, `"conflict":true`) || !strings.Contains(body, `"error"`) {
		t.Fatalf("%d %s", code, body)
	}
	if code, body = h.Do("POST", "/work/portfolio/items/task0001/torque", ""); code != 200 || !strings.Contains(body, `"created":true`) {
		t.Fatalf("%d %s", code, body)
	}
	if code, body = h.Do("POST", "/work/portfolio/items/task0001/torque", ""); code != 200 || !strings.Contains(body, `"created":false`) || f.tasks != 1 {
		t.Fatalf("%d %s", code, body)
	}
	if code, body = h.Do("GET", "/work/portfolio/items/task0001/torque", ""); code != 200 || !strings.Contains(body, `"status":"todo"`) {
		t.Fatalf("%d %s", code, body)
	}
	if code, body = h.Do("GET", "/work/portfolio/log?name=i.task0001", ""); code != 200 || !strings.Contains(body, "revisions") {
		t.Fatalf("%d %s", code, body)
	}
	if code, _ = h.Do("GET", "/work/nope", ""); code != 404 {
		t.Fatalf("missing workspace: %d", code)
	}
	if code, _ = h.Do("POST", "/work/portfolio/items", `{"kind":"task","bogus":1}`); code != 400 {
		t.Fatalf("unknown key: %d", code)
	}
}

// With no way to reach Tether, the tools say which setting to set.
func TestNotConfigured(t *testing.T) {
	h := start(t, nil)
	if _, err := h.Tool("work_list", `{}`); err == nil || !strings.Contains(err.Error(), "work.tether_command") {
		t.Fatalf("%v", err)
	}
	if code, body := h.Do("POST", "/work", `{"workspace":"x","preset":"planner"}`); code != http.StatusServiceUnavailable {
		t.Fatalf("%d %s", code, body)
	}
}

// What another writes in Caster (the team's answer to a round) reaches a window that has
// the workspace open, as work.changed, by the server's read-back; a workspace no window has
// open is not read.
func TestReadBackSaysWhatOthersChanged(t *testing.T) {
	f := seeded()
	m := &mod{caller: f}
	h := moduletest.Start(t, m)
	ctx := context.Background()
	if _, err := m.st.Refresh(ctx, IndexNS, true); err != nil {
		t.Fatal(err)
	}
	if _, err := m.st.Refresh(ctx, "w-portfolio", true); err != nil {
		t.Fatal(err)
	}
	m.readBack(ctx)
	if n := countTopic(h, "work.changed"); n != 0 {
		t.Fatalf("no window open, yet %d changes said", n)
	}
	if _, err := h.DeskState.Open(ctx, viewID, module.OpenOptions{Args: map[string]any{"workspace": "portfolio"}}); err != nil {
		t.Fatal(err)
	}
	m.readBack(ctx)
	if n := countTopic(h, "work.changed"); n != 0 {
		t.Fatalf("nothing moved, yet %d changes said", n)
	}
	f.seed("w-portfolio", "r.task0001.1", "---\nwork: v1\nround: 1\nitem: task0001\nnotes: [{id: n1, state: done, reply: fixed}]\n---\n")
	m.st.mu.Lock()
	delete(m.st.refreshed, "w-portfolio") // past the 15 s throttle
	m.st.mu.Unlock()
	m.readBack(ctx)
	if n := countTopic(h, "work.changed"); n != 1 {
		t.Fatalf("the team's answer was not said: %d", n)
	}
}

func countTopic(h *moduletest.Host, topic string) int {
	n := 0
	for _, t := range h.Bus.Topics() {
		if t == topic {
			n++
		}
	}
	return n
}
