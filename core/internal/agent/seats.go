package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/hollis-labs/libs/agent/agentdir"
	chat "github.com/hollis-labs/libs/agent/chat"
	"github.com/hollis-labs/libs/agent/steer"
	nanite "github.com/hollis-labs/nanite/agent"
	"github.com/hollis-labs/nanite/chatrunner"

	"github.com/hollis-labs/surface/core/internal/kernel"
	"github.com/hollis-labs/surface/module"
)

// Seats: more Nanite agents in the app beside the embedded one. Each seat is a folder (an
// agent directory: agent.md, host.yaml, optional .mcp.json, skills, context) rendered by
// whatever defines the seats; Surface never writes into it. A seat gets its own runner, data
// directory and durable conversation on the same wire, and shares everything the embedded
// agent has from the app: its tools and their approvals, the context slots, the router and
// the reflexes. Module profiles, the front desk, the mailbox and standing-instruction edits
// stay the embedded agent's.
//
// The Agent chat module lists seats (GET /api/agent/seats) beside a seat host's, in the same
// shape, and opens each on the wire under its agent name "seat.<id>".

// SeatPrefix starts every seat's agent name on the wire.
const SeatPrefix = "seat."

var seatIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

// seat is one seat: its folder, and its runner when it opened.
type seat struct {
	id, dir              string
	name, summary, model string
	err                  string
	runner               *chatrunner.Runner
	run                  chat.Runner
	control              *controlledRunner
}

func (st *seat) agent() string { return SeatPrefix + st.id }

// seatsDir is where the seats are: seats.dir in the agent config, or SURFACE_SEATS_DIR.
func (s *Service) seatsDir() string {
	if d := strings.TrimSpace(os.Getenv("SURFACE_SEATS_DIR")); d != "" {
		return expandHome(d)
	}
	return expandHome(strings.TrimSpace(s.cfg.Seats.Dir))
}

// findSeats reads the seats folder: each subfolder with agent.md and host.yaml is a seat. A
// folder that does not load is listed with its error and not opened.
func (s *Service) findSeats() []*seat {
	root := s.seatsDir()
	if root == "" {
		return nil
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		s.log.Warn("seats folder not read", "dir", root, "err", err)
		return nil
	}
	var out []*seat
	for _, e := range ents {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "agent.md")); err != nil {
			continue
		}
		st := &seat{id: e.Name(), dir: dir, name: e.Name()}
		if !seatIDPattern.MatchString(st.id) || st.id == s.k.AppName() {
			st.err = "the folder name is not a usable seat id (a-z, 0-9, - and _)"
			out = append(out, st)
			continue
		}
		snap, err := agentdir.Load(dir)
		if err != nil {
			st.err = err.Error()
			out = append(out, st)
			continue
		}
		if t := strings.TrimSpace(snap.Definition.Title); t != "" {
			st.name = t
		} else if n := strings.TrimSpace(snap.Definition.Name); n != "" {
			st.name = n
		}
		st.summary = strings.TrimSpace(snap.Definition.Description)
		st.model = snap.Profile.Runtime.Model
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

// openSeats opens a runner for every seat that loaded. One that fails to open is kept with
// its error.
func (s *Service) openSeats(ctx context.Context, w *wire) {
	w.seats = map[string]*seat{}
	for _, st := range s.findSeats() {
		w.seats[st.agent()] = st
		if st.err != "" {
			s.log.Warn("seat not opened", "seat", st.id, "err", st.err)
			continue
		}
		if err := s.openSeat(ctx, w, st); err != nil {
			st.err = err.Error()
			s.log.Warn("seat not opened", "seat", st.id, "err", err)
			continue
		}
		s.log.Info("seat open", "seat", st.id, "dir", st.dir)
	}
}

