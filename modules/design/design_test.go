package design

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
)

// fakeTether is an MCP server that records the tools called and checks their arguments the way
// Tether's own tools would.
type fakeTether struct {
	t     *testing.T
	mu    sync.Mutex
	calls []call
	token string
}

type call struct {
	Tool string
	Args map[string]any
}

func (f *fakeTether) handle(req map[string]any) (any, bool) {
	id, hasID := req["id"]
	method, _ := req["method"].(string)
	if !hasID {
		return nil, false // a notification
	}
	answer := func(result any) map[string]any { return map[string]any{"jsonrpc": "2.0", "id": id, "result": result} }
	switch method {
	case "initialize":
		return answer(map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "fake-tether"}}), true
	case "tools/call":
		p, _ := req["params"].(map[string]any)
		name, _ := p["name"].(string)
		args, _ := p["arguments"].(map[string]any)
		f.mu.Lock()
		f.calls = append(f.calls, call{name, args})
		f.mu.Unlock()
		text := func(s string, isErr bool) map[string]any {
			return answer(map[string]any{"content": []any{map[string]any{"type": "text", "text": s}}, "isError": isErr})
		}
		switch name {
		case "tether_message_send":
			for _, k := range []string{"from", "to", "kind", "payload_json"} {
				if s, _ := args[k].(string); s == "" {
					return text("missing "+k, true), true
				}
			}
			var body map[string]string
			if json.Unmarshal([]byte(args["payload_json"].(string)), &body) != nil || body["body"] == "" {
				return text("payload_json is not a JSON object with a body", true), true
			}
			return text(`{"ok":true,"message":{"id":"msg-1"}}`, false), true
		case "torque_task_create":
			if args["title"] == "" || args["project_id"] == "" || args["manual"] != true {
				return text("task needs title, project_id and manual=true", true), true
			}
			return text(`{"data":{"ID":"T-42","Title":"x"}}`, false), true
		case "tether_message_list": // Tether's own answer, as it came for design-d_03adcb6b
			return text(`{"count":1,"limit":100,"messages":[{"body":"Round one\npreview: http://laptop.test:8431/gallery.html","channel":"","consumed_at":null,"created_at":"2026-11-18T21:24:59.138753519Z","delivered_at":null,"from":"msg://test/designer","id":"01a1f08c","in_reply_to":"","kind":"response","metadata":{"from_unverified":"true","tether.message_id":"p35:x"},"payload":{"body":"Round one\npreview: http://laptop.test:8431/gallery.html","subject":"Round one"},"read_at":null,"subject":"Round one","thread_id":"design-d_03adcb6b","to":"msg://test/surface"}],"offset":0,"ok":true,"total":1}`, false), true
		case "tether_message_mark_read":
			return text(`{"ok":true}`, false), true
		case "torque_task_get": // Torque's legacy record: comments oldest to newest
			return text(`{"data":{"ID":"CW-1","Status":"review","CreatedAt":"2026-11-18T21:15:37Z","Comments":[{"author":"designer","content":"older","created_at":"2026-11-18T21:20:44Z","id":1},{"author":"designer","content":"newest","created_at":"2026-11-18T21:24:59Z","id":2}],"CommentsMeta":{"returned":2,"total":2}},"ok":true}`, false), true
		}
		return text("unknown tool "+name, true), true
	}
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32601, "message": "no method"}}, true
}

func (f *fakeTether) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if f.token != "" && r.Header.Get("Authorization") != "Bearer "+f.token {
		http.Error(w, "no", http.StatusUnauthorized)
		return
	}
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	out, ok := f.handle(req)
	if !ok {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Mcp-Session-Id", "s1")
	if req["method"] == "tools/call" { // answer this one as an event stream, as servers may
		w.Header().Set("Content-Type", "text/event-stream")
		b, _ := json.Marshal(out)
		_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", b)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (f *fakeTether) tools() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		out = append(out, c.Tool)
	}
	return out
}

