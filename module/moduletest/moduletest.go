// Package moduletest runs one Surface module in a test, without the core: a Host with an
// in-memory desk, live settings, an event bus and marks, and a temporary SQLite store with
// the module's migrations applied.
//
//	h := moduletest.Start(t, mymodule.New())
//	out, err := h.Tool("mymodule_add", `{"text":"hi"}`)
//	h.Do("GET", "/items", "") // the module's routes, as /api/m/<id>/items
//
// It is a stand-in, not the core: the desk keeps windows and layouts and does nothing else,
// and RunCommand runs the module's own commands plus any the test gives with OnCommand.
package moduletest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite" // the driver

	"github.com/hollis-labs/surface/module"
)

// Host is the module's Host in a test, and what the test reads back.
type Host struct {
	t   testing.TB
	m   module.Module
	man module.Manifest
	db  *sql.DB
	dir string
	mux *http.ServeMux

	DeskState *Desk
	Bus       *Bus
	MarkStore *Marks
	// CardStore records the cards the module showed.
	CardStore *Cards
	Settings  *Config

	mu       sync.Mutex
	commands map[string]func(ctx context.Context, args map[string]any) (any, error)
}

// New builds the Host for m (its store migrated) without starting the module.
func New(t testing.TB, m module.Module) *Host {
	t.Helper()
	man := m.Manifest()
	h := &Host{t: t, m: m, man: man, dir: t.TempDir(), mux: http.NewServeMux(),
		DeskState: newDesk(), Bus: &Bus{}, MarkStore: &Marks{}, CardStore: &Cards{},
		commands: map[string]func(context.Context, map[string]any) (any, error){}}
	h.Settings = &Config{defs: map[string]module.Setting{}, values: map[string]any{}, bus: h.Bus, scope: man.ID}
	for _, s := range man.Settings {
		if err := checkDefault(s); err != nil {
			// The app refuses to start on this (core/internal/config): fail here instead.
			t.Fatalf("moduletest: setting %s.%s default: %v", man.ID, s.Key, err)
		}
		h.Settings.defs[s.Key] = s
	}
	if man.Store != nil {
		db, err := sql.Open("sqlite", "file:"+filepath.Join(h.dir, "store.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		if err := migrate(db, man.Store); err != nil {
			t.Fatalf("moduletest: migrate %s: %v", man.ID, err)
		}
		h.db = db
	}
	return h
}

// Start builds the Host and starts the module; it is stopped when the test ends.
func Start(t testing.TB, m module.Module) *Host {
	t.Helper()
	h := New(t, m)
	if err := m.Start(context.Background(), h); err != nil {
		t.Fatalf("moduletest: start %s: %v", h.man.ID, err)
	}
	t.Cleanup(func() { _ = m.Stop(context.Background()) })
	return h
}

func migrate(db *sql.DB, s *module.Store) error {
	dir := s.Dir
	if dir == "" {
		dir = "."
	}
	entries, err := fs.ReadDir(s.Migrations, dir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		body, err := fs.ReadFile(s.Migrations, path.Join(dir, n))
		if err != nil {
			return err
		}
		if _, err := db.Exec(string(body)); err != nil {
			return fmt.Errorf("%s: %w", n, err)
		}
	}
	return nil
}

// Manifest is the module's manifest.
func (h *Host) Manifest() module.Manifest { return h.man }

// Tool calls one of the module's tools by name with a JSON input.
func (h *Host) Tool(name, input string) (any, error) {
	return h.ToolAs("", name, input)
}

// ToolAs calls a tool as an actor would: the agent, a reflex, an outside client, the owner.
// "" is no one in particular, as Tool.
func (h *Host) ToolAs(actor module.Actor, name, input string) (any, error) {
	ctx := context.Background()
	if actor != "" {
		ctx = module.WithActor(ctx, actor)
	}
	for _, t := range h.man.Tools {
		if t.Name == name {
			if input == "" {
				input = "{}"
			}
			return t.Run(ctx, h, json.RawMessage(input))
		}
	}
	h.t.Fatalf("moduletest: no tool %q", name)
	return nil, nil
}

// OnCommand answers RunCommand for a command the module does not have (a core one).
func (h *Host) OnCommand(id string, fn func(ctx context.Context, args map[string]any) (any, error)) {
	h.mu.Lock()
	h.commands[id] = fn
	h.mu.Unlock()
}

// ServeHTTP serves the module's routes, so a test can put the host behind httptest.NewServer
// (a WebSocket, a stream: what a recorder cannot carry).
func (h *Host) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.mux.ServeHTTP(w, r) }

