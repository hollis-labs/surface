package design

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
)

// noticeCards are the job's "not approvable yet" notices, open or ended.
func noticeCards(h *moduletest.Host, id string) []module.Card {
	cards, _ := h.CardStore.List(context.Background(), true)
	var out []module.Card
	for _, c := range cards {
		if c.Subject == ref(id)+":approvable" {
			out = append(out, c)
		}
	}
	return out
}

// GitHub lagging a few seconds behind the Designer's reply: the read is retried and the owner
// is asked, with no notice raised.
func TestGitHubLagIsWaitedOut(t *testing.T) {
	h, m, box, gh := startOnGitHub(t)
	ctx := context.Background()
	m.lag = []time.Duration{20 * time.Millisecond, 20 * time.Millisecond}
	j := newJob(t, h, "Lag")
	gh.set(headA, pull(9, headA, false))
	box.put(designerReply("l1", j.ID, "preview: http://laptop.test:1/\nhead: "+headB+"\npr: https://github.com/o/r/pull/9", 1))
	_, _ = m.refresh(ctx, j.ID)
	go func() { time.Sleep(25 * time.Millisecond); gh.set(headB, pull(9, headB, false)) }()
	card, err := m.askApproval(ctx, j.ID, time.Minute)
	if err != nil || card == nil {
		t.Fatalf("not asked once GitHub caught up: %v %v", card, err)
	}
	if n := noticeCards(h, j.ID); len(n) != 0 {
		t.Fatalf("a notice was raised for a lag that passed: %+v", n)
	}
}

// The acceptance failure: a card open on one head, a reply naming a new head while GitHub
// still shows the old one. The old card is superseded, a notice says GitHub lags, and the next
// check after GitHub catches up asks the owner on the new head on its own; the notice ends.
func TestAskedAgainOnceGitHubShowsTheNewHead(t *testing.T) {
	h, m, box, gh := startOnGitHub(t)
	ctx := context.Background()
	j := newJob(t, h, "Reask")
	gh.set(headA, pull(9, headA, false))
	box.put(designerReply("a1", j.ID, "preview: http://laptop.test:1/\nhead: "+headA+"\npr: https://github.com/o/r/pull/9", 1))
	_, _ = m.refresh(ctx, j.ID)
	old, err := m.askApproval(ctx, j.ID, time.Minute)
	if err != nil || old == nil {
		t.Fatal(old, err)
	}
	box.put(designerReply("a2", j.ID, "head: "+headB, 2)) // GitHub still at headA
	_, _ = m.refresh(ctx, j.ID)
	if c, _, _ := h.CardStore.Get(ctx, old.ID); c.Status != "superseded" {
		t.Fatalf("old card: %+v", c)
	}
	if len(openCards(h, "approval")) != 0 {
		t.Fatal("asked on a head GitHub does not show")
	}
	got, _ := m.st.get(ctx, j.ID)
	notices := noticeCards(h, j.ID)
	if !got.ApprovalOwed || len(notices) != 1 || notices[0].Status != "" || !strings.Contains(string(notices[0].Payload), "lag") {
		t.Fatalf("owed %v, notices %+v", got.ApprovalOwed, notices)
	}
	// Still lagging on the next check: the same notice, updated, not a new one.
	_, _ = m.refresh(ctx, j.ID)
	if n := noticeCards(h, j.ID); len(n) != 1 {
		t.Fatalf("a new notice per check: %d", len(n))
	}
	gh.set(headB, pull(9, headB, false))
	_, _ = m.refresh(ctx, j.ID)
	open := openCards(h, "approval")
	if len(open) != 1 || !strings.Contains(open[0].Title, "bbbbbbb") {
		t.Fatalf("not asked again on the new head: %+v", open)
	}
	if c, _, _ := h.CardStore.Get(ctx, notices[0].ID); c.Status == "" {
		t.Fatalf("the notice was left open: %+v", c)
	}
	if got, _ = m.st.get(ctx, j.ID); got.ApprovalOwed {
		t.Fatal("still owed after asking")
	}
	if res, _ := m.settle(ctx, j.ID, approve(t, h, open[0])); res["approved"] != true || res["approved_head"] != headB {
		t.Fatalf("settle: %v", res)
	}
}

