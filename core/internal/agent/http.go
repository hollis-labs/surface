package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/hollis-labs/surface/core/internal/classifier"
	"github.com/hollis-labs/surface/core/internal/reflex"
	"github.com/hollis-labs/surface/core/internal/router"
	"github.com/hollis-labs/surface/core/internal/slots"
	"github.com/hollis-labs/surface/module"
)

// Mount serves the agent under /api/agent/: the agent-host/1 wire (engine), with the turn
// route Surface's own, plus the context, slots, reflexes and rules.
//
//	GET    /api/agent                      {available, ready, model} (engine)
//	GET    /api/agent/thread               the conversation (engine)
//	DELETE /api/agent/thread               start a new one (engine)
//	POST   /api/agent/turns                {text, client_id, auto?} → {run_id}; 422 {code:"halted"} when a reflex answered
//	GET    /api/agent/runs/{id}/events     the run's chatstream/v1 events (engine)
//	POST   /api/agent/runs/{id}/approvals  {approval_id, decision} (engine)
//	POST   /api/agent/runs/{id}/cancel     (engine)
//	GET    /api/agent/context              the context the next message would carry (slots.Assembly)
//	GET    /api/agent/setup                {ready, model, config_path, reason?}
//	GET    /api/agent/slots                every slot, with notes, the active one marked
//	POST   /api/agent/slots/pin            {slot} ("" unpins)
//	GET    /api/agent/reflexes             PUT/DELETE /api/agent/reflexes/{name}
//	GET    /api/agent/rules                PUT/DELETE /api/agent/rules/{id}
//	POST   /api/agent/classify             {text} → the classifier's result for a draft (classify.Result); records nothing
//	POST   /api/agent/classify/feedback    {text, facet, label, accepted}: the owner's answer to a hint (an accepted slot pins it)
//	GET    /api/agent/classify/replay      a fresh classifier replayed over the trace, against the selector
//	GET    /api/agent/classify/emerging    ?facet=act&limit=500: clusters among turns the facet abstained on (needs an embedder)
//
// The app is loopback-only and single-user: every request is the owner's.
func (s *Service) Mount(mux *http.ServeMux) {
	owner := func(*http.Request) (string, error) { return Owner, nil }
	eng := s.engine.Handler(Prefix, owner)
	mux.Handle(Prefix, eng)
	mux.Handle(Prefix+"/", eng)

	if s.cfg.Tether.URL != "" && s.cfgErr == nil {
		s.mountTether(mux)
	}
	if s.wire != nil {
		s.mountWire(mux)
	}

	mux.HandleFunc("POST "+Prefix+"/turns", s.turn)
	mux.HandleFunc("GET "+Prefix+"/context", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, s.Preview(r.Context())) })
	mux.HandleFunc("GET "+Prefix+"/setup", func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Tether.URL != "" {
			ready := s.cfgErr == nil
			result := map[string]any{"ready": ready, "model": "tether", "config_path": ConfigPath(s.k.AppName()), "wire": "conversation", "wire_base": tetherPrefix, "agent": s.cfg.Tether.Seat}
			if !ready {
				result["reason"] = "The Tether attachment configuration is invalid."
			}
			reply(w, 200, result)
			return
		}
		// ready is the engine's (a model, a gateway and a key file are named) and the key file
		// can be read; reason says why not, in a sentence.
		if s.wire != nil {
			// On the conversation wire the runner answers, not the engine: the page binds to
			// the wire and the agent by these two fields.
			model := s.wire.runner
			if s.wire.nanite != nil {
				model = s.cfg.Model
			}
			reply(w, 200, map[string]any{"ready": true, "model": model, "runner": s.wire.runner, "config_path": ConfigPath(s.k.AppName()),
				"wire": "conversation", "agent": s.wire.agent})
			return
		}
		reason := s.SetupProblem()
		if s.engine.Ready() && reason == "" {
			reply(w, 200, map[string]any{"ready": true, "model": s.engine.Model(), "config_path": ConfigPath(s.k.AppName())})
			return
		}
		if reason == "" {
			reason = "The agent has no model configured."
		}
		reply(w, 200, map[string]any{"ready": false, "model": s.engine.Model(), "config_path": ConfigPath(s.k.AppName()), "reason": reason})
	})
	s.traceRoutes(mux)
	// What the classifier makes of a draft in the situation now: for the composer as the
	// owner types. It records nothing.
	mux.HandleFunc("POST "+Prefix+"/classify", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Text string `json:"text"`
		}
		if !decode(w, r, &in) {
			return
		}
		res, err := s.cls.Classify(r.Context(), s.k.Situation(r.Context()), s.k.Slots(), "", in.Text)
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		reply(w, 200, res)
	})
	// The owner's answer to a hint on a draft. An accepted slot hint also pins the slot, as
	// the owner's choice for the message they are about to send (not a correction of the
	// last one, so it is no pin signal).
	mux.HandleFunc("POST "+Prefix+"/classify/feedback", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Text     string `json:"text"`
			Facet    string `json:"facet"`
			Label    string `json:"label"`
			Accepted bool   `json:"accepted"`
		}
		if !decode(w, r, &in) {
			return
		}
		if in.Facet == "" || in.Label == "" {
			fail(w, 400, "facet and label are required")
			return
		}
		if in.Facet == classifier.Slot && in.Accepted {
			if _, ok := slots.Named(s.k.Slots(), in.Label); !ok {
				fail(w, 400, "no slot "+in.Label)
				return
			}
		}
		f := slots.Feedback{Text: in.Text, Facet: in.Facet, Label: in.Label, Accepted: in.Accepted, Actor: string(module.ActorOwner)}
		if s.trace != nil && s.traceOn() {
			if err := s.trace.AddFeedback(r.Context(), f); err != nil {
				fail(w, 500, err.Error())
				return
			}
		}
		s.learnFeedback(f)
		if in.Facet == classifier.Slot && in.Accepted && s.pin != nil {
			view := ""
			if win, ok := s.k.Desk().Focused(r.Context()); ok {
				view = win.View
			}
			s.pin.Pin(in.Label, view)
			s.k.Events().Publish("agent.context", s.Preview(r.Context()))
		}
		reply(w, 200, s.Preview(r.Context()))
	})
	// Clusters forming among the newest turns a facet abstained on: candidate new labels.
	mux.HandleFunc("GET "+Prefix+"/classify/emerging", func(w http.ResponseWriter, r *http.Request) {
		facet := r.URL.Query().Get("facet")
		if facet == "" {
			facet = classifier.Act
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > 1000 {
			limit = 500
		}
		groups, n, err := s.emerging(r.Context(), facet, limit)
		switch {
		case errors.Is(err, errNoEmbedder):
			fail(w, 409, err.Error())
			return
		case err != nil:
			fail(w, 502, err.Error())
			return
		}
		reply(w, 200, map[string]any{"facet": facet, "abstained": n, "groups": groups})
	})
	// How a fresh classifier would have done on the whole trace, against the focus selector.
	mux.HandleFunc("GET "+Prefix+"/classify/replay", func(w http.ResponseWriter, r *http.Request) {
		var turns []slots.TraceTurn
		if s.trace != nil {
			if err := s.trace.Each(r.Context(), func(t slots.TraceTurn) error { turns = append(turns, t); return nil }); err != nil {
				fail(w, 500, err.Error())
				return
			}
		}
		rep, err := classifier.Replay(r.Context(), turns, s.k.Slots())
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		reply(w, 200, rep)
	})
	mux.HandleFunc("GET "+Prefix+"/slots", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, s.slotList(r.Context())) })
	mux.HandleFunc("POST "+Prefix+"/slots/pin", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Slot string `json:"slot"`
		}
		if !decode(w, r, &in) {
			return
		}
		raw, _ := json.Marshal(map[string]string{"slot": in.Slot})
		if in.Slot == "" && s.pin != nil {
			s.pin.Pin("", "")
			s.signal(r.Context(), "unpin", nil)
		} else if _, err := s.callOwn(r, "context_switch", raw); err != nil {
			fail(w, 400, err.Error())
			return
		}
		reply(w, 200, s.Preview(r.Context()))
	})

	mux.HandleFunc("GET "+Prefix+"/reflexes", func(w http.ResponseWriter, r *http.Request) {
		list, err := s.reflexes.List(r.Context())
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		if list == nil {
			list = []reflex.Reflex{}
		}
		reply(w, 200, list)
	})
	mux.HandleFunc("PUT "+Prefix+"/reflexes/{name}", func(w http.ResponseWriter, r *http.Request) {
		var in reflex.Reflex
		if !decode(w, r, &in) {
			return
		}
		in.Name = r.PathValue("name")
		if err := s.reflexes.Put(r.Context(), in); err != nil {
			fail(w, 400, err.Error())
			return
		}
		s.k.Events().Publish("agent.reflexes", map[string]string{"name": in.Name})
		reply(w, 200, in)
	})
	mux.HandleFunc("DELETE "+Prefix+"/reflexes/{name}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.reflexes.Delete(r.Context(), r.PathValue("name")); err != nil {
			fail(w, 500, err.Error())
			return
		}
		s.k.Events().Publish("agent.reflexes", map[string]string{"name": r.PathValue("name")})
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET "+Prefix+"/rules", func(w http.ResponseWriter, r *http.Request) {
		own, err := s.router.Rules(r.Context())
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		if own == nil {
			own = []router.Rule{}
		}
		reply(w, 200, map[string]any{"owner": own, "builtin": s.router.Builtin()})
	})
	mux.HandleFunc("PUT "+Prefix+"/rules/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in router.Rule
		if !decode(w, r, &in) {
			return
		}
		in.ID = r.PathValue("id")
		if err := s.router.Put(r.Context(), in); err != nil {
			fail(w, 400, err.Error())
			return
		}
		reply(w, 200, in)
	})
	mux.HandleFunc("DELETE "+Prefix+"/rules/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.router.Delete(r.Context(), r.PathValue("id")); err != nil {
			fail(w, 500, err.Error())
			return
		}
		w.WriteHeader(204)
	})
}

