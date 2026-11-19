package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hollis-labs/libs/core/util/classify"

	"github.com/hollis-labs/surface/module"
)

func TestEveryTracedTurnIsClassifiedInShadow(t *testing.T) {
	s, _, _, _ := setup(t, Options{})
	ctx := context.Background()
	before := s.cls.SlotDocs()

	if _, err := s.Send(ctx, "what is in the scratch pad?", "", ""); err != nil {
		t.Fatal(err)
	}
	turns, err := s.trace.Recent(ctx, 1)
	if err != nil || len(turns) != 1 {
		t.Fatalf("turns %v %v", turns, err)
	}
	var res classify.Result
	if err := json.Unmarshal(turns[0].Classified, &res); err != nil {
		t.Fatalf("classified %q: %v", turns[0].Classified, err)
	}
	if res.Label("act") != "question" || res.Phase != classify.Submit || !strings.HasPrefix(res.InputID, "turn:") {
		t.Errorf("act: %+v", res.Facets["act"])
	}
	slot, ok := res.Facets["slot"]
	if !ok || len(slot.Probabilities) != 3 { // general, notes, scratch
		t.Fatalf("slot facet: %+v", slot)
	}
	// Shadow: the selector's choice stands, whatever the classifier thought.
	if turns[0].Slot != "notes" {
		t.Errorf("the classifier changed the slot: %q", turns[0].Slot)
	}

	// The owner's pin after the turn is a correction the slot model learns.
	learned := s.cls.SlotDocs()
	if learned <= before {
		t.Fatalf("the turn's slot was not learned: %v -> %v", before, learned)
	}
	raw, _ := json.Marshal(map[string]string{"slot": "scratch"})
	if _, err := s.callOwnCtx(module.WithActor(ctx, module.ActorOwner), "context_switch", raw); err != nil {
		t.Fatal(err)
	}
	if got := s.cls.SlotDocs(); got != learned+1 {
		t.Errorf("the owner's pin was not learned as a correction: %v -> %v", learned, got)
	}
	// The agent's own pin is not a correction.
	if _, err := s.callOwnCtx(module.WithActor(ctx, module.ActorAgent), "context_switch", raw); err != nil {
		t.Fatal(err)
	}
	if got := s.cls.SlotDocs(); got != learned+1 {
		t.Errorf("the agent's pin was learned: %v", got)
	}
}

func TestClassifyRouteRecordsNothing(t *testing.T) {
	s, _, _, _ := setup(t, Options{})
	mux := http.NewServeMux()
	s.Mount(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/agent/classify", "application/json", strings.NewReader(`{"text":"/layout focus"}`))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("classify: %v %v", err, resp.Status)
	}
	defer func() { _ = resp.Body.Close() }()
	var res classify.Result
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatal(err)
	}
	if res.Label("act") != "command" {
		t.Errorf("act: %+v", res.Facets["act"])
	}
	if turns, _ := s.trace.Recent(context.Background(), 10); len(turns) != 0 {
		t.Errorf("a draft was traced: %+v", turns)
	}
}