// The notice ends when the job closes, and a closed job keeps why it closed.
func TestNoticeEndsAndClosedReasonIsKept(t *testing.T) {
	h, m, box, gh := startOnGitHub(t)
	ctx := context.Background()
	j := newJob(t, h, "Closing")
	gh.set(headA, pull(9, headA, false))
	box.put(designerReply("c1", j.ID, "preview: http://laptop.test:1/\nhead: "+headB+"\npr: https://github.com/o/r/pull/9", 1))
	_, _ = m.refresh(ctx, j.ID)
	if c, err := m.askApproval(ctx, j.ID, time.Minute); c != nil || err == nil {
		t.Fatal("approvable at another commit")
	}
	if _, err := h.Tool("design_set", `{"id":"`+j.ID+`","status":"closed"}`); err != nil {
		t.Fatal(err)
	}
	for _, n := range noticeCards(h, j.ID) {
		if n.Status == "" {
			t.Fatalf("the notice outlived the job: %+v", n)
		}
	}
	got, _ := m.st.get(ctx, j.ID)
	if got.ClosedReason != "closed by hand in Surface" || got.ApprovalOwed {
		t.Fatalf("closed: %+v", got)
	}

	// Closed by the Designer after approval: the closed: line is the reason.
	k := approvedOn(t, h, m, box, gh, "Closed later", 12, headA)
	box.put(designerReply("Closed later-c", k.ID, "closed: superseded by job d_other", 5))
	_, _ = m.refresh(ctx, k.ID)
	if got, _ = m.st.get(ctx, k.ID); got.Status != StatusClosed || got.ClosedReason != "superseded by job d_other" {
		t.Fatalf("closed by the Designer: %q %q", got.Status, got.ClosedReason)
	}
}

// A feedback round withdraws the open approval card: answering it afterwards approves nothing.
func TestFeedbackWithdrawsTheApprovalCard(t *testing.T) {
	h, m, box := startWith(t, time.Hour)
	ctx := context.Background()
	j := newJob(t, h, "Feedback")
	box.put(designerReply("f1", j.ID, "preview: http://laptop.test:1/\nhead: "+headA+"\npr: "+prA, 1))
	_, _ = m.refresh(ctx, j.ID)
	card, err := m.askApproval(ctx, j.ID, time.Minute)
	if err != nil || card == nil {
		t.Fatal(card, err)
	}
	if _, err = h.Tool("design_feedback", `{"id":"`+j.ID+`","text":"make it bigger"}`); err != nil {
		t.Fatal(err)
	}
	if c, _, _ := h.CardStore.Get(ctx, card.ID); c.Status != "canceled" {
		t.Fatalf("the card stayed open: %+v", c)
	}
	sent := len(box.sent)
	late := module.Card{ID: card.ID, Status: "submitted", Outcome: &module.CardOutcome{Decisions: []module.CardDecision{{Action: "approve"}}}}
	if res, _ := m.settle(ctx, j.ID, late); res["approved"] != false {
		t.Fatalf("a withdrawn card approved: %v", res)
	}
	got, _ := m.st.get(ctx, j.ID)
	if got.Status != StatusChangesRequested || got.ApprovedHead != "" || len(box.sent) != sent {
		t.Fatalf("job %+v, messages %d -> %d", got, sent, len(box.sent))
	}
	// A new ask after the round is a new card, and it can be approved.
	again, err := m.askApproval(ctx, j.ID, time.Minute)
	if err != nil || again == nil || again.ID == card.ID {
		t.Fatal(again, err)
	}
	if res, _ := m.settle(ctx, j.ID, approve(t, h, *again)); res["approved"] != true {
		t.Fatalf("the new card: %v", res)
	}
}

// counted serves a githubAt, counting branch reads, and refuses (the rate limit) while refuse
// is set.
type counted struct {
	*githubAt
	compares atomic.Int32
	refuse   atomic.Bool
	fail     atomic.Bool
}

func (c *counted) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.URL.Path, "/compare/") {
		c.compares.Add(1)
	}
	if c.fail.Load() {
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	if c.refuse.Load() {
		http.Error(w, "rate limited", http.StatusForbidden)
		return
	}
	c.githubAt.ServeHTTP(w, r)
}

func startCounted(t *testing.T) (*moduletest.Host, *mod, *mailbox, *counted) {
	t.Helper()
	h, m, box := startWith(t, time.Hour)
	set(t, h, map[string]string{"repo": "o/r"})
	c := &counted{githubAt: &githubAt{}}
	srv := httptest.NewServer(c)
	t.Cleanup(srv.Close)
	m.gh.api = srv.URL
	return h, m, box, c
}

