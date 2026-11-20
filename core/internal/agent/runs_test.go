package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/hollis-labs/libs/agent/chat"
	"github.com/hollis-labs/libs/agent/chat/chattest"
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

// runWatch is what an engine calls as its runs start and end.
type runWatch struct {
	started func(id string)
	ended   func(id, status, reason string)
}
