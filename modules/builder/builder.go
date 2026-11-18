// Package builder is the Interaction Builder: the owner describes a flow ("triage my inbox",
// "write an article") and the agent sets the app up for it. That means the windows and how
// they are arranged, the owner's mode, standing instructions, and reflexes and routing rules
// that hold while the flow is in use. The result is saved as a flow the owner can come back
// to and refine.
//
// A flow is a saved layout whose Setup carries the mode, the instructions, the reflexes to
// switch on and a "flow" record (goal, version). The module keeps every version of each
// flow in its own store, so a refinement never loses the one before.
//
// The module does not compose anything itself. The agent does, with the builder slot's
// instructions, the catalog (builder_catalog) and the desk and agent tools. The module
// supplies the catalog, keeps the flows and draws the view.
package builder

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/hollis-labs/surface/module"
)

//go:embed migrations/*.sql
var migrations embed.FS

func init() { module.Register(&mod{}) }

type mod struct {
	module.Base
	h  module.Host
	db *sql.DB
}

// Flow is one saved flow.
type Flow struct {
	Name        string `json:"name"`
	Goal        string `json:"goal"`
	Description string `json:"description,omitempty"`
	Version     int    `json:"version"`
	Created     string `json:"created"`
	Updated     string `json:"updated"`
	// Layout is the flow's current layout, with its setup (in a get).
	Layout *module.Layout `json:"layout,omitempty"`
	// Versions are the flow's versions, newest first (in a get).
	Versions []Version `json:"versions,omitempty"`
}

