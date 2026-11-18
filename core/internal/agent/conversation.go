package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	chat "github.com/hollis-labs/libs/agent/chat"
	"github.com/hollis-labs/libs/agent/chat/chattest"
	"github.com/hollis-labs/libs/agent/chat/sqlitestore"
	libreflex "github.com/hollis-labs/libs/agent/core/reflexes"

	"github.com/hollis-labs/nanite/chatrunner"

	"github.com/hollis-labs/surface/core/internal/reflex"
	"github.com/hollis-labs/surface/core/internal/router"
	"github.com/hollis-labs/surface/core/internal/slots"
)

// The conversation wire (libs agent/chat): one durable conversation for the app's agent,
// which outlives a turn and a page. A page attaches as a viewer or a controller, reads one
// event stream, and submits turns when it holds the input. Surface mounts the library's
// handler and supplies what varies: who may do what (the local owner, everything), where
// the log and the control records are kept (chat.db in the data directory), what happens to
// a turn before it runs (the router, the reflexes and the context block: Intercept), and
// where a standing approval is kept (Grants).
//
// It is on when a runner for it is chosen (SURFACE_AGENT_RUNNER): "nanite", the app's agent
// as a Nanite agent with the app's tools (nanite.go), or "scripted", the library's test
// runner, which plays fixed scripts and runs no tools. With no runner chosen, the agent runs
// on the engine and the run-at-a-time wire as before.

// RunnerScripted names the library's scripted test runner.
const RunnerScripted = "scripted"

// wire is the conversation wire, when it is on.
type wire struct {
	svc      *chat.Service
	store    *sqlitestore.Store
	handler  http.Handler
	scripted *chattest.Scripted
	nanite   *chatrunner.Runner
	// asks is the tool a routing rule marked ask-first, for the turn in progress.
	asks asks
	// agent is the one agent name this app serves on the wire.
	agent  string
	runner string
}

// runnerChoice is the runner named for the conversation wire, or "".
func runnerChoice() string {
	return strings.ToLower(strings.TrimSpace(os.Getenv("SURFACE_AGENT_RUNNER")))
}

// openWire builds the conversation wire for a chosen runner.
func (s *Service) openWire(ctx context.Context, runner string) (*wire, error) {
	if runner != RunnerScripted && runner != RunnerNanite {
		return nil, fmt.Errorf("agent: no runner %q for the conversation wire (known: %s, %s)", runner, RunnerScripted, RunnerNanite)
	}
	st, err := sqlitestore.Open(ctx, filepath.Join(s.k.DataDir(), "chat.db"))
	if err != nil {
		return nil, fmt.Errorf("agent: conversation store: %w", err)
	}
	if err = openGrants(ctx, s.k.DB()); err != nil {
		_ = st.Close()
		return nil, err
	}
	w := &wire{store: st, agent: s.k.AppName(), runner: runner}
	var run chat.Runner
	if runner == RunnerNanite {
		if w.nanite, err = s.openNanite(ctx, w); err != nil {
			_ = st.Close()
			return nil, err
		}
		run = w.nanite
	} else {
		w.scripted = chattest.NewScripted(chattest.WithDelay(scriptDelay()))
		run = w.scripted
	}
	closeRunner := func() {
		if w.nanite != nil {
			_ = w.nanite.Close()
		}
	}
	w.svc, err = chat.NewService(chat.Config{
		Runner:  run,
		Log:     st,
		Control: st,
		Grants:  grants{db: s.k.DB()},
		// One agent, the app's own, owned by the one person the app serves.
		Agents: func(_ context.Context, name string) (chat.Agent, error) {
			if name != w.agent {
				return chat.Agent{}, chat.ErrNotFound
			}
			return chat.Agent{Owner: Owner, Scope: s.k.AppName(), Kind: "embedded"}, nil
		},
		Intercept: s.intercept,
		Logger:    s.log,
	})
	if err != nil {
		closeRunner()
		_ = st.Close()
		return nil, fmt.Errorf("agent: conversation service: %w", err)
	}
	// The app is loopback-only and single-user: every request is the owner's, who may do
	// everything, takeover included.
	w.handler, err = chat.NewHandler(w.svc, func(context.Context, string, chat.Permission) error { return nil })
	if err != nil {
		_ = w.svc.Close(ctx)
		closeRunner()
		_ = st.Close()
		return nil, err
	}
	return w, nil
}

// scriptDelay paces the scripted runner's events, so a stream can be watched (and a page
// reloaded in the middle of an answer). SURFACE_AGENT_SCRIPT_DELAY is a duration.
func scriptDelay() time.Duration {
	if d, err := time.ParseDuration(os.Getenv("SURFACE_AGENT_SCRIPT_DELAY")); err == nil && d >= 0 {
		return d
	}
	return 60 * time.Millisecond
}

func (w *wire) close(ctx context.Context) {
	stop, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = w.svc.Close(stop)
	if w.nanite != nil {
		_ = w.nanite.Close()
	}
	_ = w.store.Close()
}

