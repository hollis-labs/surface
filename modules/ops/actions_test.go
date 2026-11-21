package ops

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
)

type mailFake struct {
	*tether
	calls []map[string]any
	lost  bool
}

func (f *mailFake) Call(ctx context.Context, tool string, args map[string]any) (string, error) {
	if tool != "tether_message_send" {
		return f.tether.Call(ctx, tool, args)
	}
	f.calls = append(f.calls, args)
	if f.lost {
		f.lost = false
		return "", errors.New("lost response after delivery")
	}
	return `{"message":{"id":"test-message"}}`, nil
}
func TestSendOrchestrationContractAndRetry(t *testing.T) {
	ctx := context.Background()
	f := &mailFake{tether: newTether(), lost: true}
	m := &mod{caller: f}
	h := moduletest.Start(t, m)
	if err := h.Settings.Set(ctx, "sender_address", "msg://agent/test/surface"); err != nil {
		t.Fatal(err)
	}
	f.Put("ops/ws.alpha", "---\nops: v1\nid: alpha\ntitle: Alpha\nstage: planned\ncustom: keep\nlinks:\n - {kind: registry.record, id: 'workstream:test', role: identity}\n---\n## Custom\nKeep me.\n", "Alpha", "")
	in := sendInput{BaseRev: 1, RequestID: "request-one", Note: "Owner note"}
	if _, err := m.sendOrchestration(ctx, "alpha", in); err == nil {
		t.Fatal("lost response should leave a retryable request")
	}
	pending, _, _ := m.st.Get(ctx, "alpha")
	if pending.OrchestrationRequest == nil || pending.OrchestrationRequest.MessageID != "" {
		t.Fatalf("pending %#v", pending)
	}
	// The recipient can already have moved the workstream before the response is recovered.
	text, _ := f.Content("ops/ws.alpha")
	f.Put("ops/ws.alpha", strings.Replace(text, "stage: planned", "stage: in_flight", 1), "Alpha", "")
	got, err := m.sendOrchestration(ctx, "alpha", in)
	if err != nil {
		t.Fatal(err)
	}
	if got.OrchestrationRequest.MessageID != "test-message" || got.Stage != "in_flight" {
		t.Fatalf("receipt %#v", got)
	}
	if len(f.calls) != 2 || f.calls[0]["payload_json"] != f.calls[1]["payload_json"] || f.calls[0]["idempotency_key"] != f.calls[1]["idempotency_key"] {
		t.Fatal("retry changed envelope")
	}
	if _, err = m.sendOrchestration(ctx, "alpha", in); err != nil || len(f.calls) != 2 {
		t.Fatal("completed retry resent", err)
	}
	var p map[string]any
	if err = json.Unmarshal([]byte(f.calls[0]["payload_json"].(string)), &p); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]any{"kind": "request", "intent": "sequence_and_launch", "schema_version": float64(1), "awaiting": "@claude-code", "attention": "agent", "workstream_id": "alpha", "registry_id": "workstream:test", "cite": "ops:alpha", "stage": "planned", "note": "Owner note", "requested_by": "owner", "request_id": "request-one"} {
		if p[k] != want {
			t.Errorf("%s=%v want %v", k, p[k], want)
		}
	}
	if f.calls[0]["to"] != orchestratorAddress || f.calls[0]["kind"] != "request" {
		t.Fatal(f.calls[0])
	}
	content, _ := f.Content("ops/ws.alpha")
	if !strings.Contains(content, "custom: keep") || !strings.Contains(content, "Keep me.") {
		t.Fatal("lost foreign content")
	}
	if len(got.Transitions) != 1 || !strings.Contains(got.Transitions[0].Why, "sent to orchestration") {
		t.Fatal(got.Transitions)
	}
}

