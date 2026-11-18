// Package scratch is the Scratch module: quick notes, to-dos, ideas and follow-ups as small
// items, kept in the module's own store.
//
// It is also the smallest complete example of a Surface module: a view, a command, agent
// tools, a local store, live settings and a context slot, on the module contract alone.
package scratch

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
	st *store
}

func (m *mod) Manifest() module.Manifest {
	return module.Manifest{
		ID: "scratch", Title: "Scratch", Version: "0.1.0",
		About: "Quick notes, to-dos, ideas and follow-ups as small items.",
		Views: []module.View{{
			ID: "scratch", Title: "Scratch", Icon: "sticky-note", Width: 520, Height: 640, Menu: true, Bare: true,
			Render: module.Component("scratch/main"),
			Describe: func(ctx context.Context, h module.Host, w module.Window) (module.WindowContext, error) {
				open, done, err := m.st.counts(ctx)
				return module.WindowContext{Kind: "data", Ref: "scratch:items", Extra: map[string]any{"open": open, "done": done}}, err
			},
		}},
		Commands: []module.Command{
			{ID: "scratch.add", Title: "Scratch: jot something down", Section: "Scratch", Keywords: "note quick todo follow-up idea capture", Keys: "alt+shift+s",
				Run: func(ctx context.Context, h module.Host, args map[string]any) (any, error) {
					text, _ := args["text"].(string)
					if strings.TrimSpace(text) == "" {
						// Nothing to keep yet: open the window, where the box has the focus.
						return h.Desk().Open(ctx, "scratch", module.OpenOptions{Args: map[string]any{"compose": true}})
					}
					kind, _ := args["kind"].(string)
					return m.add(ctx, h, text, kind, "")
				}},
			{ID: "scratch.clear-done", Title: "Scratch: put away what is done", Section: "Scratch",
				Run: func(ctx context.Context, h module.Host, _ map[string]any) (any, error) {
					items, err := m.st.list(ctx, "", "", true, 500)
					if err != nil {
						return nil, err
					}
					n := 0
					for _, it := range items {
						if it.Done && m.st.remove(ctx, it.ID) == nil {
							n++
						}
					}
					h.Events().Publish("scratch.changed", map[string]any{"cleared": n})
					return map[string]int{"cleared": n}, nil
				}},
		},
		Tools: m.tools(),
		Store: &module.Store{Migrations: migrations, Dir: "migrations"},
		Settings: []module.Setting{
			{Key: "show_done", Title: "Show finished items", About: "Keep items in the list after they are ticked.", Type: module.SettingBool, Default: true},
			{Key: "default_kind", Title: "New items are", Type: module.SettingEnum, Default: "note", Options: []string{"note", "todo", "idea", "followup"}},
		},
		Slots: []module.Slot{{
			Name: "scratch", About: "quick capture: things the owner jotted down to deal with later (notes, to-dos, ideas, follow-ups)",
			Views:        []string{"scratch"},
			Tools:        []string{"scratch_list", "scratch_add", "scratch_update", "scratch_delete"},
			Where:        "the scratch items (scratch_list)",
			Instructions: "Keep the owner's words as they wrote them. When they ask what is outstanding, list the open items; do not tick one off unless they say it is done.",
			Knowledge: func(ctx context.Context, h module.Host, _ module.Situation) (string, error) {
				items, err := m.st.list(ctx, "", "", false, 12)
				if err != nil || len(items) == 0 {
					return "", err
				}
				var b strings.Builder
				b.WriteString("The newest open scratch items:\n")
				for _, it := range items {
					fmt.Fprintf(&b, "- [%s] (%s) %s\n", it.ID, it.Kind, clip(it.Text, 140))
				}
				return b.String(), nil
			},
		}},
	}
}

