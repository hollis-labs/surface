package agent

import (
	"context"
	"encoding/json"
	"iter"

	"github.com/hollis-labs/libs/agent/chat"
	"github.com/hollis-labs/libs/agent/host"
	chatstream "github.com/hollis-labs/libs/ui/ui-go/chatstream"
	"github.com/hollis-labs/nanite/chatrunner"
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
	RunFinished = "finished"
	RunCanceled = "canceled"
	RunFailed   = "failed"
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
	// log keeps the run log (nil: none); model is the app's configured model, for the log.
	log   *runLog
	model string
	event func(string, chatstream.Event)
	// replaced is the runner's own Replaced signal merged with the app's: at start, the
	// agent's side is new, so every attachment and lease from before is void (relaunched).
	replaced chan string
}

// Replaced is the runner's signal and the app's start, on one channel.
func (w watchedRunner) Replaced() <-chan string { return w.replaced }

// mergeReplaced forwards the runner's own Replaced signal into out until ctx ends.
func mergeReplaced(ctx context.Context, inner <-chan string, out chan<- string) {
	if inner == nil {
		return
	}
	go func() {
		for {
			select {
			case id, ok := <-inner:
				if !ok {
					return
				}
				select {
				case out <- id:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (w watchedRunner) Start(ctx context.Context, turn chat.Turn) (chat.Run, error) {
	r, err := w.Runner.Start(ctx, turn)
	if err != nil {
		return nil, err
	}
	w.started(turn.RunID)
	c := w.log.begin(turn.RunID, w.model)
	log := w.log
	ended := func(id, status, reason string) {
		log.end(c, status, reason)
		w.ended(id, status, reason)
	}
	return watchedRun{Run: r, id: turn.RunID, ended: ended, seen: c, event: w.event, conversation: turn.ConversationID}, nil
}

type watchedRun struct {
	chat.Run
	id           string
	ended        func(id, status, reason string)
	seen         *collector
	conversation string
	event        func(string, chatstream.Event)
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
			if err == nil {
				w.seen.see(ev)
				if w.event != nil {
					w.event(w.conversation, ev)
				}
			}
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

// watchWireRun gives runner-initiated recovery and notice runs the same bus and log hooks.
func (s *Service) watchWireRun(turn chat.Turn, run chat.Run) chat.Run {
	if turn.RunID == "" {
		return watchedRun{Run: run, event: s.wireEvent, conversation: turn.ConversationID, ended: func(string, string, string) {}}
	}
	c := s.runs.begin(turn.RunID, s.cfg.Model)
	if turn.RunID != "" {
		s.runStarted(turn.RunID)
	}
	return watchedRun{Run: run, id: turn.RunID, seen: c, event: s.wireEvent, conversation: turn.ConversationID,
		ended: func(id, status, reason string) {
			s.runs.end(c, status, reason)
			if id != "" {
				s.runEnded(id, status, reason)
			}
		}}
}

func (s *Service) wireEvent(conversation string, ev chatstream.Event) {
	if ev.Verb != chatstream.VerbActivity || ev.Kind != chatrunner.ActivityCompact {
		return
	}
	var value map[string]any
	if json.Unmarshal(ev.Value, &value) != nil || value["state"] != "done" {
		return
	}
	value["run_id"] = ev.RunID
	value["conversation_id"] = conversation
	s.k.Events().Publish(TopicCompacted, value)
}
