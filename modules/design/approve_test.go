package design

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
)

const (
	headA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	headB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func approve(t *testing.T, h *moduletest.Host, card module.Card) module.Card {
	t.Helper()
	done, err := h.CardStore.Respond(context.Background(), card.ID, module.CardOutcome{Status: "submitted", Decisions: []module.CardDecision{{ItemID: "approval", Action: "approve"}}})
	if err != nil {
		t.Fatal(err)
	}
	return done
}

func openCards(h *moduletest.Host, kind string) []module.Card {
	cards, _ := h.CardStore.List(context.Background(), false)
	var out []module.Card
	for _, c := range cards {
		if c.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}

// A job with no recorded head has nothing to approve: the tool and the page's route say why.
func TestApproveWithNoHeadIsRefused(t *testing.T) {
	h, _, _ := startWith(t, time.Hour)
	j := newJob(t, h, "Composer")
	if _, err := h.Tool("design_approve", `{"id":"`+j.ID+`"}`); err == nil || !strings.Contains(err.Error(), "head:") {
		t.Fatalf("approved with no head: %v", err)
	}
	if code, body := h.Do("POST", "/jobs/"+j.ID+"/approve", ""); code != 422 || !strings.Contains(body, "no head is recorded") {
		t.Fatal(code, body)
	}
	if len(openCards(h, "approval")) != 0 {
		t.Fatal("an approval card was raised")
	}
}

// The approval names the head on the card; it is recorded with it, and the go-ahead carries
// the lines the Designer checks before merging.
func TestApprovalRecordsTheHead(t *testing.T) {
	h, m, box := startWith(t, time.Hour)
	ctx := context.Background()
	j := newJob(t, h, "Composer")
	box.put(designerReply("r1", j.ID, "preview: http://laptop.test:1/gallery.html\npr: https://github.com/hollis-labs/surface/pull/9\nhead: "+headA, 1))
	_, _ = m.refresh(ctx, j.ID)
	card, err := m.askApproval(ctx, j.ID, time.Minute)
	if err != nil || card == nil {
		t.Fatal(card, err)
	}
	var payload struct {
		Head, PR string
		Summary  string `json:"summary"`
	}
	_ = json.Unmarshal(card.Payload, &payload)
	if payload.Head != headA || payload.PR != "https://github.com/hollis-labs/surface/pull/9" || !strings.Contains(payload.Summary, "aaaaaaa") || !strings.Contains(card.Title, "aaaaaaa") {
		t.Fatalf("the card does not name the head: %+v %s", payload, card.Title)
	}
	res, err := m.settle(ctx, j.ID, approve(t, h, *card))
	if err != nil || res["approved"] != true || res["approved_head"] != headA {
		t.Fatalf("settle: %v %v", res, err)
	}
	got, _ := m.st.get(ctx, j.ID)
	if got.Status != StatusApproved || got.ApprovedHead != headA || got.ApprovedAt == "" || got.HeadDiffers {
		t.Fatalf("job: %+v", got)
	}
	last := box.sent[len(box.sent)-1].Body
	for _, want := range []string{"\njob: " + j.ID + "\n", "\napproved-head: " + headA + "\n", "\npr: https://github.com/hollis-labs/surface/pull/9\n", "covers commit " + headA + " only", "needs a new round", "merged: <the full SHA"} {
		if !strings.Contains(last, want) {
			t.Errorf("the go-ahead lacks %q:\n%s", want, last)
		}
	}
	if !strings.Contains(box.sent[0].Body, "An approval names one head") {
		t.Error("the brief does not say the approval names a head")
	}
	// The same card again changes nothing.
	if again, _ := m.settle(ctx, j.ID, approve2(h, *card)); again["approved"] != false {
		t.Fatalf("approved twice: %v", again)
	}
}

func approve2(h *moduletest.Host, card module.Card) module.Card {
	c, _, _ := h.CardStore.Get(context.Background(), card.ID)
	return c
}

// A reply naming another head while the card is open: the card is superseded, nothing is
// approved on it, and the owner is asked again on the new head.
func TestHeadChangesWhileTheCardIsOpen(t *testing.T) {
	h, m, box := startWith(t, time.Hour)
	ctx := context.Background()
	j := newJob(t, h, "Composer")
	box.put(designerReply("r1", j.ID, "preview: http://laptop.test:1/gallery.html\nhead: "+headA, 1))
	_, _ = m.refresh(ctx, j.ID)
	old, err := m.askApproval(ctx, j.ID, time.Minute)
	if err != nil || old == nil {
		t.Fatal(old, err)
	}
	box.put(designerReply("r2", j.ID, "One more fix.\nhead: "+headB, 2))
	_, _ = m.refresh(ctx, j.ID)

	if c, _, _ := h.CardStore.Get(ctx, old.ID); c.Status != "superseded" {
		t.Fatalf("the old card: %+v", c)
	}
	// Answering the old card approves nothing.
	if res, _ := m.settle(ctx, j.ID, module.Card{ID: old.ID, Status: "submitted", Outcome: &module.CardOutcome{Decisions: []module.CardDecision{{Action: "approve"}}}}); res["approved"] != false {
		t.Fatalf("the superseded card approved: %v", res)
	}
	got, _ := m.st.get(ctx, j.ID)
	if got.Status == StatusApproved || got.ApprovedHead != "" {
		t.Fatalf("approved on the old head: %+v", got)
	}
	open := openCards(h, "approval")
	if len(open) != 1 || !strings.Contains(open[0].Title, "bbbbbbb") {
		t.Fatalf("not asked again on the new head: %+v", open)
	}
	if res, _ := m.settle(ctx, j.ID, approve(t, h, open[0])); res["approved"] != true {
		t.Fatalf("the new card: %v", res)
	}
	if got, _ = m.st.get(ctx, j.ID); got.ApprovedHead != headB {
		t.Fatalf("approved head: %q", got.ApprovedHead)
	}
}

// After approval: a reply naming another commit is shown as such and changes nothing; mail is
// still read until merged (of the approved commit) or closed, then no more.
func TestAfterApproval(t *testing.T) {
	h, m, box := startWith(t, time.Hour)
	ctx := context.Background()
	j := newJob(t, h, "Composer")
	box.put(designerReply("r1", j.ID, "preview: http://laptop.test:1/gallery.html\nhead: "+headA, 1))
	_, _ = m.refresh(ctx, j.ID)
	card, _ := m.askApproval(ctx, j.ID, time.Minute)
	if res, _ := m.settle(ctx, j.ID, approve(t, h, *card)); res["approved"] != true {
		t.Fatal(res)
	}

	box.put(designerReply("r2", j.ID, "Pushed a follow-up.\npreview: http://laptop.test:2/gallery.html\nhead: "+headB, 2))
	box.put(designerReply("r3", j.ID, "merged: "+headB, 3))
	_, _ = m.refresh(ctx, j.ID)
	got, _ := m.st.get(ctx, j.ID)
	if got.Status != StatusApproved || got.ApprovedHead != headA || got.HeadSHA != headA || got.PreviewURL != "http://laptop.test:1/gallery.html" {
		t.Fatalf("a reply after approval changed the job: %+v", got)
	}
	if !got.HeadDiffers || got.LatestHead != headB || got.Replies[1].Note != noteAfterApproval || !strings.Contains(got.Replies[2].Note, "not the approved commit") {
		t.Fatalf("not shown: %+v %+v", got, got.Replies)
	}
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"head_differs":true`) || !strings.Contains(string(b), `"approved_head":"`+headA) {
		t.Fatalf("json: %s", b)
	}

	box.put(designerReply("r4", j.ID, "In.\nmerged: "+headA, 4))
	_, _ = m.refresh(ctx, j.ID)
	if got, _ = m.st.get(ctx, j.ID); got.Status != StatusMerged {
		t.Fatalf("merged: %s", got.Status)
	}
	box.put(designerReply("r5", j.ID, "anything", 5))
	if c, _ := m.refresh(ctx, ""); len(c) != 0 {
		t.Fatalf("a merged job is still read: %+v", c)
	}

	// closed: closes an approved job; an approval older than a week is no longer read.
	k := newJob(t, h, "Cards")
	box.put(designerReply("k1", k.ID, "head: "+headA, 1))
	_, _ = m.refresh(ctx, k.ID)
	card, _ = m.askApproval(ctx, k.ID, time.Minute)
	_, _ = m.settle(ctx, k.ID, approve(t, h, *card))
	box.put(designerReply("k2", k.ID, "closed: superseded by another job", 2))
	_, _ = m.refresh(ctx, "")
	if got, _ = m.st.get(ctx, k.ID); got.Status != StatusClosed {
		t.Fatalf("closed: %s", got.Status)
	}
	l := newJob(t, h, "Old")
	approvedLongAgo := time.Now().Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339)
	st := StatusApproved
	_, _ = m.st.update(ctx, l.ID, Change{Status: &st})
	_, _ = m.st.db.ExecContext(ctx, `UPDATE jobs SET approved_at = ? WHERE id = ?`, approvedLongAgo, l.ID)
	if c, _ := m.refresh(ctx, ""); len(c) != 0 {
		t.Fatalf("an approval from last week is still read: %+v", c)
	}
}

