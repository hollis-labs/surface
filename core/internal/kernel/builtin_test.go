package kernel

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hollis-labs/surface/module"
)

type entityTestMod struct {
	module.Base
}

func (entityTestMod) Manifest() module.Manifest {
	return module.Manifest{
		ID:    "ops",
		Title: "Operations",
		Refs: []module.RefKind{
			{
				Kind:  "ops.workstream",
				Label: "Workstream",
				Resolve: func(_ context.Context, _ module.Host, ids []string) (map[string]module.RefEntity, error) {
					out := map[string]module.RefEntity{}
					for _, id := range ids {
						if id == "phase-1-teams-land-and-steer" {
							out[id] = module.RefEntity{
								Title:       "Phase 1: Teams Land and Steer",
								Subtitle:    "Operations · In Flight",
								Description: "Coordinate team landing and steering.",
								Status:      &module.RefStatus{Label: "In Flight", Tone: "info"},
								Fields:      []module.RefField{{Label: "Id", Value: "ops:" + id, Mono: true}},
							}
						}
					}
					return out, nil
				},
				Record: func(_ context.Context, _ module.Host, id string) (module.RefEntity, error) {
					if id != "phase-1-teams-land-and-steer" {
						return module.RefEntity{}, module.ErrRefNotFound
					}
					return module.RefEntity{
						Title:       "Phase 1: Teams Land and Steer",
						Subtitle:    "Operations · In Flight",
						Description: "Coordinate team landing and steering.",
						Status:      &module.RefStatus{Label: "In Flight", Tone: "info"},
						Fields:      []module.RefField{{Label: "Id", Value: "ops:" + id, Mono: true}},
						Body:        "## Purpose\n\nCoordinate team landing and steering.",
					}, nil
				},
			},
			{
				Kind:  "registry.record",
				Label: "Registry record",
				Resolve: func(_ context.Context, _ module.Host, ids []string) (map[string]module.RefEntity, error) {
					out := map[string]module.RefEntity{}
					for _, id := range ids {
						if id == "project:surface" {
							out[id] = module.RefEntity{
								Title:       "Surface",
								Subtitle:    "project · project/v1",
								Description: "Agent development environment framework.",
							}
						}
					}
					return out, nil
				},
			},
		},
	}
}

func newTestAppWithRefs(t *testing.T) *App {
	t.Helper()
	ctx := context.Background()
	a, err := New(ctx, Options{
		Name:    "testapp",
		Title:   "Test App",
		DataDir: t.TempDir(),
		Modules: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Stop(ctx) })
	h := &host{app: a, id: "ops", mod: entityTestMod{}, man: entityTestMod{}.Manifest()}
	if err := a.addRefs(h); err != nil {
		t.Fatal(err)
	}
	return a
}

func findTool(t *testing.T, a *App, name string) BoundTool {
	t.Helper()
	for _, bt := range a.Tools() {
		if bt.Name == name {
			return bt
		}
	}
	t.Fatalf("tool %q not found", name)
	return BoundTool{}
}

func TestEntityCardShowResolvesLiveRecord(t *testing.T) {
	ctx := context.Background()
	a := newTestAppWithRefs(t)
	show := findTool(t, a, "card_show")

	out, err := show.Call(ctx, json.RawMessage(`{
		"kind": "entity",
		"title": "Workstream Status",
		"payload": {
			"ref": "ops:phase-1-teams-land-and-steer",
			"entity_kind": "ops.workstream",
			"view": "card"
		}
	}`))
	if err != nil {
		t.Fatalf("card_show entity failed: %v", err)
	}
	resMap, _ := out.(map[string]any)
	cardID, _ := resMap["id"].(string)
	if cardID == "" {
		t.Fatalf("expected card id, got %+v", out)
	}

	c, ok, err := a.Cards().Get(ctx, cardID)
	if err != nil || !ok {
		t.Fatalf("get card %s: ok=%v err=%v", cardID, ok, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(c.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	resolved, ok := payload["resolved"].(map[string]any)
	if !ok {
		t.Fatalf("expected resolved map in payload, got: %s", c.Payload)
	}
	entry, ok := resolved["ops:phase-1-teams-land-and-steer"].(map[string]any)
	if !ok {
		t.Fatalf("expected entry for ops:phase-1-teams-land-and-steer, got: %+v", resolved)
	}
	entity, _ := entry["entity"].(map[string]any)
	record, _ := entry["record"].(map[string]any)
	if entity["title"] != "Phase 1: Teams Land and Steer" || record["body"] == "" {
		t.Fatalf("unexpected resolved entity/record: entity=%+v record=%+v", entity, record)
	}
	data, _ := payload["data"].(map[string]any)
	if data["title"] != "Phase 1: Teams Land and Steer" {
		t.Fatalf("expected single-ref data snapshot populated, got: %+v", data)
	}
}

func TestEntityCardAskWithMultipleRefsAndHold(t *testing.T) {
	ctx := context.Background()
	a := newTestAppWithRefs(t)
	ask := findTool(t, a, "card_ask")

	out, err := ask.Call(ctx, json.RawMessage(`{
		"kind": "ops.workstream",
		"title": "Review linked entities",
		"hold": true,
		"payload": {
			"refs": ["ops:phase-1-teams-land-and-steer", "registry:project:surface"],
			"view": "detail",
			"question": "Should we advance this workstream?",
			"options": [
				{"id": "approve", "label": "Approve"},
				{"id": "hold", "label": "Hold"}
			]
		}
	}`))
	if err != nil {
		t.Fatalf("card_ask entity failed: %v", err)
	}
	resMap, _ := out.(map[string]any)
	if resMap["status"] != "held" {
		t.Fatalf("expected held status, got %+v", out)
	}
	ids, _ := resMap["ids"].([]string)
	if len(ids) != 1 {
		t.Fatalf("expected 1 card id, got %+v", resMap["ids"])
	}

	c, ok, err := a.Cards().Get(ctx, ids[0])
	if err != nil || !ok {
		t.Fatalf("get card: ok=%v err=%v", ok, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(c.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["entity_kind"] != "ops.workstream" {
		t.Errorf("expected entity_kind defaulted from card kind, got %v", payload["entity_kind"])
	}
	resolved, _ := payload["resolved"].(map[string]any)
	if len(resolved) != 2 || resolved["ops:phase-1-teams-land-and-steer"] == nil || resolved["registry:project:surface"] == nil {
		t.Fatalf("expected both refs resolved, got: %+v", resolved)
	}

	// Verify the held entity card accepts a late answer from the user.
	answered, err := a.Cards().Respond(ctx, c.ID, module.CardOutcome{
		Status:  "submitted",
		Answers: []module.CardAnswer{{QuestionID: "choice", Value: "approve"}},
	})
	if err != nil || !answered.Late || answered.Status != "submitted" {
		t.Fatalf("expected held entity card to accept late answer, got: %+v err=%v", answered, err)
	}
}
