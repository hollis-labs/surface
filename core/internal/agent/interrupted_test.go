package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/libs/agent/chat"
	"github.com/hollis-labs/libs/agent/chat/chattest"
	"github.com/hollis-labs/libs/agent/chat/sqlitestore"
	nanite "github.com/hollis-labs/nanite/agent"
	"github.com/hollis-labs/nanite/chatrunner"
	"github.com/hollis-labs/surface/module"
)

// fakeRecovery records what the routes ask of the runner and answers with err.
type fakeRecovery struct {
	calls []string
	err   error
}

func (f *fakeRecovery) Resume(_ context.Context, id string) error {
	f.calls = append(f.calls, "resume "+id)
	return f.err
}

func (f *fakeRecovery) Discard(_ context.Context, id string) error {
	f.calls = append(f.calls, "discard "+id)
	return f.err
}

func (f *fakeRecovery) ResetChat(_ context.Context, id string) (string, error) {
	f.calls = append(f.calls, "reset "+id)
	if f.err != nil {
		return "", f.err
	}
	return "chat-" + id + "-g1", nil
}

func recoveryRoutes(t *testing.T, runner turnRecovery, busy func(string) bool) func(module.Actor, string) *httptest.ResponseRecorder {
	t.Helper()
	t.Setenv("SURFACE_AGENT_RUNNER", RunnerScripted)
	s, _, _, _ := setup(t, Options{})
	mux := http.NewServeMux()
	s.Mount(mux) // the wire's own routes, so the recovery routes are proven not to clash with them
	mapped := func(_ context.Context, id string) (string, error) { return "mapped-" + id, nil }
	s.mountRecovery(mux, runner, mapped, busy)
	return func(actor module.Actor, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", Prefix+"/conversations/c1/interrupted/"+path, nil)
		mux.ServeHTTP(rec, req.WithContext(module.WithActor(req.Context(), actor)))
		return rec
	}
}

func TestInterruptedTurnActionsAreTheOwnersAndReachTheRunner(t *testing.T) {
	runner := &fakeRecovery{}
	post := recoveryRoutes(t, runner, func(string) bool { return false })
	for _, actor := range []module.Actor{module.ActorOutside, module.ActorAgent, ""} {
		if res := post(actor, "reset"); res.Code != http.StatusForbidden {
			t.Fatalf("%q reset=%d %s", actor, res.Code, res.Body)
		}
	}
	if len(runner.calls) != 0 {
		t.Fatalf("a delegate reached the runner: %v", runner.calls)
	}
	for _, action := range []string{"resume", "discard"} {
		if res := post(module.ActorOwner, action); res.Code != http.StatusOK {
			t.Fatalf("%s=%d %s", action, res.Code, res.Body)
		}
	}
	res := post(module.ActorOwner, "reset")
	var out struct {
		Conversation string `json:"conversation"`
	}
	if res.Code != http.StatusOK || json.Unmarshal(res.Body.Bytes(), &out) != nil || out.Conversation != "chat-mapped-c1-g1" {
		t.Fatalf("reset=%d %s", res.Code, res.Body)
	}
	if res := post(module.ActorOwner, "explode"); res.Code != http.StatusNotFound {
		t.Fatalf("unknown=%d", res.Code)
	}
	want := []string{"resume mapped-c1", "discard mapped-c1", "reset mapped-c1"}
	if strings.Join(runner.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls %v, want %v", runner.calls, want)
	}
}