// Work that comes back without a preview is said to have come back; ready for review still
// needs a preview, and a new round clears the flag.
func TestUpdateReceived(t *testing.T) {
	h, m, box := startWith(t, time.Hour)
	ctx := context.Background()
	j := newJob(t, h, "Composer")
	box.put(designerReply("u1", j.ID, "pr: https://github.com/hollis-labs/surface/pull/9\nhead: "+headA, 1))
	_, _ = m.refresh(ctx, j.ID)
	got, _ := m.st.get(ctx, j.ID)
	if !got.UpdateReceived || got.Status != StatusInProgress {
		t.Fatalf("after a head: %+v", got)
	}
	box.put(designerReply("u2", j.ID, "preview: http://laptop.test:1/gallery.html", 2))
	_, _ = m.refresh(ctx, j.ID)
	if got, _ = m.st.get(ctx, j.ID); got.UpdateReceived || got.Status != StatusReady {
		t.Fatalf("after a preview: %+v", got)
	}
	if _, err := h.Tool("design_feedback", `{"id":"`+j.ID+`","text":"darker"}`); err != nil {
		t.Fatal(err)
	}
	box.put(designerReply("u3", j.ID, "head: "+headB, 3))
	_, _ = m.refresh(ctx, j.ID)
	if got, _ = m.st.get(ctx, j.ID); !got.UpdateReceived || got.Status != StatusChangesRequested {
		t.Fatalf("after a round and a head: %+v", got)
	}
	_, _ = m.st.db.ExecContext(ctx, `UPDATE jobs SET update_received = 1 WHERE id = ?`, j.ID)
	_, _ = h.Tool("design_feedback", `{"id":"`+j.ID+`","text":"again"}`)
	if got, _ = m.st.get(ctx, j.ID); got.UpdateReceived {
		t.Fatal("a new round did not clear the flag")
	}
}

