// Package workflows hosts the workflow designer and keeps reviewable module briefs.
package workflows

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hollis-labs/surface/module"
)

//go:embed migrations/*.sql
var migrations embed.FS

func init() { module.Register(&mod{}) }

type mod struct {
	module.Base
	h  module.Host
	st *store
}

func (m *mod) Manifest() module.Manifest {
	return module.Manifest{
		ID: "workflows", Title: "Workflows", About: "Design how people, agents and systems work together; prepare module briefs for review.", Version: "0.1.0",
		Store: &module.Store{Migrations: migrations, Dir: "migrations"},
		Views: []module.View{{ID: "workflows", Title: "Workflows", Icon: "workflow", Menu: true, Bare: true, Width: 1100, Height: 760, Render: module.Component("workflows/main"), Describe: func(_ context.Context, _ module.Host, w module.Window) (module.WindowContext, error) {
			id, _ := w.State["brief"].(string)
			return module.WindowContext{Kind: "workflow", Ref: "workflows:" + id}, nil
		}}},
		Commands: []module.Command{{ID: "workflows.open", Title: "Workflows: open", Section: "Workflows", Run: func(ctx context.Context, h module.Host, args map[string]any) (any, error) {
			return h.Desk().Open(ctx, "workflows", module.OpenOptions{Args: args})
		}}},
		Tools: m.tools(),
		Slots: []module.Slot{{Name: "workflows", About: "designing interactive user, agent and system workflows as Surface modules", Views: []string{"workflows"}, Tools: []string{"workflows_list", "workflows_get", "workflows_save", "workflows_handoff"}, Where: "the workflow briefs (workflows_list)", Instructions: "Start with the outcome and actors. Describe transitions and who moves next, then map views, tools, cards and context. Save changes as drafts; the owner marks a reviewed brief ready. A handoff is prepared text, not a sent design request. Use design_request only when the owner authorizes sending it.", Knowledge: m.knowledge}},
	}
}
func (m *mod) Start(_ context.Context, h module.Host) error {
	m.h = h
	m.st = &store{db: h.Store()}
	h.Handle("GET /briefs", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { v, err := m.st.list(r.Context()); reply(w, v, err) }))
	h.Handle("GET /briefs/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, err := m.st.get(r.Context(), r.PathValue("id"))
		reply(w, v, err)
	}))
	h.Handle("GET /briefs/{id}/handoff", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, err := m.reviewText(r.Context(), r.PathValue("id"))
		reply(w, v, err)
	}))
	h.Handle("POST /briefs", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if module.ActorOf(r.Context()).Delegate() {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"use workflows_save so the owner can approve the write"}`))
			return
		}
		var b Brief
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 70000))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&b); err != nil {
			reply(w, nil, err)
			return
		}
		v, err := m.save(r.Context(), b)
		reply(w, v, err)
	}))
	return nil
}
func (m *mod) save(ctx context.Context, b Brief) (Brief, error) {
	if module.ActorOf(ctx).Delegate() {
		if b.Stage == "ready" {
			return Brief{}, errors.New("only the owner can mark a reviewed brief ready")
		}
		for _, w := range m.h.Desk().Windows(ctx) {
			id, _ := w.State["brief"].(string)
			if w.Module == "workflows" && id == b.ID && (w.Dirty || w.Composing) {
				return Brief{}, errWorking
			}
		}
	}
	saved, err := m.st.save(ctx, b)
	if err == nil {
		m.h.Events().Publish("workflows.changed", map[string]any{"id": saved.ID, "revision": saved.Revision})
	}
	return saved, err
}
func (m *mod) reviewText(ctx context.Context, id string) (map[string]any, error) {
	b, err := m.st.get(ctx, id)
	if err != nil {
		return nil, err
	}
	if b.Stage == "draft" {
		return nil, errors.New("request review before preparing the handoff")
	}
	return map[string]any{"brief": b.ID, "revision": b.Revision, "text": handoff(b), "sent": false}, nil
}
func (m *mod) knowledge(ctx context.Context, _ module.Host, s module.Situation) (string, error) {
	if s.Focused == nil {
		return "", nil
	}
	id := strings.TrimPrefix(s.Focused.Ref, "workflows:")
	if id == "" {
		return "Workflow briefs: workflows_list. Design user, agent and system transitions before views.", nil
	}
	b, err := m.st.get(ctx, id)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Workflow %s (%s), stage %s, revision %d. Read workflows_get before editing.", b.ID, b.Title, b.Stage, b.Revision), nil
}
func reply(w http.ResponseWriter, v any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, errMissing) {
			status = http.StatusNotFound
		}
		if errors.Is(err, errConflict) || errors.Is(err, errWorking) {
			status = http.StatusConflict
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}
func (m *mod) tools() []module.Tool {
	idSchema := json.RawMessage(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}`)
	read := func(handoffOnly bool) func(context.Context, module.Host, json.RawMessage) (any, error) {
		return func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
			var a struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return nil, err
			}
			if handoffOnly {
				return m.reviewText(ctx, a.ID)
			}
			return m.st.get(ctx, a.ID)
		}
	}
	return []module.Tool{
		{Name: "workflows_list", Level: module.LevelRead, Description: "List workflow briefs with stages and revisions.", Input: json.RawMessage(`{"type":"object","properties":{}}`), Run: func(ctx context.Context, _ module.Host, _ json.RawMessage) (any, error) { return m.st.list(ctx) }},
		{Name: "workflows_get", Level: module.LevelRead, Description: "Read a workflow brief before editing or reviewing it.", Input: idSchema, Run: read(false)},
		{Name: "workflows_handoff", Level: module.LevelRead, Description: "Prepare the current reviewed workflow brief as text for the design team. Does not send it or start work.", Input: idSchema, Run: read(true)},
		{Name: "workflows_save", Level: module.LevelWrite, Description: "Create a workflow draft or replace a brief at its current revision. Supply all content fields; edits return to draft. Review requires all actor steps and acceptance checks. Only the owner marks ready.", Input: json.RawMessage(`{"type":"object","required":["title","outcome","user_steps","agent_steps","system_steps","acceptance","stage"],"properties":{"id":{"type":"string"},"revision":{"type":"integer"},"title":{"type":"string"},"outcome":{"type":"string"},"user_steps":{"type":"string"},"agent_steps":{"type":"string"},"system_steps":{"type":"string"},"acceptance":{"type":"string"},"stage":{"type":"string","enum":["draft","review"]}}}`), Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
			var b Brief
			if err := json.Unmarshal(in, &b); err != nil {
				return nil, err
			}
			return m.save(ctx, b)
		}},
	}
}