func clip(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func (m *mod) add(ctx context.Context, h module.Host, text, kind, tags string) (Item, error) {
	if kind == "" {
		if _, k := kindOf(text, ""); k == "note" {
			kind = h.Config().String("default_kind")
		}
	}
	it, err := m.st.add(ctx, text, kind, tags)
	if err == nil {
		h.Events().Publish("scratch.changed", map[string]any{"added": it.ID})
	}
	return it, err
}

func (m *mod) Start(_ context.Context, h module.Host) error {
	m.st = &store{db: h.Store()}
	changed := func(what string, id string) { h.Events().Publish("scratch.changed", map[string]any{what: id}) }

	h.Handle("GET /items", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		items, err := m.st.list(r.Context(), q.Get("q"), q.Get("kind"), h.Config().Bool("show_done"), 0)
		reply(w, items, err)
	}))
	h.Handle("POST /items", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Text, Kind, Tags string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			reply(w, nil, err)
			return
		}
		it, err := m.add(r.Context(), h, in.Text, in.Kind, in.Tags)
		reply(w, it, err)
	}))
	h.Handle("PUT /items/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p patch
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			reply(w, nil, err)
			return
		}
		it, err := m.st.update(r.Context(), r.PathValue("id"), p)
		if err == nil {
			changed("updated", it.ID)
		}
		reply(w, it, err)
	}))
	h.Handle("DELETE /items/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := m.st.remove(r.Context(), r.PathValue("id"))
		if err == nil {
			changed("removed", r.PathValue("id"))
		}
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	h.Handle("POST /items/{id}/restore", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		it, err := m.st.restore(r.Context(), r.PathValue("id"))
		if err == nil {
			changed("restored", it.ID)
		}
		reply(w, it, err)
	}))
	// The list depends on a live setting: tell the views when it changes.
	h.Config().Watch("show_done", func(any) { h.Events().Publish("scratch.changed", map[string]any{"setting": "show_done"}) })
	return nil
}

func reply(w http.ResponseWriter, v any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, errNotFound) {
			status = http.StatusNotFound
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

func (m *mod) tools() []module.Tool {
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	return []module.Tool{
		{Name: "scratch_list", Level: module.LevelRead,
			Description: "List the owner's scratch items, newest first: quick notes, to-dos, ideas and follow-ups. Filter by text (q) or kind; finished items are left out unless done is true.",
			Input:       raw(`{"type":"object","properties":{"q":{"type":"string"},"kind":{"type":"string","enum":["note","todo","idea","followup"]},"done":{"type":"boolean","description":"Include finished items."},"limit":{"type":"integer"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					Q, Kind string
					Done    bool
					Limit   int
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				return m.st.list(ctx, a.Q, a.Kind, a.Done, a.Limit)
			}},
		{Name: "scratch_add", Level: module.LevelWrite,
			Description: "Keep something in the owner's scratch, word for word. kind is note, todo, idea or followup.",
			Input:       raw(`{"type":"object","required":["text"],"properties":{"text":{"type":"string"},"kind":{"type":"string","enum":["note","todo","idea","followup"]},"tags":{"type":"string"}}}`),
			Describe: func(in json.RawMessage) string {
				var a struct{ Text string }
				_ = json.Unmarshal(in, &a)
				return "Add to scratch: “" + clip(a.Text, 80) + "”"
			},
			Run: func(ctx context.Context, h module.Host, in json.RawMessage) (any, error) {
				var a struct{ Text, Kind, Tags string }
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				return m.add(ctx, h, a.Text, a.Kind, a.Tags)
			}},
		{Name: "scratch_update", Level: module.LevelWrite,
			Description: "Change a scratch item: its text, kind or tags, or tick it done (done: true) or open again (done: false).",
			Input:       raw(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"},"text":{"type":"string"},"kind":{"type":"string","enum":["note","todo","idea","followup"]},"done":{"type":"boolean"},"tags":{"type":"string"}}}`),
			Describe: func(in json.RawMessage) string {
				var a struct {
					ID   string
					Done *bool
				}
				_ = json.Unmarshal(in, &a)
				if a.Done != nil && *a.Done {
					return "Tick off scratch item " + a.ID
				}
				return "Change scratch item " + a.ID
			},
			Run: func(ctx context.Context, h module.Host, in json.RawMessage) (any, error) {
				var a struct {
					ID string `json:"id"`
					patch
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				it, err := m.st.update(ctx, a.ID, a.patch)
				if err == nil {
					h.Events().Publish("scratch.changed", map[string]any{"updated": it.ID})
				}
				return it, err
			}},
		{Name: "scratch_delete", Level: module.LevelConfirm,
			Description: "Put a scratch item away. It is kept and can be restored.",
			Input:       raw(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}`),
			Describe: func(in json.RawMessage) string {
				var a struct{ ID string }
				_ = json.Unmarshal(in, &a)
				return "Put away scratch item " + a.ID
			},
			Run: func(ctx context.Context, h module.Host, in json.RawMessage) (any, error) {
				var a struct{ ID string }
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				if err := m.st.remove(ctx, a.ID); err != nil {
					return nil, err
				}
				h.Events().Publish("scratch.changed", map[string]any{"removed": a.ID})
				return "put away", nil
			}},
	}
}
