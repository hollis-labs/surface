package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
)

func encode(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func fixture() Brief {
	return Brief{Title: "Review changes", Outcome: "A reviewed module", UserSteps: "Approve the brief", AgentSteps: "Propose transitions", SystemSteps: "Store revisions and publish changes", Acceptance: "Check stale replies", Stage: "draft"}
}
func TestWorkflowReviewAndHandoff(t *testing.T) {
	h := moduletest.Start(t, &mod{})
	out, err := h.ToolAs(module.ActorAgent, "workflows_save", encode(t, fixture()))
	if err != nil {
		t.Fatal(err)
	}
	b := out.(Brief)
	if b.Revision != 1 {
		t.Fatal(b)
	}
	code, body := h.Do("GET", "/briefs/"+b.ID, "")
	if code != 200 || !strings.Contains(body, b.Title) {
		t.Fatalf("read: %d %s", code, body)
	}
	if _, err = h.Tool("workflows_handoff", encode(t, map[string]string{"id": b.ID})); err == nil {
		t.Fatal("draft handed off")
	}
	b.Stage = "ready"
	if _, err = h.Tool("workflows_save", encode(t, b)); err == nil {
		t.Fatal("skipped review")
	}
	b.Stage = "review"
	out, err = h.ToolAs(module.ActorAgent, "workflows_save", encode(t, b))
	if err != nil {
		t.Fatal(err)
	}
	b = out.(Brief)
	b.Stage = "ready"
	if _, err = h.ToolAs(module.ActorAgent, "workflows_save", encode(t, b)); err == nil {
		t.Fatal("agent approved")
	}
	code, body = h.Do("POST", "/briefs", encode(t, b))
	if code != 200 {
		t.Fatalf("owner ready: %d %s", code, body)
	}
	if err = json.Unmarshal([]byte(body), &b); err != nil {
		t.Fatal(err)
	}
	out, err = h.Tool("workflows_handoff", encode(t, map[string]string{"id": b.ID}))
	if err != nil {
		t.Fatal(err)
	}
	result := out.(map[string]any)
	if result["sent"] != false || !strings.Contains(result["text"].(string), "System behavior") {
		t.Fatal(result)
	}
	b.Title = "Different agreement"
	if _, err = h.Tool("workflows_save", encode(t, b)); err == nil {
		t.Fatal("changed approved content without returning to draft")
	}
	b.Stage = "draft"
	if _, err = h.Tool("workflows_save", encode(t, b)); err != nil {
		t.Fatal(err)
	}
	if len(h.Bus.Published) != 4 {
		t.Fatalf("successful changes should emit events: %v", h.Bus.Published)
	}
}
func TestOwnerRoutesAndDraftProtection(t *testing.T) {
	h := moduletest.Start(t, &mod{})
	for _, actor := range []module.Actor{module.ActorAgent, module.ActorOutside, module.ActorReflex} {
		req := httptest.NewRequest("POST", "/api/m/workflows/briefs", strings.NewReader(encode(t, fixture())))
		req = req.WithContext(module.WithActor(req.Context(), actor))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Fatalf("%s bypassed tool approval: %d", actor, rec.Code)
		}
	}
	out, err := h.Tool("workflows_save", encode(t, fixture()))
	if err != nil {
		t.Fatal(err)
	}
	b := out.(Brief)
	w, err := h.Desk().Open(context.Background(), "workflows", module.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h.DeskState.SetState(w.ID, map[string]any{"brief": b.ID})
	h.DeskState.SetWorking(w.ID, true, false)
	b.Outcome = "Overwritten"
	if _, err = h.ToolAs(module.ActorAgent, "workflows_save", encode(t, b)); !errors.Is(err, errWorking) {
		t.Fatalf("dirty brief: %v", err)
	}
	h.DeskState.SetWorking(w.ID, false, true)
	if _, err = h.ToolAs(module.ActorOutside, "workflows_save", encode(t, b)); !errors.Is(err, errWorking) {
		t.Fatalf("composing brief: %v", err)
	}
	h.DeskState.SetWorking(w.ID, false, false)
	if _, err = h.ToolAs(module.ActorAgent, "workflows_save", encode(t, b)); err != nil {
		t.Fatal(err)
	}
	if _, err = h.Tool("workflows_save", encode(t, b)); !errors.Is(err, errConflict) {
		t.Fatalf("stale save: %v", err)
	}
}
func TestConcurrentRevisionAndReviewValidation(t *testing.T) {
	m := &mod{}
	h := moduletest.Start(t, m)
	out, err := h.Tool("workflows_save", encode(t, fixture()))
	if err != nil {
		t.Fatal(err)
	}
	b := out.(Brief)
	b.Stage = "review"
	b.Acceptance = ""
	if _, err = m.st.save(context.Background(), b); err == nil {
		t.Fatal("incomplete review accepted")
	}
	b = out.(Brief)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, err := m.st.save(context.Background(), b); results <- err })
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, errConflict):
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
}
