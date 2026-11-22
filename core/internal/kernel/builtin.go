package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hollis-labs/surface/core/internal/cards"
	"github.com/hollis-labs/surface/core/internal/desk"
	"github.com/hollis-labs/surface/module"
)

// coreSettings are the core's own live settings.
var coreSettings = []module.Setting{
	{Key: "theme", Title: "Theme", About: "How the desk is drawn.", Type: module.SettingEnum, Default: "dark", Options: []string{"dark", "light"}},
	{Key: "palette", Title: "Palette", About: "The colors the desk is drawn in: a palette id from @hollis-labs/surface-theme (blade-runner, aura, tokyo-night, ...). Chosen from the bar.", Type: module.SettingString, Default: "blade-runner"},
	{Key: "type", Title: "Type", About: "Mono chrome over a sans body (mixed), or mono everywhere (mono).", Type: module.SettingEnum, Default: "mixed", Options: []string{"mixed", "mono"}},
	{Key: "gap", Title: "Window gap", About: "Space between tiled windows, in pixels.", Type: module.SettingInt, Default: 10},
	{Key: "agent_panel_width", Title: "Agent panel width", About: "Width of the agent panel, in pixels.", Type: module.SettingInt, Default: 420},
	{Key: "confirm_close_dirty", Title: "Ask before closing unsaved work", Type: module.SettingBool, Default: true},
	{Key: "agent_selector", Title: "Who picks the agent's context", About: "focus: the window in front picks the slot (and a slot you pin holds). classifier: the classifier may overrule the window in front when it is sure (p ≥ 0.7) the message is about another slot; a pin still wins. Check its record first at /api/agent/classify/replay.", Type: module.SettingEnum, Default: "focus", Options: []string{"focus", "classifier"}},
	{Key: "agent_trace", Title: "Keep a context trace", About: "Record, in this app's database, what each message put in front of the agent and why, and what you did after it (a re-pin, a move to another window). It is what a classifier that picks the agent's context learns from. Read it at /api/agent/trace.", Type: module.SettingBool, Default: true},
	{Key: "developer_mode", Title: "Developer mode", About: "Show the Agent Inspector in the agent panel: context pressure, token usage, runs, tool calls, standing notes, memory, reflexes, skills and trace.", Type: module.SettingBool, Default: false},
	{Key: "card_display", Title: "Where the agent's cards open", About: "inline: in the agent panel above the composer. window: in the Cards window (holding questions so you can keep chatting until you submit or they expire).", Type: module.SettingEnum, Default: "inline", Options: []string{"inline", "window"}},
	{Key: "card_window_slots", Title: "Slots that open cards in the Cards window", About: "Comma-separated slot names (or * for all) where the agent's questions open in the Cards window instead of inline.", Type: module.SettingString, Default: ""},
	{Key: "card_page_size", Title: "Cards shown at a time in the Cards window", About: "How many open question cards to show per wizard step in the Cards window.", Type: module.SettingInt, Default: 3},
}

// coreViews are the core's own windows. Their bodies are in the page's core (web/src/desk).
var coreViews = []module.View{
	{ID: "surface.settings", Title: "Settings", Icon: "settings", Width: 640, Height: 620, Menu: true, Render: module.Component("surface/settings")},
	{ID: "surface.layouts", Title: "Layouts", Icon: "layout-dashboard", Width: 520, Height: 560, Menu: true, Render: module.Component("surface/layouts")},
	{ID: "surface.marks", Title: "Marks", Icon: "message-square-quote", Width: 480, Height: 600, Menu: true, Render: module.Component("surface/marks")},
	{ID: "surface.cards", Title: "Cards", Icon: "gallery-vertical-end", Width: 480, Height: 640, Menu: true, Render: module.Component("surface/cards")},
	{ID: "surface.activity", Title: "Activity", Icon: "activity", Width: 660, Height: 680, Menu: true, Render: module.Component("surface/activity")},
	{ID: "surface.workflows", Title: "Workflows", Icon: "sparkles", Width: 980, Height: 740, Menu: true, Render: module.Component("surface/workflows")},
	// Why the agent gets the context it gets: now, and turn by turn (the context trace).
	{ID: "surface.context", Title: "Context", Icon: "scan-eye", Width: 1000, Height: 720, Menu: true, Render: module.Component("surface/context")},
	// Every registry component in its variants and states, for design work and screenshots.
	// A cited record (a Torque task, a Tesseract item, a fragment) read whole, for a reference
	// whose module has no window of its own to show it in. Args: {kind, id}.
	{ID: "surface.cite", Title: "Record", Icon: "quote", Width: 720, Height: 680, Multi: true, Bare: true, Render: module.Component("surface/cite"),
		Describe: func(_ context.Context, _ module.Host, w module.Window) (module.WindowContext, error) {
			kind, _ := w.Args["kind"].(string)
			id, _ := w.Args["id"].(string)
			return module.WindowContext{Kind: "record", Ref: "cite:" + kind + ":" + id, Extra: map[string]any{"kind": kind, "id": id}}, nil
		}},
	{ID: "surface.gallery", Title: "Components", Icon: "swatch-book", Width: 960, Height: 760, Menu: true, Render: module.Component("surface/gallery")},
}

func schema(s string) json.RawMessage { return json.RawMessage(s) }

func decode[T any](raw json.RawMessage) (T, error) {
	var v T
	if len(raw) == 0 {
		return v, nil
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, fmt.Errorf("bad input: %w", err)
	}
	return v, nil
}

// focusedID returns the id an action should apply to: the one given, or the focused one.
func (a *App) focusedID(id string) (string, error) {
	if id != "" {
		return id, nil
	}
	if f := a.desk.Snapshot().Focus; f != "" {
		return f, nil
	}
	return "", errors.New("no window is in front")
}