// Version is one saved state of a flow.
type Version struct {
	Version int            `json:"version"`
	Note    string         `json:"note,omitempty"`
	Created string         `json:"created"`
	Layout  *module.Layout `json:"layout,omitempty"`
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9 ._-]{0,47}$`)

const instructions = `You are setting the app up for a flow the owner described: a way of working they will come back to. Do it in this order:
1. Read what there is: builder_catalog (the views, commands and context slots of every module) and builder_flow_list (flows already saved; refine one rather than duplicate it).
2. Decide the arrangement: the views the flow needs, which window is the master (first, tiled) and which stack beside it, the window to focus, and what each opens with (args, e.g. a draft id or a URL). Keep it to what the flow uses, usually 2 to 4 windows. Do not include the Builder window itself.
3. Decide the setup: a one-word mode, standing instructions for you while the flow is in use (how to help in this flow, in a few lines), and reflexes that make the flow work. Reflexes come from agent_reflex_put, with enabled: false so they are on only in this flow, and a name prefixed with the flow's ("triage.nudge"). A useful reflex reminds you of a step, opens a view when something happens (run_command), or answers a request the flow should not take (halt). Routing rules (agent_rule_put) are for shortcuts the owner will type; make them flow-only with a when condition {"field":"layout","op":"eq","value":"<flow name>"}.
4. Save it with builder_flow_save: name, goal, the arrangement (arrange), mode, instructions, reflexes (their names). That arranges the desk, saves the layout and makes it the flow in use.
5. Tell the owner in a few lines what you set up and why, and how to refine it ("make the browser bigger", "add a reflex that…"). To refine, change what they ask and save again under the same name with a note: it becomes a new version.
Ask one question first only when the flow is too vague to arrange at all; otherwise build a first version and let them refine it.`

func (m *mod) Manifest() module.Manifest {
	return module.Manifest{
		ID: "builder", Title: "Interaction Builder", Version: "0.1.0",
		About: "Describe a way of working; the agent arranges the windows, mode and reflexes for it and saves it as a flow.",
		Views: []module.View{{
			ID: "builder", Title: "Builder", Icon: "wand-sparkles", Width: 620, Height: 720, Menu: true, Bare: true,
			Render: module.Component("builder/main"),
			Describe: func(ctx context.Context, h module.Host, w module.Window) (module.WindowContext, error) {
				c := module.WindowContext{Kind: "workspace", Ref: "builder:flows"}
				if f, _ := w.State["flow"].(string); f != "" {
					c.Ref, c.Title = "builder:"+f, f
				}
				return c, nil
			},
		}},
		Commands: []module.Command{
			{ID: "builder.open", Title: "Builder: set up a flow", Section: "Builder", Keywords: "interaction flow workflow setup layout arrange reflex",
				Run: func(ctx context.Context, h module.Host, _ map[string]any) (any, error) {
					return h.Desk().Open(ctx, "builder", module.OpenOptions{})
				}},
			{ID: "builder.flow", Title: "Builder: open a flow", Section: "Builder", Hidden: true,
				Run: func(ctx context.Context, h module.Host, args map[string]any) (any, error) {
					name, _ := args["name"].(string)
					return m.openFlow(ctx, name)
				}},
		},
		Tools: m.tools(),
		Store: &module.Store{Migrations: migrations, Dir: "migrations"},
		Slots: []module.Slot{{
			Name: "builder", About: "setting the app up for a way of working: arranging windows, mode, instructions, reflexes and rules, saved as a flow",
			Views: []string{"builder"},
			Modes: []string{"building"},
			Tools: []string{"builder_catalog", "builder_flow_list", "builder_flow_get", "builder_flow_save", "builder_flow_open",
				"agent_reflex_put", "agent_reflex_list", "agent_rule_put", "desk_views", "desk_arrange", "desk_layout_list"},
			Where:        "the saved flows (builder_flow_list, builder_flow_get); the app's parts (builder_catalog); the reflexes (agent_reflex_list)",
			Instructions: instructions,
			Knowledge:    m.knowledge,
		}},
	}
}

func (m *mod) Start(_ context.Context, h module.Host) error {
	m.h, m.db = h, h.Store()
	handle := func(pattern string, fn func(r *http.Request) (any, error)) {
		h.Handle(pattern, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			v, err := fn(r)
			w.Header().Set("Content-Type", "application/json")
			if err != nil {
				code := http.StatusUnprocessableEntity
				if errors.Is(err, sql.ErrNoRows) {
					code = http.StatusNotFound
				}
				w.WriteHeader(code)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			_ = json.NewEncoder(w).Encode(v)
		}))
	}
	handle("GET /flows", func(r *http.Request) (any, error) { return m.list(r.Context()) })
	handle("GET /flows/{name}", func(r *http.Request) (any, error) { return m.get(r.Context(), r.PathValue("name")) })
	handle("POST /flows/{name}/open", func(r *http.Request) (any, error) { return m.openFlow(r.Context(), r.PathValue("name")) })
	handle("POST /flows/{name}/versions/{v}/restore", func(r *http.Request) (any, error) {
		var v int
		_, _ = fmt.Sscan(r.PathValue("v"), &v)
		return m.restore(r.Context(), r.PathValue("name"), v)
	})
	handle("DELETE /flows/{name}", func(r *http.Request) (any, error) {
		return map[string]bool{"ok": true}, m.delete(r.Context(), r.PathValue("name"))
	})
	return nil
}

func stamp() string { return time.Now().UTC().Format(time.RFC3339) }

func (m *mod) list(ctx context.Context) ([]Flow, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT name, goal, description, version, created_at, updated_at FROM flows ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Flow{}
	for rows.Next() {
		var f Flow
		if err := rows.Scan(&f.Name, &f.Goal, &f.Description, &f.Version, &f.Created, &f.Updated); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (m *mod) get(ctx context.Context, name string) (Flow, error) {
	var f Flow
	err := m.db.QueryRowContext(ctx, `SELECT name, goal, description, version, created_at, updated_at FROM flows WHERE name = ?`, name).
		Scan(&f.Name, &f.Goal, &f.Description, &f.Version, &f.Created, &f.Updated)
	if errors.Is(err, sql.ErrNoRows) {
		return f, fmt.Errorf("no flow %q: %w", name, err)
	}
	if err != nil {
		return f, err
	}
	if l, ok, _ := m.h.Desk().Layout(ctx, name); ok {
		f.Layout = &l
	}
	rows, err := m.db.QueryContext(ctx, `SELECT version, note, created_at FROM flow_versions WHERE name = ? ORDER BY version DESC`, name)
	if err != nil {
		return f, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var v Version
		if err := rows.Scan(&v.Version, &v.Note, &v.Created); err != nil {
			return f, err
		}
		f.Versions = append(f.Versions, v)
	}
	return f, rows.Err()
}

// SaveInput is what builder_flow_save takes.
type SaveInput struct {
	Name        string `json:"name"`
	Goal        string `json:"goal"`
	Description string `json:"description"`
	// Note says what this version changed.
	Note string `json:"note"`
	// Arrange, when given, is the arrangement to put on the desk before saving: mode (tile
	// or float), ratio and windows, as desk_arrange takes them. Without it the desk is saved
	// as it is.
	Arrange *struct {
		Mode    string                `json:"mode"`
		Ratio   float64               `json:"ratio"`
		Windows []module.LayoutWindow `json:"windows"`
	} `json:"arrange"`
	Mode         string   `json:"mode"`
	Instructions string   `json:"instructions"`
	Reflexes     []string `json:"reflexes"`
}

// save arranges (when asked), saves the layout with the flow's setup, keeps the version and
// makes it the flow in use.
func (m *mod) save(ctx context.Context, in SaveInput) (Flow, error) {
	in.Name = strings.ToLower(strings.TrimSpace(in.Name))
	if !nameRe.MatchString(in.Name) {
		return Flow{}, fmt.Errorf("flow name %q: lower case letters, digits, spaces, dots and dashes, up to 48", in.Name)
	}
	prev, had, _ := m.h.Desk().Layout(ctx, in.Name)
	var cur Flow
	exists := m.db.QueryRowContext(ctx, `SELECT goal, description, version FROM flows WHERE name = ?`, in.Name).Scan(&cur.Goal, &cur.Description, &cur.Version) == nil
	if strings.TrimSpace(in.Goal) == "" {
		if !exists {
			return Flow{}, errors.New("goal is required for a new flow: what the owner does with it, in a line")
		}
		in.Goal = cur.Goal
	}
	if in.Description == "" {
		in.Description = cur.Description
	}

	// The setup: what was given, else what the flow had.
	setup := map[string]any{}
	if had {
		for k, v := range prev.Setup {
			setup[k] = v
		}
	}
	if in.Mode != "" {
		setup["mode"] = in.Mode
	}
	if in.Instructions != "" {
		setup["instructions"] = in.Instructions
	}
	if in.Reflexes != nil {
		setup["reflexes"] = in.Reflexes
	}
	version := cur.Version + 1
	setup["flow"] = map[string]any{"name": in.Name, "goal": in.Goal, "version": version}

	if in.Arrange != nil {
		if len(in.Arrange.Windows) == 0 {
			return Flow{}, errors.New("arrange needs at least one window")
		}
		mode := in.Arrange.Mode
		if mode == "" {
			mode = "tile"
		}
		if err := m.h.Desk().Arrange(ctx, module.Layout{Name: in.Name, Mode: mode, Ratio: in.Arrange.Ratio, Windows: in.Arrange.Windows, Setup: setup}); err != nil {
			return Flow{}, fmt.Errorf("arrange: %w", err)
		}
	}
	l := m.h.Desk().Capture(ctx)
	l.Name, l.About, l.Setup = in.Name, in.Goal, setup
	// A saved flow never includes the Builder's own window.
	kept := l.Windows[:0]
	for _, w := range l.Windows {
		if w.View != "builder" {
			kept = append(kept, w)
		}
	}
	l.Windows = kept
	if len(l.Windows) == 0 {
		return Flow{}, errors.New("the desk has no windows to save; give arrange")
	}
	if err := m.h.Desk().SaveLayout(ctx, l); err != nil {
		return Flow{}, err
	}
	raw, _ := json.Marshal(l)
	now := stamp()
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return Flow{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO flows (name, goal, description, version, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET goal = excluded.goal, description = excluded.description, version = excluded.version, updated_at = excluded.updated_at`,
		in.Name, in.Goal, in.Description, version, now, now); err != nil {
		return Flow{}, err
	}
	note := in.Note
	if note == "" && version == 1 {
		note = "first version"
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO flow_versions (name, version, layout, note, created_at) VALUES (?, ?, ?, ?, ?)`,
		in.Name, version, string(raw), note, now); err != nil {
		return Flow{}, err
	}
	if err := tx.Commit(); err != nil {
		return Flow{}, err
	}
	// Make it the flow in use, with its setup (mode, reflexes, instructions).
	if _, err := m.h.RunCommand(ctx, "layout.load", map[string]any{"name": in.Name}); err != nil {
		m.h.Log().Warn("builder: the saved flow was not loaded", "flow", in.Name, "err", err)
	}
	m.h.Events().Publish("builder.changed", map[string]any{"flow": in.Name, "version": version})
	return m.get(ctx, in.Name)
}

func (m *mod) openFlow(ctx context.Context, name string) (any, error) {
	if _, err := m.get(ctx, name); err != nil {
		return nil, err
	}
	return m.h.RunCommand(ctx, "layout.load", map[string]any{"name": name})
}

// restore makes an older version current again, as a new version.
func (m *mod) restore(ctx context.Context, name string, v int) (Flow, error) {
	var raw string
	if err := m.db.QueryRowContext(ctx, `SELECT layout FROM flow_versions WHERE name = ? AND version = ?`, name, v).Scan(&raw); err != nil {
		return Flow{}, fmt.Errorf("flow %q has no version %d", name, v)
	}
	var l module.Layout
	if err := json.Unmarshal([]byte(raw), &l); err != nil {
		return Flow{}, err
	}
	in := SaveInput{Name: name, Note: fmt.Sprintf("restored version %d", v)}
	in.Mode, _ = l.Setup["mode"].(string)
	in.Instructions, _ = l.Setup["instructions"].(string)
	in.Reflexes = []string{}
	if list, ok := l.Setup["reflexes"].([]any); ok {
		for _, x := range list {
			if s, ok := x.(string); ok {
				in.Reflexes = append(in.Reflexes, s)
			}
		}
	}
	in.Arrange = &struct {
		Mode    string                `json:"mode"`
		Ratio   float64               `json:"ratio"`
		Windows []module.LayoutWindow `json:"windows"`
	}{Mode: l.Mode, Ratio: l.Ratio, Windows: l.Windows}
	return m.save(ctx, in)
}

func (m *mod) delete(ctx context.Context, name string) error {
	if _, err := m.db.ExecContext(ctx, `DELETE FROM flow_versions WHERE name = ?`, name); err != nil {
		return err
	}
	if _, err := m.db.ExecContext(ctx, `DELETE FROM flows WHERE name = ?`, name); err != nil {
		return err
	}
	err := m.h.Desk().DeleteLayout(ctx, name)
	m.h.Events().Publish("builder.changed", map[string]any{"flow": name, "removed": true})
	return err
}
