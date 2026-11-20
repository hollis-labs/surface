package assist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hollis-labs/surface/module/moduletest"
)

func lifecycleTest(t *testing.T) (*moduletest.Host, *mod) {
	t.Helper()
	h, m, _ := start(t)
	m.stop()
	<-m.done
	<-m.lifecycleDone
	return h, m
}

func event(seq int64, kind string, p map[string]any) wireEvent {
	raw, _ := json.Marshal(p)
	return wireEvent{Seq: seq, Kind: kind, Time: fmt.Sprintf("2026-01-01T00:%02d:%02dZ", seq/60, seq%60), Payload: raw}
}
func outbound(id string) map[string]any {
	return map[string]any{"message_id": id, "from": me, "to": []string{chief}, "msg_kind": "request"}
}
func ingestTest(t *testing.T, m *mod, events ...wireEvent) {
	t.Helper()
	var seq int64
	for _, e := range events {
		seq = max(seq, e.Seq)
	}
	if _, err := m.st.ingest(context.Background(), "test", seq, events, []string{me}); err != nil {
		t.Fatal(err)
	}
}
func sentTest(t *testing.T, m *mod) []SentMessage {
	t.Helper()
	list, err := m.st.sentMessages(context.Background(), []string{me})
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestReceiptShapesAndOfflineAlert(t *testing.T) {
	cases := []struct{ name, raw, want string }{
		{"notify", `{"ok":true,"result":{"message":{"id":"m1","created_at":"2026-01-01T00:00:00Z"},"wake_reason":"offline","wake_delivered":false}}`, "queued"},
		{"send metadata", `{"message":{"id":"m1","metadata":{"tether.wake_outcome":"{\"reason\":\"offline\",\"delivered\":false}"}}}`, "queued"},
		{"wake object", `{"message_id":"m1","wake_outcome":{"reason":"busy","delivered":false}}`, "queued"},
		{"wake delivered", `{"result":{"message":{"id":"m1"},"wake_delivered":true}}`, "delivered"},
		{"legacy", `{"id":"m1"}`, "sent"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, m := lifecycleTest(t)
			id, err := m.trackSend(context.Background(), tc.raw, me, "request", "Review draft", chief)
			if err != nil || id != "m1" {
				t.Fatalf("receipt: %q %v", id, err)
			}
			list := sentTest(t, m)
			if len(list) != 1 || list[0].Delivery != tc.want {
				t.Fatalf("history: %+v", list)
			}
			cards, _ := h.CardStore.List(context.Background(), false)
			offline := strings.Contains(tc.raw, "offline")
			if offline && (len(cards) != 1 || !strings.Contains(list[0].Alert, "offline")) {
				t.Fatalf("offline notification: %+v / %+v", cards, list)
			}
			if !offline && len(cards) != 0 {
				t.Fatalf("unexpected alert: %+v", cards)
			}
			if offline {
				// A replay and an app restart must not recreate a dismissed notification.
				_, _ = h.CardStore.End(context.Background(), cards[0].ID, "canceled")
				another := &mod{h: h, st: m.st}
				if err := another.lifecycleAlerts(context.Background(), []string{me}); err != nil {
					t.Fatal(err)
				}
				cards, _ = h.CardStore.List(context.Background(), false)
				if len(cards) != 0 {
					t.Fatal("dismissed alert returned")
				}
			}
		})
	}
}

func TestLifecycleReplayIsolationAndReply(t *testing.T) {
	_, m := lifecycleTest(t)
	owned := outbound("m1")
	unrelated := outbound("private")
	unrelated["from"] = other
	events := []wireEvent{
		event(1, "message.sent", owned), event(2, "message.queued", owned),
		event(3, "wake.outcome", map[string]any{"message_id": "m1", "reason": "offline"}),
		event(4, "message.delivered", owned), event(5, "message.read", owned),
		event(6, "message.sent", unrelated),
		event(7, "request.acknowledged", map[string]any{"message_id": "m1", "task_id": "CW-20260101-0001"}),
		event(8, "message.sent", map[string]any{"message_id": "reply", "in_reply_to": "m1", "from": chief, "to": []string{me}}),
	}
	ingestTest(t, m, events...)
	ingestTest(t, m, events...)
	got := sentTest(t, m)
	if len(got) != 1 || len(got[0].Events) != 7 || got[0].Delivery != "replied" || got[0].Request != "acknowledged" || got[0].Alert != "" {
		t.Fatalf("projection: %+v", got)
	}
	// A separate store object reads persisted events, not in-memory state.
	again, err := (&store{db: m.st.db}).sentMessages(context.Background(), []string{me})
	if err != nil || len(again) != 1 || again[0].TaskID != "CW-20260101-0001" {
		t.Fatalf("restart: %+v %v", again, err)
	}
	hidden, err := m.st.sentMessages(context.Background(), []string{other})
	if err != nil || len(hidden) != 0 {
		t.Fatalf("settings filter: %+v %v", hidden, err)
	}
}

