package agent

import (
	"context"
	"iter"

	"github.com/hollis-labs/libs/agent/chat"
	"github.com/hollis-labs/libs/agent/host"
	chatstream "github.com/hollis-labs/libs/ui/ui-go/chatstream"
)

// The agent's runs announce themselves on the app's bus, the same way whichever engine runs
// them, so the attention list and the context trace can follow them (docs/attention.md):
//
//	agent.run.started  {run_id}
//	agent.run.ended    {run_id, status, reason}   status: finished | canceled | failed
//
// Neither engine offers a hook for this, so each is watched through what Surface hands it:
// the store the host engine keeps its runs in, and the runner the conversation wire drives.

// Run statuses as the bus carries them.
const (
	RunFinished  = "finished"
	RunCanceled = "canceled"
	RunFailed    = "failed"
)

// runWatch is what an engine calls as its runs start and end.
type runWatch struct {
	started func(id string)
	ended   func(id, status, reason string)
}

func (s *Service) watch() runWatch { return runWatch{started: s.runStarted, ended: s.runEnded} }

// runStarted and runEnded publish a run's start and end.
func (s *Service) runStarted(id string) {
	s.k.Events().Publish("agent.run.started", map[string]any{"run_id": id})
}

func (s *Service) runEnded(id, status, reason string) {
	s.k.Events().Publish("agent.run.ended", map[string]any{"run_id": id, "status": status, "reason": reason})
	s.signal(context.Background(), "run_end", map[string]string{"run_id": id, "status": status})
}

// watchedStore is the host engine's store, announcing each run's start and end.
type watchedStore struct {
	host.Store
	started func(id string)
	ended   func(id, status, reason string)
}

func (w watchedStore) StartRun(ctx context.Context, id, thread, owner, model string) error {
	err := w.Store.StartRun(ctx, id, thread, owner, model)
	if err == nil {
		w.started(id)
	}
	return err
}

func (w watchedStore) FinishRun(ctx context.Context, id string, end host.RunEnd) error {
	err := w.Store.FinishRun(ctx, id, end)
	status := RunFinished
	switch end.Status {
	case "cancelled": //nolint:misspell // the host library's own word for it
		status = RunCanceled
	case "error":
		status = RunFailed
	}
	w.ended(id, status, end.Reason)
	return err
}

// watchedRunner is the conversation wire's runner, announcing each run's start and its
// terminal event.
type watchedRunner struct {
	chat.Runner
	started func(id string)
	ended   func(id, status, reason string)
}

func (w watchedRunner) Start(ctx context.Context, turn chat.Turn) (chat.Run, error) {
	r, err := w.Runner.Start(ctx, turn)
	if err != nil {
		return nil, err
	}
	w.started(turn.RunID)
	return watchedRun{Run: r, id: turn.RunID, ended: w.ended}, nil
}

type watchedRun struct {
	chat.Run
	id    string
	ended func(id, status, reason string)
}

func (w watchedRun) Events() iter.Seq2[chatstream.Event, error] {
	return func(yield func(chatstream.Event, error) bool) {
		done := false
		defer func() {
			// A stream that stopped without a terminal event did not finish.
			if !done {
				w.ended(w.id, RunFailed, "the run's stream ended without a terminal event")
			}
		}()
		for ev, err := range w.Run.Events() {
			if err == nil && ev.IsTerminal() && !done {
				done = true
				switch ev.Verb {
				case chatstream.VerbRunFinish:
					w.ended(w.id, RunFinished, ev.Reason)
				case chatstream.VerbRunAbort:
					w.ended(w.id, RunCanceled, ev.Reason)
				default:
					w.ended(w.id, RunFailed, firstOf(ev.Message, ev.Reason))
				}
			}
			if !yield(ev, err) {
				if !done {
					// The consumer stopped reading: the service is done with this run.
					done = true
					w.ended(w.id, RunCanceled, "no longer followed")
				}
				return
			}
		}
	}
}

func firstOf(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
