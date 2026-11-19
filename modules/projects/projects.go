// Package projects is the owner's projects for the agent. A slot is a kind of work; a project
// is what the work is on. A project has words that select it (found in the title, ref, address
// or details of the window in front), where its state lives (a Torque project, a Tesseract
// namespace, a repository, anything else), the owner's standing instructions for it, and the
// agent's own note. The project holds while the owner moves between kinds of work, until a
// window selects another or it is changed by hand; its lines ride with every message.
//
// Ported from Basalisk (internal/agent/projects.go).
package projects

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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

// Project is one of the owner's.
type Project struct {
	Name  string   `json:"name"`
	About string   `json:"about,omitempty"`
	Match []string `json:"match,omitempty"`
	// Where its state lives.
	Torque    string `json:"torque,omitempty"`
	Tesseract string `json:"tesseract,omitempty"`
	Repo      string `json:"repo,omitempty"`
	Where     string `json:"where,omitempty"`
	// Instructions are the owner's standing instructions for work on it.
	Instructions string `json:"instructions,omitempty"`
	// Note is the agent's own, and when it was written.
	Note   string `json:"note,omitempty"`
	NoteAt string `json:"note_written,omitempty"`
}

const maxNote = 4000

func (m *mod) Manifest() module.Manifest {
	return module.Manifest{
		ID: "projects", Title: "Projects", Version: "0.1.0",
		About: "The owner's projects: what selects each, where its state lives, standing instructions, and the agent's note.",
		Store: &module.Store{Migrations: migrations, Dir: "migrations"},
		Tools: m.tools(),
		Around: func(ctx context.Context, _ module.Host) (string, error) {
			p, why, ok := m.current(ctx)
			if !ok {
				return "", nil
			}
			return lines(p, why), nil
		},
	}
}

func (m *mod) Start(_ context.Context, h module.Host) error {
	m.h, m.db = h, h.Store()
	return nil
}

func stamp() string { return time.Now().UTC().Format(time.RFC3339) }

