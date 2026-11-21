package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"iter"
	"net/http"
	"strings"
	"sync"

	"github.com/hollis-labs/libs/agent/chat"
	chatstream "github.com/hollis-labs/libs/ui/ui-go/chatstream"
	nanite "github.com/hollis-labs/nanite/agent"
	"github.com/hollis-labs/surface/module"
)

const dailyBudgetMessage = "Daily token budget reached for this agent; resets at 00:00 UTC."
const stoppedMessage = "All agent work is stopped. Restart the app to accept new turns."

// controlledRunner serializes admission with stop-all and tracks host-started runs.
// Nanite's StopAll additionally stops its own subtasks and background work.
type controlledRunner struct {
	chat.Runner
	mu        sync.Mutex
	active    map[*controlledRun]struct{}
	stopped   bool
	stopAgent func(context.Context) error
}

func (r *controlledRunner) Start(ctx context.Context, turn chat.Turn) (chat.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return nil, &chat.RejectedError{Code: "agent_stopped", Message: stoppedMessage}
	}
	run, err := r.Runner.Start(ctx, turn)
	if errors.Is(err, nanite.ErrDailyBudget) {
		return nil, &chat.RejectedError{Code: "daily_budget", Message: dailyBudgetMessage}
	}
	if errors.Is(err, nanite.ErrStopped) {
		return nil, &chat.RejectedError{Code: "agent_stopped", Message: stoppedMessage}
	}
	if err != nil {
		return nil, err
	}
	tracked := &controlledRun{Run: run, owner: r, conversation: turn.ConversationID, done: make(chan struct{})}
	if r.active == nil {
		r.active = make(map[*controlledRun]struct{})
	}
	r.active[tracked] = struct{}{}
	return tracked, nil
}

func (r *controlledRunner) Record(ctx context.Context, turn chat.Turn, answer string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return &chat.RejectedError{Code: "agent_stopped", Message: stoppedMessage}
	}
	return r.Runner.Record(ctx, turn, answer)
}

func (r *controlledRunner) stopAll(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var waiting []<-chan struct{}
	for run := range r.active {
		run.Cancel()
		waiting = append(waiting, run.done)
	}
	if r.stopAgent != nil {
		// StopAll is terminal for this Nanite instance, even when cleanup times out.
		r.stopped = true
		if err := r.stopAgent(ctx); err != nil {
			return err
		}
	}
	for _, done := range waiting {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

type controlledRun struct {
	chat.Run
	owner        *controlledRunner
	conversation string
	done         chan struct{}
}

// busy reports a run the host started on the conversation that has not ended.
func (r *controlledRunner) busy(conversation string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for run := range r.active {
		if run.conversation == conversation {
			return true
		}
	}
	return false
}

func (r *controlledRun) Events() iter.Seq2[chatstream.Event, error] {
	return func(yield func(chatstream.Event, error) bool) {
		defer func() {
			// Signal before taking the lock: stopAll holds it while draining runs.
			close(r.done)
			r.owner.mu.Lock()
			delete(r.owner.active, r)
			r.owner.mu.Unlock()
		}()
		for ev, err := range r.Run.Events() {
			if !yield(ev, err) {
				return
			}
		}
	}
}

func (s *Service) stopAll(w http.ResponseWriter, r *http.Request) {
	if module.ActorOf(r.Context()) != module.ActorOwner {
		fail(w, http.StatusForbidden, "Only the owner can stop all agent work.")
		return
	}
	if err := s.wire.control.stopAll(r.Context()); err != nil {
		fail(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	restart := s.wire.nanite != nil
	message := "All running turns stopped."
	if restart {
		message = stoppedMessage
	}
	s.k.Events().Publish("agent.stopped", map[string]any{"restart_required": restart})
	reply(w, http.StatusOK, map[string]any{"restart_required": restart, "message": message})
}

// The shared wire maps typed rejections to 422. Preserve its authorization and
// decoding, changing only Surface's budget/stop refusal statuses on turn POSTs.
func refusalStatus(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/turns") {
			next.ServeHTTP(w, r)
			return
		}
		out := &turnResponse{header: make(http.Header), status: http.StatusOK}
		next.ServeHTTP(out, r)
		var body struct {
			Code string `json:"code"`
		}
		if out.status == http.StatusUnprocessableEntity && json.Unmarshal(out.Bytes(), &body) == nil {
			switch body.Code {
			case "daily_budget":
				out.status = http.StatusTooManyRequests
			case "agent_stopped":
				out.status = http.StatusServiceUnavailable
			}
		}
		for key, values := range out.header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(out.status)
		_, _ = w.Write(out.Bytes())
	})
}

type turnResponse struct {
	bytes.Buffer
	header http.Header
	status int
}

func (w *turnResponse) Header() http.Header    { return w.header }
func (w *turnResponse) WriteHeader(status int) { w.status = status }