// coreCommands are the desk's own commands. Commands that only make sense in the page (the
// palette, the panels) are declared by the page.
func coreCommands(a *App) []module.Command {
	onFocused := func(typ string) func(context.Context, module.Host, map[string]any) (any, error) {
		return func(ctx context.Context, _ module.Host, args map[string]any) (any, error) {
			given, _ := args["window"].(string)
			id, err := a.focusedID(given)
			if err != nil {
				return nil, err
			}
			_, err = a.desk.Apply(ctx, desk.Action{Type: typ, ID: id})
			return nil, err
		}
	}
	return []module.Command{
		{ID: "layout.toggle", Title: "Tile every window, or float them all", Section: "Layout", Keys: "ctrl+alt+t",
			Run: func(ctx context.Context, _ module.Host, _ map[string]any) (any, error) {
				_, err := a.desk.Apply(ctx, desk.Action{Type: "layout"})
				return nil, err
			}},
		{ID: "window.next", Title: "Next window", Section: "Windows", Keys: "ctrl+alt+j",
			Run: func(ctx context.Context, _ module.Host, _ map[string]any) (any, error) {
				_, err := a.desk.Apply(ctx, desk.Action{Type: "cycle", Delta: 1})
				return nil, err
			}},
		{ID: "window.previous", Title: "Previous window", Section: "Windows", Keys: "ctrl+alt+k",
			Run: func(ctx context.Context, _ module.Host, _ map[string]any) (any, error) {
				_, err := a.desk.Apply(ctx, desk.Action{Type: "cycle", Delta: -1})
				return nil, err
			}},
		{ID: "window.back", Title: "Back to the window before", Section: "Windows", Keys: "ctrl+alt+b",
			Run: func(ctx context.Context, _ module.Host, _ map[string]any) (any, error) {
				_, err := a.desk.Apply(ctx, desk.Action{Type: "back"})
				return nil, err
			}},
		{ID: "window.close", Title: "Close window", Section: "Windows", Keys: "ctrl+alt+w", Run: onFocused("close")},
		{ID: "window.maximize", Title: "Maximize or restore window", Section: "Windows", Keys: "ctrl+alt+return", Run: onFocused("maximize")},
		{ID: "window.float", Title: "Float or dock window", Section: "Windows", Keys: "ctrl+alt+f", Run: onFocused("float")},
		{ID: "window.master", Title: "Move window to the first place", Section: "Windows", Keys: "ctrl+alt+m", Run: onFocused("promote")},
		{ID: "layout.grow", Title: "Widen the first column", Section: "Layout", Keys: "ctrl+alt+right",
			Run: func(ctx context.Context, _ module.Host, _ map[string]any) (any, error) {
				_, err := a.desk.Apply(ctx, desk.Action{Type: "ratio", Ratio: a.desk.Snapshot().Ratio + 0.04})
				return nil, err
			}},
		{ID: "layout.shrink", Title: "Narrow the first column", Section: "Layout", Keys: "ctrl+alt+left",
			Run: func(ctx context.Context, _ module.Host, _ map[string]any) (any, error) {
				_, err := a.desk.Apply(ctx, desk.Action{Type: "ratio", Ratio: a.desk.Snapshot().Ratio - 0.04})
				return nil, err
			}},
		{ID: "surface.catalog", Title: "What this app is made of", Section: "System", Hidden: true,
			Run: func(context.Context, module.Host, map[string]any) (any, error) { return a.Catalog(), nil }},
		{ID: "layout.save", Title: "Save this arrangement as a layout", Section: "Layout", Hidden: true,
			Run: func(ctx context.Context, _ module.Host, args map[string]any) (any, error) {
				name, _ := args["name"].(string)
				l := a.desk.Capture(ctx)
				l.Name = strings.TrimSpace(name)
				if about, ok := args["about"].(string); ok {
					l.About = about
				}
				if prev, ok, _ := a.desk.Layout(ctx, l.Name); ok {
					// Saving over a layout keeps what goes with it (its mode, its reflexes).
					if l.About == "" {
						l.About = prev.About
					}
					for k, v := range prev.Setup {
						if _, set := l.Setup[k]; !set {
							if l.Setup == nil {
								l.Setup = map[string]any{}
							}
							l.Setup[k] = v
						}
					}
				}
				return l, a.desk.SaveLayout(ctx, l)
			}},
		{ID: "layout.load", Title: "Arrange the desk from a saved layout", Section: "Layout", Hidden: true,
			Run: func(ctx context.Context, _ module.Host, args map[string]any) (any, error) {
				name, _ := args["name"].(string)
				l, ok, err := a.desk.Layout(ctx, name)
				if err != nil {
					return nil, err
				}
				if !ok {
					return nil, fmt.Errorf("no saved layout %q", name)
				}
				return l, a.desk.Arrange(ctx, l)
			}},
	}
}

