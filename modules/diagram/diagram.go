// Package diagram is the Diagram module: architecture and flow drawings the owner and the agent
// keep together. A diagram is plain JSON (nodes, edges, lanes, positions) the agent can read and
// write; every save is a revision, and any two can be compared on the canvas. Ported from
// Basalisk's Diagram app. A drawing is kept in the module's own store or, as in Basalisk, in
// Caster (caster://collab/diagram.<name>, caster.go); both are listed.
package diagram

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/collab"
	"github.com/hollis-labs/surface/module/tetherlink"
)

//go:embed migrations/*.sql
var migrations embed.FS

func init() { module.Register(&mod{}) }

type mod struct {
	module.Base
	h    module.Host
	st   backend
	link tetherlink.Caller // tests: Caster's stand-in; else from the settings
}

const viewID = "diagram"

// bodyFormat is how a diagram is written, for the agent.
const bodyFormat = `A diagram body is JSON: {"diagram":"v1","nodes":[...],"edges":[...],"groups":[...]}.
A node: {"id","label","kind"?,"group"?,"subtitle"?,"note"?,"tone"?,"step"?,"tag"?,"items"?,"x"?,"y"?}; kind is one of service, store, queue, client, external, actor, agent, step, decision, start, end, note; tone is blue, amber, red, green, violet, teal, pink or slate. Leave x and y out to have the window lay it out.
An edge: {"from","to","label"?,"style"?,"both"?}; style is flow, human, feedback, repair, async, data or muted. from and to are node ids.
A group (a lane): {"id","label","subtitle"?,"tone"?}; a node joins it with "group".
Optional: "theme" (app, dark, light, neon), "grid" (lines, dots, none), "legend" (labels for the edge styles, or false).`

func (m *mod) Manifest() module.Manifest {
	return module.Manifest{
		ID: "diagram", Title: "Diagram", Version: "0.1.0",
		About: "Architecture and flow drawings, kept as JSON with every save a revision.",
		Views: []module.View{{
			ID: viewID, Title: "Diagram", Icon: "workflow", Width: 1200, Height: 760,
			Menu: true, Multi: true, Bare: true, DefaultKey: "diagram",
			Render: module.Component("diagram/main"),
			Describe: func(ctx context.Context, _ module.Host, w module.Window) (module.WindowContext, error) {
				id, _ := w.Args["id"].(string)
				if id == "" {
					return module.WindowContext{Kind: "data", Ref: "diagram:list"}, nil
				}
				d, err := m.st.get(ctx, id)
				if err != nil {
					return module.WindowContext{Kind: "data", Ref: "diagram:" + id}, nil //nolint:nilerr // a window on a deleted diagram still has a context
				}
				return module.WindowContext{Kind: "data", Ref: "diagram:" + id, Extra: map[string]any{"title": d.Title, "rev": d.Rev}}, nil
			},
		}},
		Commands: []module.Command{
			{ID: "diagram.open", Title: "Diagram: open", Section: "Diagram", Keywords: "draw architecture flow chart drawing",
				Run: func(ctx context.Context, h module.Host, args map[string]any) (any, error) {
					id, _ := args["id"].(string)
					return m.open(ctx, h, id)
				}},
		},
		Tools:    m.tools(),
		Store:    &module.Store{Migrations: migrations, Dir: "migrations"},
		Settings: casterSettings,
		Slots: []module.Slot{{
			Name: "diagram", About: "architecture and flow drawings the owner and the agent keep together",
			Views:        []string{viewID},
			Tools:        []string{"diagram_list", "diagram_get", "diagram_put", "diagram_open"},
			Where:        "the diagrams (diagram_list)",
			Instructions: "Read a diagram (diagram_get) before changing it, and save the whole body back (diagram_put); every save is a revision the owner can compare. " + bodyFormat,
		}},
	}
}

func (m *mod) open(ctx context.Context, h module.Host, id string) (module.Window, error) {
	if id == "" {
		return h.Desk().Open(ctx, viewID, module.OpenOptions{Key: "diagram"})
	}
	d, err := m.st.get(ctx, id)
	if err != nil {
		return module.Window{}, err
	}
	return h.Desk().Open(ctx, viewID, module.OpenOptions{Key: d.ID, Title: "Diagram: " + d.Title, Args: map[string]any{"id": d.ID}})
}

