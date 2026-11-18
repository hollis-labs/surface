package genui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	contract "github.com/hollis-labs/substrate/genui"
	"github.com/hollis-labs/substrate/genui/gateway"

	"github.com/hollis-labs/surface/core/internal/kernel"
	"github.com/hollis-labs/surface/module"
)

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, gateway.ErrInvalid):
		status = http.StatusUnprocessableEntity
	case errors.Is(err, gateway.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, gateway.ErrForbidden):
		status = http.StatusForbidden
	case errors.Is(err, gateway.ErrUnavailable):
		status = http.StatusServiceUnavailable
	}
	reply(w, status, map[string]string{"error": err.Error()})
}

func body(r *http.Request) ([]byte, error) {
	return io.ReadAll(http.MaxBytesReader(nil, r.Body, contract.MaxActionBytes))
}

// Mount adds the routes the page's GenUI controller talks to. The app serves on the
// loopback interface to its one owner, which is the scope every route acts in.
func (s *Service) Mount(mux *http.ServeMux) {
	// Show a surface from outside (a script, another agent).
	mux.HandleFunc("POST /api/genui/surfaces", func(w http.ResponseWriter, r *http.Request) {
		raw, err := body(r)
		if err != nil {
			fail(w, gateway.ErrInvalid)
			return
		}
		var q ShowRequest
		if json.Unmarshal(raw, &q) != nil {
			fail(w, gateway.ErrInvalid)
			return
		}
		q.Source = "api"
		card, env, err := s.Show(r.Context(), q)
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, http.StatusOK, map[string]any{"card": card.ID, "envelope": env})
	})
	// The owner acted on a surface. The body is the contract's native call.
	mux.HandleFunc("POST /api/genui/actions", func(w http.ResponseWriter, r *http.Request) {
		raw, err := body(r)
		if err != nil {
			fail(w, gateway.ErrInvalid)
			return
		}
		res, err := s.gw.Submit(r.Context(), owner, raw)
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, http.StatusOK, res)
	})
	// Read what an action came to. It never executes anything.
	mux.HandleFunc("GET /api/genui/actions/{action}", func(w http.ResponseWriter, r *http.Request) {
		rec, err := s.gw.Recover(r.Context(), owner, r.PathValue("action"), r.URL.Query().Get("functionCallId"))
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, http.StatusOK, rec)
	})
	// The owner's second step on a high-risk choice.
	mux.HandleFunc("POST /api/genui/confirm", func(w http.ResponseWriter, r *http.Request) {
		raw, err := body(r)
		if err != nil {
			fail(w, gateway.ErrInvalid)
			return
		}
		proof, err := s.Confirm(r.Context(), raw)
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, http.StatusOK, map[string]string{"proof": proof})
	})
	// A click on a list item: the host answers with a new surface.
	mux.HandleFunc("POST /api/genui/surfaces/{id}/select", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ID string `json:"id"`
		}
		raw, err := body(r)
		if err != nil || json.Unmarshal(raw, &in) != nil {
			fail(w, gateway.ErrInvalid)
			return
		}
		env, err := s.Select(r.Context(), r.PathValue("id"), in.ID)
		if err != nil {
			fail(w, err)
			return
		}
		reply(w, http.StatusOK, map[string]any{"envelope": env})
	})
}

// Tools is what the agent gets: one tool that shows a reviewed pattern.
func (s *Service) Tools() []kernel.BoundTool {
	return []kernel.BoundTool{{
		Module: "genui",
		Tool: module.Tool{
			Name:  "genui_show",
			Level: module.LevelAct,
			Description: "Show the user a small interface of your own making, from one of three reviewed patterns, and (for approval and form) wait for what they do. You give parameters, never components, markup or code. " +
				"approval: {title, summary?, changes?: [{label, before, after}], choices: [{id, label, risk?: low|medium|high}]} with one to four choices; a high-risk choice makes the user confirm twice. " +
				"form: {title, fields: [{key, label, type: text|textarea|checkbox|integer, required, help?, min?, max?, maxLength?}], submit_label?, draft?: {key: value}}. " +
				"list_detail: {title, items: [{id, title, summary?}], details?: {id: {title, body}}, selected?}; it does not wait. " +
				"The result has status and, when the user acted, input: {choice} for an approval, {choice: \"submit\", draft: {...}} for a form. Prefer card_ask for a plain question; use this when the shape matters: a before/after review, a typed form, a list to browse.",
			Input: json.RawMessage(`{"type":"object","required":["pattern","params"],"properties":{"pattern":{"type":"string","enum":["approval","form","list_detail"]},"params":{"type":"object"},"timeout_seconds":{"type":"integer","description":"How long to wait for the user. Default 600, at most 3600."}}}`),
		},
		Call: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var in struct {
				ShowRequest
				Timeout int `json:"timeout_seconds"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return nil, fmt.Errorf("bad input: %w", err)
			}
			in.Source = "agent"
			in.Blocking = in.Pattern != contract.PatternListDetail
			card, env, err := s.Show(ctx, in.ShowRequest)
			if err != nil {
				return nil, fmt.Errorf("the surface was not shown (%w): check the pattern's parameters", err)
			}
			if !in.Blocking {
				return map[string]any{"id": card.ID, "surface": env.ID}, nil
			}
			wait := time.Duration(min(max(in.Timeout, 0), 3600)) * time.Second
			if wait == 0 {
				wait = 10 * time.Minute
			}
			waitCtx, cancel := context.WithTimeout(ctx, wait)
			defer cancel()
			done, err := s.cards.Await(waitCtx, card.ID)
			if err != nil {
				status := "canceled"
				if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
					status = "expired"
				}
				ended, _ := s.cards.End(context.WithoutCancel(ctx), card.ID, status)
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return map[string]any{"id": card.ID, "status": ended.Status}, nil
			}
			out := map[string]any{"id": done.ID, "status": done.Status}
			if done.Outcome != nil && len(done.Outcome.Data) > 0 {
				out["input"] = done.Outcome.Data
			}
			return out, nil
		},
	}}
}
