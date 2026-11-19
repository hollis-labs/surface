package kernel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/hollis-labs/surface/core/internal/cards"
	"github.com/hollis-labs/surface/core/internal/config"
	"github.com/hollis-labs/surface/core/internal/desk"
	"github.com/hollis-labs/surface/core/internal/events"
	"github.com/hollis-labs/surface/core/internal/marks"
	"github.com/hollis-labs/surface/core/internal/storage"
	"github.com/hollis-labs/surface/module"
)

// CoreScope is the settings scope of the core's own settings.
const CoreScope = "surface"

// Options are what an app gives the kernel.
type Options struct {
	// Name is the app's name ("desk"); Title is how a person reads it.
	Name  string
	Title string
	// DataDir is the app's data directory. It is created, private.
	DataDir string
	// DB is the core database. When nil the kernel opens <DataDir>/surface.db itself.
	DB  *sql.DB
	Log *slog.Logger
	// Modules are the ids to start, in order. Nil starts every registered module.
	Modules []string
}

// App is the assembled app: the Kernel the core's services are given.
type App struct {
	name, title, dataDir string
	db                   *sql.DB
	ownDB                bool
	log                  *slog.Logger

	bus   *events.Bus
	cfg   *config.Store
	desk  *desk.Desk
	marks *marks.Store
	cards *cards.Store

	mods     []*host
	byModule map[string]*host
	commands map[string]boundCommand
	order    []string // command ids in declaration order
	tools    []BoundTool
	slots    []BoundSlot
	arounds  []BoundAround
	asker    atomic.Pointer[askerBox]
	routes   *http.ServeMux
	started  bool
	mu       sync.RWMutex
}

type boundCommand struct {
	module.Command
	Module string
	h      module.Host
}

// CommandInfo is a command as the page and the agent are told it.
type CommandInfo struct {
	module.Command
	Module string `json:"module"`
}

// ModuleInfo is one started module, for the app's manifest.
type ModuleInfo struct {
	module.Manifest
}