// mount serves the wire's routes under the agent's prefix, beside Surface's own.
//
//	POST   /api/agent/agents/{agent}/conversation[/reset]
//	GET    /api/agent/conversations/{id}            and its attachments, events, lease, turns,
//	                                                runs/{run}/cancel and approvals/{approval}
//	GET    /api/agent/grants                        the standing approvals kept
//	POST   /api/agent/test/replace                  scripted runner only: replace the agent's side
func (s *Service) mountWire(mux *http.ServeMux) {
	w := s.wire
	h := http.StripPrefix(Prefix, w.handler)
	mux.Handle(Prefix+"/agents/", h)
	mux.Handle(Prefix+"/conversations/", h)
	mux.HandleFunc("GET "+Prefix+"/grants", func(rw http.ResponseWriter, r *http.Request) {
		list, err := listGrants(r.Context(), s.k.DB())
		if err != nil {
			fail(rw, 500, err.Error())
			return
		}
		reply(rw, 200, list)
	})
	if w.scripted != nil {
		// A test hook, and only with the test runner: what a relaunch of the agent's side
		// does to a conversation (the generation rises; attachments and the lease go).
		mux.HandleFunc("POST "+Prefix+"/test/replace", func(rw http.ResponseWriter, r *http.Request) {
			view, err := w.svc.EnsureConversation(r.Context(), w.agent)
			if err != nil {
				fail(rw, 500, err.Error())
				return
			}
			w.scripted.Replace(view.ConversationID)
			reply(rw, 202, map[string]string{"conversation_id": view.ConversationID})
		})
	}
}

// intercept is the host hook: it sees every turn before it runs, wherever it came from (a
// page, a reflex, another agent). It does what Send does on the engine: a routing rule may
// run a call first, the reflexes may answer instead of the agent, and the context block for
// what is in front is assembled and attached. The library can call it for a turn that then
// loses the race to another; a routed call's effect stands either way, as it did before.
func (s *Service) intercept(ctx context.Context, turn *chat.Turn) chat.Outcome {
	text := strings.TrimSpace(turn.Text)
	var meta struct {
		Auto any `json:"auto"`
	}
	_ = json.Unmarshal(turn.Meta, &meta)
	auto := meta.Auto != nil && meta.Auto != false && meta.Auto != ""

	if s.wire != nil && s.wire.scripted != nil {
		// With the test runner, two turns exercise the hook itself, as the library's example
		// server does.
		switch first, _, _ := strings.Cut(text, " "); first {
		case "/no":
			return chat.Reject("refused", "The host refused this turn.")
		case "/ping":
			return chat.Answer("pong")
		}
	}

	sit := s.k.Situation(ctx)
	sit.Input = text
	sel := s.selector.Select(ctx, sit, s.k.Slots())
	extra := slots.Extra{Instructions: s.layoutInstructions(ctx, sit)}
	var routed *router.Route
	askFirst := ""
	if !auto {
		f := router.Fields{Text: text, Mode: sit.Mode, Layout: sit.Layout, Slot: sel.Primary().Name}
		if sit.Focused != nil {
			f.View, f.Module = sit.Focused.View, sit.Focused.Module
		}
		route, err := s.router.Route(ctx, f)
		if err != nil {
			s.log.Warn("message not routed", "err", err)
		}
		if route != nil {
			routed = route
			if route.Kind == "tool" && route.Ask {
				askFirst = route.Name
			}
			extra.Routed = s.runRoute(ctx, route)
			sit = s.k.Situation(ctx)
			sit.Input = text
			sel = s.selector.Select(ctx, sit, s.k.Slots())
		}
	}

	if s.wire != nil {
		s.wire.asks.set(turn.ConversationID, askFirst)
	}

	prior := s.priorMessages(ctx, turn.ConversationID)
	debug := os.Getenv("SURFACE_AGENT_DEBUG") != ""
	if debug {
		s.log.Info("turn intercepted", "run_id", turn.RunID, "prior_messages", len(prior), "ask_first", askFirst)
	}
	out := s.reflexes.OnMessage(ctx, reflex.Message{Text: text, Prior: prior, Sit: sit}, s.layoutReflexes(ctx, sit))
	if out.Halt != "" {
		// A reflex answered instead of the agent. On this wire that is a turn like any other,
		// so every attached page sees the message and the answer.
		s.k.Events().Publish("agent.halted", map[string]any{"text": text, "message": out.Halt, "fired": out.Fired})
		return chat.Answer(out.Halt)
	}
	extra.Reminders = out.Reminders

	asm := slots.Assemble(ctx, sit, sel, s.allTools(), s.notes, extra)
	// The runner places the context as text in one slot for this turn: the block alone, with
	// the config's extra standing instructions (if any) in front. The panel reads the
	// assembly from the app's own events, not from the turn.
	block := asm.Block
	if extra := strings.TrimSpace(s.cfg.System); extra != "" && s.wire != nil && s.wire.nanite != nil {
		block = extra + "\n\n" + block
	}
	raw, err := json.Marshal(block)
	if err == nil && len(raw) <= chat.MaxContextBytes {
		turn.Context = raw
	} else {
		s.log.Warn("the context block is too large for a turn; sent without it", "bytes", len(raw))
	}
	if debug {
		s.log.Info("turn context", "run_id", turn.RunID, "bytes", len(block), "block", block)
	}
	s.k.Events().Publish("agent.turn", map[string]any{"run_id": turn.RunID, "slot": asm.Slot, "slots": sel.Names(),
		"top_of_mind": asm.TopOfMind, "fired": out.Fired, "routed": routed})
	return chat.Continue()
}