func (m *mod) tools() []module.Tool {
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	return []module.Tool{
		{Name: "diagram_list", Level: module.LevelRead,
			Description: "List the diagrams: id, title, revision, when last saved.",
			Input:       raw(`{"type":"object","properties":{}}`),
			Run: func(ctx context.Context, _ module.Host, _ json.RawMessage) (any, error) {
				return m.st.list(ctx)
			}},
		{Name: "diagram_get", Level: module.LevelRead,
			Description: "Read a diagram: its title, revision and JSON body, and its revisions. Give rev to read an older revision. " + bodyFormat,
			Input:       raw(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"},"rev":{"type":"integer"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					ID  string `json:"id"`
					Rev int    `json:"rev"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				return m.read(ctx, a.ID, a.Rev)
			}},
		{Name: "diagram_put", Level: module.LevelWrite,
			Description: "Create a diagram (no id) or save a new revision of one (id): the whole body, as JSON (a string or an object). base_rev, when given, refuses the save if someone saved since. " + bodyFormat,
			Input:       raw(`{"type":"object","required":["body"],"properties":{"id":{"type":"string"},"title":{"type":"string"},"body":{"description":"The diagram, as a JSON object or a JSON string."},"base_rev":{"type":"integer"}}}`),
			Run: func(ctx context.Context, h module.Host, in json.RawMessage) (any, error) {
				var a struct {
					ID    string          `json:"id"`
					Title string          `json:"title"`
					Body  json.RawMessage `json:"body"`
					Base  int             `json:"base_rev"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				body := string(a.Body)
				var s string
				if json.Unmarshal(a.Body, &s) == nil {
					body = s // given as a string of JSON
				}
				var d Diagram
				var err error
				if a.ID == "" {
					d, err = m.st.create(ctx, a.Title, body)
				} else {
					d, err = m.st.save(ctx, a.ID, body, a.Title, a.Base)
				}
				if err != nil {
					return nil, err
				}
				changed(h, d.ID)
				return map[string]any{"id": d.ID, "title": d.Title, "rev": d.Rev}, nil
			}},
		{Name: "diagram_open", Level: module.LevelAct,
			Description: "Open a diagram's window (or the list, with no id).",
			Input:       raw(`{"type":"object","properties":{"id":{"type":"string"}}}`),
			Run: func(ctx context.Context, h module.Host, in json.RawMessage) (any, error) {
				var a struct {
					ID string `json:"id"`
				}
				_ = json.Unmarshal(in, &a)
				return m.open(ctx, h, a.ID)
			}},
	}
}

// read is a diagram with its revisions, or one revision of it as the body.
func (m *mod) read(ctx context.Context, id string, rev int) (map[string]any, error) {
	d, err := m.st.get(ctx, id)
	if err != nil {
		return nil, err
	}
	revs, err := m.st.revisions(ctx, id)
	if err != nil {
		return nil, err
	}
	if rev > 0 && rev != d.Rev {
		r, rerr := m.st.revision(ctx, id, rev)
		if rerr != nil {
			return nil, rerr
		}
		d.Body, d.Title = r.Body, r.Title
		return map[string]any{"diagram": d, "shown_rev": rev, "revisions": revs}, nil
	}
	return map[string]any{"diagram": d, "shown_rev": d.Rev, "revisions": revs}, nil
}

func changed(h module.Host, id string) {
	h.Events().Publish("diagram.changed", map[string]any{"id": id})
}

func (m *mod) Start(_ context.Context, h module.Host) error {
	m.h = h
	m.st = &router{m: m, local: &store{db: h.Store()}, cast: &casterStore{docs: &collab.Store{Link: m.tether}}}
	h.Handle("GET /diagrams", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		list, err := m.st.list(r.Context())
		reply(w, list, err)
	}))
	h.Handle("POST /diagrams", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Title, Body string }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2*maxBody)).Decode(&in); err != nil {
			reply(w, nil, err)
			return
		}
		d, err := m.st.create(r.Context(), in.Title, in.Body)
		if err == nil {
			changed(h, d.ID)
		}
		reply(w, d, err)
	}))
	h.Handle("GET /diagrams/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out, err := m.read(r.Context(), r.PathValue("id"), 0)
		reply(w, out, err)
	}))
	h.Handle("PUT /diagrams/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Title string  `json:"title"`
			Body  *string `json:"body"`
			Rev   int     `json:"rev"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2*maxBody)).Decode(&in); err != nil {
			reply(w, nil, err)
			return
		}
		var d Diagram
		var err error
		if in.Body == nil {
			d, err = m.st.rename(r.Context(), r.PathValue("id"), in.Title)
		} else {
			d, err = m.st.save(r.Context(), r.PathValue("id"), *in.Body, in.Title, in.Rev)
		}
		if err == nil {
			changed(h, d.ID)
		}
		reply(w, d, err)
	}))
	h.Handle("DELETE /diagrams/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := m.st.remove(r.Context(), r.PathValue("id"))
		if err == nil {
			changed(h, r.PathValue("id"))
		}
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	h.Handle("GET /diagrams/{id}/revisions/{n}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, err := strconv.Atoi(r.PathValue("n"))
		if err != nil || n < 1 {
			reply(w, nil, fmt.Errorf("a revision is a number from 1, not %q", r.PathValue("n")))
			return
		}
		rev, err := m.st.revision(r.Context(), r.PathValue("id"), n)
		reply(w, rev, err)
	}))
	return nil
}

func reply(w http.ResponseWriter, v any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		status := http.StatusUnprocessableEntity
		switch {
		case errors.Is(err, errNotFound):
			status = http.StatusNotFound
		case errors.Is(err, errConflict):
			status = http.StatusConflict
		case strings.Contains(err.Error(), "request body too large"):
			status = http.StatusRequestEntityTooLarge
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}
