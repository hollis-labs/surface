package reflex

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/hollis-labs/surface/core/internal/storage"
	"github.com/hollis-labs/surface/module"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// The keep-state reflexes are seeded on, valid, and the compaction one fires on the
// host runner's agent.compacted.
func TestTheKeepStateReflexesAreSeededOn(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, openDB(t), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	all, err := s.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Reflex{}
	for _, r := range all {
		byName[r.Name] = r
		if err := r.Validate(); err != nil {
			t.Errorf("seeded %s does not validate: %v", r.Name, err)
		}
	}
	for _, n := range []string{"save-state", "standing-rule"} {
		if r, ok := byName[n]; !ok || !r.Enabled {
			t.Fatalf("%s not seeded on: %+v", n, r)
		}
	}

	s.OnEvent(ctx, module.Event{Topic: "agent.compacted"}, "", nil)
	out := s.OnMessage(ctx, Message{Text: "what next?"}, nil)
	if len(out.Reminders) != 1 || !strings.Contains(out.Reminders[0], "Older turns were just summarized") {
		t.Fatalf("the compaction reminder was not given with the next message: %+v", out)
	}
	if out := s.OnMessage(ctx, Message{Text: "and then?"}, nil); len(out.Reminders) != 0 {
		t.Fatalf("the compaction reminder was given twice: %+v", out)
	}
	s.OnEvent(ctx, module.Event{Topic: "agent.compactedness"}, "", nil)
	s.OnEvent(ctx, module.Event{Topic: "agent.context"}, "", nil)
	if out := s.OnMessage(ctx, Message{Text: "hm"}, nil); len(out.Reminders) != 0 {
		t.Fatalf("another topic fired save-state: %+v", out)
	}
}

// The standing-rule pattern fires on the owner setting a rule, and not on ordinary talk.
func TestTheStandingRulePatternMatchesRulesOnly(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, openDB(t), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	fires := func(text string) bool {
		return slices.Contains(s.OnMessage(ctx, Message{Text: text}, nil).Fired, "standing-rule")
	}
	for _, text := range []string{
		"From now on, send delegations through Chief.",
		"going forward use the cards for questions",
		"Always copy Chief on those.",
		"ok. never send that kind of mail to the owner",
		"Remember that Archie owns conventions.",
		"please remember to log friction",
		"That's a standing rule.",
		"Don't ever delete a draft without asking.",
		"do not ever restart a seat",
		"You should always lead with the conclusion.",
		"make that a rule",
		"Never again page all of Tesseract at once.",
	} {
		if !fires(text) {
			t.Errorf("did not fire on %q", text)
		}
	}
	for _, text := range []string{
		"I always forget where the blue folder is.",
		"It never arrived, can you check?",
		"Never mind, it's fine.",
		"Do you remember that file from Tuesday?",
		"the forward going cursor is stuck",
		"what is the status of the deploy?",
		"Is it always this slow?",
	} {
		if fires(text) {
			t.Errorf("fired on %q", text)
		}
	}
}

// The seed leaves a reflex of the same name the owner already has as it is.
func TestTheSeedDoesNotOverwriteTheOwnersReflex(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	first, err := migrations.ReadFile("migrations/0001_reflexes.sql")
	if err != nil {
		t.Fatal(err)
	}
	// An app from before the seed: the table, and the owner's own save-state.
	if err = storage.Migrate(ctx, db, "reflex", fstest.MapFS{"m/0001_reflexes.sql": {Data: first}}, "m"); err != nil {
		t.Fatal(err)
	}
	own := Reflex{Name: "save-state", TriggerKind: TriggerText, Trigger: json.RawMessage(`{"pattern":"(?i)save"}`),
		ActionKind: ActionRemind, Action: json.RawMessage(`{"body":"mine"}`)}
	pre := &Set{db: db, log: func(string, ...any) {}}
	if err = pre.Put(ctx, own); err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, db, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Get(ctx, "save-state")
	if err != nil || !ok || got.TriggerKind != TriggerText || got.Enabled || !strings.Contains(string(got.Action), "mine") {
		t.Fatalf("the owner's save-state was changed: %+v %v", got, err)
	}
	if _, ok, _ := s.Get(ctx, "standing-rule"); !ok {
		t.Fatal("standing-rule was not seeded beside it")
	}
	// Deleted, it is not put back by a later start.
	if err = s.Delete(ctx, "standing-rule"); err != nil {
		t.Fatal(err)
	}
	if s, err = Open(ctx, db, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Get(ctx, "standing-rule"); ok {
		t.Fatal("a deleted seed came back")
	}
}
