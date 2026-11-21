package ops

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestLegacyStagesReadWithoutRewritingHistory(t *testing.T) {
	for old, want := range map[string]string{
		"planning": StagePlanned, "planned": StagePlanned, "parked": StageParked,
		"queued": StageQueued, "flight": StageInFlight, "in flight": StageInFlight,
		"in_flight": StageInFlight, "review": StageLanding, "in_review": StageLanding,
		"adoption": StageLanding, "landing": StageLanding, "active": StageComplete,
		"complete": StageComplete, "dropped": StageDropped,
	} {
		t.Run(old, func(t *testing.T) {
			content := fmt.Sprintf("---\nops: v1\nid: legacy\nstage: %s\nstage_since: yesterday\ntransitions:\n  - {from: planning, to: adoption, at: yesterday}\nextra: preserved\n---\n## Notes\nKeep this.\n", old)
			w := parseWorkstream("legacy", content, 7)
			if w.Stage != want || w.StageSince != "yesterday" || w.Rev != 7 || w.Notes != "Keep this." {
				t.Fatalf("read: %+v", w)
			}
			if len(w.Transitions) != 1 || w.Transitions[0].From != "planning" || w.Transitions[0].To != "adoption" {
				t.Fatalf("history changed: %+v", w.Transitions)
			}
		})
	}
	if ReadStage("unknown") != "unknown" || ValidStage("planning") || ValidStage("active") {
		t.Fatal("legacy/unknown stages accepted for new writes")
	}
}

func TestNewStagesQueueAndGoalCompletion(t *testing.T) {
	f := newTether()
	_, m := start(t, f)
	ctx := context.Background()
	w, err := m.st.Create(ctx, NewWorkstream{ID: "stages", Title: "Stages"}, "owner")
	if err != nil || w.Stage != StagePlanned {
		t.Fatalf("default: %+v %v", w, err)
	}
	want := []string{StagePlanned, StageParked, StageQueued, StageInFlight, StageLanding, StageComplete, StageDropped}
	if !slices.Equal(Stages, want) {
		t.Fatalf("stage order: %v", Stages)
	}
	for _, stage := range want[1:] {
		w, err = m.st.Move(ctx, w.ID, stage, "owner move", "owner")
		if err != nil || w.Stage != stage {
			t.Fatalf("move to %s: %+v %v", stage, w, err)
		}
		if queued := slices.Contains(queue(t, m), w.ID); queued != (stage == StageQueued) {
			t.Fatalf("queue membership at %s: %v", stage, queued)
		}
	}
	for _, stage := range want {
		suggested, _ := suggest(stage, map[string]int{"done": 3})
		if suggested == StageComplete {
			t.Fatalf("task completion completed workstream at %s", stage)
		}
	}
}

func TestHeldLegacyAndUnknownStagePreserveDocument(t *testing.T) {
	for _, stage := range []string{"planning", "planned", "queued", "in_flight", "landing", "complete", "dropped", "future_stage"} {
		t.Run(stage, func(t *testing.T) {
			content := fmt.Sprintf("---\nops: v1\nid: held-doc\ntitle: Held doc\nstage: %s\nheld: {reason: awaiting window}\nsettled_at: someday\norchestration_request: request-123\nunknown_key: keep\n---\n## Notes\nOriginal.\n", stage)
			w := parseWorkstream("held-doc", content, 1)
			want := stage
			if stage == "planning" || stage == "planned" {
				want = StageParked
			}
			if w.Stage != want || w.Held == nil {
				t.Fatalf("held stage: %+v", w)
			}
			if stage == "future_stage" && !strings.Contains(w.Problem, "unknown stage") {
				t.Fatalf("unknown stage hidden: %+v", w)
			}
			f := newTether()
			f.Put("ops/ws.held-doc", content, "Held doc", "")
			h, _ := start(t, f)
			call(t, h, "ops_update", `{"id":"held-doc","base_rev":1,"notes":"Edited."}`)
			raw, _ := f.Content("ops/ws.held-doc")
			d, err := ParseDoc(raw)
			if err != nil {
				t.Fatal(err)
			}
			for key, value := range map[string]string{"stage": stage, "settled_at": "someday", "orchestration_request": "request-123", "unknown_key": "keep"} {
				if d.Str(key) != value {
					t.Fatalf("%s changed: %s", key, raw)
				}
			}
		})
	}
}
