package approvals

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
)

const ownerKey = "test-owner"

// fakeGate is Gatecrasher for a surface: an event log served as a stream (from
// Last-Event-ID), the cards, and decide.
type fakeGate struct {
	mu         sync.Mutex
	events     []feedEvent
	cards      map[string]map[string]any
	decided    map[string]decideRequest
	lastIDs    []string // the Last-Event-ID of each connection
	ignoreLID  bool     // replay from the start whatever the client says
	failDecide bool     // decide answers 503
	failCard   bool     // the card route answers 503
	push       chan struct{}
	srv        *httptest.Server
}

func newFake(t *testing.T) *fakeGate {
	f := &fakeGate{cards: map[string]map[string]any{}, decided: map[string]decideRequest{}, push: make(chan struct{}, 16)}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGate) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+ownerKey {
		http.Error(w, "no", http.StatusUnauthorized)
		return
	}
	switch {
	case r.URL.Path == pathEvents:
		f.mu.Lock()
		lid := r.Header.Get("Last-Event-ID")
		f.lastIDs = append(f.lastIDs, lid)
		after, _ := strconv.ParseInt(lid, 10, 64)
		if f.ignoreLID {
			after = 0
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		sent := after
		for {
			f.mu.Lock()
			var out []feedEvent
			for _, e := range f.events {
				if e.Seq > sent {
					out = append(out, e)
				}
			}
			f.mu.Unlock()
			for _, e := range out {
				raw, _ := json.Marshal(e)
				fmt.Fprintf(w, "id: %d\nevent: approval\ndata: %s\n\n", e.Seq, raw)
				sent = e.Seq
			}
			fl.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-f.push:
			case <-time.After(200 * time.Millisecond):
				fmt.Fprint(w, ": keepalive\n\n")
			}
		}
	case strings.HasSuffix(r.URL.Path, "/card"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, pathApprovals+"/"), "/card")
		f.mu.Lock()
		if f.failCard {
			f.mu.Unlock()
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		c, ok := f.cards[id]
		f.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(c)
	case strings.HasSuffix(r.URL.Path, "/decide"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, pathApprovals+"/"), "/decide")
		var d decideRequest
		_ = json.NewDecoder(r.Body).Decode(&d)
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.failDecide {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if _, done := f.decided[id]; done {
			w.WriteHeader(http.StatusConflict)
			return
		}
		f.decided[id] = d
		_, _ = w.Write([]byte(`{"status":"` + d.Outcome + `"}`))
	default:
		http.NotFound(w, r)
	}
}