// TestMain doubles as the stdio MCP server: the stdio transport runs this test binary.
func TestMain(m *testing.M) {
	if os.Getenv("DESIGN_FAKE_MCP") == "1" {
		f := &fakeTether{}
		in := bufio.NewScanner(os.Stdin)
		enc := json.NewEncoder(os.Stdout)
		for in.Scan() {
			var req map[string]any
			if json.Unmarshal(in.Bytes(), &req) != nil {
				continue
			}
			if out, ok := f.handle(req); ok {
				_ = enc.Encode(out)
			}
		}
		if p := os.Getenv("DESIGN_FAKE_LOG"); p != "" {
			b, _ := json.Marshal(f.calls)
			_ = os.WriteFile(p, b, 0o600) //nolint:gosec // the test names the file
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeGitHub serves the compare and pulls endpoints for one branch.
func fakeGitHub(t *testing.T, branch string, pr string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/o/r/compare/main..."+branch):
			_, _ = io.WriteString(w, `{"commits":[{"sha":"abc123","commit":{"message":"theme: warmer primary\n\nbody","author":{"date":"2026-11-18T10:00:00Z"}}}],"files":[{"filename":"registry/theme/surface.css"},{"filename":"web/src/gallery/controls.tsx"}]}`)
		case r.URL.Path == "/repos/o/r/pulls":
			if r.URL.Query().Get("head") != "o:"+branch {
				t.Errorf("pulls head = %q", r.URL.Query().Get("head"))
			}
			_, _ = io.WriteString(w, pr)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func start(t *testing.T) (*moduletest.Host, *mod) {
	t.Helper()
	m := &mod{gh: &github{client: http.DefaultClient}}
	h := moduletest.Start(t, m)
	return h, m
}

func set(t *testing.T, h *moduletest.Host, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		if err := h.Settings.Set(context.Background(), k, v); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRequestWithoutTetherStillMakesTheJob(t *testing.T) {
	h, _ := start(t)
	out, err := h.Tool("design_request", `{"title":"Warmer buttons","brief":"Make the primary button warmer.","references":"controls/button"}`)
	if err != nil {
		t.Fatal(err)
	}
	res := out.(map[string]any)
	j := res["job"].(Job)
	ob := res["outbox"].(Outbox)
	if ob.Sent || !strings.Contains(ob.Problem, "not configured") || !strings.Contains(ob.Manual, "Make the primary button warmer.") || !strings.Contains(ob.Manual, j.Branch) {
		t.Fatalf("fallback: %+v", ob)
	}
	if j.Status != StatusRequested || !strings.HasPrefix(j.Branch, "design/") || !strings.HasSuffix(j.Branch, "-warmer-buttons") || len(j.Rounds) != 1 || j.Rounds[0].Sent {
		t.Fatalf("job: %+v", j)
	}
	if w, ok := h.DeskState.Focused(context.Background()); !ok || w.View != "design.job" || w.Args["id"] != j.ID {
		t.Fatalf("window: %+v", w)
	}
	if !strings.Contains(strings.Join(h.Bus.Topics(), ","), "design.changed") {
		t.Fatal("no design.changed")
	}
}

func TestRequestOverHTTPMCP(t *testing.T) {
	f := &fakeTether{t: t, token: "sekrit"}
	srv := httptest.NewServer(f)
	defer srv.Close()
	tok := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tok, []byte("sekrit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, _ := start(t)
	set(t, h, map[string]string{"tether_url": srv.URL, "tether_token_file": tok, "sender_urn": "msg://test/surface", "designer_urn": "msg://test/designer", "torque_project": "PRJ-1"})
	out, err := h.Tool("design_request", `{"title":"Denser tables","brief":"Tighter rows."}`)
	if err != nil {
		t.Fatal(err)
	}
	ob := out.(map[string]any)["outbox"].(Outbox)
	j := out.(map[string]any)["job"].(Job)
	if !ob.Sent || ob.TaskID != "T-42" || ob.MessageID != "msg-1" || ob.Manual != "" || j.TaskID != "T-42" || !j.Rounds[0].Sent {
		t.Fatalf("outbox %+v job %+v", ob, j)
	}
	if got := strings.Join(f.tools(), ","); got != "torque_task_create,tether_message_send" {
		t.Fatalf("calls: %s", got)
	}
	msg := f.calls[1].Args
	if msg["from"] != "msg://test/surface" || msg["to"] != "msg://test/designer" || msg["kind"] != "request" || msg["thread_id"] != "design-"+j.ID {
		t.Fatalf("message args: %v", msg)
	}
	var body map[string]string
	_ = json.Unmarshal([]byte(msg["payload_json"].(string)), &body)
	for _, want := range []string{"Tighter rows.", j.Branch, "docs/gui.md", "make design-preview", "Merge the pull request yourselves", "T-42"} {
		if !strings.Contains(body["body"], want) {
			t.Errorf("the brief lacks %q:\n%s", want, body["body"])
		}
	}
	if task := f.calls[0].Args; task["project_id"] != "PRJ-1" || task["manual"] != true {
		t.Fatalf("task args: %v", task)
	}

	// A wrong token: refused, said plainly, the token never in the message.
	if err := os.WriteFile(tok, []byte("wrong"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _ = h.Tool("design_request", `{"title":"x","brief":"y"}`)
	ob = out.(map[string]any)["outbox"].(Outbox)
	if ob.Sent || !strings.Contains(ob.Problem, "refused the token") || strings.Contains(ob.Problem, "wrong") || ob.Manual == "" {
		t.Fatalf("refused: %+v", ob)
	}
}

func TestStdioMCP(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls.json")
	t.Setenv("DESIGN_FAKE_MCP", "1")
	t.Setenv("DESIGN_FAKE_LOG", log)
	c := &stdioMCP{argv: []string{os.Args[0]}, timeout: 20e9}
	d := mcpDispatcher{c}
	id, err := d.Send(context.Background(), Message{From: "a", To: "b", Kind: "notice", Subject: "s", Body: "hello", Thread: "t"})
	if err != nil || id != "msg-1" {
		t.Fatalf("send: %q %v", id, err)
	}
	var calls []call
	b, _ := os.ReadFile(log) //nolint:gosec // the test names the file
	_ = json.Unmarshal(b, &calls)
	if len(calls) != 1 || calls[0].Tool != "tether_message_send" || calls[0].Args["to"] != "b" {
		t.Fatalf("calls: %+v", calls)
	}
	if _, err := d.CreateTask(context.Background(), Task{Title: "t"}); err == nil || err.Error() != "tether: the call was refused" {
		t.Fatalf("a task with no project: %v", err)
	}
	if _, err := c.Call(context.Background(), "tether_message_delete", nil); err == nil || !strings.Contains(err.Error(), "not a tool the Design module calls") {
		t.Fatalf("a third tool was reachable: %v", err)
	}
	bad := mcpDispatcher{&stdioMCP{argv: []string{"/no/such/tether"}, timeout: 5e9}}
	if _, err := bad.Send(context.Background(), Message{}); err == nil || !strings.Contains(err.Error(), "did not start") {
		t.Fatalf("missing command: %v", err)
	}
}

// recorder is a Dispatcher that keeps what it was given.
type recorder struct {
	mu   sync.Mutex
	sent []Message
}

func (r *recorder) Send(_ context.Context, m Message) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, m)
	return fmt.Sprintf("m%d", len(r.sent)), nil
}
func (r *recorder) CreateTask(context.Context, Task) (string, error) { return "T-1", nil }

func TestNotesGoOutAsFeedbackAndApprovalSendsTheGoAhead(t *testing.T) {
	h, m := start(t)
	rec := &recorder{}
	m.dispatch = rec
	set(t, h, map[string]string{"torque_project": "PRJ-1"})
	out, err := h.Tool("design_request", `{"title":"Cards","brief":"Softer card borders."}`)
	if err != nil {
		t.Fatal(err)
	}
	j := out.(map[string]any)["job"].(Job)
	ctx := context.Background()
	anchor, _ := json.Marshal(Anchor{Section: "cards", Component: "card-notice"})
	if _, err = h.MarkStore.Add(ctx, module.Mark{Ref: ref(j.ID), Anchor: anchor, Body: "The border is still too dark.", Author: "owner"}); err != nil {
		t.Fatal(err)
	}
	// The job in front: its notes are in the slot's knowledge.
	k, _ := m.knowledge(ctx, h, module.Situation{Focused: &module.WindowContext{Ref: ref(j.ID)}})
	if !strings.Contains(k, "[cards / card-notice] The border is still too dark.") || !strings.Contains(k, "No preview URL yet") {
		t.Fatalf("knowledge:\n%s", k)
	}
	// The tools default to the job in front.
	res, err := h.Tool("design_feedback", `{"text":"Also check dark mode."}`)
	if err != nil {
		t.Fatal(err)
	}
	if r := res.(map[string]any); r["round"] != 2 || r["notes"] != 1 || !r["outbox"].(Outbox).Sent {
		t.Fatalf("feedback: %v", r)
	}
	last := rec.sent[len(rec.sent)-1]
	if !strings.Contains(last.Body, "Also check dark mode.") || !strings.Contains(last.Body, "- [cards / card-notice] The border is still too dark.") {
		t.Fatalf("round 2:\n%s", last.Body)
	}
	if marks, _ := h.MarkStore.List(ctx, ref(j.ID)); marks[0].Status != "resolved" {
		t.Fatalf("the note stayed open: %+v", marks[0])
	}
	if _, err = h.Tool("design_feedback", `{}`); err == nil {
		t.Fatal("an empty round was sent")
	}
	got, _ := m.st.get(ctx, j.ID)
	if got.Status != StatusChangesRequested || len(got.Rounds) != 2 {
		t.Fatalf("after feedback: %+v", got)
	}

	// Approval: the card is raised for the recorded head; the owner's answer settles it.
	head := "b600fc99dffb1c895c4b6d908e988378f632a467"
	pr := "https://github.com/hollis-labs/surface/pull/9"
	if _, err = m.st.update(ctx, j.ID, Change{HeadSHA: &head, PRURL: &pr}); err != nil {
		t.Fatal(err)
	}
	card, err := m.askApproval(ctx, j.ID, 60e9)
	if err != nil || card == nil || card.Kind != "approval" || !card.Blocking || card.Subject != ref(j.ID) {
		t.Fatalf("card: %+v %v", card, err)
	}
	rejected, _ := m.settle(ctx, j.ID, module.Card{Status: "submitted", Outcome: &module.CardOutcome{Decisions: []module.CardDecision{{ItemID: "approval", Action: "reject"}}}})
	if rejected["approved"] != false {
		t.Fatalf("reject: %v", rejected)
	}
	done, err := h.CardStore.Respond(ctx, card.ID, module.CardOutcome{Status: "submitted", Decisions: []module.CardDecision{{ItemID: "approval", Action: "approve"}}})
	if err != nil {
		t.Fatal(err)
	}
	ok, err := m.settle(ctx, j.ID, done)
	if err != nil || ok["approved"] != true || ok["status"] != StatusApproved {
		t.Fatalf("approve: %v %v", ok, err)
	}
	if last := rec.sent[len(rec.sent)-1]; last.Kind != "notice" || !strings.Contains(last.Body, "Merge that pull request") || !strings.Contains(last.Body, "\napproved-head: "+head+"\n") {
		t.Fatalf("go-ahead: %+v", last)
	}
	if again, _ := m.askApproval(ctx, j.ID, 60e9); again != nil {
		t.Fatal("an approved job was asked again")
	}
}

func TestProgressFromTheBranch(t *testing.T) {
	h, m := start(t)
	set(t, h, map[string]string{"repo": "o/r"})
	out, _ := h.Tool("design_request", `{"title":"Theme","brief":"Warmer."}`)
	j := out.(map[string]any)["job"].(Job)
	srv := fakeGitHub(t, j.Branch, `[{"number":7,"html_url":"https://example.test/pr/7","state":"open","draft":false,"title":"Warmer theme","merged_at":null}]`)
	m.gh.api = srv.URL

	v, err := m.view(context.Background(), j.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Progress.Exists || v.Progress.Head != "abc123" || v.Progress.HeadMessage != "theme: warmer primary" || len(v.Progress.Files) != 2 {
		t.Fatalf("progress: %+v", v.Progress)
	}
	if v.Status != StatusReady || v.PRURL != "https://example.test/pr/7" {
		t.Fatalf("status %q pr %q", v.Status, v.PRURL)
	}
	code, body := h.Do("GET", "/jobs/"+j.ID, "")
	if code != 200 || !strings.Contains(body, `"registry/theme/surface.css"`) {
		t.Fatal(code, body)
	}
	// A reviewer's status is kept until the pull request is merged.
	if advance(StatusChangesRequested, v.Progress) != StatusChangesRequested {
		t.Fatal("changes requested was overridden")
	}
	merged := Progress{PR: &PullRequest{State: "merged", HeadSHA: "abc"}}
	if advance(StatusApproved, merged) != StatusApproved || advance(StatusRequested, merged) != StatusRequested {
		t.Fatal("advance read a merged pull request")
	}
	merged.PR.URL = "https://github.com/o/r/pull/7"
	if ok, _ := mergedOnGitHub(Job{Status: StatusApproved, ApprovedHead: "abc", ApprovedPR: "https://github.com/o/r/pull/7"}, merged.PR, nil); !ok {
		t.Fatal("the approved commit merged is not merged")
	}
	if advance(StatusRequested, Progress{}) != StatusRequested {
		t.Fatal("moved with nothing pushed")
	}
}

func TestSetAndProbe(t *testing.T) {
	h, _ := start(t)
	out, _ := h.Tool("design_request", `{"title":"Probe","brief":"b"}`)
	j := out.(map[string]any)["job"].(Job)
	if _, err := h.Tool("design_set", `{"id":"`+j.ID+`","preview_url":"file:///etc/passwd"}`); err == nil {
		t.Fatal("a non-http preview URL was accepted")
	}
	if _, err := h.Tool("design_set", `{"id":"`+j.ID+`","status":"done"}`); err == nil {
		t.Fatal("an unknown status was accepted")
	}
	preview := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html></html>") }))
	defer preview.Close()
	if _, err := h.Tool("design_set", `{"id":"`+j.ID+`","preview_url":"`+preview.URL+`/gallery.html","base_url":"http://127.0.0.1:1/gallery.html"}`); err != nil {
		t.Fatal(err)
	}
	if code, body := h.Do("GET", "/jobs/"+j.ID+"/probe", ""); code != 200 || !strings.Contains(body, `"reachable":true`) {
		t.Fatal(code, body)
	}
	if code, body := h.Do("GET", "/jobs/"+j.ID+"/probe?which=base", ""); code != 200 || !strings.Contains(body, `"reachable":false`) || !strings.Contains(body, "does not answer") {
		t.Fatal(code, body)
	}
	if code, _ := h.Do("GET", "/jobs/d_nope", ""); code != 404 {
		t.Fatal("unknown job:", code)
	}
}

func TestBranchFor(t *testing.T) {
	if got := branchFor("d_12ab", "Warmer Buttons & Cards!"); got != "design/12ab-warmer-buttons-cards" {
		t.Fatal(got)
	}
	if got := branchFor("d_f336da25", "Warmer primary buttons, softer card borders"); got != "design/f336da25-warmer-primary-buttons-softer-card" {
		t.Fatal(got)
	}
	if got := branchFor("d_12ab", "!!!"); got != "design/12ab" {
		t.Fatal(got)
	}
}
