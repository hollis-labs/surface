package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hollis-labs/surface/module"
)

// catalog is the app's catalog as the core gives it (command surface.catalog). It is
// decoded here from JSON so the module depends on the contract alone.
type catalog struct {
	App     string `json:"app"`
	Title   string `json:"title"`
	Modules []struct {
		ID       string        `json:"id"`
		Title    string        `json:"title"`
		About    string        `json:"about"`
		Views    []item        `json:"views"`
		Commands []item        `json:"commands"`
		Tools    []item        `json:"tools"`
		Slots    []module.Slot `json:"slots"`
	} `json:"modules"`
}

type item struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	About string `json:"about"`
	Level string `json:"level"`
}

func (m *mod) catalog(ctx context.Context) (catalog, error) {
	var c catalog
	v, err := m.h.RunCommand(ctx, "surface.catalog", nil)
	if err != nil {
		return c, err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(raw, &c)
}

// catalogText is the catalog in a few lines per module, for the agent.
func (c catalog) text(withTools bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)\n", c.Title, c.App)
	for _, mod := range c.Modules {
		if mod.ID == "builder" {
			continue
		}
		fmt.Fprintf(&b, "\n%s [%s]: %s\n", mod.Title, mod.ID, mod.About)
		if len(mod.Views) > 0 {
			var vs []string
			for _, v := range mod.Views {
				vs = append(vs, fmt.Sprintf("%s (%s)", v.ID, v.Title))
			}
			b.WriteString("  views: " + strings.Join(vs, ", ") + "\n")
		}
		for _, s := range mod.Slots {
			fmt.Fprintf(&b, "  slot %s: %s\n", s.Name, s.About)
		}
		if len(mod.Commands) > 0 {
			var cs []string
			for _, cmd := range mod.Commands {
				cs = append(cs, cmd.ID)
			}
			b.WriteString("  commands: " + strings.Join(cs, ", ") + "\n")
		}
		if withTools && len(mod.Tools) > 0 {
			var ts []string
			for _, t := range mod.Tools {
				ts = append(ts, t.ID+"("+t.Level+")")
			}
			b.WriteString("  tools: " + strings.Join(ts, ", ") + "\n")
		}
	}
	return b.String()
}

func (m *mod) knowledge(ctx context.Context, _ module.Host, _ module.Situation) (string, error) {
	var b strings.Builder
	flows, err := m.list(ctx)
	if err != nil {
		return "", err
	}
	if len(flows) == 0 {
		b.WriteString("No flows saved yet.\n")
	} else {
		b.WriteString("Saved flows:\n")
		for _, f := range flows {
			fmt.Fprintf(&b, "- %s (v%d): %s\n", f.Name, f.Version, f.Goal)
		}
	}
	if c, err := m.catalog(ctx); err == nil {
		b.WriteString("\nWhat the app has to arrange:\n" + c.text(false))
	}
	return b.String(), nil
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }

const windowSchema = `{"type":"object","required":["view"],"properties":{"view":{"type":"string"},"key":{"type":"string"},"title":{"type":"string"},"args":{"type":"object"},"floating":{"type":"boolean"},"focus":{"type":"boolean"},"rect":{"type":"object","properties":{"x":{"type":"integer"},"y":{"type":"integer"},"w":{"type":"integer"},"h":{"type":"integer"}}}}}`

func (m *mod) tools() []module.Tool {
	return []module.Tool{
		{Name: "builder_catalog", Level: module.LevelRead,
			Description: "What the app is made of, to arrange a flow from: every module's views (by id), context slots, commands and tools (with their levels).",
			Input:       raw(`{"type":"object","properties":{}}`),
			Run: func(ctx context.Context, _ module.Host, _ json.RawMessage) (any, error) {
				c, err := m.catalog(ctx)
				if err != nil {
					return nil, err
				}
				return c.text(true), nil
			}},
		{Name: "builder_flow_list", Level: module.LevelRead,
			Description: "List the saved flows: name, goal, version.",
			Input:       raw(`{"type":"object","properties":{}}`),
			Run:         func(ctx context.Context, _ module.Host, _ json.RawMessage) (any, error) { return m.list(ctx) }},
		{Name: "builder_flow_get", Level: module.LevelRead,
			Description: "Read a flow: its goal, its layout (windows and setup: mode, instructions, reflexes) and its versions.",
			Input:       raw(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct{ Name string }
				_ = json.Unmarshal(in, &a)
				return m.get(ctx, strings.ToLower(strings.TrimSpace(a.Name)))
			}},
		{Name: "builder_flow_save", Level: module.LevelAct,
			Description: "Save a flow, or a new version of one: arrange the desk (arrange: mode tile|float, ratio, windows as desk_arrange takes them; leave out to save the desk as it is), set what goes with it (mode, instructions, reflexes by name; left out, the previous version's are kept), save it as a layout, keep the version, and make it the flow in use. note says what this version changed.",
			Input: raw(`{"type":"object","required":["name"],"properties":{"name":{"type":"string","description":"Short, lower case: \"inbox triage\"."},"goal":{"type":"string","description":"What the owner does with it, in a line. Required for a new flow."},"description":{"type":"string"},"note":{"type":"string"},` +
				`"arrange":{"type":"object","required":["windows"],"properties":{"mode":{"type":"string","enum":["tile","float"]},"ratio":{"type":"number"},"windows":{"type":"array","items":` + windowSchema + `}}},` +
				`"mode":{"type":"string","description":"The owner's mode in this flow, one word."},"instructions":{"type":"string","description":"Standing guidance for the agent while the flow is in use."},"reflexes":{"type":"array","items":{"type":"string"},"description":"Names of reflexes (agent_reflex_put) on while the flow is in use."}}}`),
			Describe: func(in json.RawMessage) string {
				var a struct{ Name, Note string }
				_ = json.Unmarshal(in, &a)
				if a.Note != "" {
					return fmt.Sprintf("Save flow “%s”: %s", a.Name, a.Note)
				}
				return fmt.Sprintf("Save flow “%s”", a.Name)
			},
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a SaveInput
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				f, err := m.save(ctx, a)
				if err != nil {
					return nil, err
				}
				return map[string]any{"flow": f.Name, "version": f.Version, "windows": len(f.Layout.Windows), "setup": f.Layout.Setup}, nil
			}},
		{Name: "builder_flow_open", Level: module.LevelAct,
			Description: "Put a saved flow on the desk: its windows, mode, instructions and reflexes.",
			Input:       raw(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct{ Name string }
				_ = json.Unmarshal(in, &a)
				return m.openFlow(ctx, strings.ToLower(strings.TrimSpace(a.Name)))
			}},
		{Name: "builder_flow_restore", Level: module.LevelAct,
			Description: "Make an older version of a flow current again (as a new version).",
			Input:       raw(`{"type":"object","required":["name","version"],"properties":{"name":{"type":"string"},"version":{"type":"integer"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					Name    string
					Version int
				}
				_ = json.Unmarshal(in, &a)
				f, err := m.restore(ctx, a.Name, a.Version)
				if err != nil {
					return nil, err
				}
				return map[string]any{"flow": f.Name, "version": f.Version}, nil
			}},
		{Name: "builder_flow_delete", Level: module.LevelConfirm,
			Description: "Delete a flow, its versions and its layout. Its reflexes stay (agent_reflex_delete removes them).",
			Input:       raw(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct{ Name string }
				_ = json.Unmarshal(in, &a)
				return "deleted", m.delete(ctx, a.Name)
			}},
	}
}