func TestSettleUnsettlePreservesDocument(t *testing.T) {
	ctx := context.Background()
	f := newTether()
	_, m := start(t, f)
	f.Put("ops/ws.done", "---\nops: v1\nid: done\ntitle: Done\nstage: complete\ncustom: keep\n---\n## Custom\nKeep me.\n", "Done", "")
	if _, err := m.st.Settle(ctx, "done", 2, Rollup{}, "owner"); err == nil {
		t.Fatal("stale revision accepted")
	}
	got, err := m.st.Settle(ctx, "done", 1, Rollup{Counts: map[string]int{"done": 2, "doing": 1}, FetchedAt: now(), Errors: []string{"source unavailable"}}, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if got.SettledAt == "" || got.SettledBy != "owner" || !strings.Contains(got.SettledSummary, "source unavailable") {
		t.Fatalf("settlement %#v", got)
	}
	content, _ := f.Content("ops/ws.done")
	if !strings.Contains(content, "stage: complete") || !strings.Contains(content, "Keep me.") || strings.Contains(content, "final_tracking") {
		t.Fatal(content)
	}
	got, err = m.st.Unsettle(ctx, "done", got.Rev, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if got.SettledAt != "" || got.SettledSummary != "" || got.SettledBy != "" {
		t.Fatal(got)
	}
	content, _ = f.Content("ops/ws.done")
	if !strings.Contains(content, "custom: keep") || !strings.Contains(content, "stage: complete") {
		t.Fatal(content)
	}
}
func TestSettledProjectionKeepsUnfinishedChildren(t *testing.T) {
	b := Board{Workstreams: []Workstream{{ID: "hidden", SettledAt: now()}, {ID: "child", Parent: "hidden", Stage: StageQueued}}, Queue: []string{"child"}}
	visible := b.unsettled()
	if len(visible.Workstreams) != 1 || visible.Workstreams[0].Parent != "" || len(visible.Queue) != 1 {
		t.Fatal(visible)
	}
	if b.Workstreams[1].Parent != "hidden" {
		t.Fatal("projection mutated source")
	}
}
func TestOwnerActionsRejectDelegates(t *testing.T) {
	h, _ := start(t, newTether())
	for _, action := range []string{"settle", "unsettle", "orchestration"} {
		for _, actor := range []module.Actor{module.ActorAgent, module.ActorOutside} {
			r := httptest.NewRequest(http.MethodPost, "/api/m/ops/workstreams/test/"+action, strings.NewReader(`{}`))
			r = r.WithContext(module.WithActor(r.Context(), actor))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusForbidden {
				t.Fatalf("%s %s: %d", actor, action, w.Code)
			}
		}
	}
}

func TestSendOrchestrationPreconditions(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, stage, sender, note string
		rev                       int
	}{
		{"no sender", "planned", "", "", 1},
		{"stage", "in_flight", "msg://agent/test/surface", "", 1},
		{"stale", "planned", "msg://agent/test/surface", "", 2},
		{"note", "planned", "msg://agent/test/surface", strings.Repeat("x", 4097), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &mailFake{tether: newTether()}
			m := &mod{caller: f}
			h := moduletest.Start(t, m)
			_ = h.Settings.Set(ctx, "sender_address", tc.sender)
			f.Put("ops/ws.alpha", "---\nops: v1\nid: alpha\ntitle: Alpha\nstage: "+tc.stage+"\n---\n", "Alpha", "")
			_, err := m.sendOrchestration(ctx, "alpha", sendInput{BaseRev: tc.rev, RequestID: "request-one", Note: tc.note})
			if err == nil || len(f.calls) != 0 {
				t.Fatalf("unexpected send: %v %#v", err, f.calls)
			}
		})
	}
}

func TestSettledBoardListAndHistory(t *testing.T) {
	f := newTether()
	h, m := start(t, f)
	f.Put("ops/ws.done", "---\nops: v1\nid: done\ntitle: Done\nstage: complete\nsettled_at: '2026-11-21T00:00:00Z'\nsettled_by: owner\nsettled_summary: Final snapshot.\n---\n", "Done", "")
	for _, show := range []bool{false, true} {
		out, err := m.boardResult(context.Background(), true, show)
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if show {
			want = 1
		}
		if len(out["workstreams"].([]Card)) != want {
			t.Fatal(out)
		}
		list, err := m.toolList(context.Background(), "", "", "", nil, true, show)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(js(list), `"id":"done"`) != show {
			t.Fatal(list)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/m/ops/workstreams/done/history", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Final snapshot.") {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, tool := range m.Manifest().Tools {
		if tool.Name == "ops_settle" && tool.Level != module.LevelWrite {
			t.Fatal("settle must ask owner")
		}
	}
	if !allowed["tether_message_send"] || allowed["tether_message_notify"] {
		t.Fatal("only message_send should be added")
	}
}
