package frag

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
)

const testKey = "test-key-not-a-secret"

// fakeFE is a Fragments Engine's capture API: it checks the key, keeps what it was sent and
// answers each capture as FE does, unless a test queued another answer.
type fakeFE struct {
	mu        sync.Mutex
	accepts   []map[string]any
	completes []map[string]any
	reads     int
	auth      []string
	// next answers, by path kind ("accept", "complete"): a status to send instead.
	fail map[string][]int
	// badReceipt makes the accept answer name another capture.
	badReceipt bool
}

func (f *fakeFE) pop(kind string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	q := f.fail[kind]
	if len(q) == 0 {
		return 0
	}
	f.fail[kind] = q[1:]
	return q[0]
}

func (f *fakeFE) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+testKey {
		w.WriteHeader(401)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var in map[string]any
	_ = json.Unmarshal(body, &in)
	answer := func(v any) { w.Header().Set("Content-Type", "application/json"); _ = json.NewEncoder(w).Encode(v) }
	switch {
	case r.Method == "GET" && r.URL.Path == "/v1/capabilities":
		f.mu.Lock()
		f.reads++
		f.mu.Unlock()
		answer(map[string]any{"schema_version": "fe.capabilities.v1"})
	case r.Method == "POST" && r.URL.Path == "/v1/captures":
		if code := f.pop("accept"); code != 0 {
			w.WriteHeader(code)
			return
		}
		f.mu.Lock()
		f.accepts = append(f.accepts, in)
		bad := f.badReceipt
		f.mu.Unlock()
		id, _ := in["capture_id"].(string)
		if bad {
			id = "someone-else"
		}
		w.WriteHeader(201)
		answer(map[string]any{"schema_version": "fe.capture.result.v1", "capture_id": id, "fragment_id": "frag1", "fragment_revision_id": "rev1",
			"capture_attempt_id": "att1", "completion": "accepting", "asset_instructions": []any{}})
	case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/v1/captures/") && strings.HasSuffix(r.URL.Path, "/complete"):
		if code := f.pop("complete"); code != 0 {
			w.WriteHeader(code)
			return
		}
		f.mu.Lock()
		f.completes = append(f.completes, in)
		f.mu.Unlock()
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/captures/"), "/complete")
		answer(map[string]any{"schema_version": "fe.capture.status.v1", "capture_id": id, "fragment_id": "frag1", "fragment_revision_id": "rev1",
			"capture_attempt_id": "att1", "completion": "complete"})
	default:
		w.WriteHeader(404)
	}
}

type rig struct {
	t  *testing.T
	m  *Mod
	h  *moduletest.Host
	fe *fakeFE
}