// Do calls one of the module's routes. p is relative to the module's prefix ("/items").
// It returns the status and the body.
func (h *Host) Do(method, p, body string) (int, string) {
	req := httptest.NewRequest(method, "/api/m/"+h.man.ID+p, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// The module.Host methods.

func (h *Host) ModuleID() string { return h.man.ID }
func (h *Host) Log() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
func (h *Host) Desk() module.Desk     { return h.DeskState }
func (h *Host) Store() *sql.DB        { return h.db }
func (h *Host) DataDir() string       { return h.dir }
func (h *Host) Config() module.Config { return h.Settings }
func (h *Host) Events() module.Events { return h.Bus }
func (h *Host) Marks() module.Marks   { return h.MarkStore }
func (h *Host) Cards() module.Cards   { return h.CardStore }

func (h *Host) Handle(pattern string, handler http.Handler) {
	method, p, ok := strings.Cut(pattern, " ")
	if !ok {
		method, p = "", pattern
	}
	full := "/api/m/" + h.man.ID + "/" + strings.TrimPrefix(p, "/")
	if method != "" {
		full = method + " " + full
	}
	h.mux.Handle(full, handler)
}

func (h *Host) RunCommand(ctx context.Context, id string, args map[string]any) (any, error) {
	h.mu.Lock()
	fn := h.commands[id]
	h.mu.Unlock()
	if fn != nil {
		return fn(ctx, args)
	}
	for _, c := range h.man.Commands {
		if c.ID == id {
			return c.Run(ctx, h, args)
		}
	}
	return nil, fmt.Errorf("moduletest: no command %q (give it with OnCommand)", id)
}

// Bus is an in-memory event bus that also keeps what was published.
type Bus struct {
	mu   sync.Mutex
	subs []sub
	n    int
	// Published is every event so far, oldest first.
	Published []module.Event
}

type sub struct {
	id     int
	prefix string
	fn     func(module.Event)
}

func (b *Bus) Publish(topic string, data any) {
	raw, _ := json.Marshal(data)
	ev := module.Event{Topic: topic, Data: raw}
	b.mu.Lock()
	b.Published = append(b.Published, ev)
	subs := slices.Clone(b.subs)
	b.mu.Unlock()
	for _, s := range subs {
		if s.prefix == "" || topic == s.prefix || strings.HasPrefix(topic, s.prefix+".") {
			s.fn(ev)
		}
	}
}

func (b *Bus) Subscribe(prefix string, fn func(module.Event)) func() {
	b.mu.Lock()
	b.n++
	id := b.n
	b.subs = append(b.subs, sub{id, prefix, fn})
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		b.subs = slices.DeleteFunc(b.subs, func(s sub) bool { return s.id == id })
		b.mu.Unlock()
	}
}

// Topics lists the topics published so far.
func (b *Bus) Topics() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.Published))
	for i, e := range b.Published {
		out[i] = e.Topic
	}
	return out
}

// Config is the module's live settings, in memory.
type Config struct {
	mu     sync.Mutex
	defs   map[string]module.Setting
	values map[string]any
	bus    *Bus
	scope  string
	n      int
	watch  map[int]watcher
}

type watcher struct {
	key string
	fn  func(any)
}

func (c *Config) get(key string) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.values[key]; ok {
		return v
	}
	return c.defs[key].Default
}

func (c *Config) String(key string) string { s, _ := c.get(key).(string); return s }
func (c *Config) Bool(key string) bool     { b, _ := c.get(key).(bool); return b }
func (c *Config) Int(key string) int {
	switch v := c.get(key).(type) {
	case int:
		return v
	case float64:
		return int(v)
	}
	return 0
}

func (c *Config) Set(_ context.Context, key string, value any) error {
	if _, ok := c.defs[key]; !ok {
		return fmt.Errorf("no setting %q", key)
	}
	c.mu.Lock()
	c.values[key] = value
	var fns []func(any)
	for _, w := range c.watch {
		if w.key == key {
			fns = append(fns, w.fn)
		}
	}
	c.mu.Unlock()
	c.bus.Publish("config.changed", map[string]any{"key": c.scope + "." + key, "value": value})
	for _, fn := range fns {
		fn(value)
	}
	return nil
}

func (c *Config) Watch(key string, fn func(any)) func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.watch == nil {
		c.watch = map[int]watcher{}
	}
	c.n++
	id := c.n
	c.watch[id] = watcher{key, fn}
	return func() {
		c.mu.Lock()
		delete(c.watch, id)
		c.mu.Unlock()
	}
}

// Marks are the collaboration marks, in memory.
type Marks struct {
	mu   sync.Mutex
	list []module.Mark
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func (m *Marks) Add(_ context.Context, mk module.Mark) (module.Mark, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	mk.ID = fmt.Sprintf("mk_%d", len(m.list)+1)
	if mk.Status == "" {
		mk.Status = "open"
	}
	if mk.Author == "" {
		mk.Author = "owner"
	}
	mk.Created = now()
	m.list = append(m.list, mk)
	return mk, nil
}

func (m *Marks) List(_ context.Context, ref string) ([]module.Mark, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []module.Mark
	for _, mk := range m.list {
		if (ref == "" && mk.Status == "open") || mk.Ref == ref {
			out = append(out, mk)
		}
	}
	return out, nil
}

func (m *Marks) Reply(_ context.Context, id string, r module.MarkReply) (module.Mark, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.list {
		if m.list[i].ID == id {
			r.Created = now()
			m.list[i].Replies = append(m.list[i].Replies, r)
			return m.list[i], nil
		}
	}
	return module.Mark{}, fmt.Errorf("no mark %q", id)
}

func (m *Marks) Resolve(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.list {
		if m.list[i].ID == id {
			m.list[i].Status = "resolved"
			return nil
		}
	}
	return fmt.Errorf("no mark %q", id)
}

// checkDefault is the app's rule for a setting's default (core/internal/config.Store.Declare):
// it must be a value of the setting's type, so a text setting needs Default: "".
func checkDefault(st module.Setting) error {
	v := st.Default
	switch st.Type {
	case module.SettingBool:
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("want true or false, not %#v", v)
		}
	case module.SettingInt:
		switch n := v.(type) {
		case int, int64:
		case float64:
			if n != float64(int(n)) {
				return fmt.Errorf("want a whole number, not %v", n)
			}
		default:
			return fmt.Errorf("want a whole number, not %#v", v)
		}
	case module.SettingEnum:
		str, _ := v.(string)
		if !slices.Contains(st.Options, str) {
			return fmt.Errorf("%#v is not one of the options", v)
		}
	case module.SettingString, "":
		if _, ok := v.(string); !ok {
			return fmt.Errorf("want text (Default: \"\"), not %#v", v)
		}
	}
	return nil
}