func TestConsumedDoesNotAcknowledgeAndDelayedQueueDoesNotRegress(t *testing.T) {
	_, m := lifecycleTest(t)
	p := outbound("m1")
	ingestTest(t, m, event(1, "message.sent", p), event(2, "message.consumed", p), event(3, "wake.outcome", map[string]any{"message_id": "m1", "reason": "offline"}), event(4, "message.queued", p))
	got := sentTest(t, m)[0]
	if got.Delivery != "consumed" || got.Request != "" || got.Alert != "" {
		t.Fatalf("consumed is not claimed: %+v", got)
	}
}

func TestRequestTaskCorrelationAndAliases(t *testing.T) {
	_, m := lifecycleTest(t)
	p := outbound("m1")
	p["task_id"] = "CW-20260101-0001"
	ingestTest(t, m, event(1, "message.sent", p), event(2, "request.submitted", p), event(3, "request.unacknowledged_alert", map[string]any{"task_id": p["task_id"], "reason": "ack_timeout"}))
	if got := sentTest(t, m)[0]; !strings.Contains(got.Alert, "not been acknowledged") {
		t.Fatalf("missing timeout: %+v", got)
	}
	//nolint:misspell // Torque wire status aliases.
	states := []struct{ wire, want string }{{"acknowledged", "acknowledged"}, {"in_progress", "in progress"}, {"doing", "in progress"}, {"blocked", "blocked"}, {"done", "done"}, {"cancelled", "declined"}, {"declined", "declined"}}
	for i, tc := range states {
		ingestTest(t, m, event(int64(i+4), "request.state_changed", map[string]any{"task_id": p["task_id"], "state": tc.wire, "summary": "State changed", "future_field": true}))
		got := sentTest(t, m)[0]
		if got.Request != tc.want || got.Alert != "" {
			t.Fatalf("state %s: %+v", tc.wire, got)
		}
	}
	// Reply delivery failure is independent from request completion.
	ingestTest(t, m, event(20, "routing.reply_undeliverable", map[string]any{"parent_id": "m1", "reply_id": "r1", "reason": "offline"}))
	if got := sentTest(t, m)[0]; got.Request != "declined" || got.Alert == "" {
		t.Fatalf("reply failure: %+v", got)
	}
	ingestTest(t, m, event(21, "routing.reply_delivered", map[string]any{"parent_id": "m1", "reply_id": "r1"}))
	if got := sentTest(t, m)[0]; got.Delivery != "replied" || got.Alert != "" {
		t.Fatalf("reply recovered: %+v", got)
	}
}

func TestMalformedBatchDoesNotAdvanceCursor(t *testing.T) {
	_, m := lifecycleTest(t)
	ingestTest(t, m, event(1, "message.sent", outbound("m1")))
	_, err := m.st.ingest(context.Background(), "test", 2, []wireEvent{{Seq: 2, Kind: "message.read", Payload: json.RawMessage(`broken`)}}, []string{me})
	if err == nil {
		t.Fatal("malformed batch accepted")
	}
	var cursor int64
	if err = m.st.db.QueryRow(`SELECT seq FROM lifecycle_cursor WHERE source='test'`).Scan(&cursor); err != nil || cursor != 1 {
		t.Fatalf("cursor %d %v", cursor, err)
	}
}

type eventCaller struct {
	calls   []map[string]any
	results []string
	err     error
}

func (f *eventCaller) Call(_ context.Context, _ string, args map[string]any) (string, error) {
	f.calls = append(f.calls, args)
	if f.err != nil {
		return "", f.err
	}
	result := f.results[0]
	f.results = f.results[1:]
	return result, nil
}