func (s *Service) callOwn(r *http.Request, name string, input json.RawMessage) (any, error) {
	return s.callOwnCtx(r.Context(), name, input)
}

func (s *Service) callOwnCtx(ctx context.Context, name string, input json.RawMessage) (any, error) {
	for _, t := range s.ownTools() {
		if t.Name == name {
			return t.Call(ctx, input)
		}
	}
	return nil, errors.New("no such tool")
}

// turn is the wire's POST /turns, with the context assembled here. A page's own context and
// tools are ignored: the desk is the server's, and every tool is a server tool.
func (s *Service) turn(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text     string `json:"text"`
		ClientID string `json:"client_id"`
		Auto     string `json:"auto"`
	}
	if !decode(w, r, &in) {
		return
	}
	res, err := s.Send(r.Context(), in.Text, in.ClientID, in.Auto)
	switch {
	case errors.Is(err, ErrNotReady):
		fail(w, 503, "The agent is not set up yet: write "+ConfigPath(s.k.AppName())+" and restart the app (see docs/agent.md).")
	case errors.Is(err, ErrBusy):
		fail(w, 409, "The agent is still working on the last message.")
	case err != nil:
		fail(w, 400, err.Error())
	case res.Halted != "":
		reply(w, 422, map[string]any{"code": "halted", "message": res.Halted, "fired": res.Fired})
	default:
		reply(w, 200, res)
	}
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, message string) {
	code := map[int]string{400: "invalid_request", 404: "not_found", 409: "conflict", 422: "halted", 500: "internal", 503: "unavailable"}[status]
	reply(w, status, map[string]string{"code": code, "message": message})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512<<10)).Decode(v); err != nil {
		fail(w, 400, "The request body is not valid JSON.")
		return false
	}
	return true
}