// newRig starts the module with the worker driven by the test, a fake FE and a key file.
// Delivery stays as the module defaults it (off) unless on is true.
func newRig(t *testing.T, on bool) *rig {
	t.Helper()
	fe := &fakeFE{fail: map[string][]int{}}
	srv := httptest.NewServer(fe)
	t.Cleanup(srv.Close)
	m := &Mod{noLoop: true}
	h := moduletest.Start(t, m)
	key := filepath.Join(t.TempDir(), "fe-key")
	if err := os.WriteFile(key, []byte(testKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_ = h.Settings.Set(ctx, "fe_url", srv.URL)
	_ = h.Settings.Set(ctx, "fe_token_file", key)
	if on {
		_ = h.Settings.Set(ctx, "delivery", true)
	}
	return &rig{t: t, m: m, h: h, fe: fe}
}

func (r *rig) once() bool {
	r.t.Helper()
	did, err := r.m.w.once(context.Background())
	if err != nil {
		r.t.Fatalf("once: %v", err)
	}
	return did
}

func (r *rig) capture(text string, tags ...string) Capture {
	r.t.Helper()
	if tags == nil {
		tags = []string{}
	}
	c, _, err := r.m.capture(context.Background(), Payload{Content: text, Tags: tags})
	if err != nil {
		r.t.Fatalf("capture: %v", err)
	}
	return c
}

func (r *rig) get(seq int64) Capture {
	r.t.Helper()
	c, err := r.m.st.get(context.Background(), seq)
	if err != nil {
		r.t.Fatal(err)
	}
	return c
}

// due makes a capture that is backing off due now.
func (r *rig) due(seq int64) {
	_, _ = r.h.Store().Exec(`UPDATE submissions SET next_attempt=0 WHERE sequence=?`, seq)
}

func TestDeliveryIsOffByDefault(t *testing.T) {
	r := newRig(t, false)
	for _, s := range r.h.Manifest().Settings {
		if s.Key == "delivery" && s.Default != false {
			t.Fatalf("delivery defaults to %v", s.Default)
		}
		if (s.Key == "fe_url" || s.Key == "fe_token_file") && s.Default != "" {
			t.Fatalf("%s defaults to %v", s.Key, s.Default)
		}
	}
	c := r.capture("kept here only")
	if r.once() {
		t.Fatal("the worker delivered with delivery off")
	}
	if got := r.get(c.Sequence); got.State != stateQueued || got.MayHaveSent {
		t.Fatalf("capture is %+v", got)
	}
	if len(r.fe.auth) != 0 {
		t.Fatalf("FE was called %d times", len(r.fe.auth))
	}
	st, _ := r.m.status(context.Background())
	if st.Ready || !strings.Contains(st.Reason, "off") {
		t.Fatalf("status %+v", st)
	}
	// On, but with no address: still nothing.
	_ = r.h.Settings.Set(context.Background(), "delivery", true)
	_ = r.h.Settings.Set(context.Background(), "fe_url", "")
	if r.once() {
		t.Fatal("delivered with no address")
	}
}

func TestDeliverySendsTheManifest(t *testing.T) {
	r := newRig(t, true)
	// The owner is in a Notes window.
	w, _ := r.h.DeskState.Open(context.Background(), "notes", module.OpenOptions{Title: "Ideas"})
	r.h.DeskState.Contexts[w.ID] = module.WindowContext{Window: w.ID, View: "notes", Module: "notes", Title: "Ideas", Kind: "document", Ref: "notes:ideas.md"}

	c := r.capture("# Title\n\nbody  with  spaces\n", "idea", "later")
	if !r.once() {
		t.Fatal("nothing delivered")
	}
	got := r.get(c.Sequence)
	if got.State != stateSaved || got.Code != "confirmed" || got.FragmentID != "frag1" || got.FragmentRevisionID != "rev1" {
		t.Fatalf("after delivery: %+v", got)
	}
	if !strings.HasSuffix(got.FragmentURL, "/sysop/reader/frag1") {
		t.Fatalf("fragment url %q", got.FragmentURL)
	}
	if len(r.fe.accepts) != 1 || len(r.fe.completes) != 1 {
		t.Fatalf("accepts %d completes %d", len(r.fe.accepts), len(r.fe.completes))
	}
	man := r.fe.accepts[0]
	up, _ := man["capture_id"].(string)
	if man["schema_version"] != "fe.capture.v1" || !strings.HasPrefix(up, "surface-") || man["principal_id"] != "local-user" || man["captured_at"] == "" {
		t.Fatalf("manifest head: %v", man)
	}
	doc := man["document"].(map[string]any)
	if body := doc["content"].(map[string]any); body["body"] != "# Title\n\nbody  with  spaces\n" || body["format"] != "markdown" {
		t.Fatalf("content changed: %v", body)
	}
	if tags := man["tags"].([]any); len(tags) != 2 || tags[0] != "idea" || tags[1] != "later" {
		t.Fatalf("tags %v", tags)
	}
	src := man["source"].(map[string]any)
	if src["source_item_key"] != "note:"+up || src["segment_key"] != "root" || !strings.HasPrefix(src["submitted_url"].(string), "http://localhost/") {
		t.Fatalf("source %v", src)
	}
	prov := doc["extensions"].(map[string]any)["provider.surface"].(map[string]any)
	if prov["capture_id"] != c.CaptureID || prov["device_id"] != "owner" {
		t.Fatalf("provenance %v", prov)
	}
	raw, _ := json.Marshal(man)
	if strings.Contains(string(raw), "notes:ideas.md") || strings.Contains(string(raw), "Ideas") {
		t.Fatal("the owner's context was sent to FE")
	}
	if len(man["media"].([]any)) != 0 || len(man["annotations"].([]any)) != 0 {
		t.Fatal("media or annotations sent")
	}
	if r.fe.completes[0]["idempotency_key"] != "complete-"+up || r.fe.completes[0]["schema_version"] != "fe.capture.completion.v1" {
		t.Fatalf("completion %v", r.fe.completes[0])
	}
	for _, a := range r.fe.auth {
		if a != "Bearer "+testKey {
			t.Fatalf("authorization %q", a)
		}
	}
	// The owner's context stays with the row, and the key is nowhere in the store.
	if !strings.Contains(string(got.Context), "notes:ideas.md") {
		t.Fatalf("context %s", got.Context)
	}
	var n int
	_ = r.h.Store().QueryRow(`SELECT count(*) FROM submissions WHERE payload LIKE ? OR manifest LIKE ? OR receipt LIKE ? OR owner_context LIKE ?`,
		"%"+testKey+"%", "%"+testKey+"%", "%"+testKey+"%", "%"+testKey+"%").Scan(&n)
	if n != 0 {
		t.Fatal("the key was stored")
	}
	if r.once() {
		t.Fatal("a saved capture was sent again")
	}
}

func TestCaptureIdempotency(t *testing.T) {
	r := newRig(t, false)
	body := `{"capture_id":"frag-0123456789abcdef","content":"one","tags":["a"]}`
	code, out := r.h.Do("POST", "/captures", body)
	if code != http.StatusAccepted {
		t.Fatalf("first: %d %s", code, out)
	}
	code, out2 := r.h.Do("POST", "/captures", body)
	if code != http.StatusOK || !strings.Contains(out2, `"sequence":1`) {
		t.Fatalf("repeat: %d %s", code, out2)
	}
	code, out = r.h.Do("POST", "/captures", `{"capture_id":"frag-0123456789abcdef","content":"two","tags":["a"]}`)
	if code != http.StatusConflict {
		t.Fatalf("other text: %d %s", code, out)
	}
	if code, _ := r.h.Do("POST", "/captures", `{"capture_id":"frag-0123456789abcdef","content":"one","tags":["b"]}`); code != http.StatusConflict {
		t.Fatalf("other tags: %d", code)
	}
	// Without an id: one made from the text and tags, so the same capture is queued once.
	a := r.capture("same", "x")
	b := r.capture("same", "x")
	c := r.capture("same", "y")
	if a.Sequence != b.Sequence || a.Sequence == c.Sequence {
		t.Fatalf("ids: %v %v %v", a.CaptureID, b.CaptureID, c.CaptureID)
	}
	for _, bad := range []string{`{"content":"   "}`, `{"content":"x","tags":["` + strings.Repeat("t", 101) + `"]}`, `{"capture_id":"short","content":"x"}`} {
		if code, out := r.h.Do("POST", "/captures", bad); code != http.StatusBadRequest {
			t.Fatalf("%s: %d %s", bad, code, out)
		}
	}
}

func TestDraftKeepsItsIDUntilTheTextChanges(t *testing.T) {
	r := newRig(t, false)
	w, _ := r.h.DeskState.Open(context.Background(), "files", module.OpenOptions{Title: "Files"})
	r.h.DeskState.Contexts[w.ID] = module.WindowContext{Window: w.ID, View: "files", Module: "files", Title: "Files", Kind: "folder", Ref: "files:/src"}

	if code, out := r.h.Do("PUT", "/draft", `{"content":"hello","tags":"a, b"}`); code != 200 {
		t.Fatalf("draft: %d %s", code, out)
	}
	_, out := r.h.Do("GET", "/draft", "")
	if !strings.Contains(out, "files:/src") || !strings.Contains(out, `"content":"hello"`) {
		t.Fatalf("draft kept: %s", out)
	}
	// The draft's id stays while the text does: a commit whose answer was lost is the same capture.
	d, _ := r.m.st.draft(context.Background())
	d = d.withID()
	if d.withID().CaptureID != d.CaptureID {
		t.Fatal("id changed for the same text")
	}
	d2 := d
	d2.Content = "hello!"
	if d2.withID().CaptureID == d.CaptureID {
		t.Fatal("id kept for other text")
	}

	// Another window has the focus at save.
	w2, _ := r.h.DeskState.Open(context.Background(), "browser", module.OpenOptions{Title: "Page"})
	r.h.DeskState.Contexts[w2.ID] = module.WindowContext{Window: w2.ID, View: "browser", Module: "browser", Title: "Page", Kind: "url", URL: "https://example.test/"}
	code, out := r.h.Do("POST", "/draft/commit", "")
	if code != http.StatusAccepted {
		t.Fatalf("commit: %d %s", code, out)
	}
	var c Capture
	_ = json.Unmarshal([]byte(out), &c)
	if c.Content != "hello" || len(c.Tags) != 2 || c.Tags[1] != "b" {
		t.Fatalf("captured %+v", c)
	}
	var oc OwnerContext
	_ = json.Unmarshal(c.Context, &oc)
	if oc.Draft == nil || oc.Draft.Window.Ref != "files:/src" || oc.Save == nil || oc.Save.Window.URL != "https://example.test/" {
		t.Fatalf("context %s", c.Context)
	}
	if _, out := r.h.Do("GET", "/draft", ""); strings.Contains(out, "hello") {
		t.Fatalf("draft not cleared: %s", out)
	}
	if code, _ := r.h.Do("POST", "/draft/commit", ""); code != http.StatusBadRequest {
		t.Fatalf("empty commit: %d", code)
	}
	if !contains(r.h.Bus.Topics(), "frag.draft") || !contains(r.h.Bus.Topics(), "frag.outbox") {
		t.Fatalf("events %v", r.h.Bus.Topics())
	}
}

func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

func TestRetryAndBackoff(t *testing.T) {
	r := newRig(t, true)
	c := r.capture("flaky")
	r.fe.fail["accept"] = []int{503, 429}
	before := time.Now().Unix()
	r.once()
	got := r.get(c.Sequence)
	if got.State != stateQueued || got.Code != "service_unavailable" || got.Attempts != 1 || got.NextAttempt < before+5 || got.NextAttempt > before+6 {
		t.Fatalf("after 503: %+v", got)
	}
	if r.once() {
		t.Fatal("retried before the backoff")
	}
	r.due(c.Sequence)
	before = time.Now().Unix()
	r.once()
	got = r.get(c.Sequence)
	if got.Code != "service_busy" || got.Attempts != 2 || got.NextAttempt < before+10 {
		t.Fatalf("after 429: %+v", got)
	}
	// Accepted, then the completion fails: the next attempt completes the same capture and
	// does not send it again.
	r.fe.fail["complete"] = []int{502}
	r.due(c.Sequence)
	r.once()
	if got = r.get(c.Sequence); got.State != stateQueued || !got.MayHaveSent {
		t.Fatalf("after 502 on complete: %+v", got)
	}
	r.due(c.Sequence)
	r.once()
	if got = r.get(c.Sequence); got.State != stateSaved {
		t.Fatalf("not saved: %+v", got)
	}
	if len(r.fe.accepts) != 1 || len(r.fe.completes) != 1 {
		t.Fatalf("accepts %d completes %d", len(r.fe.accepts), len(r.fe.completes))
	}
	for i, want := range []int{5, 10, 20, 40, 80, 160, 300, 300} {
		if got := backoff(i); got != want {
			t.Fatalf("backoff(%d) = %d, want %d", i, got, want)
		}
	}
}

func TestReviewStates(t *testing.T) {
	cases := []struct {
		name string
		set  func(r *rig)
		code string
	}{
		{"auth", func(r *rig) { r.fe.fail["accept"] = []int{401} }, "authorization_required"},
		{"forbidden", func(r *rig) { r.fe.fail["accept"] = []int{403} }, "authorization_required"},
		{"conflict", func(r *rig) { r.fe.fail["accept"] = []int{409} }, "capture_conflict"},
		{"rejected", func(r *rig) { r.fe.fail["accept"] = []int{422} }, "capture_rejected"},
		{"receipt", func(r *rig) { r.fe.badReceipt = true }, "invalid_receipt"},
		{"key", func(r *rig) {
			_ = r.h.Settings.Set(context.Background(), "fe_token_file", filepath.Join(r.t.TempDir(), "missing"))
		}, "credential_unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, true)
			c := r.capture("needs review " + tc.name)
			tc.set(r)
			r.once()
			got := r.get(c.Sequence)
			if got.State != stateAttention || got.Code != tc.code {
				t.Fatalf("got %s/%s", got.State, got.Code)
			}
			r.due(c.Sequence)
			if r.once() {
				t.Fatal("a capture needing attention was sent again by itself")
			}
		})
	}
}

func TestRetryAfterReview(t *testing.T) {
	r := newRig(t, true)
	c := r.capture("rejected once")
	r.fe.fail["accept"] = []int{422}
	r.once()
	first, _ := r.h.Store().Query(`SELECT upstream_capture_id FROM submissions`)
	var up1 string
	for first.Next() {
		_ = first.Scan(&up1)
	}
	_ = first.Close()
	// A tool retries it: refused input is built again, under a new upstream id.
	if _, err := r.h.Tool("frag_retry", `{"id":1}`); err != nil {
		t.Fatal(err)
	}
	if got := r.get(c.Sequence); got.State != stateQueued || got.Code != "retry_requested" {
		t.Fatalf("after retry: %+v", got)
	}
	r.once()
	if got := r.get(c.Sequence); got.State != stateSaved {
		t.Fatalf("not saved: %+v", got)
	}
	if up2, _ := r.fe.accepts[0]["capture_id"].(string); up1 == "" || up2 == up1 {
		t.Fatalf("upstream ids %q %q", up1, up2)
	}
	// A saved capture cannot be retried; an unknown one is not found.
	if code, _ := r.h.Do("POST", "/outbox/1/retry", ""); code != http.StatusConflict {
		t.Fatalf("retry saved: %d", code)
	}
	if code, _ := r.h.Do("POST", "/outbox/99/retry", ""); code != http.StatusNotFound {
		t.Fatalf("retry missing: %d", code)
	}
}

func TestDiscardTombstone(t *testing.T) {
	r := newRig(t, true)
	// Before any send: discarded, text cleared.
	a := r.capture("never sent")
	_ = r.h.Settings.Set(context.Background(), "delivery", false)
	if code, out := r.h.Do("DELETE", "/outbox/1", ""); code != 200 {
		t.Fatalf("discard: %d %s", code, out)
	}
	got := r.get(a.Sequence)
	if got.State != stateDiscarded || got.Code != "owner_discarded" || got.Content != "" || got.Context != nil {
		t.Fatalf("discarded: %+v", got)
	}
	// The same capture again answers with the tombstone; it is not queued again.
	again := r.capture("never sent")
	if again.Sequence != a.Sequence || again.State != stateDiscarded {
		t.Fatalf("resurrected: %+v", again)
	}

	// After a possible send: a tombstone that says delivery was stopped.
	_ = r.h.Settings.Set(context.Background(), "delivery", true)
	b := r.capture("maybe sent")
	r.fe.fail["complete"] = []int{503}
	r.once()
	if code, _ := r.h.Do("DELETE", "/outbox/2", ""); code != 200 {
		t.Fatal("discard after send")
	}
	if got := r.get(b.Sequence); got.State != stateDiscarded || got.Code != "owner_stopped_delivery" {
		t.Fatalf("tombstone: %+v", got)
	}
	r.due(b.Sequence)
	if r.once() {
		t.Fatal("a discarded capture was delivered")
	}

	// Refused while delivering, and once saved.
	c := r.capture("in flight")
	_, _ = r.h.Store().Exec(`UPDATE submissions SET lease_until=? WHERE sequence=?`, time.Now().Unix()+60, c.Sequence)
	if code, _ := r.h.Do("DELETE", "/outbox/3", ""); code != http.StatusConflict {
		t.Fatalf("discard while delivering: %d", code)
	}
	_, _ = r.h.Store().Exec(`UPDATE submissions SET lease_until=0 WHERE sequence=?`, c.Sequence)
	r.once()
	if got := r.get(c.Sequence); got.State != stateSaved {
		t.Fatalf("not saved: %+v", got)
	}
	if code, _ := r.h.Do("DELETE", "/outbox/3", ""); code != http.StatusConflict {
		t.Fatalf("discard saved: %d", code)
	}
}

func TestToolsAndCheck(t *testing.T) {
	r := newRig(t, false)
	for _, tool := range r.h.Manifest().Tools {
		var schema map[string]any
		if err := json.Unmarshal(tool.Input, &schema); err != nil || schema["properties"] == nil {
			t.Fatalf("%s: schema without properties", tool.Name)
		}
	}
	levels := map[string]module.Level{"frag_capture": module.LevelAct, "frag_outbox": module.LevelRead, "frag_retry": module.LevelAct}
	for _, tool := range r.h.Manifest().Tools {
		if levels[tool.Name] != tool.Level {
			t.Fatalf("%s is %s", tool.Name, tool.Level)
		}
	}
	if _, err := r.h.Tool("frag_capture", `{"text":"from the agent","tags":["agent"]}`); err != nil {
		t.Fatal(err)
	}
	out, err := r.h.Tool("frag_outbox", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	if !strings.Contains(string(raw), "from the agent") || !strings.Contains(string(raw), `"delivery":false`) {
		t.Fatalf("outbox %s", raw)
	}
	if len(r.fe.auth) != 0 {
		t.Fatal("a tool reached FE with delivery off")
	}
	// The check reads, and works with delivery off; it sends nothing.
	code, body := r.h.Do("POST", "/check", "")
	if code != 200 || !strings.Contains(body, `"ok":true`) || r.fe.reads != 1 || len(r.fe.accepts) != 0 {
		t.Fatalf("check: %d %s", code, body)
	}
	if strings.Contains(body, testKey) {
		t.Fatal("the key was answered")
	}
	_ = r.h.Settings.Set(context.Background(), "fe_url", "http://fragments.example.test")
	if _, body := r.h.Do("POST", "/check", ""); !strings.Contains(body, "https") {
		t.Fatalf("plain http to another host: %s", body)
	}
}

func TestTheLoopDelivers(t *testing.T) {
	fe := &fakeFE{fail: map[string][]int{}}
	srv := httptest.NewServer(fe)
	t.Cleanup(srv.Close)
	m := New()
	h := moduletest.Start(t, m)
	key := filepath.Join(t.TempDir(), "k")
	_ = os.WriteFile(key, []byte(testKey), 0o600)
	ctx := context.Background()
	_ = h.Settings.Set(ctx, "fe_url", srv.URL)
	_ = h.Settings.Set(ctx, "fe_token_file", key)
	if code, _ := h.Do("POST", "/captures", `{"content":"queued while off"}`); code != http.StatusAccepted {
		t.Fatal(code)
	}
	time.Sleep(1500 * time.Millisecond)
	if c, _ := m.st.get(ctx, 1); c.State != stateQueued {
		t.Fatalf("delivered while off: %+v", c)
	}
	_ = h.Settings.Set(ctx, "delivery", true) // turning it on wakes the worker
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if c, _ := m.st.get(ctx, 1); c.State == stateSaved {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the worker did not deliver")
}
