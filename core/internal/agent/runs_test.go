package agent

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/hollis-labs/libs/agent/chat"
	"github.com/hollis-labs/libs/agent/chat/chattest"
	"github.com/hollis-labs/libs/agent/host"
	"github.com/hollis-labs/surface/core/internal/storage"
)

type ends struct {
	mu      sync.Mutex
	started []string
	ended   map[string]string
}

func (e *ends) watch() runWatch {
	e.ended = map[string]string{}
	return runWatch{
		started: func(id string) { e.mu.Lock(); e.started = append(e.started, id); e.mu.Unlock() },
		ended:   func(id, status, _ string) { e.mu.Lock(); e.ended[id] = status; e.mu.Unlock() },
	}
}

func TestTheConversationWiresRunsSayHowTheyEnded(t *testing.T) {
	var e ends
	w := e.watch()
	r := watchedRunner{Runner: chattest.NewScripted(), started: w.started, ended: w.ended}
	for id, text := range map[string]string{"r-hello": "hello", "r-error": "error", "r-abort": "abort"} {
		run, err := r.Start(context.Background(), chat.Turn{ConversationID: "c", RunID: id, Text: text})
		if err != nil {
			t.Fatal(err)
		}
		for range run.Events() { //nolint:revive // drain the run
		}
	}
	want := map[string]string{"r-hello": RunFinished, "r-error": RunFailed, "r-abort": RunCanceled}
	for id, status := range want {
		if e.ended[id] != status {
			t.Errorf("%s ended %q, want %q", id, e.ended[id], status)
		}
	}
	if len(e.started) != 3 {
		t.Errorf("started: %v", e.started)
	}
}

func TestTheHostEnginesRunsSayHowTheyEnded(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	sq, err := host.NewSQLite(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	var e ends
	w := e.watch()
	st := watchedStore{Store: sq, started: w.started, ended: w.ended}
	thread, err := st.MainThread(ctx, Owner)
	if err != nil {
		t.Fatal(err)
	}
	hostWords := map[string]string{"a": "finished", "b": "cancelled", "c": "error"} //nolint:misspell // the host library's own words
	for id, status := range hostWords {
		if err := st.StartRun(ctx, id, thread, Owner, "m"); err != nil {
			t.Fatal(err)
		}
		if err := st.FinishRun(ctx, id, host.RunEnd{Status: status}); err != nil {
			t.Fatal(err)
		}
	}
	want := map[string]string{"a": RunFinished, "b": RunCanceled, "c": RunFailed}
	for id, status := range want {
		if e.ended[id] != status {
			t.Errorf("%s ended %q, want %q", id, e.ended[id], status)
		}
	}
}