// coreTools are the desk's own tools: what the agent uses to see and arrange the desk, to
// mark things, and to read and change settings.
func coreTools(a *App) []module.Tool {
	type idIn struct {
		Window string `json:"window"`
	}
	return []module.Tool{
		{Name: "desk_windows", Level: module.LevelRead,
			Description: "List the open windows: id, view, title, what each was opened with, and which is in front. Also the layout mode, the user's mode and the saved layout in use.",
			Run: func(ctx context.Context, _ module.Host, _ json.RawMessage) (any, error) {
				st := a.desk.Snapshot()
				return map[string]any{"windows": a.desk.Windows(ctx), "layout": st.Layout, "mode": st.Mode, "active_layout": st.Active}, nil
			}},
		{Name: "desk_views", Level: module.LevelRead,
			Description: "List the views this app can open in a window (id, title, module), and its modules.",
			Run: func(context.Context, module.Host, json.RawMessage) (any, error) {
				type v struct {
					ID     string `json:"id"`
					Title  string `json:"title"`
					Module string `json:"module"`
					Multi  bool   `json:"multi,omitempty"`
				}
				var out []v
				for _, vi := range a.sortedViews() {
					out = append(out, v{ID: vi.ID, Title: vi.Title, Module: vi.Module, Multi: vi.Multi})
				}
				type m struct {
					ID    string `json:"id"`
					Title string `json:"title"`
					About string `json:"about,omitempty"`
				}
				var mods []m
				for _, mi := range a.Modules() {
					mods = append(mods, m{ID: mi.ID, Title: mi.Title, About: mi.About})
				}
				return map[string]any{"views": out, "modules": mods}, nil
			}},
		{Name: "cite_resolve", Level: module.LevelRead,
			Description: "Look up references named in text: what a Torque task id, a Tesseract item, a fragment, a path or a Planner item stands for, through the module that owns it. Give kind and ids for short previews, or kind and one id with full for the whole record. With no kind, lists the kinds this app resolves. Reading only.",
			Input:       schema(`{"type":"object","properties":{"kind":{"type":"string","description":"The kind, for example torque.task, tesseract.item, fragments.fragment, path, work.item."},"ids":{"type":"array","items":{"type":"string"},"description":"Up to 50 ids of that kind."},"full":{"type":"boolean","description":"With one id: the whole record, with its body."}}}`),
			Run: func(ctx context.Context, _ module.Host, raw json.RawMessage) (any, error) {
				in, err := decode[struct {
					Kind string   `json:"kind"`
					IDs  []string `json:"ids"`
					Full bool     `json:"full"`
				}](raw)
				if err != nil {
					return nil, err
				}
				if in.Kind == "" {
					return map[string]any{"kinds": a.RefKinds(ctx)}, nil
				}
				if in.Full {
					if len(in.IDs) != 1 {
						return nil, errors.New("full reads one record: give one id")
					}
					return a.RecordRef(ctx, in.Kind, in.IDs[0])
				}
				return a.ResolveRefs(ctx, in.Kind, in.IDs)
			}},
		{Name: "window_context", Level: module.LevelRead,
			Description: "What a window is showing: the document, page, file or record in front, and the selection. The focused window when no id is given.",
			Input:       schema(`{"type":"object","properties":{"window":{"type":"string","description":"Window id; omit for the one in front."}}}`),
			Run: func(ctx context.Context, _ module.Host, raw json.RawMessage) (any, error) {
				in, err := decode[idIn](raw)
				if err != nil {
					return nil, err
				}
				wc, ok := a.desk.Context(ctx, in.Window)
				if !ok {
					return "No such window, or no window is in front.", nil
				}
				return wc, nil
			}},
		{Name: "desk_open", Level: module.LevelAct,
			Description: "Open a view in a window, or bring its window forward. args are what the view opens with (a note path, a URL); key identifies one of several windows of the same view. Say what it is for in reason. retention is transient for a window that serves the task at hand, deliverable (the default) for a result the owner keeps. A window you open never takes the focus from the owner's unsaved work; when they close it, the focus goes back to the window they had.",
			Input:       schema(`{"type":"object","required":["view"],"properties":{"view":{"type":"string"},"args":{"type":"object"},"key":{"type":"string"},"title":{"type":"string"},"background":{"type":"boolean","description":"Open without taking the focus."},"reason":{"type":"string","description":"What the window is for, in a few words."},"retention":{"type":"string","enum":["transient","deliverable"]}}}`),
			Describe: func(raw json.RawMessage) string {
				in, _ := decode[struct{ View string }](raw)
				return "Open " + in.View
			},
			Run: func(ctx context.Context, _ module.Host, raw json.RawMessage) (any, error) {
				in, err := decode[struct {
					View       string         `json:"view"`
					Args       map[string]any `json:"args"`
					Key        string         `json:"key"`
					Title      string         `json:"title"`
					Background bool           `json:"background"`
					Reason     string         `json:"reason"`
					Retention  string         `json:"retention"`
				}](raw)
				if err != nil {
					return nil, err
				}
				return a.desk.Open(ctx, in.View, module.OpenOptions{Args: in.Args, Key: in.Key, Title: in.Title, Background: in.Background, Reason: in.Reason, Retention: in.Retention})
			}},
		{Name: "desk_focus", Level: module.LevelAct, Description: "Bring a window to the front.",
			Input: schema(`{"type":"object","required":["window"],"properties":{"window":{"type":"string"}}}`),
			Run: func(ctx context.Context, _ module.Host, raw json.RawMessage) (any, error) {
				in, err := decode[idIn](raw)
				if err != nil {
					return nil, err
				}
				return "done", a.desk.Focus(ctx, in.Window)
			}},
		{Name: "desk_close", Level: module.LevelAct, Description: "Close a window you opened. A window holding unsaved work is not closed, and neither is the owner's (one they opened or took over, kept: true in desk_windows): ask them instead.",
			Input: schema(`{"type":"object","required":["window"],"properties":{"window":{"type":"string"}}}`),
			Run: func(ctx context.Context, _ module.Host, raw json.RawMessage) (any, error) {
				in, err := decode[idIn](raw)
				if err != nil {
					return nil, err
				}
				return "done", a.desk.Close(ctx, in.Window)
			}},
		{Name: "desk_release", Level: module.LevelAct,
			Description: "Say you are done with the transient windows you opened: the one named, or all of them. Each closes unless the owner kept it or has unsaved work in it, and the focus goes back to the owner's window.",
			Input:       schema(`{"type":"object","properties":{"window":{"type":"string","description":"One window; omit for all your transient windows."}}}`),
			Run: func(ctx context.Context, _ module.Host, raw json.RawMessage) (any, error) {
				in, err := decode[idIn](raw)
				if err != nil {
					return nil, err
				}
				w, err := a.desk.Apply(ctx, desk.Action{Type: "release", ID: in.Window})
				if err != nil {
					return nil, err
				}
				if w.ID == "" {
					return "Nothing to release: no transient window of yours that the owner has not kept.", nil
				}
				return "done", nil
			}},
		{Name: "desk_window", Level: module.LevelAct,
			Description: "Change how one window sits: float (toggle in or out of the tiling), maximize (toggle; a tiled window covers the tiling), first (give it the first place in the tiling), tab (put it in the same stack as the window named in with, as a tab behind which that one sits), or rect (float it at a rectangle, or move it if it floats).",
			Input:       schema(`{"type":"object","required":["window","do"],"properties":{"window":{"type":"string"},"do":{"type":"string","enum":["float","maximize","first","tab","rect"]},"with":{"type":"string"},"rect":{"type":"object","properties":{"x":{"type":"integer"},"y":{"type":"integer"},"w":{"type":"integer"},"h":{"type":"integer"}}}}}`),
			Run: func(ctx context.Context, _ module.Host, raw json.RawMessage) (any, error) {
				in, err := decode[struct {
					Window string       `json:"window"`
					Do     string       `json:"do"`
					With   string       `json:"with"`
					Rect   *module.Rect `json:"rect"`
				}](raw)
				if err != nil {
					return nil, err
				}
				// "master" is the old word for "first"; a saved prompt or flow may still use it.
				typ := map[string]string{"float": "float", "maximize": "maximize", "first": "promote", "master": "promote", "tab": "tab", "rect": "rect"}[in.Do]
				if typ == "" {
					return nil, fmt.Errorf("do must be float, maximize, first, tab or rect")
				}
				_, err = a.desk.Apply(ctx, desk.Action{Type: typ, ID: in.Window, Other: in.With, Rect: in.Rect})
				return "done", err
			}},
		{Name: "desk_arrange", Level: module.LevelAct,
			Description: "Arrange the whole desk at once: replace the open windows with the ones given, tiled (the first takes the left, the rest stack on the right; ratio is the first one's share of the width) or floating (each with a rect). setup goes with the arrangement: mode (the user's mode, a word like \"writing\"), instructions (standing guidance for the agent while this arrangement is in use), reflexes (names of reflexes to switch on). Windows holding unsaved work are kept. Give it a name to make it the layout in use; save it with desk_layout_save.",
			Input:       schema(`{"type":"object","required":["windows"],"properties":{"name":{"type":"string"},"about":{"type":"string"},"mode":{"type":"string","enum":["tile","float"]},"ratio":{"type":"number"},"windows":{"type":"array","items":{"type":"object","required":["view"],"properties":{"view":{"type":"string"},"key":{"type":"string"},"title":{"type":"string"},"args":{"type":"object"},"floating":{"type":"boolean"},"focus":{"type":"boolean"},"rect":{"type":"object","properties":{"x":{"type":"integer"},"y":{"type":"integer"},"w":{"type":"integer"},"h":{"type":"integer"}}}}}},"setup":{"type":"object"}}}`),
			Describe:    func(json.RawMessage) string { return "Rearrange the desk" },
			Run: func(ctx context.Context, _ module.Host, raw json.RawMessage) (any, error) {
				l, err := decode[module.Layout](raw)
				if err != nil {
					return nil, err
				}
				if l.Mode == "" {
					l.Mode = "tile"
				}
				if err := a.desk.Arrange(ctx, l); err != nil {
					return nil, err
				}
				return map[string]any{"arranged": true, "windows": a.desk.Windows(ctx)}, nil
			}},
		{Name: "desk_layout_list", Level: module.LevelRead, Description: "List the saved layouts: name, what each is for, its windows and its setup.",
			Run: func(ctx context.Context, _ module.Host, _ json.RawMessage) (any, error) { return a.desk.Layouts(ctx) }},
		{Name: "desk_layout_save", Level: module.LevelAct,
			Description: "Save the desk as it is now under a name, so the user can come back to it. Saving over an existing name replaces its windows and keeps its setup unless setup is given. about says what the layout is for.",
			Input:       schema(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"},"about":{"type":"string"},"setup":{"type":"object","description":"mode, instructions, reflexes: what goes with the layout."}}}`),
			Describe: func(raw json.RawMessage) string {
				in, _ := decode[struct{ Name string }](raw)
				return "Save the layout “" + in.Name + "”"
			},
			Run: func(ctx context.Context, _ module.Host, raw json.RawMessage) (any, error) {
				in, err := decode[struct {
					Name  string         `json:"name"`
					About string         `json:"about"`
					Setup map[string]any `json:"setup"`
				}](raw)
				if err != nil {
					return nil, err
				}
				l := a.desk.Capture(ctx)
				l.Name = strings.TrimSpace(in.Name)
				l.About = in.About
				prev, had, _ := a.desk.Layout(ctx, l.Name)
				switch {
				case in.Setup != nil:
					l.Setup = in.Setup
				case had:
					l.Setup = prev.Setup
				}
				if l.About == "" && had {
					l.About = prev.About
				}
				if err := a.desk.SaveLayout(ctx, l); err != nil {
					return nil, err
				}
				_, _ = a.desk.Apply(ctx, desk.Action{Type: "active", Layout: l.Name})
				return l, nil
			}},
		{Name: "desk_layout_load", Level: module.LevelAct, Description: "Arrange the desk from a saved layout, with its setup.",
			Input: schema(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`),
			Run: func(ctx context.Context, _ module.Host, raw json.RawMessage) (any, error) {
				in, err := decode[struct{ Name string }](raw)
				if err != nil {
					return nil, err
				}
				return a.RunCommand(ctx, "layout.load", map[string]any{"name": in.Name})
			}},
		{Name: "desk_layout_delete", Level: module.LevelConfirm, Description: "Delete a saved layout.",
			Input: schema(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`),
			Describe: func(raw json.RawMessage) string {
				in, _ := decode[struct{ Name string }](raw)
				return "Delete the layout “" + in.Name + "”"
			},
			Run: func(ctx context.Context, _ module.Host, raw json.RawMessage) (any, error) {
				in, err := decode[struct{ Name string }](raw)
				if err != nil {
					return nil, err
				}
				return "deleted", a.desk.DeleteLayout(ctx, in.Name)
			}},
		{Name: "desk_set_mode", Level: module.LevelAct,
			Description: "Set the user's mode: one word for the kind of work they are in (\"writing\", \"triage\", \"research\"). It selects which context the agent brings. Empty clears it.",
			Input:       schema(`{"type":"object","properties":{"mode":{"type":"string"}}}`),
			Run: func(ctx context.Context, _ module.Host, raw json.RawMessage) (any, error) {
				in, err := decode[struct{ Mode string }](raw)
				if err != nil {
					return nil, err
				}
				_, err = a.desk.Apply(ctx, desk.Action{Type: "mode", Mode: strings.TrimSpace(in.Mode)})
				return "done", err
			}},
		{Name: "desk_commands", Level: module.LevelRead, Description: "List the commands the palette offers (id, title, section) that the server can run.",
			Run: func(context.Context, module.Host, json.RawMessage) (any, error) {
				type c struct {
					ID      string `json:"id"`
					Title   string `json:"title"`
					Section string `json:"section,omitempty"`
				}
				var out []c
				for _, ci := range a.CommandInfos() {
					if ci.Run != nil {
						out = append(out, c{ID: ci.ID, Title: ci.Title, Section: ci.Section})
					}
				}
				return out, nil
			}},
		{Name: "desk_run_command", Level: module.LevelWrite, Description: "Run a palette command by id, with its arguments.",
			Input: schema(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"},"args":{"type":"object"}}}`),
			Describe: func(raw json.RawMessage) string {
				in, _ := decode[struct{ ID string }](raw)
				return "Run the command " + in.ID
			},
			Run: func(ctx context.Context, _ module.Host, raw json.RawMessage) (any, error) {
				in, err := decode[struct {
					ID   string         `json:"id"`
					Args map[string]any `json:"args"`
				}](raw)
				if err != nil {
					return nil, err
				}
				out, err := a.RunCommand(ctx, in.ID, in.Args)
				if err != nil {
					return nil, err
				}
				if out == nil {
					return "done", nil
				}
				return out, nil
			}},
		{Name: "card_show", Level: module.LevelAct,
			Description: "Put a card in front of the user, outside the conversation's text: a notice, progress on long work, a plan, or a module entity card. It does not wait for an answer; use card_ask to ask something. kind and payload: info {body, tone?: info|success|warning}; error {body, detail?}; progress {label?, value?, max?, status?: running|done|failed, detail?}; plan {steps: [{id, title, status?: pending|active|done|skipped, detail?}]}; entity (or module-scoped kind such as ops.workstream, ops.agent, ops.host, ops.followup, registry.record, tracker.list, tracker.item, align.proposal, align.droppad) {ref?, refs?: [...], entity_kind?, view?: card|compact|detail, data?}. Give a subject (what it is about) to have a later card of the same kind replace this one, and keep the id to update it with card_update.",
			Input:       schema(`{"type":"object","required":["kind"],"properties":{"kind":{"type":"string","enum":["info","error","progress","plan","entity","ops.workstream","ops.agent","ops.host","ops.followup","registry.record","tracker.list","tracker.item","align.proposal","align.droppad","work.item"]},"title":{"type":"string"},"subject":{"type":"string"},"payload":{"type":"object"}}}`),
			Describe:    func(json.RawMessage) string { return "Show a card" },
			Run: func(ctx context.Context, _ module.Host, raw json.RawMessage) (any, error) {
				in, err := decode[module.Card](raw)
				if err != nil {
					return nil, err
				}
				if cards.Answerable(in.Kind) {
					return nil, fmt.Errorf("a %s card asks the user something: use card_ask", in.Kind)
				}
				if cards.IsModuleEntityKind(in.Kind) {
					in = a.enrichEntityCard(ctx, in)
				}
				in.Source = "agent"
				c, err := a.cards.Show(ctx, in)
				if err != nil {
					return nil, err
				}
				return map[string]any{"id": c.ID}, nil
			}},
		{Name: "card_ask", Level: module.LevelAct,
			Description: "Ask the user something with a card (or a batch of cards in questions) and wait for the answer—or set hold: true (or surface: \"window\") to place the questions in the Cards window and return immediately so the user can keep chatting with you while answering them (wizard style, page_size at a time). Use it when you need a decision, a choice between options, a few fields filled in, or approval for something consequential, instead of asking in prose. kind and payload: confirm {body, confirm_label?, cancel_label?, danger?}; prompt {question, body?, placeholder?, multiline?}; choice {question, options: [{id, label, description?}], multiple?, allow_other?}; form {schema: a JSON Schema object with properties (string, number, integer, boolean, enum; title, description, default), body?, values?, submit_label?}; approval {summary, changes?: [{label, before?, after?}]} with risk low|medium|high; entity (or module-scoped kind such as ops.workstream, ops.agent, ops.host, ops.followup, registry.record, tracker.list, tracker.item, align.proposal, align.droppad) {ref?, refs?: [...], entity_kind?, view?: card|compact|detail, question?, options?: [{id, label}], actions?: [{id, label}], data?}. All cards also let the user pick Other and tell you something in their own words. When held or answered later, the answers come to you as a message.",
			Input:       schema(`{"type":"object","properties":{"kind":{"type":"string","enum":["confirm","prompt","choice","form","approval","entity","ops.workstream","ops.agent","ops.host","ops.followup","registry.record","tracker.list","tracker.item","align.proposal","align.droppad","work.item"]},"title":{"type":"string"},"subject":{"type":"string"},"risk":{"type":"string","enum":["low","medium","high"]},"payload":{"type":"object"},"questions":{"type":"array","description":"Ask several card questions at once in the Cards window wizard.","items":{"type":"object","required":["kind","payload"],"properties":{"kind":{"type":"string","enum":["confirm","prompt","choice","form","approval","entity","ops.workstream","ops.agent","ops.host","ops.followup","registry.record","tracker.list","tracker.item","align.proposal","align.droppad","work.item"]},"title":{"type":"string"},"subject":{"type":"string"},"risk":{"type":"string","enum":["low","medium","high"]},"payload":{"type":"object"}}}},"hold":{"type":"boolean","description":"Do not block the conversation: hold the question(s) open in the Cards window so the user can chat with you before submitting."},"surface":{"type":"string","enum":["inline","window"],"description":"Where to display the card(s): inline in the chat panel or in the Cards window."},"page_size":{"type":"integer","description":"How many cards to show at a time in the Cards window wizard (e.g. 3)."},"timeout_seconds":{"type":"integer","description":"How long to wait for the answer. Default 600, at most 3600."}}}`),
			Describe:    func(json.RawMessage) string { return "Ask you something" },
			Run: func(ctx context.Context, _ module.Host, raw json.RawMessage) (any, error) {
				in, err := decode[struct {
					module.Card
					Questions []module.Card `json:"questions"`
					Hold      bool          `json:"hold"`
					Surface   string        `json:"surface"`
					PageSize  int           `json:"page_size"`
					Timeout   int           `json:"timeout_seconds"`
				}](raw)
				if err != nil {
					return nil, err
				}
				wait := time.Duration(min(max(in.Timeout, 0), 3600)) * time.Second
				if wait == 0 {
					wait = 10 * time.Minute
				}
				expires := time.Now().Add(wait).UTC().Format(time.RFC3339)
				if in.PageSize > 0 {
					_ = a.cfg.Set(ctx, "surface.card_page_size", in.PageSize)
				}
				batch := in.Questions
				if len(batch) == 0 {
					batch = []module.Card{in.Card}
				}
				for i, item := range batch {
					if !cards.Answerable(item.Kind) && !cards.IsModuleEntityKind(item.Kind) {
						return nil, fmt.Errorf("a %s card does not ask anything: use card_show", item.Kind)
					}
					if cards.IsModuleEntityKind(item.Kind) {
						batch[i] = a.enrichEntityCard(ctx, item)
					}
				}
				// Check whether the app settings or tool args route these cards to the Cards window (held).
				hold := in.Hold || in.Surface == "window" || len(batch) > 1
				if !hold && in.Surface != "inline" {
					coreCfg := a.cfg.Scoped(CoreScope)
					if coreCfg.String("card_display") == "window" {
						hold = true
					} else if slotsCSV := strings.TrimSpace(coreCfg.String("card_window_slots")); slotsCSV != "" {
						activeSlot := ""
						if f, ok := a.desk.Focused(ctx); ok {
							activeSlot = f.Module
						}
						for _, part := range strings.Split(slotsCSV, ",") {
							p := strings.TrimSpace(part)
							if p == "*" || (p != "" && strings.EqualFold(p, activeSlot)) {
								hold = true
								break
							}
						}
					}
				}
				if hold {
					var ids []string
					for _, item := range batch {
						item.Source, item.Blocking = "agent", false
						item.ExpiresAt = expires
						shown, serr := a.cards.Show(ctx, item)
						if serr != nil {
							return nil, serr
						}
						// Mark as abandoned (waiting for late/held answer) so the card stays open in the
						// Cards window while freeing the conversation wire for the user to chat.
						_, _ = a.cards.End(context.WithoutCancel(ctx), shown.ID, cards.Abandoned)
						ids = append(ids, shown.ID)
					}
					_, _ = a.desk.Open(ctx, "surface.cards", module.OpenOptions{
						Background: false,
						Args:       map[string]any{"from_chat": true},
						Reason:     "Questions waiting for your answer",
					})
					return map[string]any{"ids": ids, "status": "held", "note": "The questions are open in the Cards window so the user can review and chat with you before submitting. Their answers will arrive as a message when submitted."}, nil
				}
				c := batch[0]
				c.Source, c.Blocking = "agent", true
				c.ExpiresAt = expires
				shown, err := a.cards.Show(ctx, c)
				if err != nil {
					return nil, err
				}
				done, err := a.cards.Await(ctx, shown.ID)
				if err != nil {
					// The run ended while the card was up (stopped, or the app shutting down): the
					// card is abandoned, not canceled, which would say the user declined. The
					// user can still answer it, and the answer comes to the agent as a message.
					_, _ = a.cards.End(context.WithoutCancel(ctx), shown.ID, cards.Abandoned)
					return nil, err
				}
				if done.Status == cards.Abandoned {
					return map[string]any{"id": done.ID, "status": "held", "note": "The user moved this question to the Cards window to answer after checking or chatting. The answer will arrive as a message when submitted."}, nil
				}
				out := map[string]any{"id": done.ID, "status": done.Status}
				if o := done.Outcome; o != nil {
					if len(o.Data) > 0 {
						out["data"] = o.Data
					}
					if len(o.Answers) > 0 {
						out["answers"] = o.Answers
					}
					if len(o.Decisions) > 0 {
						out["decisions"] = o.Decisions
					}
				}
				return out, nil
			}},
		{Name: "card_update", Level: module.LevelAct,
			Description: "Change a card you showed with card_show: give the new payload (progress moving on, a plan step done), or end it with status done to close it.",
			Input:       schema(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"},"title":{"type":"string"},"payload":{"type":"object"},"end":{"type":"boolean","description":"Close the card: it is finished with."}}}`),
			Describe:    func(json.RawMessage) string { return "Update a card" },
			Run: func(ctx context.Context, _ module.Host, raw json.RawMessage) (any, error) {
				in, err := decode[struct {
					ID      string          `json:"id"`
					Title   string          `json:"title"`
					Payload json.RawMessage `json:"payload"`
					End     bool            `json:"end"`
				}](raw)
				if err != nil {
					return nil, err
				}
				// Only the agent's own cards: another's card (a module's approval, say) is not
				// the agent's to rewrite or close.
				if c, ok, err := a.cards.Get(ctx, in.ID); err != nil {
					return nil, err
				} else if !ok || c.Source != "agent" {
					return nil, fmt.Errorf("card %s is not one you showed", in.ID)
				}
				if len(in.Payload) > 0 || in.Title != "" {
					if _, err := a.cards.Update(ctx, in.ID, in.Title, in.Payload); err != nil {
						return nil, err
					}
				}
				if in.End {
					if _, err := a.cards.End(ctx, in.ID, "canceled"); err != nil {
						return nil, err
					}
				}
				return "done", nil
			}},
		{Name: "marks_list", Level: module.LevelRead,
			Description: "List the marks (notes pinned to a part of a document, page or file) on a ref, with their replies. With no ref: every open mark.",
			Input:       schema(`{"type":"object","properties":{"ref":{"type":"string","description":"What the marks are on, as window_context gives it (ref)."}}}`),
			Run: func(ctx context.Context, _ module.Host, raw json.RawMessage) (any, error) {
				in, err := decode[struct{ Ref string }](raw)
				if err != nil {
					return nil, err
				}
				return a.marks.List(ctx, in.Ref)
			}},
		{Name: "marks_add", Level: module.LevelAct,
			Description: "Pin a note of yours to a part of something the user is working on, instead of editing it: quote the passage it is about. The user sees it beside the document.",
			Input:       schema(`{"type":"object","required":["ref","body"],"properties":{"ref":{"type":"string"},"quote":{"type":"string","description":"The exact passage the note is about."},"body":{"type":"string"},"anchor":{"type":"object"}}}`),
			Run: func(ctx context.Context, _ module.Host, raw json.RawMessage) (any, error) {
				in, err := decode[module.Mark](raw)
				if err != nil {
					return nil, err
				}
				in.Author = "agent"
				return a.marks.Add(ctx, in)
			}},
		{Name: "marks_reply", Level: module.LevelAct, Description: "Reply on a mark.",
			Input: schema(`{"type":"object","required":["id","body"],"properties":{"id":{"type":"string"},"body":{"type":"string"}}}`),
			Run: func(ctx context.Context, _ module.Host, raw json.RawMessage) (any, error) {
				in, err := decode[struct{ ID, Body string }](raw)
				if err != nil {
					return nil, err
				}
				return a.marks.Reply(ctx, in.ID, module.MarkReply{Body: in.Body, Author: "agent"})
			}},
		{Name: "marks_resolve", Level: module.LevelAct, Description: "Resolve a mark once what it asked for is done.",
			Input: schema(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}`),
			Run: func(ctx context.Context, _ module.Host, raw json.RawMessage) (any, error) {
				in, err := decode[struct{ ID string }](raw)
				if err != nil {
					return nil, err
				}
				return "resolved", a.marks.Resolve(ctx, in.ID)
			}},
		{Name: "settings_list", Level: module.LevelRead, Description: "List the app's live settings (the core's and each module's) with their values and options.",
			Run: func(context.Context, module.Host, json.RawMessage) (any, error) { return a.cfg.Entries(), nil }},
		{Name: "settings_set", Level: module.LevelWrite, Description: "Change a live setting by its full key (\"surface.theme\"). It takes effect at once.",
			Input: schema(`{"type":"object","required":["key","value"],"properties":{"key":{"type":"string"},"value":{}}}`),
			Describe: func(raw json.RawMessage) string {
				in, _ := decode[struct{ Key string }](raw)
				return "Change the setting " + in.Key
			},
			Run: func(ctx context.Context, _ module.Host, raw json.RawMessage) (any, error) {
				in, err := decode[struct {
					Key   string `json:"key"`
					Value any    `json:"value"`
				}](raw)
				if err != nil {
					return nil, err
				}
				if a.ownerSetting(in.Key) {
					return nil, fmt.Errorf("%s is the owner's to change, in Settings", in.Key)
				}
				return "set", a.cfg.Set(ctx, in.Key, in.Value)
			}},
	}
}

// Catalog is what the app is made of, as data: its modules with their views, commands,
// tools and slots. It is what a module that composes setups (the Interaction Builder) and
// the agent read to know what there is to arrange.
type Catalog struct {
	App     string          `json:"app"`
	Title   string          `json:"title"`
	Modules []CatalogModule `json:"modules"`
}

// CatalogModule is one module of the catalog. The core's own entries are under "surface".
type CatalogModule struct {
	ID       string           `json:"id"`
	Title    string           `json:"title"`
	About    string           `json:"about,omitempty"`
	Views    []CatalogItem    `json:"views,omitempty"`
	Commands []CatalogItem    `json:"commands,omitempty"`
	Tools    []CatalogItem    `json:"tools,omitempty"`
	Slots    []module.Slot    `json:"slots,omitempty"`
	Settings []module.Setting `json:"settings,omitempty"`
}

// CatalogItem is a view, a command or a tool, by id and what it is for.
type CatalogItem struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	About string `json:"about,omitempty"`
	Level string `json:"level,omitempty"`
}

// Catalog returns the app's catalog.
func (a *App) Catalog() Catalog {
	c := Catalog{App: a.name, Title: a.title}
	by := map[string]*CatalogModule{}
	order := []string{CoreScope}
	by[CoreScope] = &CatalogModule{ID: CoreScope, Title: "Desk", About: "The core: windows, layouts, marks and settings."}
	for _, h := range a.mods {
		by[h.id] = &CatalogModule{ID: h.id, Title: h.man.Title, About: h.man.About, Slots: h.man.Slots, Settings: h.man.Settings}
		order = append(order, h.id)
	}
	for _, v := range a.sortedViews() {
		if m := by[v.Module]; m != nil {
			m.Views = append(m.Views, CatalogItem{ID: v.ID, Title: v.Title})
		}
	}
	for _, ci := range a.CommandInfos() {
		// An "Open <view>" command belongs to the view's module but says nothing new.
		if strings.HasPrefix(ci.ID, "open.") || ci.Run == nil {
			continue
		}
		if m := by[ci.Module]; m != nil {
			m.Commands = append(m.Commands, CatalogItem{ID: ci.ID, Title: ci.Title})
		}
	}
	for _, t := range a.tools {
		if m := by[t.Module]; m != nil {
			about, _, _ := strings.Cut(t.Description, ". ")
			m.Tools = append(m.Tools, CatalogItem{ID: t.Name, About: about, Level: string(t.Level)})
		}
	}
	for _, id := range order {
		c.Modules = append(c.Modules, *by[id])
	}
	return c
}

// ownerSetting reports whether a setting (by full key, "<module>.<key>") is marked Owner.
func (a *App) ownerSetting(full string) bool {
	scope, key, ok := strings.Cut(full, ".")
	if !ok {
		return false
	}
	for _, h := range a.mods {
		if h.id != scope {
			continue
		}
		for _, s := range h.man.Settings {
			if s.Key == key {
				return s.Owner
			}
		}
	}
	return false
}

// enrichEntityCard resolves live module records for an entity card when ref/refs are
// provided and data/entities are not already pre-populated, attaching
// "resolved": { "<ref>": { "entity": ..., "record": ... } } to card.Payload.
func (a *App) enrichEntityCard(ctx context.Context, c module.Card) module.Card {
	var p map[string]any
	if len(c.Payload) == 0 || json.Unmarshal(c.Payload, &p) != nil || p == nil {
		return c
	}
	if c.Kind != "entity" && p["entity_kind"] == nil {
		p["entity_kind"] = c.Kind
	}
	var refs []string
	seen := map[string]bool{}
	addRef := func(r string) {
		r = strings.TrimSpace(r)
		if r != "" && !seen[r] {
			seen[r] = true
			refs = append(refs, r)
		}
	}
	if r, ok := p["ref"].(string); ok {
		addRef(r)
	}
	switch list := p["refs"].(type) {
	case []any:
		for _, item := range list {
			if s, ok := item.(string); ok {
				addRef(s)
			}
		}
	case []string:
		for _, s := range list {
			addRef(s)
		}
	}
	hasData := false
	if d, ok := p["data"].(map[string]any); ok && len(d) > 0 {
		hasData = true
	}
	if e, ok := p["entities"].([]any); ok && len(e) > 0 {
		hasData = true
	} else if e, ok := p["entities"].(map[string]any); ok && len(e) > 0 {
		hasData = true
	}
	if r, ok := p["resolved"].(map[string]any); ok && len(r) > 0 {
		hasData = true
	}
	if len(refs) > 0 && !hasData {
		entityKind, _ := p["entity_kind"].(string)
		resolved := map[string]any{}
		for _, rawRef := range refs {
			kind, id := a.parseEntityRef(rawRef, entityKind)
			if kind == "" || id == "" {
				continue
			}
			entry := map[string]any{"kind": kind, "id": id}
			found := false
			if ents, err := a.ResolveRefs(ctx, kind, []string{id}); err == nil {
				if ent := ents[id]; ent != nil {
					entry["entity"] = ent
					found = true
				}
			}
			if rec, err := a.RecordRef(ctx, kind, id); err == nil {
				entry["record"] = rec
				if _, ok := entry["entity"]; !ok {
					preview := rec
					preview.Body = ""
					entry["entity"] = preview
				}
				found = true
			}
			if found {
				resolved[rawRef] = entry
			}
		}
		if len(resolved) > 0 {
			p["resolved"] = resolved
			if len(refs) == 1 && p["data"] == nil {
				if first, ok := resolved[refs[0]].(map[string]any); ok {
					if rec, ok := first["record"]; ok {
						p["data"] = rec
					} else if ent, ok := first["entity"]; ok {
						p["data"] = ent
					}
				}
			}
		}
	}
	if raw, err := json.Marshal(p); err == nil {
		c.Payload = raw
	}
	return c
}

// parseEntityRef maps a reference string (such as "ops:phase-1", "registry:project:surface",
// "tracker:trk_1", "tracker.item:ti_1", "align:ap_1", "droppad:dp_1", "CW-20261122-0008")
// and an optional entityKind hint into a Cite (kind, canonicalID) pair.
func (a *App) parseEntityRef(rawRef, hintKind string) (string, string) {
	rawRef = strings.TrimSpace(rawRef)
	hintKind = strings.TrimSpace(hintKind)
	if rawRef == "" {
		return "", ""
	}
	if strings.HasPrefix(rawRef, "caster://") {
		return "caster.doc", rawRef
	}
	if strings.HasPrefix(rawRef, "msg://") {
		return "tether.agent", rawRef
	}
	if strings.HasPrefix(rawRef, "/") || strings.HasPrefix(rawRef, "~/") || strings.HasPrefix(rawRef, "remote:") {
		return "path", rawRef
	}
	prefixMap := map[string]string{
		"ops":            "ops.workstream",
		"ops.workstream": "ops.workstream",
		"ops.agent":      "ops.agent",
		"ops.host":       "ops.host",
		"ops.followup":   "ops.followup",
		"registry":       "registry.record",
		"registry.record": "registry.record",
		"work":           "work.item",
		"work.item":      "work.item",
		"tracker":        "tracker.list",
		"tracker.list":   "tracker.list",
		"tracker.item":   "tracker.item",
		"align":          "align.proposal",
		"align.proposal": "align.proposal",
		"droppad":        "align.droppad",
		"align.droppad":  "align.droppad",
		"tesseract":      "tesseract.item",
		"fragment":       "fragments.fragment",
		"frag":           "fragments.fragment",
	}
	if prefix, rest, ok := strings.Cut(rawRef, ":"); ok && rest != "" {
		if k, mapped := prefixMap[prefix]; mapped {
			if k == "tesseract.item" && strings.Contains(rest, "#") {
				return "tesseract.key", rest
			}
			if k == "tracker.list" && hintKind == "tracker.item" {
				return "tracker.item", rest
			}
			return k, rest
		}
		if _, exists := a.refs[prefix]; exists {
			return prefix, rest
		}
		// Bare registry canonical id like "project:surface" when hint is registry.record.
		if hintKind == "registry.record" || hintKind == "registry" {
			return "registry.record", rawRef
		}
	}
	for _, tp := range []struct{ p, k string }{
		{"CW-", "torque.task"},
		{"EP-", "torque.epic"},
		{"PRJ-", "torque.project"},
		{"SP-", "torque.sprint"},
		{"PL-", "torque.plan"},
	} {
		if strings.HasPrefix(rawRef, tp.p) {
			return tp.k, rawRef
		}
	}
	if hintKind != "" {
		if k, mapped := prefixMap[hintKind]; mapped {
			return k, rawRef
		}
		if _, exists := a.refs[hintKind]; exists {
			return hintKind, rawRef
		}
	}
	for k, r := range a.refs {
		if r.ID != nil && r.ID.MatchString(rawRef) {
			return k, rawRef
		}
	}
	return hintKind, rawRef
}