// New assembles the app: opens the stores, reads every module's manifest and builds its
// Host. Start then starts the modules.
func New(ctx context.Context, o Options) (*App, error) {
	if !module.ValidID(o.Name) {
		return nil, fmt.Errorf("surface: invalid app name %q", o.Name)
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.DataDir == "" {
		return nil, errors.New("surface: no data directory")
	}
	if err := os.MkdirAll(o.DataDir, 0o700); err != nil {
		return nil, err
	}
	a := &App{name: o.Name, title: o.Title, dataDir: o.DataDir, db: o.DB, log: o.Log,
		bus: events.New(), byModule: map[string]*host{}, commands: map[string]boundCommand{}, routes: http.NewServeMux()}
	if a.title == "" {
		a.title = o.Name
	}
	if a.db == nil {
		db, err := storage.Open(ctx, filepath.Join(o.DataDir, "surface.db"))
		if err != nil {
			return nil, err
		}
		a.db, a.ownDB = db, true
	}
	if err := storage.MigrateCore(ctx, a.db); err != nil {
		return nil, a.fail(err)
	}
	var err error
	if a.cfg, err = config.Open(ctx, a.db, a.bus); err != nil {
		return nil, a.fail(err)
	}
	if err = a.cfg.Declare(CoreScope, coreSettings); err != nil {
		return nil, a.fail(err)
	}
	a.marks = marks.New(a.db, a.bus)
	a.cards = cards.New(a.db, a.bus)

	ids := o.Modules
	if ids == nil {
		ids = module.Registered()
	}
	var views []desk.ViewInfo
	for _, v := range coreViews {
		views = append(views, desk.ViewInfo{View: v, Module: CoreScope})
	}
	for _, id := range ids {
		m, ok := module.Lookup(id)
		if !ok {
			return nil, a.fail(fmt.Errorf("surface: module %q is not compiled into this binary", id))
		}
		man := m.Manifest()
		h := &host{app: a, id: id, mod: m, man: man, log: o.Log.With("module", id)}
		h.dir = filepath.Join(o.DataDir, "modules", id)
		if err = os.MkdirAll(h.dir, 0o700); err != nil {
			return nil, a.fail(err)
		}
		if man.Store != nil {
			if man.Store.Migrations == nil {
				return nil, a.fail(fmt.Errorf("surface: module %q declares a store with no migrations", id))
			}
			if h.db, err = storage.Open(ctx, filepath.Join(h.dir, id+".db")); err != nil {
				return nil, a.fail(fmt.Errorf("surface: module %q store: %w", id, err))
			}
			if err = storage.Migrate(ctx, h.db, id, man.Store.Migrations, man.Store.Dir); err != nil {
				return nil, a.fail(err)
			}
		}
		if err = a.cfg.Declare(id, man.Settings); err != nil {
			return nil, a.fail(err)
		}
		for _, v := range man.Views {
			if v.Render.Kind == "" || v.Render.Entry == "" {
				return nil, a.fail(fmt.Errorf("surface: view %q of module %q has no render", v.ID, id))
			}
			views = append(views, desk.ViewInfo{View: v, Module: id})
		}
		a.mods = append(a.mods, h)
		a.byModule[id] = h
	}
	if a.desk, err = desk.Open(ctx, a.db, a.bus, views); err != nil {
		return nil, a.fail(err)
	}
	a.desk.SetDescriber(a.describe)

	// Commands: the core's, an "Open" for every menu view, then each module's.
	core := &host{app: a, id: CoreScope, log: o.Log.With("module", CoreScope), dir: o.DataDir}
	for _, c := range coreCommands(a) {
		if err := a.addCommand(c, CoreScope, core); err != nil {
			return nil, a.fail(err)
		}
	}
	for _, v := range views {
		if !v.Menu {
			continue
		}
		view := v
		if err := a.addCommand(module.Command{
			ID: "open." + view.ID, Title: "Open " + view.Title, Section: "Apps",
			Run: func(ctx context.Context, h module.Host, args map[string]any) (any, error) {
				return h.Desk().Open(ctx, view.ID, module.OpenOptions{Args: args})
			},
		}, view.Module, core); err != nil {
			return nil, a.fail(err)
		}
	}
	for _, t := range coreTools(a) {
		a.tools = append(a.tools, bindTool(t, CoreScope, core))
	}
	seenTool := map[string]string{}
	for _, t := range a.tools {
		seenTool[t.Name] = CoreScope
	}
	seenSlot := map[string]string{}
	for _, h := range a.mods {
		for _, c := range h.man.Commands {
			if err := a.addCommand(c, h.id, h); err != nil {
				return nil, a.fail(err)
			}
		}
		for _, t := range h.man.Tools {
			if by, dup := seenTool[t.Name]; dup {
				return nil, a.fail(fmt.Errorf("surface: tool %q is declared by both %s and %s", t.Name, by, h.id))
			}
			if t.Run == nil {
				return nil, a.fail(fmt.Errorf("surface: tool %q of module %q has no Run", t.Name, h.id))
			}
			seenTool[t.Name] = h.id
			a.tools = append(a.tools, bindTool(t, h.id, h))
		}
		for _, s := range h.man.Slots {
			if by, dup := seenSlot[s.Name]; dup {
				return nil, a.fail(fmt.Errorf("surface: slot %q is declared by both %s and %s", s.Name, by, h.id))
			}
			seenSlot[s.Name] = h.id
			bs := BoundSlot{Slot: s, Module: h.id}
			if s.Knowledge != nil {
				hh, fn := h, s.Knowledge
				bs.Knowledge = func(ctx context.Context, sit module.Situation) (string, error) { return fn(ctx, hh, sit) }
			}
			a.slots = append(a.slots, bs)
		}
		if h.man.Around != nil {
			hh, fn := h, h.man.Around
			a.arounds = append(a.arounds, BoundAround{Module: h.id, Read: func(ctx context.Context) (string, error) { return fn(ctx, hh) }})
		}
	}
	return a, nil
}

func (a *App) fail(err error) error {
	a.closeStores()
	return err
}

func (a *App) closeStores() {
	for _, h := range a.mods {
		if h.db != nil {
			_ = h.db.Close()
		}
	}
	if a.ownDB && a.db != nil {
		_ = a.db.Close()
	}
}

func bindTool(t module.Tool, mod string, h module.Host) BoundTool {
	switch t.Level {
	case module.LevelRead, module.LevelAct, module.LevelWrite, module.LevelConfirm:
	default:
		t.Level = module.LevelWrite
	}
	if len(t.Input) == 0 {
		t.Input = json.RawMessage(`{"type":"object","properties":{}}`)
	}
	run := t.Run
	return BoundTool{Tool: t, Module: mod, Call: func(ctx context.Context, input json.RawMessage) (any, error) {
		if len(input) == 0 {
			input = json.RawMessage(`{}`)
		}
		return run(ctx, h, input)
	}}
}

func (a *App) addCommand(c module.Command, mod string, h module.Host) error {
	if c.ID == "" || c.Title == "" {
		return fmt.Errorf("surface: a command of %s has no id or title", mod)
	}
	if _, dup := a.commands[c.ID]; dup {
		return fmt.Errorf("surface: command %q is declared twice", c.ID)
	}
	a.commands[c.ID] = boundCommand{Command: c, Module: mod, h: h}
	a.order = append(a.order, c.ID)
	return nil
}

// Start starts the modules in order. Routes a module adds in Start are served afterwards.
func (a *App) Start(ctx context.Context) error {
	for _, h := range a.mods {
		if err := h.mod.Start(ctx, h); err != nil {
			return fmt.Errorf("surface: module %q did not start: %w", h.id, err)
		}
		h.started = true
	}
	a.mu.Lock()
	a.started = true
	a.mu.Unlock()
	a.bus.Publish("app.ready", map[string]any{"app": a.name})
	return nil
}

// Stop stops the started modules in reverse order and closes the stores.
func (a *App) Stop(ctx context.Context) error {
	var errs []error
	for i := len(a.mods) - 1; i >= 0; i-- {
		if h := a.mods[i]; h.started {
			if err := h.mod.Stop(ctx); err != nil {
				errs = append(errs, fmt.Errorf("module %q: %w", h.id, err))
			}
		}
	}
	a.closeStores()
	return errors.Join(errs...)
}

// describe asks the module that owns a window's view what the window is showing.
func (a *App) describe(ctx context.Context, w module.Window) (module.WindowContext, bool) {
	h, ok := a.byModule[w.Module]
	if !ok {
		return module.WindowContext{}, false
	}
	for _, v := range h.man.Views {
		if v.ID == w.View && v.Describe != nil {
			wc, err := v.Describe(ctx, h, w)
			if err != nil {
				h.log.Warn("describe failed", "view", v.ID, "err", err)
				return module.WindowContext{}, false
			}
			return wc, true
		}
	}
	return module.WindowContext{}, false
}

// The Kernel.

// AppName is the app's name.
func (a *App) AppName() string { return a.name }

// Title is the app's name as a person reads it.
func (a *App) Title() string { return a.title }

// DataDir is the app's data directory.
func (a *App) DataDir() string { return a.dataDir }

// DB is the core database.
func (a *App) DB() *sql.DB { return a.db }

// Log is the app's logger.
func (a *App) Log() *slog.Logger { return a.log }

// Desk is the windows and layouts.
func (a *App) Desk() module.Desk { return a.desk }

// WM is the desk with the whole of its state, for the core's own API.
func (a *App) WM() *desk.Desk { return a.desk }

// Events is the bus.
func (a *App) Events() module.Events { return a.bus }

// Bus is the bus as the core's API uses it.
func (a *App) Bus() *events.Bus { return a.bus }

// Marks are the collaboration marks.
func (a *App) Marks() module.Marks { return a.marks }

// Cards are what the app shows the user outside a window.
func (a *App) Cards() module.Cards { return a.cards }

// Config is one scope's live settings.
func (a *App) Config(scope string) module.Config { return a.cfg.Scoped(scope) }

// Settings is the whole settings store.
func (a *App) Settings() *config.Store { return a.cfg }

// DeclareSettings adds a core service's settings (the agent's, say). Call before serving.
func (a *App) DeclareSettings(scope string, s []module.Setting) error { return a.cfg.Declare(scope, s) }

// Tools is every tool in the app.
func (a *App) Tools() []BoundTool { return append([]BoundTool(nil), a.tools...) }

// AddTools adds a core service's own tools (the agent's: slots, reflexes, rules), so they
// are offered at /api/tools and /mcp like every other. Call before serving. A name already
// taken is an error.
func (a *App) AddTools(tools ...BoundTool) error {
	have := map[string]string{}
	for _, t := range a.tools {
		have[t.Name] = t.Module
	}
	for _, t := range tools {
		if by, dup := have[t.Name]; dup {
			return fmt.Errorf("surface: tool %q is declared by both %s and %s", t.Name, by, t.Module)
		}
		if t.Call == nil {
			return fmt.Errorf("surface: tool %q of %s has no call", t.Name, t.Module)
		}
		have[t.Name] = t.Module
	}
	a.tools = append(a.tools, tools...)
	return nil
}

// Slots is every module's slots.
func (a *App) Slots() []BoundSlot { return append([]BoundSlot(nil), a.slots...) }

// Arounds is every module's Around, bound to its Host.
func (a *App) Arounds() []BoundAround { return append([]BoundAround(nil), a.arounds...) }

type askerBox struct{ Asker }

// SetAsker names what takes the modules' errands for the agent. Until it is set (the agent
// service starts after the modules), and when it is nil, an errand gets module.ErrNoAgent.
func (a *App) SetAsker(x Asker) { a.asker.Store(&askerBox{x}) }

// moduleAgent is one module's way to the agent.
type moduleAgent struct {
	app *App
	id  string
}

func (m moduleAgent) Ask(ctx context.Context, x module.Ask) (string, error) {
	b := m.app.asker.Load()
	if b == nil || b.Asker == nil {
		return "", module.ErrNoAgent
	}
	return b.AskFrom(ctx, m.id, x)
}

// Situation is what is happening now.
func (a *App) Situation(ctx context.Context) module.Situation {
	st := a.desk.Snapshot()
	s := module.Situation{Windows: a.desk.Windows(ctx), Mode: st.Mode, Layout: st.Active}
	if wc, ok := a.desk.Context(ctx, ""); ok {
		s.Focused = &wc
	}
	return s
}

// Commands lists every command in declaration order.
func (a *App) Commands() []module.Command {
	out := make([]module.Command, 0, len(a.order))
	for _, id := range a.order {
		out = append(out, a.commands[id].Command)
	}
	return out
}

// CommandInfos lists every command with the module that brought it.
func (a *App) CommandInfos() []CommandInfo {
	out := make([]CommandInfo, 0, len(a.order))
	for _, id := range a.order {
		c := a.commands[id]
		out = append(out, CommandInfo{Command: c.Command, Module: c.Module})
	}
	return out
}

// RunCommand runs a command by id.
func (a *App) RunCommand(ctx context.Context, id string, args map[string]any) (any, error) {
	c, ok := a.commands[id]
	if !ok {
		return nil, fmt.Errorf("no command %q", id)
	}
	if c.Run == nil {
		return nil, fmt.Errorf("command %q runs in the page, not the server", id)
	}
	out, err := c.Run(ctx, c.h, args)
	if err == nil {
		a.bus.Publish("command.ran", map[string]any{"id": id})
	}
	return out, err
}

// Modules lists the started modules' manifests.
func (a *App) Modules() []ModuleInfo {
	out := make([]ModuleInfo, 0, len(a.mods))
	for _, h := range a.mods {
		out = append(out, ModuleInfo{Manifest: h.man})
	}
	return out
}

// ModuleRoutes serves the modules' own routes (/api/m/<id>/...).
func (a *App) ModuleRoutes() http.Handler { return a.routes }

// host is one module's Host.
type host struct {
	app     *App
	id      string
	mod     module.Module
	man     module.Manifest
	log     *slog.Logger
	db      *sql.DB
	dir     string
	started bool
}

func (h *host) ModuleID() string      { return h.id }
func (h *host) Log() *slog.Logger     { return h.log }
func (h *host) Desk() module.Desk     { return h.app.desk }
func (h *host) Store() *sql.DB        { return h.db }
func (h *host) DataDir() string       { return h.dir }
func (h *host) Config() module.Config { return h.app.cfg.Scoped(h.id) }
func (h *host) Events() module.Events { return h.app.bus }
func (h *host) Marks() module.Marks   { return h.app.marks }
func (h *host) Cards() module.Cards   { return h.app.cards }
func (h *host) Agent() module.Agent   { return moduleAgent{app: h.app, id: h.id} }
func (h *host) RunCommand(ctx context.Context, id string, args map[string]any) (any, error) {
	return h.app.RunCommand(ctx, id, args)
}

// Handle serves a route of the module's under /api/m/<id>/.
func (h *host) Handle(pattern string, handler http.Handler) {
	h.app.mu.RLock()
	started := h.app.started
	h.app.mu.RUnlock()
	if started {
		panic(fmt.Sprintf("surface: module %q added a route after the app started", h.id))
	}
	method, path, ok := strings.Cut(strings.TrimSpace(pattern), " ")
	if !ok {
		method, path = "", method
	}
	prefix := "/api/m/" + h.id
	full := prefix + "/" + strings.TrimPrefix(path, "/")
	if method != "" {
		full = method + " " + full
	}
	h.app.routes.Handle(full, http.StripPrefix(prefix, handler))
}

var _ Kernel = (*App)(nil)
var _ module.Host = (*host)(nil)

// sortedViews returns the desk's views by title, for listings.
func (a *App) sortedViews() []desk.ViewInfo {
	vs := a.desk.Views()
	sort.Slice(vs, func(i, j int) bool { return vs[i].ID < vs[j].ID })
	return vs
}