// GitHub showing the branch at another commit: nothing is approvable, and a notice says why.
func TestBranchAtAnotherCommit(t *testing.T) {
	h, m, box := startWith(t, time.Hour)
	ctx := context.Background()
	set(t, h, map[string]string{"repo": "o/r"})
	j := newJob(t, h, "Composer")
	srv := fakeGitHub(t, j.Branch, `[]`) // its branch head is abc123
	m.gh.api = srv.URL
	box.put(designerReply("g1", j.ID, "head: "+headA, 1))
	_, _ = m.refresh(ctx, j.ID)
	if card, err := m.askApproval(ctx, j.ID, time.Minute); card != nil || err == nil || !strings.Contains(err.Error(), "abc123") {
		t.Fatalf("approvable at another commit: %v %v", card, err)
	}
	if len(openCards(h, "approval")) != 0 || len(openCards(h, "info")) == 0 {
		t.Fatal("no notice, or an approval card")
	}
}

// GitHub refusing (the rate limit): it is left alone for a while and the last good read is
// shown with its time.
func TestGitHubBackoff(t *testing.T) {
	var hits atomic.Int32
	refuse := atomic.Bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if refuse.Load() {
			http.Error(w, "rate limited", http.StatusForbidden)
			return
		}
		if strings.Contains(r.URL.Path, "/compare/") {
			_, _ = io.WriteString(w, `{"commits":[{"sha":"`+headA+`","commit":{"message":"m","author":{"date":"2026-11-18T10:00:00Z"}}}],"files":[]}`)
			return
		}
		_, _ = io.WriteString(w, `[]`)
	}))
	defer srv.Close()
	g := &github{api: srv.URL, client: http.DefaultClient, tokenFile: func() string { return "" }}
	ctx := context.Background()
	if p := g.progress(ctx, "o/r", "main", "b", true); p.Head != headA || p.Problem != "" {
		t.Fatalf("first read: %+v", p)
	}
	refuse.Store(true)
	p := g.progress(ctx, "o/r", "main", "b", true)
	if p.Head != headA || !strings.Contains(p.Problem, "showing what was read at") {
		t.Fatalf("refused: %+v", p)
	}
	before := hits.Load()
	p = g.progress(ctx, "o/r", "main", "b", true)
	if hits.Load() != before || p.Head != headA || !strings.Contains(p.Problem, "not asked again until") {
		t.Fatalf("not backed off: hits %d -> %d, %+v", before, hits.Load(), p)
	}
}
