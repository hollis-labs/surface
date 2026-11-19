package work

import (
	"context"

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
