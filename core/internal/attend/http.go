package attend

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/hollis-labs/surface/core/internal/kernel"
	"github.com/hollis-labs/surface/module"
)

// Mount serves the items:
//
//	GET  /api/attend        {items, counts}: the most pressing first
//	POST /api/attend/seen   {subject}: the owner has seen it (a delegate is refused)
func (s *Service) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/attend", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, 200, map[string]any{"items": s.List(), "counts": s.Counts()})
	})
	mux.HandleFunc("POST /api/attend/seen", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Subject string `json:"subject"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil || in.Subject == "" {
			reply(w, 400, map[string]string{"error": "want {\"subject\": \"...\"}"})
			return
		}
		if err := s.Seen(r.Context(), in.Subject); err != nil {
			reply(w, 403, map[string]string{"error": err.Error()})
			return
		}
		reply(w, 200, map[string]any{"items": s.List(), "counts": s.Counts()})
	})
}

// Tools lets an agent read what is waiting for the owner. It cannot add to it or see for
// them: items come from what happens, and seeing is the owner's.
func (s *Service) Tools() []kernel.BoundTool {
	return []kernel.BoundTool{{
		Tool: module.Tool{Name: "attend_list", Level: module.LevelRead,
			Description: "What is waiting for the owner, the most pressing first: needs_you (a card waiting on their answer), failed (an error they have not looked at), unseen (a window opened while they were elsewhere, or a question that expired unanswered). Use it before asking the owner something new, so you do not pile on, and to tell them what is waiting.",
			Input:       json.RawMessage(`{"type":"object","properties":{}}`)},
		Module: "attend",
		Call: func(context.Context, json.RawMessage) (any, error) {
			return map[string]any{"items": s.List(), "counts": s.Counts()}, nil
		},
	}}
}

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
