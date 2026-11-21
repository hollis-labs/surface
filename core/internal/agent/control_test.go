package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hollis-labs/libs/agent/chat"
	"github.com/hollis-labs/libs/agent/chat/chattest"
	chatstream "github.com/hollis-labs/libs/ui/ui-go/chatstream"
	nanite "github.com/hollis-labs/nanite/agent"
	"github.com/hollis-labs/surface/module"
)

type refusalRunner struct{ chat.Runner }

func (r refusalRunner) Start(context.Context, chat.Turn) (chat.Run, error) {
	return nil, fmt.Errorf("admission: %w", nanite.ErrDailyBudget)
}

func TestDailyBudgetRefusalOnTurnRoute(t *testing.T) {
	t.Setenv("SURFACE_AGENT_RUNNER", RunnerScripted)
	s, _, _, _ := setup(t, Options{})
	s.wire.control.Runner = refusalRunner{Runner: s.wire.scripted}
	mux := http.NewServeMux()
	s.Mount(mux)
	post := func(path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(rec, req)
		return rec
	}
	view, err := s.wire.svc.EnsureConversation(t.Context(), s.wire.agent)
	if err != nil {
		t.Fatal(err)
	}
	base := Prefix + "/conversations/" + view.ConversationID
	att := post(base+"/attachments", `{"role":"controller","stream":"committed"}`)
	var attached struct {
		ID string `json:"attachment_id"`
	}
	if err = json.Unmarshal(att.Body.Bytes(), &attached); err != nil || attached.ID == "" {
		t.Fatalf("attach: %s", att.Body)
	}
	res := post(base+"/turns", `{"attachment":"`+attached.ID+`","text":"hello"}`)
	if res.Code != 429 || !strings.Contains(res.Body.String(), dailyBudgetMessage) || !strings.Contains(res.Body.String(), `"code":"daily_budget"`) {
		t.Fatalf("%d %s", res.Code, res.Body)
	}
}

type cancelRunner struct {
	*chattest.Scripted
	run *cancelRun
}

func (r cancelRunner) Start(context.Context, chat.Turn) (chat.Run, error) { return r.run, nil }

type cancelRun struct {
	stopped chan struct{}
	once    sync.Once
}

func (r *cancelRun) Cancel()                                             { r.once.Do(func() { close(r.stopped) }) }
func (r *cancelRun) Answer(context.Context, string, chat.Decision) error { return nil }
func (r *cancelRun) Events() iter.Seq2[chatstream.Event, error] {
	return func(y func(chatstream.Event, error) bool) {
		if !y(chatstream.Event{Verb: chatstream.VerbRunStart}, nil) {
			return
		}
		<-r.stopped
		y(chatstream.Event{Verb: chatstream.VerbRunAbort}, nil)
	}
}

func TestStopAllOwnerRouteCancelsAndScriptedNextTurnWorks(t *testing.T) {
	t.Setenv("SURFACE_AGENT_RUNNER", RunnerScripted)
	s, _, _, _ := setup(t, Options{})
	control := s.wire.control
	run := &cancelRun{stopped: make(chan struct{})}
	control.Runner = cancelRunner{Scripted: chattest.NewScripted(), run: run}
	active, err := control.Start(t.Context(), chat.Turn{})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range active.Events() {
		}
	}()
	mux := http.NewServeMux()
	s.Mount(mux)
	stop := func(actor module.Actor) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", Prefix+"/stop-all", nil)
		mux.ServeHTTP(rec, req.WithContext(module.WithActor(req.Context(), actor)))
		return rec
	}
	if res := stop(module.ActorOutside); res.Code != 403 {
		t.Fatalf("outside=%d", res.Code)
	}
	select {
	case <-run.stopped:
		t.Fatal("outside stopped work")
	default:
	}
	if res := stop(module.ActorOwner); res.Code != 200 || !strings.Contains(res.Body.String(), `"restart_required":false`) {
		t.Fatalf("stop=%d %s", res.Code, res.Body)
	}
	<-done
	control.Runner = chattest.NewScripted()
	next, err := control.Start(t.Context(), chat.Turn{Text: "next"})
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range next.Events() {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestStopAllNaniteRemainsStoppedEvenIfCleanupTimesOut(t *testing.T) {
	called := false
	r := &controlledRunner{Runner: chattest.NewScripted(), stopAgent: func(context.Context) error { called = true; return context.DeadlineExceeded }}
	if err := r.stopAll(t.Context()); !errors.Is(err, context.DeadlineExceeded) || !called {
		t.Fatal(err)
	}
	_, err := r.Start(t.Context(), chat.Turn{})
	var refusal *chat.RejectedError
	if !errors.As(err, &refusal) || refusal.Code != "agent_stopped" {
		t.Fatalf("%v", err)
	}
}