func TestSubscriberBootResumeGapAndFailure(t *testing.T) {
	_, m := lifecycleTest(t)
	p, _ := json.Marshal(outbound("m1"))
	history, _ := json.Marshal(map[string]any{"ok": true, "events": []wireEvent{{Seq: 10, At: "2026-01-01T00:00:00Z", Kind: "message.sent", PayloadJSON: string(p)}}, "next_cursor": 10})
	f := &eventCaller{results: []string{string(history), `{"ok":true,"events":[],"next_cursor":20}`, `{"ok":true,"gap":{"reason":"too_large","earliest_available":30,"high_water_seq":40},"next_cursor":20}`, `{"ok":true,"events":[],"next_cursor":40}`}}
	m.link = f
	for i := 0; i < 3; i++ {
		if err := m.syncLifecycle(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if f.calls[0]["limit"] != 25 || f.calls[1]["after_seq"] != int64(10) || f.calls[2]["after_seq"] != int64(20) {
		t.Fatalf("cursor calls: %+v", f.calls)
	}
	var cursor int64
	if err := m.st.db.QueryRow(`SELECT seq FROM lifecycle_cursor WHERE source=?`, m.lifecycleSource()).Scan(&cursor); err != nil || cursor != 40 {
		t.Fatalf("gap cursor %d %v", cursor, err)
	}
	f.err = errors.New("offline")
	if err := m.syncLifecycle(context.Background()); err == nil {
		t.Fatal("unavailable stream ignored")
	}
	if len(sentTest(t, m)) != 1 {
		t.Fatal("failure lost history")
	}
}

func TestReceiptCarriesTaskAndMissingReceiptDoesNotClaimSuccess(t *testing.T) {
	_, m := lifecycleTest(t)
	_, err := m.trackSend(context.Background(), `{"result":{"message":{"id":"m1","payload":{"task_id":"CW-20260101-0001"}}}}`, me, "request", "Review", chief)
	if err != nil || sentTest(t, m)[0].TaskID != "CW-20260101-0001" {
		t.Fatalf("task: %v", err)
	}
	for _, raw := range []string{`{}`, `not json`} {
		if _, err := parseReceipt(raw); err == nil || !strings.Contains(err.Error(), "do not resend") {
			t.Fatalf("missing receipt: %v", err)
		}
	}
}

func TestTwoOfflineSendsAndAlertRecovery(t *testing.T) {
	h, m := lifecycleTest(t)
	ctx := context.Background()
	for _, id := range []string{"m1", "m2"} {
		receipt := fmt.Sprintf(`{"result":{"message":{"id":%q},"wake_reason":"offline"}}`, id)
		if _, err := m.trackSend(ctx, receipt, me, "request", "Review", chief); err != nil {
			t.Fatal(err)
		}
	}
	cards, _ := h.CardStore.List(ctx, false)
	if len(cards) != 2 {
		t.Fatalf("two sends need distinct receipts: %+v", cards)
	}
	ingestTest(t, m, event(50, "message.read", outbound("m1")), event(51, "request.acknowledged", map[string]any{"message_id": "m2"}))
	if err := m.lifecycleAlerts(ctx, []string{me}); err != nil {
		t.Fatal(err)
	}
	cards, _ = h.CardStore.List(ctx, false)
	if len(cards) != 0 {
		t.Fatalf("resolved alerts remained: %+v", cards)
	}
}

func TestOwnerSendAndReplyTrackRealNotifyResult(t *testing.T) {
	h, m := lifecycleTest(t)
	m.link = &eventCaller{results: []string{`{"ok":true,"result":{"message":{"id":"new1"},"wake_reason":"offline"}}`}}
	got, err := h.Tool("owner_send", `{"to":"chief","kind":"request","subject":"Review","body":"Please review"}`)
	if err != nil || got.(map[string]any)["sent"] != "new1" {
		t.Fatalf("tool: %+v %v", got, err)
	}
	if list := sentTest(t, m); len(list) != 1 || list[0].Alert == "" {
		t.Fatalf("tool did not track: %+v", list)
	}
	m.link = &eventCaller{results: []string{`{"ok":true,"result":{"message":{"id":"reply1"},"wake_delivered":true}}`}}
	box, err := m.box()
	if err != nil {
		t.Fatal(err)
	}
	if err = box.reply(context.Background(), Item{ID: "original", Account: me, From: chief, Subject: "Question"}, "Yes", "", nil); err != nil {
		t.Fatal(err)
	}
	if list := sentTest(t, m); len(list) != 2 {
		t.Fatalf("reply not tracked: %+v", list)
	}
}