// priorMessages is the recent conversation as the reflexes see it, most recent first. On the
// conversation wire it is read from the runner's history (Nanite's), which includes the
// turns the host answered itself: the library records those through the runner.
func (s *Service) priorMessages(ctx context.Context, conversation string) []libreflex.MessageSignal {
	if s.wire != nil {
		if s.wire.nanite != nil && conversation != "" {
			return s.wire.naniteHistory(ctx, conversation)
		}
		return nil // the scripted runner keeps no history
	}
	var prior []libreflex.MessageSignal
	hist, err := s.engine.History(ctx, Owner)
	if err != nil {
		return nil
	}
	for i := len(hist) - 1; i >= 0 && len(prior) < 40; i-- {
		h := hist[i]
		if h.Role == "user" && h.Text == "" {
			continue // tool results only
		}
		prior = append(prior, libreflex.MessageSignal{Role: h.Role, Content: h.Text, CreatedAt: h.CreatedAt,
			ToolCalls: len(h.ToolNames), ToolNames: h.ToolNames})
	}
	return prior
}

// sendOnWire starts a turn from inside the app (a reflex, a trigger, another agent through
// the tools): no attachment and no lease, the same hook, and every attached page sees it.
func (s *Service) sendOnWire(ctx context.Context, text, auto string) (TurnResult, error) {
	w := s.wire
	view, err := w.svc.EnsureConversation(ctx, w.agent)
	if err != nil {
		return TurnResult{}, err
	}
	turn := chat.Turn{Text: text}
	if auto != "" {
		turn.Meta, _ = json.Marshal(map[string]string{"auto": auto})
	}
	receipt, err := w.svc.Submit(ctx, view.ConversationID, turn)
	var rejected *chat.RejectedError
	switch {
	case errors.Is(err, chat.ErrBusy):
		return TurnResult{}, ErrBusy
	case errors.As(err, &rejected):
		return TurnResult{Halted: rejected.Message}, nil
	case err != nil:
		return TurnResult{}, err
	}
	return TurnResult{RunID: receipt.RunID}, nil
}

// Standing approvals ("always"): the wire hands them to the host to keep, and the runner
// asks the same store before it asks the owner again. They are in the core database, so
// they survive a restart and a new conversation.

func openGrants(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS agent_grants (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		agent       TEXT NOT NULL,
		tool        TEXT NOT NULL DEFAULT '',
		reason      TEXT NOT NULL DEFAULT '',
		descriptor  TEXT NOT NULL DEFAULT '{}',
		approval_id TEXT NOT NULL DEFAULT '',
		created_at  TEXT NOT NULL)`)
	if err != nil {
		return fmt.Errorf("agent: grants: %w", err)
	}
	return nil
}

type grants struct{ db *sql.DB }

// Grant keeps one standing approval. The tool it is for is read from the request's own
// descriptor.
func (g grants) Grant(ctx context.Context, in chat.Grant) error {
	var d struct {
		Tool string `json:"tool"`
	}
	_ = json.Unmarshal(in.Descriptor, &d)
	desc := string(in.Descriptor)
	if desc == "" {
		desc = "{}"
	}
	_, err := g.db.ExecContext(ctx, `INSERT INTO agent_grants (agent, tool, reason, descriptor, approval_id, created_at) VALUES (?,?,?,?,?,?)`,
		in.Agent, d.Tool, in.Reason, desc, in.ApprovalID, time.Now().UTC().Format(time.RFC3339))
	return err
}

// StandingGrant is one kept approval, as the API lists it.
type StandingGrant struct {
	Agent   string `json:"agent"`
	Tool    string `json:"tool"`
	Reason  string `json:"reason,omitempty"`
	Created string `json:"created"`
}

func listGrants(ctx context.Context, db *sql.DB) ([]StandingGrant, error) {
	rows, err := db.QueryContext(ctx, `SELECT agent, tool, reason, created_at FROM agent_grants ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []StandingGrant{}
	for rows.Next() {
		var g StandingGrant
		if err := rows.Scan(&g.Agent, &g.Tool, &g.Reason, &g.Created); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
