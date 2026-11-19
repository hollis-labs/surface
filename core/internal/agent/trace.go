package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/hollis-labs/surface/core/internal/kernel"
	"github.com/hollis-labs/surface/core/internal/router"
	"github.com/hollis-labs/surface/core/internal/slots"
	"github.com/hollis-labs/surface/module"
)

// traceOn reports whether the context trace is kept (the live setting surface.agent_trace).
func (s *Service) traceOn() bool {
	if s.trace == nil {
		return false
	}
	// With no settings store (a host built without one), the trace is on, as by default.
	cfg := s.k.Config(kernel.CoreScope)
	return cfg == nil || cfg.Bool("agent_trace")
}

// record keeps one turn in the context trace and returns its id (0 when the trace is off).
func (s *Service) record(ctx context.Context, sit module.Situation, a slots.Assembly, routed *router.Route, fired []string, halted bool) int64 {
	if !s.traceOn() {
		return 0
	}
	view, mod := "", ""
	if sit.Focused != nil {
		view, mod = sit.Focused.View, sit.Focused.Module
	}
	open := make([]string, 0, len(sit.Windows))
	for _, w := range sit.Windows {
		open = append(open, w.View)
	}
	r := ""
	if routed != nil {
		r = routed.Rule + " -> " + routed.Kind + ":" + routed.Name
	}
	id, err := s.trace.Record(ctx, slots.TurnFrom(sit.Input, view, mod, sit.Mode, sit.Layout, open, a, r, fired, halted))
	if err != nil {
		s.log.Warn("context trace: turn not kept", "err", err)
	}
	return id
}

// signal keeps something that followed a turn, when the trace is on.
func (s *Service) signal(ctx context.Context, kind string, detail any) {
	if !s.traceOn() {
		return
	}
	s.trace.Signal(ctx, kind, string(module.ActorOf(ctx)), detail)
}

// traceEvent turns the app's events into signals: where the owner went after a turn.
func (s *Service) traceEvent(ev module.Event) {
	if ev.Topic != "desk.focus" {
		return
	}
	var f struct {
		Window string `json:"window"`
		View   string `json:"view"`
		By     string `json:"by"`
	}
	if json.Unmarshal(ev.Data, &f) != nil || module.Actor(f.By).Delegate() || f.Window == "" {
		return
	}
	s.signal(module.WithActor(context.Background(), module.ActorOwner), "focus", map[string]string{"window": f.Window, "view": f.View})
}

// traceRoutes serves the trace: the newest turns, and every turn as JSON lines for training.
func (s *Service) traceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+Prefix+"/trace", func(w http.ResponseWriter, r *http.Request) {
		if s.trace == nil {
			reply(w, 200, []slots.TraceTurn{})
			return
		}
		n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		turns, err := s.trace.Recent(r.Context(), n)
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		if turns == nil {
			turns = []slots.TraceTurn{}
		}
		reply(w, 200, map[string]any{"on": s.traceOn(), "turns": turns})
	})
	mux.HandleFunc("GET "+Prefix+"/trace/export", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Content-Disposition", `attachment; filename="context-trace.jsonl"`)
		if s.trace == nil {
			return
		}
		if err := s.trace.Export(r.Context(), w); err != nil {
			s.log.Warn("context trace: export failed", "err", err)
		}
	})
}
