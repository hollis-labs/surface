package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hollis-labs/surface/module"
)

func TestEveryTurnIsTracedWithWhatFollowed(t *testing.T) {
	s, k, fe, _ := setup(t, Options{})
	ctx := context.Background()

	res, err := sendTest(ctx, t, s, "tighten the second paragraph", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// The owner disagrees with the slot: they pin another one, then move to another window.
	owner := module.WithActor(ctx, module.ActorOwner)
	raw, _ := json.Marshal(map[string]string{"slot": "scratch"})
	if _, err = s.callOwnCtx(owner, "context_switch", raw); err != nil {
		t.Fatal(err)
	}
	s.traceEvent(module.Event{Topic: "desk.focus", Data: json.RawMessage(`{"window":"w2","view":"scratch","by":"owner"}`)})
	// The agent moving the focus is not a hint about the owner.
	s.traceEvent(module.Event{Topic: "desk.focus", Data: json.RawMessage(`{"window":"w1","view":"notes","by":"agent"}`)})

	turns, err := s.trace.Recent(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("want one turn traced, got %d", len(turns))
	}
	got := turns[0]
	if got.Text != "tighten the second paragraph" || got.Slot != "notes" || got.View != "notes" || got.RunID != res.RunID || got.RunID == "" {
		t.Errorf("the turn: %+v (run %q)", got, res.RunID)
	}
	if got.Why == "" || len(got.OpenViews) != 2 || got.BlockChars == 0 || len(got.TopOfMind) == 0 {
		t.Errorf("the turn lacks its context: %+v", got)
	}
	if len(got.Signals) != 3 || got.Signals[0].Kind != "run_end" || got.Signals[1].Kind != "pin" || got.Signals[1].Actor != "owner" ||
		!strings.Contains(string(got.Signals[1].Detail), `"from":"notes"`) || got.Signals[2].Kind != "focus" {
		t.Errorf("the signals: %+v", got.Signals)
	}

	var out bytes.Buffer
	if err := s.trace.Export(ctx, &out); err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(out.String(), "\n"); lines != 1 || !strings.Contains(out.String(), `"kind":"pin"`) {
		t.Errorf("export: %q", out.String())
	}
	_, _ = k, fe
}

func TestASignalWithNoTurnBeforeItIsDropped(t *testing.T) {
	s, _, _, _ := setup(t, Options{})
	ctx := context.Background()
	s.signal(module.WithActor(ctx, module.ActorOwner), "unpin", nil)
	if _, err := sendTest(ctx, t, s, "hello", "", ""); err != nil {
		t.Fatal(err)
	}
	turns, err := s.trace.Recent(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || len(turns[0].Signals) != 1 || turns[0].Signals[0].Kind != "run_end" {
		t.Errorf("a signal before any turn was kept: %+v", turns)
	}
}
