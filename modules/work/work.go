package work

import (
	"context"
	"embed"
	"encoding/json"
	"sort"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/tetherlink"
)

func init() { module.Register(&mod{}) }

//go:embed migrations/*.sql
var migrations embed.FS

type mod struct {
	module.Base
	h  module.Host
	st *Store
	// caller replaces the configured link (tests).
	caller tetherlink.Caller
}

const viewID = "work"

// allowed are the only Tether tools the Work module calls: Caster for the items, Torque for
// Send to Torque and a round's comment, Tether mail for rounds and sign-offs.
var allowed = tetherlink.Allowed{
	"caster_ls": true, "caster_get": true, "caster_put": true, "caster_log": true, "caster_snap": true,
	"torque_task_create": true, "torque_task_get": true, "torque_comment_add": true,
	"tether_message_send": true,
}

func (m *mod) Manifest() module.Manifest {
	return module.Manifest{
		ID: "work", Title: "Work", Version: "0.1.0",
		About: "The Work engine under Planner, Content and Writer: workspaces of typed items, links and saved views, kept in Caster (work/v1).",
		Views: []module.View{
			appView(viewID, "planner", "Planner", "target"),
			appView(viewID+".content", "content", "Content", "megaphone"),
			appView(viewID+".writer", "writer", "Writer", "pen-line"),
		},
		Commands: []module.Command{
			openCommand("work.open", "planner", "Planner: open", "planner plans goals milestones features tasks strategy"),
			openCommand("work.content", "content", "Content: open", "content articles videos posts publishing calendar"),
			openCommand("work.writer", "writer", "Writer: open", "writer books chapters scenes characters codex manuscript ttrpg"),
		},
		Settings: []module.Setting{
			{Key: "tether_command", Title: "Tether command", About: "A command that speaks MCP over stdio to Tether (the local bridge): Caster, Torque and mail are reached through it.", Type: module.SettingString, Default: ""},
			{Key: "tether_url", Title: "Tether MCP URL", About: "Instead of a command: Tether's MCP endpoint over HTTP.", Type: module.SettingString, Default: ""},
			{Key: "tether_token_file", Title: "Tether token file", About: "With the URL: a file holding the bearer token.", Type: module.SettingString, Default: ""},
		},
		Store: &module.Store{Migrations: migrations, Dir: "migrations"},
		Tools: m.tools(),
		Slots: []module.Slot{{
			Name:  "work",
			About: "plans and what is in flight, and the owner's own writing: goals, plans, tasks (Planner), content on its way to being published (Content), books and settings (Writer)",
			Views: []string{viewID, viewID + ".content", viewID + ".writer"},
			Tools: []string{"work_list", "work_get", "work_note", "work_space_create", "work_space_update", "work_space_delete",
				"work_item_create", "work_item_update", "work_item_place", "work_item_stage", "work_item_delete", "work_item_restore",
				"work_note_update", "work_note_delete", "work_history", "work_view_save"},
			Where:        "the Planner, Content and Writer workspaces (work_list, work_get); an item's revisions in Caster (work_history)",
			Instructions: "Read before you write: work_list for a workspace's kinds, stages and items, work_get for an item's text and its rev (base_rev for work_item_update). Draft notes are the owner's to send: never send a round, sign off or publish.",
			Knowledge: func(ctx context.Context, _ module.Host, _ module.Situation) (string, error) {
				return m.overview(ctx), nil
			},
		}},
	}
}

func (m *mod) Start(_ context.Context, h module.Host) error {
	m.h = h
	m.st = NewStore(h.Store(), GatewayCaster{Link: link{m}})
	m.routes(h)
	return nil
}

var errNotConfigured = ErrNoCaster