// openSeat opens one seat's Nanite runner on its folder, with the app's tools.
func (s *Service) openSeat(ctx context.Context, w *wire, st *seat) error {
	c := s.cfg
	groups, err := builtinGroups(c.Builtins)
	if err != nil {
		return err
	}
	compact, keep, _, _, err := naniteCompactionFallback(st.dir)
	if err != nil {
		return fmt.Errorf("host config: %w", err)
	}
	contextBudget, err := s.naniteContextBudget(st.dir)
	if err != nil {
		return fmt.Errorf("context budget: %w", err)
	}
	ref := module.AgentRef{Name: st.name, ReplyTo: st.agent()}
	db := s.k.DB()
	agentName := st.agent()
	r, err := chatrunner.Open(ctx, chatrunner.Options{
		Agent: nanite.Options{
			Dir:               st.dir,
			DataDir:           filepath.Join(s.k.DataDir(), "agents", "seats", st.id),
			GlobalSkillsDir:   "-",
			Gateway:           gatewayOf(c),
			Logger:            s.log.With("part", "nanite", "seat", st.id),
			Builtins:          groups,
			MaxOutputTokens:   c.MaxTokens,
			MaxSteps:          c.MaxSteps,
			AutoApprove:       c.AutoApprove,
			CompactAt:         compact,
			CompactKeepTokens: keep,
			SlotBudgets:       map[nanite.SlotName]int{nanite.SlotContext: contextBudget},
			Steering:          s.naniteSteering(),
		},
		ContextSlot: nanite.SlotContext,
		Tools:       seatTools(s.runnerTools(), ref),
		Observe: func(ctx context.Context, id string, turn chat.Turn, run chat.Run) error {
			if w.svc == nil {
				return errors.New("agent: chat service is not ready")
			}
			turn.ConversationID = id
			return w.svc.Observe(ctx, id, turn, s.watchWireRun(turn, run))
		},
		Situation: func(turn chat.Turn) steer.Situation { return s.steerSituation(context.Background(), turn.Text) },
		OnSteered: func(_ string, event nanite.Steered) {
			s.launchEvent(func() { s.applySteered(event.Fired, event.Actions) })
		},
		Ask: func(_ context.Context, call chatrunner.Call) bool {
			return w.asks.has(call.ConversationID, call.Tool)
		},
		Granted: func(ctx context.Context, call chatrunner.Call) bool {
			ok, gerr := granted(ctx, db, agentName, call.Tool)
			if gerr != nil {
				s.log.Warn("standing approvals not read; asking", "err", gerr)
			}
			return ok
		},
	})
	if err != nil {
		return fmt.Errorf("nanite: %w", err)
	}
	st.runner = r
	st.control = &controlledRunner{Runner: r, stopAgent: r.Agent().StopAll}
	replaced := make(chan string, 4)
	mergeReplaced(context.WithoutCancel(ctx), st.control.Replaced(), replaced)
	st.run = watchedRunner{Runner: st.control, started: s.runStarted, ended: s.runEnded, log: s.runs,
		model: firstNonEmpty(st.model, c.Model), event: s.wireEvent, replaced: replaced}
	return nil
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// seatOnly are the embedded agent's own tools a seat does not get: its standing
// instructions and module profiles belong to the embedded agent.
var seatOnly = map[string]bool{"standing_get": true, "standing_update": true, "profile_switch": true}

// seatTools are the app's tools for a seat: every call carries who is calling, so cards,
// late answers and the desk know it is this seat and not the embedded agent.
func seatTools(tools []chatrunner.Tool, ref module.AgentRef) []chatrunner.Tool {
	out := make([]chatrunner.Tool, 0, len(tools))
	for _, t := range tools {
		if seatOnly[t.Name] {
			continue
		}
		run := t.Run
		t.Run = func(ctx context.Context, call nanite.ToolCall) (nanite.ToolResult, error) {
			return run(module.WithAgent(ctx, ref), call)
		}
		out = append(out, t)
	}
	return out
}

// agentOf names the agent a conversation belongs to ("" when unknown).
func (w *wire) agentOf(ctx context.Context, conversation string) string {
	if conversation == "" || w.store == nil {
		return ""
	}
	c, err := w.store.Get(ctx, conversation)
	if err != nil {
		return ""
	}
	return c.Agent
}

// isSeat reports whether a conversation is a seat's (not the embedded agent's).
func (w *wire) isSeat(ctx context.Context, conversation string) bool {
	return strings.HasPrefix(w.agentOf(ctx, conversation), SeatPrefix)
}

// --- HTTP: the seats, in the seat host's shape ---

type seatSession struct {
	ID             string     `json:"id"`
	Seat           string     `json:"seat"`
	Title          string     `json:"title,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	ConversationID string     `json:"conversation_id,omitempty"`
	State          string     `json:"state"`
	LastEventAt    *time.Time `json:"last_event_at,omitempty"`
	ActiveRun      string     `json:"active_run,omitempty"`
}

type seatView struct {
	Seat        string        `json:"seat"`
	Name        string        `json:"name"`
	Summary     string        `json:"summary,omitempty"`
	Shape       string        `json:"shape"`
	MaxSessions int           `json:"max_sessions"`
	Runtime     string        `json:"runtime"`
	Adapter     string        `json:"adapter"`
	Model       string        `json:"model,omitempty"`
	Dir         string        `json:"dir,omitempty"`
	Error       string        `json:"error,omitempty"`
	Local       bool          `json:"local"`
	WireBase    string        `json:"wire_base"`
	Sessions    []seatSession `json:"sessions"`
}

// seatSession is a seat's one conversation, as a session (a local seat keeps one).
func (s *Service) seatSession(ctx context.Context, w *wire, st *seat) (*seatSession, error) {
	view, err := w.svc.EnsureConversation(ctx, st.agent())
	if err != nil || view.ConversationID == "" {
		return nil, err
	}
	out := &seatSession{ID: st.agent(), Seat: st.id, ConversationID: view.ConversationID, State: "idle"}
	if rec, rerr := w.store.Get(ctx, view.ConversationID); rerr == nil {
		out.CreatedAt = rec.CreatedAt
	}
	if view.ActiveRun != nil && *view.ActiveRun != "" {
		out.State, out.ActiveRun = "running", *view.ActiveRun
		now := time.Now().UTC()
		out.LastEventAt = &now
	}
	return out, nil
}

func (s *Service) mountSeats(mux *http.ServeMux) {
	w := s.wire
	mux.HandleFunc("GET "+Prefix+"/seats", func(rw http.ResponseWriter, r *http.Request) {
		out := []seatView{}
		for _, st := range w.sortedSeats() {
			v := seatView{Seat: st.id, Name: st.name, Summary: st.summary, Shape: "continuity", MaxSessions: 1,
				Runtime: "nanite", Adapter: "nanite", Model: firstNonEmpty(st.model, s.cfg.Model), Dir: st.dir,
				Error: st.err, Local: true, WireBase: Prefix, Sessions: []seatSession{}}
			if st.err == "" {
				if sess, err := s.seatSession(r.Context(), w, st); err == nil && sess != nil {
					v.Sessions = append(v.Sessions, *sess)
				}
			}
			out = append(out, v)
		}
		reply(rw, 200, map[string]any{"host": "local", "seats": out})
	})
	mux.HandleFunc("POST "+Prefix+"/seats/{seat}/sessions", func(rw http.ResponseWriter, r *http.Request) {
		st, ok := w.seats[SeatPrefix+r.PathValue("seat")]
		if !ok {
			fail(rw, 404, "no such seat")
			return
		}
		if st.err != "" {
			fail(rw, 502, st.err)
			return
		}
		sess, err := s.seatSession(r.Context(), w, st)
		if err != nil || sess == nil {
			fail(rw, 500, fmt.Sprint(err))
			return
		}
		reply(rw, 200, sess)
	})
	mux.HandleFunc("POST "+Prefix+"/seats/{seat}/send", func(rw http.ResponseWriter, r *http.Request) {
		st, ok := w.seats[SeatPrefix+r.PathValue("seat")]
		if !ok || st.err != "" {
			fail(rw, 404, "no such seat")
			return
		}
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(rw, r.Body, 1<<20)).Decode(&body); err != nil || strings.TrimSpace(body.Text) == "" {
			fail(rw, 400, "body: {text}")
			return
		}
		view, err := w.svc.EnsureConversation(r.Context(), st.agent())
		if err != nil {
			fail(rw, 500, err.Error())
			return
		}
		meta, _ := json.Marshal(map[string]any{"forwarded": true})
		rc, err := w.svc.Submit(r.Context(), view.ConversationID, chat.Turn{Text: body.Text, Meta: meta})
		if errors.Is(err, chat.ErrBusy) {
			reply(rw, 409, map[string]any{"code": "run_active", "message": "the agent is busy with a turn; try again when it finishes"})
			return
		}
		if err != nil {
			fail(rw, 500, err.Error())
			return
		}
		reply(rw, 202, map[string]any{"session": st.agent(), "run_id": rc.RunID})
	})
}

func (w *wire) sortedSeats() []*seat {
	out := make([]*seat, 0, len(w.seats))
	for _, st := range w.seats {
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

// withoutSeatOnly leaves out the embedded agent's own tools, for a seat's context block.
func withoutSeatOnly(tools []kernel.BoundTool) []kernel.BoundTool {
	out := make([]kernel.BoundTool, 0, len(tools))
	for _, t := range tools {
		if !seatOnly[t.Name] {
			out = append(out, t)
		}
	}
	return out
}
