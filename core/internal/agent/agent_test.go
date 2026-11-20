package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/hollis-labs/libs/agent/chat"
	"github.com/hollis-labs/libs/agent/chat/chattest"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

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
	dataDir  string
	db       *sql.DB
	bus      *events.Bus
	desk     *fakeDesk
	tools    []kernel.BoundTool
	slots    []kernel.BoundSlot
	arounds  []kernel.BoundAround
	profiles []kernel.BoundProfile
	layout   string
	ran      []string
	cfg      mapConfig // nil: no settings store
}

func (k *fakeKernel) AppName() string       { return "surface-test" }
func (k *fakeKernel) DataDir() string       { return k.dataDir }
func (k *fakeKernel) DB() *sql.DB           { return k.db }
func (k *fakeKernel) Log() *slog.Logger     { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
func (k *fakeKernel) Desk() module.Desk     { return k.desk }
func (k *fakeKernel) Events() module.Events { return k.bus }
func (k *fakeKernel) Marks() module.Marks   { return nil }
func (k *fakeKernel) Cards() module.Cards   { return nil }
func (k *fakeKernel) Config(string) module.Config {
	if k.cfg == nil {
		return nil
	}
	return k.cfg
}

// mapConfig is a settings store for tests: agent_trace is on unless set.
type mapConfig map[string]any

func (m mapConfig) String(key string) string { v, _ := m[key].(string); return v }
func (m mapConfig) Bool(key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return key == "agent_trace"
}
func (m mapConfig) Int(key string) int                             { v, _ := m[key].(int); return v }
func (m mapConfig) Set(_ context.Context, key string, v any) error { m[key] = v; return nil }
func (m mapConfig) Watch(string, func(any)) func()                 { return func() {} }
func (k *fakeKernel) Tools() []kernel.BoundTool                    { return k.tools }
func (k *fakeKernel) Slots() []kernel.BoundSlot                    { return k.slots }
func (k *fakeKernel) Arounds() []kernel.BoundAround                { return k.arounds }
func (k *fakeKernel) Profiles() []kernel.BoundProfile              { return k.profiles }
func (k *fakeKernel) Commands() []module.Command                   { return nil }
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

type testTurnResult struct{ RunID, Halted string }

// sendTest waits for the scripted conversation service to release the run.
func sendTest(ctx context.Context, t *testing.T, s *Service, text, _, auto string) (testTurnResult, error) {
	t.Helper()
	before := len(s.wire.scripted.Recorded())
	view, err := s.wire.svc.EnsureConversation(ctx, s.wire.agent)
	if err != nil {
		return testTurnResult{}, err
	}
	turn := chat.Turn{Text: text}
	if auto != "" {
		turn.Meta, _ = json.Marshal(map[string]string{"auto": auto})
	}
	receipt, err := s.wire.svc.Submit(ctx, view.ConversationID, turn)
	res := testTurnResult{RunID: receipt.RunID}
	if err != nil {
		return res, err
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		view, err := s.wire.svc.EnsureConversation(ctx, s.wire.agent)
		if err != nil {
			return res, err
		}
		if view.ActiveRun == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("scripted run did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	if recorded := s.wire.scripted.Recorded(); len(recorded) > before {
		res.Halted = recorded[len(recorded)-1].Answer
	}
	return res, nil
}

func setup(t *testing.T, o Options) (*Service, *fakeKernel, *chattest.Scripted, *[]string) {
	t.Helper()
	if os.Getenv("SURFACE_AGENT_CONFIG") == "" { // a test may name its own config file first
		t.Setenv("SURFACE_AGENT_CONFIG", filepath.Join(t.TempDir(), "none.json"))
	}
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	calls := &[]string{}
	k := &fakeKernel{dataDir: t.TempDir(), db: db, bus: events.New(), desk: &fakeDesk{layouts: map[string]module.Layout{}}}
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
	t.Setenv("SURFACE_AGENT_RUNNER", RunnerScripted)
	t.Setenv("SURFACE_AGENT_SCRIPT_DELAY", "0s")
	s, err := NewWith(k, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	<-s.trained // the start-up training reads the trace; let it finish before the test writes
	return s, k, s.wire.scripted, calls
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
	res, err := sendTest(ctx, t, s, "tidy this", "c1", "")
	if err != nil || res.RunID == "" {
		t.Fatalf("send: %v %+v", err, res)
	}
	if got := describe(fe.Turns()[0].Context); !strings.Contains(got, "Slot: notes") {
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
	_, err := sendTest(ctx, t, s, "/open scratch", "c1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0] != `desk_open {"view":"scratch"}` {
		t.Fatalf("routed calls %v", *calls)
	}
	if !strings.Contains(describe(fe.Turns()[0].Context), "Already done for the owner") {
		t.Fatalf("block: %s", describe(fe.Turns()[0].Context))
	}
	_ = k
	// A writing tool is not run by a rule; the agent is told to call it.
	if err := s.router.Put(ctx, mustRule(`{"id":"jot","when":[{"field":"slash","op":"eq","value":"jot"}],"target":"tool:scratch_add","auto":true,"params":{"text":"{arg}"}}`)); err != nil {
		t.Fatal(err)
	}
	_, _ = sendTest(ctx, t, s, "/jot buy milk", "c1", "")
	if len(*calls) != 1 || !strings.Contains(describe(fe.Turns()[1].Context), "Call it.") {
		t.Fatalf("write route ran: %v %s", *calls, describe(fe.Turns()[1].Context))
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

	if _, err := sendTest(ctx, t, s, "summarize this", "c1", ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(describe(fe.Turns()[0].Context), "Reminder: Cite the note") {
		t.Fatalf("no reminder: %s", describe(fe.Turns()[0].Context))
	}
	// no-deploys is off until a layout that lists it is in use.
	if res, _ := sendTest(ctx, t, s, "deploy it", "c1", ""); res.Halted != "" {
		t.Fatal("halted while off")
	}
	k.desk.layouts["ops"] = module.Layout{Name: "ops", Setup: map[string]any{"reflexes": []any{"no-deploys"}, "instructions": "Ops only."}}
	k.layout = "ops"
	res, _ := sendTest(ctx, t, s, "deploy it", "c1", "")
	if res.Halted != "Not from here." || len(fe.Turns()) != 2 {
		t.Fatalf("halt: %+v, turns %d", res, len(fe.Turns()))
	}
	if !strings.Contains(s.Preview(ctx).Block, "For this layout: Ops only.") {
		t.Fatal("layout instructions missing")
	}
}

func TestApproverReplacesThePanelQuestion(t *testing.T) {
	ap := &countingApprover{allow: false}
	s, _, _, calls := setup(t, Options{Approver: ap})
	_ = s
	var write, read ToolBinding
	for _, et := range s.toolBindings() {
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
	for _, route := range []struct{ method, path string }{{"GET", ""}, {"GET", "/thread"}, {"DELETE", "/thread"}, {"POST", "/turns"}, {"GET", "/runs/old/events"}, {"POST", "/runs/old/approvals"}, {"POST", "/runs/old/cancel"}} {
		req, _ := http.NewRequest(route.method, srv.URL+Prefix+route.path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("retired route %s %s: %d", route.method, route.path, resp.StatusCode)
		}
	}
	resp, err := http.Get(srv.URL + "/api/agent/context")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("context: %v", err)
	}
	defer resp.Body.Close()
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

// With no runtime config the agent is not ready,
// and a turn says where to write one.
func TestAgentWithoutConfig(t *testing.T) {
	s, _, _, _ := setup(t, Options{})
	s.wire.close(context.Background())
	s.wire = nil
	mux := http.NewServeMux()
	s.Mount(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/agent/turns", "application/json", strings.NewReader(`{"text":"hi"}`))
	if err != nil || resp.StatusCode != 503 {
		t.Fatalf("turn: %v %v", err, resp.Status)
	}
	resp, err = http.Get(srv.URL + "/api/agent/setup")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("state: %v %v", err, resp.Status)
	}
	resp, err = http.Get(srv.URL + "/api/agent/thread")
	if err != nil || resp.StatusCode != 404 {
		t.Fatalf("thread: %v %v", err, resp.Status)
	}
}

func describe(raw json.RawMessage) string {
	var text string
	_ = json.Unmarshal(raw, &text)
	return text
}
