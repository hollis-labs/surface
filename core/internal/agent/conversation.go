package agent

import (
	"context"
	"database/sql"
	"encoding/json"
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
	"github.com/hollis-labs/surface/core/internal/agent/profile"

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
// Nanite is the default; scripted plays fixed scripts without a model for tests.

// RunnerScripted names the library's scripted test runner.
const RunnerScripted = "scripted"

// wire is the conversation wire, when it is on.
type wire struct {
	control  *controlledRunner
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

// runnerChoice selects Nanite unless an explicit runner is named.
func runnerChoice() string {
	choice := strings.ToLower(strings.TrimSpace(os.Getenv("SURFACE_AGENT_RUNNER")))
	if choice == "" {
		return RunnerNanite
	}
	return choice
}

// openWire builds the conversation wire for a chosen runner.
func (s *Service) openWire(ctx context.Context, runner string) (*wire, error) {
	if runner != RunnerScripted && runner != RunnerNanite {
		return nil, fmt.Errorf("agent: no runner %q for the conversation wire (known: %s, %s)", runner, RunnerNanite, RunnerScripted)
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
	switch runner {
	case RunnerNanite:
		if w.nanite, err = s.openNanite(ctx, w); err != nil {
			_ = st.Close()
			return nil, err
		}
		run = naniteWireRunner{Runner: w.nanite, wire: w}
	default:
		w.scripted = chattest.NewScripted(chattest.WithDelay(scriptDelay()))
		run = w.scripted
	}
	w.control = &controlledRunner{Runner: run}
	if w.nanite != nil {
		w.control.stopAgent = w.nanite.Agent().StopAll
	}
	run = w.control
	closeRunner := func() { closeRunnerNow(w) }
	// Runs announce themselves on the app bus.
	if s.runs, err = openRunLog(ctx, s.k.DB()); err != nil {
		closeRunnerNow(w)
		_ = st.Close()
		return nil, err
	}
	replaced := make(chan string, 4)
	mergeReplaced(context.WithoutCancel(ctx), run.Replaced(), replaced)
	run = watchedRunner{Runner: run, started: s.runStarted, ended: s.runEnded, log: s.runs, model: s.cfg.Model, event: s.wireEvent, replaced: replaced}
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
	// The app has just started, so the agent's side is new: an attachment or an input lease
	// kept from before (a page the old process served, which never got to let go) belongs
	// to nobody. Saying so bumps the conversation's generation: every earlier attachment is
	// stale (live pages attach again on their own) and the lease is free, so a reload does
	// not find a window that is gone holding the input.
	view, err := w.svc.EnsureConversation(ctx, w.agent)
	if err == nil {
		_, err = st.Bump(ctx, view.ConversationID)
	}
	if err == nil {
		// Reconcile before serving HTTP: an asynchronous Replaced signal can
		// invalidate a new attachment created just after startup.
		_, err = w.svc.Conversation(ctx, view.ConversationID)
	}
	if err != nil {
		w.close(ctx)
		return nil, fmt.Errorf("agent: invalidate previous attachments: %w", err)
	}
	if w.nanite != nil {
		if _, err := w.nanite.RecoverTurns(ctx); err != nil {
			w.close(ctx)
			return nil, fmt.Errorf("agent: recover turns: %w", err)
		}
		if d := w.nanite.Desk(); d != nil {
			if err := d.Recover(ctx); err != nil {
				w.close(ctx)
				return nil, fmt.Errorf("agent: recover front desk: %w", err)
			}
		}
	}
	return w, nil
}

// closeRunnerNow closes the wire's runner when its setup fails part way.
func closeRunnerNow(w *wire) {
	if w.nanite != nil {
		_ = w.nanite.Close()
	}
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
	closeRunnerNow(w)
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
	s.mountFrontDesk(mux)
	h := http.StripPrefix(Prefix, refusalStatus(w.handler))
	mux.HandleFunc("POST "+Prefix+"/stop-all", s.stopAll)
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
// page, a reflex, another agent). A routing rule may
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
	extra := s.extra(ctx, sit)
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
	var out reflex.Outcome
	if s.wire == nil || s.wire.nanite == nil {
		out = s.reflexes.OnMessage(ctx, reflex.Message{Text: text, Prior: prior, Sit: sit}, s.layoutReflexes(ctx, sit))
	}
	if out.Halt != "" {
		if errandOf(turn.Meta) == "" {
			s.record(ctx, sit, slots.Assemble(ctx, sit, sel, s.allTools(), s.notes, extra), routed, out.Fired, true)
		}
		// A reflex answered instead of the agent. On this wire that is a turn like any other,
		// so every attached page sees the message and the answer.
		s.k.Events().Publish("agent.halted", map[string]any{"text": text, "message": out.Halt, "fired": out.Fired})
		return chat.Answer(out.Halt)
	}
	extra.Reminders = out.Reminders
	extra.Asks = s.keepingAsks(ctx, sel.Primary().Name, !auto)
	extra.Limit = s.contextLimit()

	// The profile this turn runs as, staged for the runner before the turn starts.
	choice := s.chooseProfile(ctx, turn.ConversationID, sit)
	if err := s.stageProfile(choice); err != nil {
		return chat.Reject("agent_profile", err.Error())
	}
	// Capture the selected directory on this turn so a concurrent focus or setting
	// change cannot select a different agent between interception and dispatch.
	var turnMeta map[string]any
	_ = json.Unmarshal(turn.Meta, &turnMeta)
	if turnMeta == nil {
		turnMeta = map[string]any{}
	}
	delete(turnMeta, "surface_agent_dir")
	if dir := choice.AgentDir; dir != "" {
		bound, err := profile.LoadBinding(choice.ID, s.modules[choice.ID], dir)
		if err != nil {
			return chat.Reject("agent_binding", err.Error())
		}
		staged, err := bound.StageBinding(filepath.Join(s.k.DataDir(), "agents", "bindings"))
		if err != nil {
			return chat.Reject("agent_binding", err.Error())
		}
		dir, _ = profile.BindingPath(dir)
		turnMeta["surface_agent_dir"] = staged
		s.log.Info("the turn uses a bound agent", "module", choice.ID, "agent_dir", dir, "run_id", turn.RunID)
	}
	turn.Meta, _ = json.Marshal(turnMeta)
	extra.Profile, extra.ProfileID = s.profileLine(choice), choice.ID

	asm := slots.Assemble(ctx, sit, sel, s.allTools(), s.notes, extra)
	if len(asm.Trimmed) > 0 {
		s.log.Info("the context block was trimmed to fit", "run_id", turn.RunID, "trimmed", asm.Trimmed, "bytes", len(asm.Block))
	}
	// A module's errand is not the owner speaking: it stays out of the trace.
	if errandOf(turn.Meta) == "" {
		if traced := s.record(ctx, sit, asm, routed, out.Fired, false); traced != 0 && turn.RunID != "" {
			s.trace.SetRun(ctx, traced, turn.RunID)
		}
	}
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
	origin := "owner"
	if mod := errandOf(turn.Meta); mod != "" {
		origin = "errand:" + mod
	} else if auto {
		origin = "auto"
	}
	s.runs.turn(turn.RunID, origin, asm.Slot, profileName(choice.ID), choice.AgentDir)
	s.k.Events().Publish("agent.turn", map[string]any{"run_id": turn.RunID, "slot": asm.Slot, "slots": sel.Names(),
		"top_of_mind": asm.TopOfMind, "fired": out.Fired, "routed": routed, "profile": profileName(choice.ID)})
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
	return nil
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

// naniteContextBytes is the room Surface gives its turn block in the protected Context slot:
// 2,000 tokens, counted as bytes/4. Authored context files have additional reserved room.
// A little is kept back for how the slot is rendered.
const naniteContextTokens = 2000
const naniteContextBytes = naniteContextTokens*4 - 200

// contextLimit is the hard cap, in bytes, on the context block for this agent's runner, or
// 0 when the runner has no fixed room for it. The config's extra standing instructions go
// in front of the block, in the same slot, so they come out of the room.
//
// Scripted runners have no Nanite context-slot cap.
func (s *Service) contextLimit() int {
	if s.wire == nil || s.wire.nanite == nil {
		return 0
	}
	room := naniteContextBytes
	if extra := strings.TrimSpace(s.cfg.System); extra != "" {
		room -= len(extra) + 2
	}
	return max(room, 1000)
}