// submit adds an approval and its submitted event.
func (f *fakeGate) submit(id, typ, tier string, extra map[string]any) {
	c := map[string]any{"version": cardVersion, "approval_id": id, "type": typ, "status": "pending", "tier": tier, "kind": "credential_grant",
		"source": "warden", "title": "Grant for " + id, "summary": "needed for the build", "principal": map[string]string{"sub": "agt_x", "kind": "agent"},
		"evidence": map[string]string{"task_id": "CW-1"}, "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339), "deciders": "owner"}
	for k, v := range extra {
		c[k] = v
	}
	f.mu.Lock()
	f.cards[id] = c
	f.events = append(f.events, feedEvent{Seq: int64(len(f.events) + 1), ApprovalID: id, Type: "submitted", Status: "pending", Tier: tier, Kind: "credential_grant", Source: "warden"})
	f.mu.Unlock()
	f.push <- struct{}{}
}

func (f *fakeGate) close(id, status string) {
	f.mu.Lock()
	f.cards[id]["status"] = status
	f.events = append(f.events, feedEvent{Seq: int64(len(f.events) + 1), ApprovalID: id, Type: "decided", Status: status})
	f.mu.Unlock()
	f.push <- struct{}{}
}

func start(t *testing.T, f *fakeGate) (*moduletest.Host, *mod) {
	t.Helper()
	keyFile := filepath.Join(t.TempDir(), "owner.key")
	if err := os.WriteFile(keyFile, []byte(ownerKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &mod{}
	h := moduletest.Start(t, m)
	ctx := context.Background()
	_ = h.Settings.Set(ctx, "key_file", keyFile)
	_ = h.Settings.Set(ctx, "url", f.srv.URL)
	m.reconnect()
	return h, m
}

// eventually waits for cond, the way a card shows within the 2 s the task asks for.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("not within 2s: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func openCards(h *moduletest.Host) []module.Card {
	cs, _ := h.CardStore.List(context.Background(), false)
	return cs
}

func answer(c module.Card, o module.CardOutcome) module.Card {
	c.Status = "submitted"
	o.Status = "submitted"
	c.Outcome = &o
	return c
}

func TestAnApprovalIsShownAndDecided(t *testing.T) {
	f := newFake(t)
	h, m := start(t, f)
	f.submit("apr_1", "approve", "critical", map[string]any{"requested": map[string]any{"audience": "llm-gateway", "scopes": []string{"llm-gateway.decide", "llm-gateway.embed"}, "ttl_seconds": 600}})
	eventually(t, "the card shows", func() bool { return len(openCards(h)) == 1 })
	c := openCards(h)[0]
	var p map[string]any
	_ = json.Unmarshal(c.Payload, &p)
	if c.Kind != "approval" || c.Subject != "approval:apr_1" || c.Risk != "high" || p["phrase"] != phrase || !strings.Contains(p["summary"].(string), "from warden. Audience llm-gateway, for 10 min.") {
		t.Fatalf("%+v %v", c, p)
	}
	if p["per_change"] != true {
		t.Fatalf("a two-scope grant is not split per scope: %v", p)
	}
	// A card the module did not show, with the same subject, decides nothing.
	forged := answer(module.Card{ID: "forged", Kind: "approval", Subject: "approval:apr_1"}, module.CardOutcome{Decisions: []module.CardDecision{{ItemID: "approval", Action: "approve"}}})
	m.answered(context.Background(), forged)
	f.mu.Lock()
	_, early := f.decided["apr_1"]
	f.mu.Unlock()
	if early {
		t.Fatal("a forged card decided the approval")
	}
	// The owner approves one of the two scopes: Gatecrasher gets the approval narrowed.
	m.answered(context.Background(), answer(c, module.CardOutcome{Decisions: []module.CardDecision{{ItemID: "change:0", Action: "approve"}, {ItemID: "change:1", Action: "reject"}}}))
	f.mu.Lock()
	d := f.decided["apr_1"]
	f.mu.Unlock()
	if d.Outcome != "approved" || d.Narrowed == nil || len(d.Narrowed.Scopes) != 1 || d.Narrowed.Scopes[0] != "llm-gateway.decide" {
		t.Fatalf("decide: %+v", d)
	}
	// Gatecrasher's decided event closes it here.
	f.close("apr_1", "approved")
	eventually(t, "the decided status is kept", func() bool { r, _ := m.shown(context.Background(), "apr_1"); return r.status == "approved" })
}

func TestDecisionsByCardKind(t *testing.T) {
	sh := shownRow{scopes: []string{"a.read", "a.write"}}
	sub := func(kind string, o module.CardOutcome) module.Card {
		return module.Card{Kind: kind, Status: "submitted", Outcome: &o}
	}
	cases := []struct {
		name string
		card module.Card
		want decideRequest
		ok   bool
	}{
		{"approve all", sub("approval", module.CardOutcome{Decisions: []module.CardDecision{{ItemID: "approval", Action: "approve"}}}), decideRequest{Outcome: "approved"}, true},
		{"reject", sub("approval", module.CardOutcome{Decisions: []module.CardDecision{{ItemID: "approval", Action: "reject"}}}), decideRequest{Outcome: "denied"}, true},
		{"split, both", sub("approval", module.CardOutcome{Decisions: []module.CardDecision{{ItemID: "change:0", Action: "approve"}, {ItemID: "change:1", Action: "approve"}}}), decideRequest{Outcome: "approved"}, true},
		{"split, none", sub("approval", module.CardOutcome{Decisions: []module.CardDecision{{ItemID: "change:0", Action: "reject"}, {ItemID: "change:1", Action: "reject"}}}), decideRequest{Outcome: "denied"}, true},
		{"split, ids not changes: denied, never a full approval", sub("approval", module.CardOutcome{Decisions: []module.CardDecision{{ItemID: "x", Action: "approve"}, {ItemID: "change:9", Action: "approve"}}}), decideRequest{Outcome: "denied"}, true},
		{"choose", sub("choice", module.CardOutcome{Answers: []module.CardAnswer{{Value: "b"}}}), decideRequest{Outcome: "approved", Choice: "b"}, true},
		{"decline a choice", sub("choice", module.CardOutcome{Answers: []module.CardAnswer{{Value: declineID}}}), decideRequest{Outcome: "denied"}, true},
		{"answer", sub("prompt", module.CardOutcome{Answers: []module.CardAnswer{{Value: "use staging"}}}), decideRequest{Outcome: "approved", Answer: "use staging"}, true},
		{"empty answer declines", sub("prompt", module.CardOutcome{Answers: []module.CardAnswer{{Value: " "}}}), decideRequest{Outcome: "denied"}, true},
		{"dismissed declines", module.Card{Kind: "prompt", Status: "canceled"}, decideRequest{Outcome: "denied"}, true},
		{"expired: nothing", module.Card{Kind: "approval", Status: "expired"}, decideRequest{}, false},
		{"secret provided: the producer reports it", sub(kindForm, module.CardOutcome{Data: map[string]any{"provided": true}}), decideRequest{}, false},
		{"secret declined", sub(kindForm, module.CardOutcome{Data: map[string]any{"declined": true}}), decideRequest{Outcome: "denied"}, true},
	}
	for _, c := range cases {
		got, ok := decision(c.card, sh)
		if ok != c.ok || got.Outcome != c.want.Outcome || got.Choice != c.want.Choice || got.Answer != c.want.Answer || (got.Narrowed != nil) != (c.want.Narrowed != nil) {
			t.Errorf("%s: %+v %v", c.name, got, ok)
		}
	}
	got, _ := decision(sub("approval", module.CardOutcome{Decisions: []module.CardDecision{{ItemID: "change:1", Action: "approve"}, {ItemID: "change:0", Action: "reject"}}}), sh)
	if got.Outcome != "approved" || got.Narrowed == nil || len(got.Narrowed.Scopes) != 1 || got.Narrowed.Scopes[0] != "a.write" {
		t.Fatalf("narrowed: %+v", got)
	}
}

func TestChoiceQuestionAndSecretCards(t *testing.T) {
	f := newFake(t)
	h, _ := start(t, f)
	f.submit("apr_c", "choose", "elevated", map[string]any{"prompt": map[string]any{"question": "Which key?", "choices": []map[string]string{{"id": "a", "label": "Staging"}, {"id": "b", "label": "Prod"}}}})
	f.submit("apr_q", "answer", "elevated", map[string]any{"prompt": map[string]any{"question": "Why?"}})
	f.submit("apr_s", "request_secret", "critical", map[string]any{"secret": map[string]any{"intake_id": "int_1", "post_url": "https://warden.example.test/intake/int_1", "field": "value", "write_only": true, "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}})
	f.submit("apr_bad", "request_secret", "critical", map[string]any{"secret": map[string]any{"post_url": "http://evil.example.test/x"}})
	eventually(t, "three cards and the error for the bad one show", func() bool { return len(openCards(h)) == 4 })
	kinds := map[string]module.Card{}
	for _, c := range openCards(h) {
		kinds[c.Kind] = c
	}
	var p map[string]any
	_ = json.Unmarshal(kinds["choice"].Payload, &p)
	if opts := p["options"].([]any); len(opts) != 3 || opts[2].(map[string]any)["id"] != declineID {
		t.Fatalf("choice: %v", p)
	}
	s := kinds[kindForm]
	if !s.Blocking || strings.Contains(string(s.Payload), "warden.example.test") {
		t.Fatalf("secret: the intake is in the card, or it does not block: %+v", s)
	}
	// The page gets the intake from the module, for the card it showed only.
	code, body := h.Do("GET", "/form/"+s.ID, "")
	if code != http.StatusOK || !strings.Contains(body, `"origin":"https://warden.example.test"`) || !strings.Contains(body, `"field":"value"`) {
		t.Fatalf("form: %d %s", code, body)
	}
	if code, _ := h.Do("GET", "/form/not-a-card", ""); code != http.StatusNotFound {
		t.Fatalf("a form for a card it did not show: %d", code)
	}
	if _, ok := kinds["prompt"]; !ok {
		t.Fatal("no question card")
	}
	// The secret request whose intake is not https is not shown: the owner is told, and the
	// stream went on past it.
	eventually(t, "the unshowable one is reported", func() bool {
		for _, c := range openCards(h) {
			if c.Kind == "error" && c.Subject == "approval-error:apr_bad" {
				return true
			}
		}
		return false
	})
}

func TestAProducersChoiceCannotTakeTheDeclineID(t *testing.T) {
	f := newFake(t)
	h, _ := start(t, f)
	f.submit("apr_c", "choose", "elevated", map[string]any{"prompt": map[string]any{"question": "Which?", "choices": []map[string]string{{"id": declineID, "label": "Sneaky"}, {"id": "a", "label": "A"}}}})
	f.submit("apr_next", "approve", "elevated", nil)
	eventually(t, "both show, the stream did not stick", func() bool { return len(openCards(h)) == 2 })
}

func TestAFailedDecideIsShownAgain(t *testing.T) {
	f := newFake(t)
	h, m := start(t, f)
	f.submit("apr_1", "approve", "elevated", nil)
	eventually(t, "the card shows", func() bool { return len(openCards(h)) == 1 })
	c := openCards(h)[0]
	f.mu.Lock()
	f.failDecide = true
	f.mu.Unlock()
	_, _ = h.CardStore.Respond(context.Background(), c.ID, module.CardOutcome{Status: "submitted", Decisions: []module.CardDecision{{ItemID: "approval", Action: "approve"}}})
	got, _, _ := h.CardStore.Get(context.Background(), c.ID)
	m.answered(context.Background(), got)
	var kinds []string
	for _, x := range openCards(h) {
		kinds = append(kinds, x.Kind)
	}
	if len(kinds) != 2 {
		t.Fatalf("want the error and the approval again, got %v", kinds)
	}
}

func TestDecidedElsewhereKeepsGatecrashersWord(t *testing.T) {
	f := newFake(t)
	h, m := start(t, f)
	f.submit("apr_1", "approve", "elevated", nil)
	eventually(t, "the card shows", func() bool { return len(openCards(h)) == 1 })
	c := openCards(h)[0]
	f.mu.Lock()
	f.decided["apr_1"] = decideRequest{Outcome: "denied"} // someone else denied it first
	f.cards["apr_1"]["status"] = "denied"
	f.mu.Unlock()
	m.answered(context.Background(), answer(c, module.CardOutcome{Decisions: []module.CardDecision{{ItemID: "approval", Action: "approve"}}}))
	if r, _ := m.shown(context.Background(), "apr_1"); r.status != "denied" {
		t.Fatalf("recorded %q, Gatecrasher says denied", r.status)
	}
}

func TestReconnectResumesWithoutDuplicates(t *testing.T) {
	f := newFake(t)
	f.ignoreLID = true // the worst case: the server replays from the start
	h, m := start(t, f)
	f.submit("apr_1", "approve", "elevated", nil)
	eventually(t, "the card shows", func() bool { return len(openCards(h)) == 1 })
	m.reconnect()
	f.submit("apr_2", "approve", "elevated", nil)
	eventually(t, "the second shows", func() bool { return len(openCards(h)) == 2 })
	m.reconnect()
	time.Sleep(300 * time.Millisecond)
	if n := len(openCards(h)); n != 2 {
		t.Fatalf("%d cards after reconnects, want 2", n)
	}
	f.mu.Lock()
	lids := append([]string(nil), f.lastIDs...)
	f.mu.Unlock()
	if len(lids) < 2 || lids[len(lids)-1] == "" {
		t.Fatalf("a reconnect did not send Last-Event-ID: %v", lids)
	}
	if n := m.cursor(context.Background(), f.srv.URL); n != 2 {
		t.Fatalf("cursor %d", n)
	}
}

func TestNotConfiguredFollowsNothing(t *testing.T) {
	m := &mod{}
	h := moduletest.Start(t, m)
	out, err := h.Tool("approvals_status", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if st := out.(map[string]any); st["following"] != false || !strings.Contains(fmt.Sprint(st["problem"]), "approvals.url") {
		t.Fatalf("%v", st)
	}
}

func TestTheKeyIsNeverInAnError(t *testing.T) {
	g, err := newGate("http://127.0.0.1:1", "/nonexistent/owner.key")
	if err != nil {
		t.Fatal(err)
	}
	_, err = g.card(context.Background(), "apr_1")
	if err == nil || strings.Contains(err.Error(), ownerKey) {
		t.Fatalf("%v", err)
	}
	if _, err := newGate("http://gate.example.test", "/k"); err == nil {
		t.Fatal("plain http to a non-loopback host was accepted")
	}
}

// The request_secret canary, server side: whatever the page put in a secret card's answer,
// even the value itself, it is not sent to Gatecrasher and not kept in the module's store.
// (The page never puts it there: web/index.tsx posts it to the intake only; the browser run
// is in the PR.)
func TestASecretNeverReachesGatecrasherOrTheStore(t *testing.T) {
	const canary = "CANARY-7f3e1d2c9b8a"
	f := newFake(t)
	h, m := start(t, f)
	f.submit("apr_s", "request_secret", "critical", map[string]any{"secret": map[string]any{"intake_id": "int_1", "post_url": "https://warden.example.test/intake", "field": "value"}})
	eventually(t, "the secret card shows", func() bool { return len(openCards(h)) == 1 })
	c := openCards(h)[0]
	if strings.Contains(string(c.Payload), canary) {
		t.Fatal("impossible: the canary is in the card before it was typed")
	}
	m.answered(context.Background(), answer(c, module.CardOutcome{Data: map[string]any{"provided": true, "value": canary}}))
	if r, _ := m.shown(context.Background(), "apr_s"); strings.Contains(r.post+r.field+r.title, canary) {
		t.Fatal("the canary is in the module's record")
	}
	f.mu.Lock()
	_, decided := f.decided["apr_s"]
	f.mu.Unlock()
	if decided {
		t.Fatal("a provided secret made a decide call")
	}
	var dump strings.Builder
	rows, err := m.db.Query(`SELECT approval_id, card_id, type, status, title, scopes, post_url, field FROM shown`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var a, b, c, d, e, f2, g, h2 string
		_ = rows.Scan(&a, &b, &c, &d, &e, &f2, &g, &h2)
		dump.WriteString(a + b + c + d + e + f2 + g + h2)
	}
	_ = rows.Close()
	if strings.Contains(dump.String(), canary) {
		t.Fatal("the canary is in the module's store")
	}
	// The module's one route is GET: nothing on this server can take a value.
	for _, path := range []string{"/form/" + c.ID, "/secret", "/"} {
		if code, _ := h.Do("POST", path, "value="+canary); code < 400 {
			t.Fatalf("POST %s took a post: %d", path, code)
		}
	}
}

// Gatecrasher down when the owner answered: the answer fails, the card cannot be fetched
// again, and the approval comes back once Gatecrasher answers (the next connection).
func TestAnApprovalComesBackAfterGatecrasherWasDown(t *testing.T) {
	f := newFake(t)
	h, m := start(t, f)
	f.submit("apr_1", "approve", "elevated", nil)
	eventually(t, "the card shows", func() bool { return len(openCards(h)) == 1 })
	c := openCards(h)[0]
	f.mu.Lock()
	f.failDecide, f.failCard = true, true
	f.mu.Unlock()
	_, _ = h.CardStore.Respond(context.Background(), c.ID, module.CardOutcome{Status: "submitted", Decisions: []module.CardDecision{{ItemID: "approval", Action: "approve"}}})
	got, _, _ := h.CardStore.Get(context.Background(), c.ID)
	m.answered(context.Background(), got)
	f.mu.Lock()
	f.failDecide, f.failCard = false, false
	f.mu.Unlock()
	eventually(t, "the approval is shown again", func() bool {
		for _, x := range openCards(h) {
			if x.Subject == "approval:apr_1" {
				return true
			}
		}
		return false
	})
}
