package agent

import (
	"fmt"
	"github.com/hollis-labs/libs/agent/chat"
	"github.com/hollis-labs/libs/agent/chat/sqlitestore"
	"path/filepath"
	"strings"
	"testing"

	nanite "github.com/hollis-labs/nanite/agent"
	"github.com/hollis-labs/nanite/chatrunner"
)

func TestNaniteConcurrentFirstUseCreatesOneConversation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "seat")
	if _, err := plantSeat(dir, "seat"); err != nil {
		t.Fatal(err)
	}
	runner, err := chatrunner.Open(t.Context(), chatrunner.Options{Agent: nanite.Options{Dir: dir, DataDir: t.TempDir(), GlobalSkillsDir: "-"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runner.Close() }()
	w := &wire{nanite: runner}
	const rounds, callers = 10, 32
	for round := range rounds {
		start := make(chan struct{})
		results := make(chan error, callers)
		id := fmt.Sprintf("first-use-%d", round)
		for range callers {
			go func() { <-start; results <- w.ensureNaniteConversation(t.Context(), id) }()
		}
		close(start)
		for range callers {
			if initErr := <-results; initErr != nil {
				t.Fatal(initErr)
			}
		}
		c, getErr := runner.Agent().Conversation(t.Context(), chatrunner.ConversationID(id))
		if getErr != nil {
			t.Fatal(getErr)
		}
		// A completed initialization must release the writer for the first real turn.
		if recordErr := c.Record(t.Context(), "first turn", "first answer"); recordErr != nil {
			t.Fatal(recordErr)
		}
	}
	conversations, err := runner.Agent().Conversations(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(conversations) != rounds {
		t.Fatalf("conversations=%d, want %d", len(conversations), rounds)
	}
}

func TestNaniteSeedsLegacyThreadWithoutChatRunRecords(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "seat")
	if _, err := plantSeat(dir, "seat"); err != nil {
		t.Fatal(err)
	}
	runner, err := chatrunner.Open(t.Context(), chatrunner.Options{Agent: nanite.Options{Dir: dir, DataDir: t.TempDir(), GlobalSkillsDir: "-"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runner.Close() }()
	store, err := sqlitestore.Open(t.Context(), filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if _, _, err = store.Ensure(t.Context(), chat.NewConversation{ID: "legacy", Agent: "seat", Owner: "owner"}); err != nil {
		t.Fatal(err)
	}
	if err = store.SetRunnerRef(t.Context(), "legacy", "thr_previous"); err != nil {
		t.Fatal(err)
	}
	wrapper := naniteWireRunner{Runner: runner, wire: &wire{nanite: runner, store: store}}
	turn := chat.Turn{ConversationID: "legacy", Text: "hello"}
	prepared, err := wrapper.prepare(t.Context(), turn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(prepared.Text, historySeed) {
		t.Fatal("missing continuity note")
	}
	if err = wrapper.Record(t.Context(), turn, "welcome"); err != nil {
		t.Fatal(err)
	}
	prepared, err = wrapper.prepare(t.Context(), turn)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Text != turn.Text {
		t.Fatal("continuity note repeated")
	}
}
