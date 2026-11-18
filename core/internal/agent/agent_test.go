package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/hollis-labs/surface/core/internal/events"
	"github.com/hollis-labs/surface/core/internal/kernel"
	"github.com/hollis-labs/surface/core/internal/router"
	"github.com/hollis-labs/surface/core/internal/storage"
	"github.com/hollis-labs/surface/module"
)

// fakeDesk is a desk of windows with one in front.
type fakeDesk struct {
	module.Desk // unimplemented methods panic
	mu          sync.Mutex
	wins        []module.Window
	focus       string
	layouts     map[string]module.Layout
	opened      []string
}

func (d *fakeDesk) Windows(context.Context) []module.Window {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.wins)
}

func (d *fakeDesk) Focused(context.Context) (module.Window, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, w := range d.wins {
		if w.ID == d.focus {
			return w, true
		}
	}
	return module.Window{}, false
}

func (d *fakeDesk) Open(_ context.Context, view string, _ module.OpenOptions) (module.Window, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.opened = append(d.opened, view)
	w := module.Window{ID: "w-" + view, View: view, Module: view, Title: view}
	d.wins = append(d.wins, w)
	d.focus = w.ID
	return w, nil
}

func (d *fakeDesk) Layout(_ context.Context, name string) (module.Layout, bool, error) {
	l, ok := d.layouts[name]
	return l, ok, nil
}

func (d *fakeDesk) focusOn(id string) {
	d.mu.Lock()
	d.focus = id
	d.mu.Unlock()
}

// fakeKernel is the kernel with a notes and a scratch module.
type fakeKernel struct {
	db     *sql.DB
	bus    *events.Bus
	desk   *fakeDesk
	tools  []kernel.BoundTool
	slots  []kernel.BoundSlot
	layout string
	ran    []string
}

func (k *fakeKernel) AppName() string             { return "surface-test" }
func (k *fakeKernel) DataDir() string             { return "" }
func (k *fakeKernel) DB() *sql.DB                 { return k.db }
func (k *fakeKernel) Log() *slog.Logger           { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
func (k *fakeKernel) Desk() module.Desk           { return k.desk }
func (k *fakeKernel) Events() module.Events       { return k.bus }
func (k *fakeKernel) Marks() module.Marks         { return nil }
func (k *fakeKernel) Cards() module.Cards         { return nil }
func (k *fakeKernel) Config(string) module.Config { return nil }
func (k *fakeKernel) Tools() []kernel.BoundTool   { return k.tools }
func (k *fakeKernel) Slots() []kernel.BoundSlot   { return k.slots }
func (k *fakeKernel) Commands() []module.Command  { return nil }
func (k *fakeKernel) RunCommand(_ context.Context, id string, _ map[string]any) (any, error) {
	k.ran = append(k.ran, id)
	return nil, nil
}

func (k *fakeKernel) Situation(ctx context.Context) module.Situation {
	sit := module.Situation{Windows: k.desk.Windows(ctx), Layout: k.layout}
	if w, ok := k.desk.Focused(ctx); ok {
		sit.Focused = &module.WindowContext{Window: w.ID, View: w.View, Module: w.Module, Title: w.Title, Kind: "document", Ref: w.Module + ":" + w.Title}
	}
	return sit
}

func tool(name string, lvl module.Level, mod string, calls *[]string) kernel.BoundTool {
	return kernel.BoundTool{Tool: module.Tool{Name: name, Description: name, Level: lvl, Input: json.RawMessage(`{"type":"object"}`)}, Module: mod,
		Call: func(_ context.Context, in json.RawMessage) (any, error) {
			*calls = append(*calls, name+" "+string(in))
			return map[string]string{"ok": name}, nil
		}}
}

// fakeEngine records turns.
type fakeEngine struct {
	mu    sync.Mutex
	tools []EngineTool
	turns []Turn
}

func (e *fakeEngine) Ready() bool   { return true }
func (e *fakeEngine) Model() string { return "fake" }
func (e *fakeEngine) Close()        {}
func (e *fakeEngine) AddTools(t ...EngineTool) error {
	e.tools = append(e.tools, t...)
	return nil
}
func (e *fakeEngine) Start(_ context.Context, t Turn) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.turns = append(e.turns, t)
	return "run_1", nil
}
func (e *fakeEngine) History(context.Context, string) ([]Said, error) { return nil, nil }
func (e *fakeEngine) Handler(string, func(*http.Request) (string, error)) http.Handler {
	return http.NotFoundHandler()
}