// owedJob is a job whose ask is owed: the Designer named headB, GitHub shows headA.
func owedJob(t *testing.T, h *moduletest.Host, m *mod, box *mailbox, gh *counted, title string) Job {
	t.Helper()
	ctx := context.Background()
	j := newJob(t, h, title)
	gh.set(headA, pull(9, headA, false))
	box.put(designerReply(title+"-1", j.ID, "preview: http://laptop.test:1/\nhead: "+headB+"\npr: https://github.com/o/r/pull/9", 1))
	_, _ = m.refresh(ctx, j.ID)
	if c, err := m.askApproval(ctx, j.ID, time.Minute); c != nil || err == nil {
		t.Fatal("approvable at another commit")
	}
	if got, _ := m.st.get(ctx, j.ID); !got.ApprovalOwed {
		t.Fatal("not owed")
	}
	return j
}

// Review finding: an ask in flight (waiting out GitHub's lag) when a feedback round is sent.
// Whatever card the ask raises is withdrawn by the round; answering it approves nothing.
func TestFeedbackDuringAnAskInFlight(t *testing.T) {
	h, m, box, gh := startCounted(t)
	ctx := context.Background()
	m.lag = []time.Duration{60 * time.Millisecond, 60 * time.Millisecond}
	j := newJob(t, h, "Race")
	gh.set(headA, pull(9, headA, false))
	box.put(designerReply("x1", j.ID, "preview: http://laptop.test:1/\nhead: "+headB+"\npr: https://github.com/o/r/pull/9", 1))
	_, _ = m.refresh(ctx, j.ID)
	asked := make(chan *module.Card, 1)
	go func() { c, _ := m.askApproval(ctx, j.ID, time.Minute); asked <- c }()
	time.Sleep(20 * time.Millisecond)
	go func() { time.Sleep(40 * time.Millisecond); gh.set(headB, pull(9, headB, false)) }()
	if _, err := h.Tool("design_feedback", `{"id":"`+j.ID+`","text":"smaller"}`); err != nil {
		t.Fatal(err)
	}
	card := <-asked
	if card == nil {
		t.Skip("the ask did not raise a card in time; the ordering under test did not happen")
	}
	if c, _, _ := h.CardStore.Get(ctx, card.ID); c.Status == "" {
		t.Fatalf("the card raised before the round is still open: %+v", c)
	}
	late := module.Card{ID: card.ID, Status: "submitted", Outcome: &module.CardOutcome{Decisions: []module.CardDecision{{Action: "approve"}}}}
	if res, _ := m.settle(ctx, j.ID, late); res["approved"] != false {
		t.Fatalf("approved after the feedback round: %v", res)
	}
	if got, _ := m.st.get(ctx, j.ID); got.Status != StatusChangesRequested || got.ApprovedHead != "" {
		t.Fatalf("job: %+v", got)
	}
}

// Review finding: GitHub refusing while an ask is owed. The last good read (the old head) is
// not taken as showing the new head: no card is raised for a head GitHub never showed.
func TestOwedAskNeedsAFreshRead(t *testing.T) {
	h, m, box, gh := startCounted(t)
	ctx := context.Background()
	j := owedJob(t, h, m, box, gh, "Limited")
	gh.refuse.Store(true)
	_, _ = m.refresh(ctx, j.ID)
	if open := openCards(h, "approval"); len(open) != 0 {
		t.Fatalf("asked on a read GitHub refused: %+v", open)
	}
	if got, _ := m.st.get(ctx, j.ID); !got.ApprovalOwed {
		t.Fatal("no longer owed")
	}
}

// Review finding: an owed ask that GitHub keeps not showing reads GitHub once per try and backs
// off between tries, instead of several reads on every check.
func TestOwedAskBacksOff(t *testing.T) {
	h, m, box, gh := startCounted(t)
	ctx := context.Background()
	m.lag = []time.Duration{time.Millisecond, time.Millisecond}
	m.owedBase = time.Hour
	j := owedJob(t, h, m, box, gh, "Never")
	before := gh.compares.Load()
	for range 10 {
		_, _ = m.refresh(ctx, j.ID)
	}
	if n := gh.compares.Load() - before; n > 1 {
		t.Fatalf("%d branch reads over 10 checks", n)
	}
	if open := openCards(h, "approval"); len(open) != 0 {
		t.Fatal("asked on a head GitHub does not show")
	}
}