// tether is the configured link to Tether, read from the settings at each call.
func (m *mod) tether() (tetherlink.Caller, error) {
	if m.caller != nil {
		return m.caller, nil
	}
	cfg := m.h.Config()
	c := tetherlink.New("work", tetherlink.Config{URL: cfg.String("tether_url"), TokenFile: cfg.String("tether_token_file"), Command: cfg.String("tether_command")}, allowed)
	if c == nil {
		return nil, errNotConfigured
	}
	return c, nil
}

// link is the engine's Caller (caster.go) over module/tetherlink: the settings are read on
// every call, so a change takes effect at once.
type link struct{ m *mod }

func (l link) Call(ctx context.Context, tool string, args map[string]any) ([]byte, error) {
	c, err := l.m.tether()
	if err != nil {
		return nil, err
	}
	out, err := c.Call(ctx, tool, args)
	return []byte(out), err
}

// CallAll reads the whole of a long listing the gateway cut short.
func (l link) CallAll(ctx context.Context, tool string, args map[string]any) ([]byte, error) {
	c, err := l.m.tether()
	if err != nil {
		return nil, err
	}
	out, err := tetherlink.CallAll(ctx, c, tool, args)
	return []byte(out), err
}

func (m *mod) notifier() GatewayNotifier { return GatewayNotifier{Link: link{m}} }

// changed tells the views (and anything listening) that a workspace changed.
func (m *mod) changed(ws, id, what string) {
	if m.h == nil {
		return
	}
	data := map[string]any{"workspace": ws, "what": what}
	if id != "" {
		data["id"] = id
	}
	m.h.Events().Publish("work.changed", data)
}

// sync brings the copy of a workspace up to date with Caster, as Basalisk's open does: its
// namespace when the copy knows the workspace, else the index first. At most every 15 s per
// namespace unless forced. The error is a problem to report, not a failure: the copy is
// still read.
func (m *mod) sync(ctx context.Context, ws string, force bool) error {
	if ns := m.st.NSOf(ctx, ws); ns != "" {
		_, err := m.st.Refresh(ctx, ns, force)
		return err
	}
	if _, err := m.st.Refresh(ctx, IndexNS, true); err != nil {
		return err
	}
	if ns := m.st.NSOf(ctx, ws); ns != "" {
		_, err := m.st.Refresh(ctx, ns, true)
		return err
	}
	return nil
}

// overview is what the agent is told about the owner's plans when Work is in front: each
// workspace's items by stage and the unfinished ones last touched, from the copy only.
func (m *mod) overview(ctx context.Context) string {
	if m.st == nil {
		return ""
	}
	spaces, err := m.st.Workspaces(ctx, "")
	if err != nil || len(spaces) == 0 {
		return ""
	}
	finished := map[string]bool{"published": true, "done": true, "final": true, "dropped": true, "decided": true, "closed": true}
	out := []map[string]any{}
	for i, w := range spaces {
		if i == 12 {
			break
		}
		space, ok, err := m.st.Open(ctx, w.Slug)
		if err != nil || !ok {
			continue
		}
		byStage := map[string]int{}
		var open []Card
		for _, it := range space.Items {
			if it.Cut || it.Stage == "" {
				continue
			}
			byStage[it.Stage]++
			if !finished[it.Stage] {
				open = append(open, it)
			}
		}
		sort.Slice(open, func(a, b int) bool { return open[a].Updated > open[b].Updated })
		if len(open) > 8 {
			open = open[:8]
		}
		items := []map[string]any{}
		for _, it := range open {
			r := map[string]any{"id": it.ID, "kind": it.Kind, "title": it.Title, "stage": it.Stage, "updated": it.Updated}
			due := it.Fields["due"]
			if due == nil {
				due = it.Fields["publish_on"]
			}
			if due != nil && due != "" {
				r["date"] = due
			}
			items = append(items, r)
		}
		out = append(out, map[string]any{"workspace": w.Slug, "title": w.Title, "app": w.App, "updated": w.Updated, "by_stage": byStage, "unfinished_last_touched": items})
	}
	b, _ := json.Marshal(out)
	return "The owner's Work workspaces (from the working copy):\n" + string(b)
}