func (m *mod) list(ctx context.Context) ([]Project, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT def, note, note_at FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Project{}
	for rows.Next() {
		var def, note, at string
		if err := rows.Scan(&def, &note, &at); err != nil {
			return nil, err
		}
		var p Project
		if json.Unmarshal([]byte(def), &p) == nil {
			p.Note, p.NoteAt = note, at
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

func (m *mod) get(ctx context.Context, name string) (Project, bool) {
	list, _ := m.list(ctx)
	for _, p := range list {
		if strings.EqualFold(p.Name, name) {
			return p, true
		}
	}
	return Project{}, false
}

func (m *mod) save(ctx context.Context, p Project) error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || strings.ContainsAny(p.Name, "\n\r") || len(p.Name) > 80 {
		return errors.New("a project needs a short name on one line")
	}
	var match []string
	for _, w := range p.Match {
		if w = strings.ToLower(strings.TrimSpace(w)); len(w) >= 3 {
			match = append(match, w)
		}
	}
	p.Match = match
	note, at := p.Note, p.NoteAt
	p.Note, p.NoteAt = "", ""
	def, _ := json.Marshal(p)
	if _, err := m.db.ExecContext(ctx, `INSERT INTO projects(name, def, updated_at) VALUES(?,?,?)
		ON CONFLICT(name) DO UPDATE SET def=excluded.def, updated_at=excluded.updated_at`, p.Name, string(def), stamp()); err != nil {
		return err
	}
	if note != "" {
		if at == "" {
			at = stamp()
		}
		_, err := m.db.ExecContext(ctx, `UPDATE projects SET note=?, note_at=? WHERE name=?`, clip(note, maxNote), at, p.Name)
		return err
	}
	return nil
}

// windowText is what of the window in front a project's words are looked for in.
func windowText(wc module.WindowContext) string {
	parts := []string{wc.Title, wc.View, wc.Ref, wc.URL}
	if len(wc.Extra) > 0 {
		raw, _ := json.Marshal(wc.Extra)
		parts = append(parts, string(raw))
	}
	return strings.ToLower(strings.Join(parts, " "))
}

// forWindow is the project the window selects: the one with the longest matching word, so
// "apps/basalisk-wt" beats "basalisk".
func forWindow(list []Project, wc module.WindowContext) (Project, string, bool) {
	text := windowText(wc)
	var best Project
	bestLen, word := 0, ""
	for _, p := range list {
		for _, w := range p.Match {
			if len(w) > bestLen && strings.Contains(text, w) {
				best, bestLen, word = p, len(w), w
			}
		}
	}
	return best, word, bestLen > 0
}

// current is the project in play: the window in front selects one when it can; otherwise the
// last one chosen holds.
func (m *mod) current(ctx context.Context) (Project, string, bool) {
	list, err := m.list(ctx)
	if err != nil || len(list) == 0 {
		return Project{}, "", false
	}
	if w, ok := m.h.Desk().Focused(ctx); ok {
		if wc, ok := m.h.Desk().Context(ctx, w.ID); ok {
			if p, word, ok := forWindow(list, wc); ok {
				why := fmt.Sprintf("the window in front (%q matches %q)", wc.Title, word)
				m.setCurrent(ctx, p.Name, why)
				return p, why, true
			}
		}
	}
	var name, why string
	if m.db.QueryRowContext(ctx, `SELECT name, why FROM current WHERE one=1`).Scan(&name, &why) != nil {
		return Project{}, "", false
	}
	for _, p := range list {
		if p.Name == name {
			return p, why + ", still", true
		}
	}
	return Project{}, "", false
}

func (m *mod) setCurrent(ctx context.Context, name, why string) {
	_, _ = m.db.ExecContext(ctx, `INSERT INTO current(one, name, why, at) VALUES(1,?,?,?)
		ON CONFLICT(one) DO UPDATE SET name=excluded.name, why=excluded.why, at=excluded.at`, name, why, stamp())
}

func (m *mod) clearCurrent(ctx context.Context) {
	_, _ = m.db.ExecContext(ctx, `DELETE FROM current`)
}

// lines are the project as the agent carries it with every message.
func lines(p Project, why string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Project: %s", p.Name)
	if p.About != "" {
		b.WriteString(" (" + p.About + ")")
	}
	b.WriteString(", chosen by " + why + "\n")
	var where []string
	for _, kv := range [][2]string{{"Torque", p.Torque}, {"Tesseract", p.Tesseract}, {"repo", p.Repo}} {
		if kv[1] != "" {
			where = append(where, kv[0]+" "+kv[1])
		}
	}
	if p.Where != "" {
		where = append(where, p.Where)
	}
	if len(where) > 0 {
		b.WriteString("Where: " + strings.Join(where, "; ") + "\n")
	}
	if p.Instructions != "" {
		b.WriteString("The owner's instructions for it: " + p.Instructions + "\n")
	}
	if p.Note != "" {
		fmt.Fprintf(&b, "Your note on it (%s): %s\n", p.NoteAt, clip(p.Note, 800))
	} else {
		b.WriteString("Your note on it: none yet (project_note keeps one)\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func (m *mod) tools() []module.Tool {
	return []module.Tool{
		{Name: "project_list", Level: module.LevelRead,
			Description: "The owner's projects: what selects each (words in the window in front), where its state lives, the owner's instructions, your note; and which is current.",
			Input:       raw(`{"type":"object","properties":{}}`),
			Run: func(ctx context.Context, _ module.Host, _ json.RawMessage) (any, error) {
				list, err := m.list(ctx)
				p, why, ok := m.current(ctx)
				out := map[string]any{"projects": list}
				if ok {
					out["current"], out["why"] = p.Name, why
				}
				return out, err
			}},
		{Name: "project_define", Level: module.LevelWrite,
			Describe: func(in json.RawMessage) string {
				var p Project
				_ = json.Unmarshal(in, &p)
				return "Define the project " + p.Name
			},
			Description: "Add or change one of the owner's projects: name, about, match (words that select it when found in the window in front: a path, a repo, a title), torque (a Torque project id), tesseract (a namespace), repo, where (anything else worth knowing), instructions (the owner's standing instructions for it). Replaces the definition whole; your note is kept.",
			Input: raw(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"},"about":{"type":"string"},"match":{"type":"array","items":{"type":"string"}},` +
				`"torque":{"type":"string"},"tesseract":{"type":"string"},"repo":{"type":"string"},"where":{"type":"string"},"instructions":{"type":"string"}}}`),
			Run: func(ctx context.Context, h module.Host, in json.RawMessage) (any, error) {
				var p Project
				if err := json.Unmarshal(in, &p); err != nil {
					return nil, err
				}
				p.Note, p.NoteAt = "", ""
				if err := m.save(ctx, p); err != nil {
					return nil, err
				}
				h.Events().Publish("projects.changed", map[string]string{"name": p.Name})
				return "saved", nil
			}},
		{Name: "project_remove", Level: module.LevelWrite,
			Description: "Remove one of the owner's projects by name, with your note on it.",
			Input:       raw(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`),
			Run: func(ctx context.Context, h module.Host, in json.RawMessage) (any, error) {
				var a struct{ Name string }
				_ = json.Unmarshal(in, &a)
				if _, err := m.db.ExecContext(ctx, `DELETE FROM projects WHERE name=?`, a.Name); err != nil {
					return nil, err
				}
				h.Events().Publish("projects.changed", map[string]string{"name": a.Name})
				return "removed", nil
			}},
		{Name: "project_note", Level: module.LevelAct,
			Description: "Keep your note on a project (the current one when name is left out): where things stand, what is next, pointers such as task ids and document names. The whole note, under a page; it replaces the last.",
			Input:       raw(`{"type":"object","required":["note"],"properties":{"name":{"type":"string"},"note":{"type":"string"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct{ Name, Note string }
				_ = json.Unmarshal(in, &a)
				if a.Name == "" {
					p, _, ok := m.current(ctx)
					if !ok {
						return nil, errors.New("no project is current: name one")
					}
					a.Name = p.Name
				}
				p, ok := m.get(ctx, a.Name)
				if !ok {
					return nil, fmt.Errorf("no project %q (project_list)", a.Name)
				}
				if _, err := m.db.ExecContext(ctx, `UPDATE projects SET note=?, note_at=? WHERE name=?`, clip(a.Note, maxNote), stamp(), p.Name); err != nil {
					return nil, err
				}
				return "kept", nil
			}},
		{Name: "project_switch", Level: module.LevelAct,
			Description: "Make a project current by hand (\"\" for none), when the work is on it but no window says so. A window that selects another project moves it again.",
			Input:       raw(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct{ Name string }
				_ = json.Unmarshal(in, &a)
				if a.Name == "" {
					m.clearCurrent(ctx)
					return "no project is current", nil
				}
				p, ok := m.get(ctx, a.Name)
				if !ok {
					names := []string{}
					list, _ := m.list(ctx)
					for _, x := range list {
						names = append(names, x.Name)
					}
					sort.Strings(names)
					return nil, fmt.Errorf("no project %q; there are: %s", a.Name, strings.Join(names, ", "))
				}
				m.setCurrent(ctx, p.Name, "you, by hand")
				return "current: " + p.Name, nil
			}},
	}
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
