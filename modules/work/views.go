package work

import (
	"context"
	"fmt"
	"strings"

	"github.com/hollis-labs/surface/module"
)

// The Work engine's three windows: one component ("work/main") opened on a preset's app.
// A window's args say what is in front: {workspace, view, item}; the page keeps them current
// with desk.setArgs, so Describe answers from them with no page open.

// viewApps names each view's app (its preset), by view id.
var viewApps = map[string]string{viewID: "planner", viewID + ".content": "content", viewID + ".writer": "writer"}

func appView(id, app, title, icon string) module.View {
	return module.View{
		ID: id, Title: title, Icon: icon, Width: 1180, Height: 720, Menu: true, Bare: true, Multi: true, DefaultKey: app,
		Render: module.Component("work/main"),
		Describe: func(_ context.Context, _ module.Host, w module.Window) (module.WindowContext, error) {
			return describeWindow(app, w.Args), nil
		},
	}
}

// describeWindow is what a Work window shows: the app's list, a workspace, or an item in it.
func describeWindow(app string, args map[string]any) module.WindowContext {
	wc := module.WindowContext{Kind: "workspace", Ref: "work", Extra: map[string]any{"app": app}}
	ws, _ := args["workspace"].(string)
	if !ValidSlug(ws) {
		return wc
	}
	wc.Ref = "work:" + ws
	wc.Extra["workspace"] = ws
	if v, _ := args["view"].(string); v != "" {
		wc.Extra["view"] = v
	}
	if id, _ := args["item"].(string); ValidID(id) {
		wc.Kind = "item"
		wc.Ref = "work:" + ws + "/" + id
		wc.Extra["item"] = id
	}
	return wc
}

// openCommand opens an app's window: on its list, or on a workspace (args.workspace) and an
// item in it (args.item). A workspace gets a window of its own, keyed by its slug.
func openCommand(id, app, title, keywords string) module.Command {
	view := viewID
	for v, a := range viewApps {
		if a == app {
			view = v
		}
	}
	return module.Command{
		ID: id, Title: title, Section: "Work", Keywords: keywords,
		Run: func(ctx context.Context, h module.Host, args map[string]any) (any, error) {
			opts := module.OpenOptions{Key: app}
			if ws, _ := args["workspace"].(string); ValidSlug(ws) {
				a := map[string]any{"workspace": ws}
				if it, _ := args["item"].(string); ValidID(it) {
					a["item"] = it
				}
				opts = module.OpenOptions{Key: ws, Args: a}
			}
			return h.Desk().Open(ctx, view, opts)
		},
	}
}

// openRefCommand opens a caster://<ns>/<name> reference in the app whose workspace holds it:
// "caster://work/<slug>" is the workspace, "caster://<its ns>/i.<id>" an item of it. Cite's
// card runs it for a Caster reference (modules/tether/cite.go); a ref no workspace holds is
// an error, and the page then shows the record instead.
func (m *mod) openRefCommand() module.Command {
	return module.Command{
		ID: "work.open-ref", Title: "Work: open a Caster reference", Section: "Work", Hidden: true,
		Run: func(ctx context.Context, h module.Host, args map[string]any) (any, error) {
			ref, _ := args["ref"].(string)
			ws, item, ok := m.placeOf(ctx, ref)
			if !ok {
				return nil, fmt.Errorf("%s is not in a Work workspace", ref)
			}
			w, _ := m.st.workspace(ctx, ws)
			cmd := appCommands[w.App]
			if cmd == "" {
				cmd = "work.open"
			}
			a := map[string]any{"workspace": ws}
			if item != "" {
				a["item"] = item
			}
			return h.RunCommand(ctx, cmd, a)
		},
	}
}

// placeOf finds the workspace (and item) a Caster ref is in, from the working copy.
func (m *mod) placeOf(ctx context.Context, ref string) (ws, item string, ok bool) {
	rest, found := strings.CutPrefix(ref, "caster://")
	if !found {
		return "", "", false
	}
	rest, _, _ = strings.Cut(rest, "@")
	ns, name, found := strings.Cut(rest, "/")
	if !found {
		return "", "", false
	}
	if ns == IndexNS {
		_, ok = m.st.workspace(ctx, name)
		return name, "", ok
	}
	list, err := m.st.Workspaces(ctx, "")
	if err != nil {
		return "", "", false
	}
	for _, w := range list {
		if w.NS != ns {
			continue
		}
		if id, isItem := strings.CutPrefix(name, "i."); isItem && ValidID(id) {
			return w.Slug, id, true
		}
		return w.Slug, "", true
	}
	return "", "", false
}