// Review finding: closing again keeps why it closed; reopening clears it.
func TestClosedReasonByHand(t *testing.T) {
	h, m, box, gh := startCounted(t)
	ctx := context.Background()
	k := newJob(t, h, "Reopen")
	gh.set(headA, pull(9, headA, false))
	box.put(designerReply("o1", k.ID, "preview: http://laptop.test:1/\nhead: "+headA+"\npr: https://github.com/o/r/pull/9", 1))
	_, _ = m.refresh(ctx, k.ID)
	if _, err := h.Tool("design_set", `{"id":"`+k.ID+`","status":"closed"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := m.st.db.ExecContext(ctx, `UPDATE jobs SET closed_reason = 'superseded by d_x' WHERE id = ?`, k.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Tool("design_set", `{"id":"`+k.ID+`","status":"closed"}`); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.st.get(ctx, k.ID); got.ClosedReason != "superseded by d_x" {
		t.Fatalf("closing again replaced the reason: %q", got.ClosedReason)
	}
	if _, err := h.Tool("design_set", `{"id":"`+k.ID+`","status":"changes requested"}`); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.st.get(ctx, k.ID); got.ClosedReason != "" {
		t.Fatalf("a reopened job keeps a closed reason: %q", got.ClosedReason)
	}
}

// Review finding: GitHub failing (5xx, timeout) while an ask is owed is not taken as showing
// the head: no card is raised until a clean read shows it.
func TestOwedAskNotOnAGitHubFailure(t *testing.T) {
	h, m, box, gh := startCounted(t)
	ctx := context.Background()
	j := owedJob(t, h, m, box, gh, "Failing")
	gh.fail.Store(true)
	m.gh.mu.Lock()
	m.gh.cache = nil // nothing read before: no stale read to fall back on
	m.gh.mu.Unlock()
	_, _ = m.refresh(ctx, j.ID)
	if open := openCards(h, "approval"); len(open) != 0 {
		t.Fatalf("asked while GitHub failed: %+v", open)
	}
	gh.fail.Store(false)
	gh.set(headB, pull(9, headB, false))
	_, _ = m.refresh(ctx, j.ID)
	if open := openCards(h, "approval"); len(open) != 1 {
		t.Fatalf("not asked once GitHub showed the head: %+v", open)
	}
}

// Closing a job by hand tells the Design team to stop and close the pull request; a merged job
// cannot be closed, and closing again sends nothing more.
func TestClosingByHandTellsTheDesigner(t *testing.T) {
	h, m, box := startWith(t, time.Hour)
	ctx := context.Background()
	j := newJob(t, h, "Dismissed")
	box.put(designerReply("d1", j.ID, "head: "+headA+"\npr: "+prA, 1))
	_, _ = m.refresh(ctx, j.ID)
	sent := len(box.sent)
	if _, err := h.Tool("design_set", `{"id":"`+j.ID+`","status":"closed"}`); err != nil {
		t.Fatal(err)
	}
	if len(box.sent) != sent+1 {
		t.Fatalf("messages %d -> %d", sent, len(box.sent))
	}
	last := box.sent[len(box.sent)-1]
	for _, want := range []string{"closed by the owner", "Close " + prA + " without merging", "\nclosed: closed by hand in Surface\n"} {
		if !strings.Contains(last.Body, want) {
			t.Errorf("the closing notice lacks %q:\n%s", want, last.Body)
		}
	}
	if _, err := h.Tool("design_set", `{"id":"`+j.ID+`","status":"closed"}`); err != nil || len(box.sent) != sent+1 {
		t.Fatalf("closing again: %v, messages %d", err, len(box.sent))
	}
	if _, err := m.st.db.ExecContext(ctx, `UPDATE jobs SET status = 'merged' WHERE id = ?`, j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Tool("design_set", `{"id":"`+j.ID+`","status":"closed"}`); err == nil {
		t.Fatal("closed a merged job")
	}
}

// Review finding: a close computed from an older read never overwrites a merge that landed
// meanwhile, and then sends no closing notice.
func TestCloseDoesNotOverwriteAMerge(t *testing.T) {
	h, m, box := startWith(t, time.Hour)
	ctx := context.Background()
	j := newJob(t, h, "Raced")
	box.put(designerReply("r1", j.ID, "head: "+headA+"\npr: "+prA, 1))
	_, _ = m.refresh(ctx, j.ID)
	// The read set() makes says "in progress"; the merge lands before its write.
	if _, err := m.st.db.ExecContext(ctx, `UPDATE jobs SET status = 'merged' WHERE id = ?`, j.ID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := m.st.setStatus(ctx, j.ID, StatusInProgress, StatusClosed, "closed by hand in Surface"); ok {
		t.Fatal("the close overwrote the merge")
	}
	sent := len(box.sent)
	if _, err := h.Tool("design_set", `{"id":"`+j.ID+`","status":"changes requested"}`); err == nil {
		t.Fatal("a merged job's status was changed by hand")
	}
	if got, _ := m.st.get(ctx, j.ID); got.Status != StatusMerged || len(box.sent) != sent {
		t.Fatalf("job %s, messages %d -> %d", got.Status, sent, len(box.sent))
	}
}