func setup(t *testing.T, o Options) (*Service, *fakeKernel, *fakeEngine, *[]string) {
	t.Helper()
	t.Setenv("SURFACE_AGENT_CONFIG", filepath.Join(t.TempDir(), "none.json"))
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	calls := &[]string{}
	k := &fakeKernel{db: db, bus: events.New(), desk: &fakeDesk{layouts: map[string]module.Layout{}}}
	k.desk.wins = []module.Window{
		{ID: "w1", View: "notes", Module: "notes", Title: "ideas.md"},
		{ID: "w2", View: "scratch", Module: "scratch", Title: "Scratch"},
	}
	k.desk.focus = "w1"
	k.tools = []kernel.BoundTool{
		tool("notes_read", module.LevelRead, "notes", calls),
		tool("notes_write", module.LevelWrite, "notes", calls),
		tool("scratch_add", module.LevelWrite, "scratch", calls),
		tool("scratch_list", module.LevelRead, "scratch", calls),
		tool("desk_open", module.LevelAct, "desk", calls),
	}
	k.slots = []kernel.BoundSlot{
		{Slot: module.Slot{Name: "notes", About: "notes and what the owner keeps", Views: []string{"notes"}, Tools: []string{"notes_read", "notes_write"},
			Instructions: "Keep the owner's wording.", Where: "the notes folder"}, Module: "notes",
			Knowledge: func(_ context.Context, s module.Situation) (string, error) {
				return "Outline of " + s.Focused.Title, nil
			}},
		{Slot: module.Slot{Name: "scratch", About: "quick capture", Views: []string{"scratch"}, Tools: []string{"scratch_add", "scratch_list", "missing_tool"}}, Module: "scratch"},
	}
	fe := &fakeEngine{}
	if o.Engine == nil {
		o.Engine = fe
	}
	s, err := NewWith(k, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	return s, k, fe, calls
}

func TestFocusChangesSlotToolsAndBlock(t *testing.T) {
	s, k, _, _ := setup(t, Options{})
	ctx := context.Background()

	a := s.Preview(ctx)
	if a.Slot != "notes" || !slices.Equal(a.TopOfMind, []string{"notes_read", "notes_write"}) {
		t.Fatalf("notes in front: slot %q, top of mind %v", a.Slot, a.TopOfMind)
	}
	for _, want := range []string{"Slot: notes", "Keep the owner's wording.", "Outline of ideas.md", "Top of mind: notes_read, notes_write", `In front: "ideas.md"`} {
		if !strings.Contains(a.Block, want) {
			t.Errorf("notes block lacks %q:\n%s", want, a.Block)
		}
	}

	k.desk.focusOn("w2")
	b := s.Preview(ctx)
	if b.Slot != "scratch" || !slices.Equal(b.TopOfMind, []string{"scratch_add", "scratch_list"}) {
		t.Fatalf("scratch in front: slot %q, top of mind %v", b.Slot, b.TopOfMind)
	}
	if strings.Contains(b.Block, "Outline of") || !strings.Contains(b.Block, "Slot: scratch") {
		t.Errorf("scratch block:\n%s", b.Block)
	}

	// A window with no slot of its own keeps the slot where it was.
	k.desk.wins = append(k.desk.wins, module.Window{ID: "w3", View: "files", Module: "files", Title: "~"})
	k.desk.focusOn("w3")
	if c := s.Preview(ctx); c.Slot != "scratch" {
		t.Errorf("files in front: slot %q, want scratch kept", c.Slot)
	}
}

func TestSendCarriesTheBlockAndPinHolds(t *testing.T) {
	s, k, fe, _ := setup(t, Options{})
	ctx := context.Background()
	res, err := s.Send(ctx, "tidy this", "c1", "")
	if err != nil || res.RunID != "run_1" {
		t.Fatalf("send: %v %+v", err, res)
	}
	if got := describe(fe.turns[0].Context); !strings.Contains(got, "Slot: notes") {
		t.Fatalf("turn context: %s", got)
	}
	// The agent pins scratch; it holds while notes stays in front, and goes when the owner moves.
	if _, err := s.ownTool("context_switch").Call(ctx, json.RawMessage(`{"slot":"scratch"}`)); err != nil {
		t.Fatal(err)
	}
	if a := s.Preview(ctx); a.Slot != "scratch" || a.Why != "pinned" {
		t.Fatalf("pinned: %q %q", a.Slot, a.Why)
	}
	k.desk.focusOn("w2")
	s.Preview(ctx) // the worker does this on every desk event
	k.desk.focusOn("w1")
	if a := s.Preview(ctx); a.Slot != "notes" {
		t.Fatalf("after moving: %q", a.Slot)
	}
	// The note is kept and shown.
	if _, err := s.ownTool("context_note").Call(ctx, json.RawMessage(`{"note":"halfway through ideas.md"}`)); err != nil {
		t.Fatal(err)
	}
	if a := s.Preview(ctx); !strings.Contains(a.Block, "halfway through ideas.md") {
		t.Fatalf("note missing:\n%s", a.Block)
	}
}

func TestRouterRunsLookingCallsDirectly(t *testing.T) {
	s, k, fe, calls := setup(t, Options{})
	ctx := context.Background()
	res, err := s.Send(ctx, "/open scratch", "c1", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Routed == nil || res.Routed.Name != "desk_open" || len(*calls) != 1 || (*calls)[0] != `desk_open {"view":"scratch"}` {
		t.Fatalf("routed %+v, calls %v", res.Routed, *calls)
	}
	if !strings.Contains(describe(fe.turns[0].Context), "Already done for the owner") {
		t.Fatalf("block: %s", describe(fe.turns[0].Context))
	}
	_ = k
	// A writing tool is not run by a rule; the agent is told to call it.
	if err := s.router.Put(ctx, mustRule(`{"id":"jot","when":[{"field":"slash","op":"eq","value":"jot"}],"target":"tool:scratch_add","auto":true,"params":{"text":"{arg}"}}`)); err != nil {
		t.Fatal(err)
	}
	res, _ = s.Send(ctx, "/jot buy milk", "c1", "")
	if len(*calls) != 1 || res.Routed == nil || !strings.Contains(describe(fe.turns[1].Context), "Call it.") {
		t.Fatalf("write route ran: %v %s", *calls, describe(fe.turns[1].Context))
	}
}

func TestReflexesRemindHaltAndFollowTheLayout(t *testing.T) {
	s, k, fe, _ := setup(t, Options{})
	ctx := context.Background()
	put := func(js string) {
		t.Helper()
		if _, err := s.ownTool("agent_reflex_put").Call(ctx, json.RawMessage(js)); err != nil {
			t.Fatal(err)
		}
	}
	put(`{"name":"cite","trigger_kind":"text","trigger":{"pattern":"(?i)summar"},"action_kind":"remind","action":{"body":"Cite the note you summarize."},"enabled":true}`)
	put(`{"name":"no-deploys","trigger_kind":"text","trigger":{"pattern":"(?i)deploy"},"action_kind":"halt","action":{"message":"Not from here."}}`)

	if _, err := s.Send(ctx, "summarize this", "c1", ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(describe(fe.turns[0].Context), "Reminder: Cite the note") {
		t.Fatalf("no reminder: %s", describe(fe.turns[0].Context))
	}
	// no-deploys is off until a layout that lists it is in use.
	if res, _ := s.Send(ctx, "deploy it", "c1", ""); res.Halted != "" {
		t.Fatal("halted while off")
	}
	k.desk.layouts["ops"] = module.Layout{Name: "ops", Setup: map[string]any{"reflexes": []any{"no-deploys"}, "instructions": "Ops only."}}
	k.layout = "ops"
	res, _ := s.Send(ctx, "deploy it", "c1", "")
	if res.Halted != "Not from here." || len(fe.turns) != 2 {
		t.Fatalf("halt: %+v, turns %d", res, len(fe.turns))
	}
	if !strings.Contains(s.Preview(ctx).Block, "For this layout: Ops only.") {
		t.Fatal("layout instructions missing")
	}
}

func TestApproverReplacesThePanelQuestion(t *testing.T) {
	ap := &countingApprover{allow: false}
	s, _, fe, calls := setup(t, Options{Approver: ap})
	_ = s
	var write, read EngineTool
	for _, et := range fe.tools {
		switch et.Name {
		case "notes_write":
			write = et
		case "notes_read":
			read = et
		}
	}
	if write.Level != "act" || read.Level != "read" {
		t.Fatalf("levels: write %q read %q", write.Level, read.Level)
	}
	if _, err := write.Run(context.Background(), json.RawMessage(`{}`)); err == nil || ap.n != 1 || len(*calls) != 0 {
		t.Fatalf("declined write ran: %v, asked %d, calls %v", err, ap.n, *calls)
	}
	if _, err := read.Run(context.Background(), json.RawMessage(`{}`)); err != nil || ap.n != 1 {
		t.Fatalf("read asked: %v %d", err, ap.n)
	}
}

func TestMountServesTurnAndContext(t *testing.T) {
	s, _, _, _ := setup(t, Options{})
	mux := http.NewServeMux()
	s.Mount(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/agent/turns", "application/json", strings.NewReader(`{"text":"hi","client_id":"c"}`))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("turn: %v %v", err, resp.Status)
	}
	resp, err = http.Get(srv.URL + "/api/agent/context")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("context: %v %v", err, resp.Status)
	}
	var a struct {
		Slot string `json:"slot"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&a)
	if a.Slot != "notes" {
		t.Fatalf("context slot %q", a.Slot)
	}
}

type countingApprover struct {
	allow bool
	n     int
}

func (a *countingApprover) Approve(context.Context, Request) (bool, error) {
	a.n++
	return a.allow, nil
}

func (s *Service) ownTool(name string) kernel.BoundTool {
	for _, t := range s.ownTools() {
		if t.Name == name {
			return t
		}
	}
	panic(name)
}

func mustRule(js string) (r router.Rule) {
	if err := json.Unmarshal([]byte(js), &r); err != nil {
		panic(err)
	}
	return r
}

// The real engine (libs agent/host) over the core database: with no config it is not ready,
// and a turn says where to write one.
func TestHostEngineWithoutConfig(t *testing.T) {
	s, _, _, _ := setup(t, Options{Engine: nil})
	eng, err := newHostEngine(context.Background(), FileConfig{}, systemPrompt("t", ""), s.k.DB())
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	s.engine = eng
	mux := http.NewServeMux()
	s.Mount(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/agent/turns", "application/json", strings.NewReader(`{"text":"hi"}`))
	if err != nil || resp.StatusCode != 503 {
		t.Fatalf("turn: %v %v", err, resp.Status)
	}
	resp, err = http.Get(srv.URL + "/api/agent")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("state: %v %v", err, resp.Status)
	}
	resp, err = http.Get(srv.URL + "/api/agent/thread")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("thread: %v %v", err, resp.Status)
	}
}