func TestResetIsRefusedWhileARunIsActive(t *testing.T) {
	// A run the wire started on the conversation: refused before the runner is asked.
	runner := &fakeRecovery{}
	control := &controlledRunner{}
	run := &cancelRun{stopped: make(chan struct{})}
	control.Runner = cancelRunner{Scripted: chattest.NewScripted(), run: run}
	active, err := control.Start(t.Context(), chat.Turn{ConversationID: "c1"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range active.Events() {
		}
	}()
	post := recoveryRoutes(t, runner, control.busy)
	if res := post(module.ActorOwner, "reset"); res.Code != http.StatusConflict || !strings.Contains(res.Body.String(), resetBusyMessage) {
		t.Fatalf("reset during a run=%d %s", res.Code, res.Body)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("the runner was asked during a run: %v", runner.calls)
	}
	if control.busy("other") {
		t.Fatal("a run on c1 made another conversation busy")
	}
	run.Cancel()
	<-done
	if control.busy("c1") {
		t.Fatal("an ended run still holds the conversation")
	}

	// The runner's own refusals: a run or a resume it holds, and an interrupted turn left.
	for err, want := range map[error]string{
		nanite.ErrBusy:                   resetBusyMessage,
		chatrunner.ErrResetInterrupted:   resetInterruptedMessage,
		errors.New("chatrunner: closed"): "chatrunner: closed",
	} {
		runner = &fakeRecovery{err: err}
		post = recoveryRoutes(t, runner, control.busy)
		if res := post(module.ActorOwner, "reset"); res.Code != http.StatusConflict || !strings.Contains(res.Body.String(), want) {
			t.Fatalf("%v: reset=%d %s", err, res.Code, res.Body)
		}
	}
}

// A reset moves the chat to a new Nanite conversation, keeps the old one on disk, and the
// wire's next turn goes to the new one.
func TestResetKeepsTheOldConversationAndTurnsGoToTheNewOne(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "seat")
	if _, err := plantSeat(dir, "seat"); err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	runner, err := chatrunner.Open(t.Context(), chatrunner.Options{Agent: nanite.Options{Dir: dir, DataDir: data, GlobalSkillsDir: "-"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runner.Close() }()
	store, err := sqlitestore.Open(t.Context(), filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	w := &wire{nanite: runner, store: store}
	wrapper := naniteWireRunner{Runner: runner, wire: w}
	turn := chat.Turn{ConversationID: "c1", Text: "before"}
	if err = wrapper.Record(t.Context(), turn, "old answer"); err != nil {
		t.Fatal(err)
	}
	previous := runner.ConversationOf("c1")

	t.Setenv("SURFACE_AGENT_RUNNER", RunnerScripted)
	s, _, _, _ := setup(t, Options{})
	mux := http.NewServeMux()
	s.mountRecovery(mux, runner, w.naniteChatID, func(string) bool { return false })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", Prefix+"/conversations/c1/interrupted/reset", nil)
	mux.ServeHTTP(rec, req.WithContext(module.WithActor(req.Context(), module.ActorOwner)))
	if rec.Code != http.StatusOK {
		t.Fatalf("reset=%d %s", rec.Code, rec.Body)
	}
	next := runner.ConversationOf("c1")
	if next == previous || !strings.Contains(rec.Body.String(), next) {
		t.Fatalf("reset to %s from %s: %s", next, previous, rec.Body)
	}

	if err = wrapper.Record(t.Context(), chat.Turn{ConversationID: "c1", Text: "after"}, "new answer"); err != nil {
		t.Fatal(err)
	}
	texts := func(id string) string {
		c, convErr := runner.Agent().Conversation(t.Context(), id)
		if convErr != nil {
			t.Fatalf("%s: %v", id, convErr)
		}
		hist, histErr := c.History(t.Context())
		if histErr != nil {
			t.Fatal(histErr)
		}
		var out []string
		for _, m := range hist {
			for _, b := range m.Blocks {
				if b.Type == "text" {
					out = append(out, b.Text)
				}
			}
		}
		return strings.Join(out, "|")
	}
	if got := texts(previous); !strings.Contains(got, "before") || strings.Contains(got, "after") {
		t.Fatalf("the old conversation was not kept as it was: %q", got)
	}
	if got := texts(next); !strings.Contains(got, "after") || strings.Contains(got, "old answer") {
		t.Fatalf("the new conversation: %q", got)
	}
}
