// Package config is the app's live settings: declared by the core and by modules, kept in
// the app's database, and read at the moment of use. A change is stored, then announced on
// the bus and to watchers; nothing has to restart.
package config

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/hollis-labs/surface/module"
)

// Publisher announces a change. The events bus satisfies it.
type Publisher interface {
	Publish(topic string, data any)
}

// Entry is one setting with its present value, as the settings window lists it.
type Entry struct {
	// Scope is the module id, or "surface" for the core's own.
	Scope string `json:"scope"`
	module.Setting
	// FullKey is "<scope>.<key>".
	FullKey string `json:"full_key"`
	Value   any    `json:"value"`
	// Set is true when the user has set a value; false means Value is the default.
	Set bool `json:"set"`
}

// Store holds the declarations and the values.
type Store struct {
	db  *sql.DB
	pub Publisher

	mu       sync.RWMutex
	decl     map[string]declared // by full key
	values   map[string]any      // by full key; only what the user set
	watchers map[string]map[int]func(any)
	nextW    int
}

type declared struct {
	scope string
	s     module.Setting
}

// Open loads the stored values. The table is created by the core's migrations.
func Open(ctx context.Context, db *sql.DB, pub Publisher) (*Store, error) {
	s := &Store{db: db, pub: pub, decl: map[string]declared{}, values: map[string]any{}, watchers: map[string]map[int]func(any){}}
	rows, err := db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var k, raw string
		if err := rows.Scan(&k, &raw); err != nil {
			return nil, err
		}
		var v any
		if json.Unmarshal([]byte(raw), &v) == nil {
			s.values[k] = v
		}
	}
	return s, rows.Err()
}

// Declare registers a scope's settings. A default that does not fit its type is an error.
func (s *Store) Declare(scope string, settings []module.Setting) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range settings {
		if st.Key == "" {
			return fmt.Errorf("config: %s declares a setting with no key", scope)
		}
		def, err := coerce(st, st.Default)
		if err != nil {
			return fmt.Errorf("config: %s.%s default: %w", scope, st.Key, err)
		}
		st.Default = def
		s.decl[scope+"."+st.Key] = declared{scope: scope, s: st}
	}
	return nil
}

// Get returns the present value of a full key: the user's, or the default.
func (s *Store) Get(fullKey string) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.decl[fullKey]
	if !ok {
		return nil, false
	}
	if v, set := s.values[fullKey]; set {
		if cv, err := coerce(d.s, v); err == nil {
			return cv, true
		}
	}
	return d.s.Default, true
}

// Set validates and stores a value, then tells watchers and the bus ("config.changed").
func (s *Store) Set(ctx context.Context, fullKey string, value any) error {
	s.mu.Lock()
	d, ok := s.decl[fullKey]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("config: no setting %q", fullKey)
	}
	v, err := coerce(d.s, value)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("config: %s: %w", fullKey, err)
	}
	if d.s.Validate != nil {
		if err := d.s.Validate(v); err != nil {
			s.mu.Unlock()
			return fmt.Errorf("config: %s: %w", fullKey, err)
		}
	}
	raw, _ := json.Marshal(v)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, datetime('now'))
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, fullKey, string(raw)); err != nil {
		s.mu.Unlock()
		return err
	}
	s.values[fullKey] = v
	fns := make([]func(any), 0, len(s.watchers[fullKey]))
	for _, fn := range s.watchers[fullKey] {
		fns = append(fns, fn)
	}
	s.mu.Unlock()
	for _, fn := range fns {
		fn(v)
	}
	if s.pub != nil {
		s.pub.Publish("config.changed", map[string]any{"key": fullKey, "value": v})
	}
	return nil
}

// Reset removes the user's value, so the default applies again.
func (s *Store) Reset(ctx context.Context, fullKey string) error {
	s.mu.RLock()
	d, ok := s.decl[fullKey]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("config: no setting %q", fullKey)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM settings WHERE key=?`, fullKey); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.values, fullKey)
	fns := make([]func(any), 0, len(s.watchers[fullKey]))
	for _, fn := range s.watchers[fullKey] {
		fns = append(fns, fn)
	}
	s.mu.Unlock()
	for _, fn := range fns {
		fn(d.s.Default)
	}
	if s.pub != nil {
		s.pub.Publish("config.changed", map[string]any{"key": fullKey, "value": d.s.Default})
	}
	return nil
}

// Watch calls fn after fullKey changes.
func (s *Store) Watch(fullKey string, fn func(any)) (stop func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextW
	s.nextW++
	if s.watchers[fullKey] == nil {
		s.watchers[fullKey] = map[int]func(any){}
	}
	s.watchers[fullKey][id] = fn
	return func() {
		s.mu.Lock()
		delete(s.watchers[fullKey], id)
		s.mu.Unlock()
	}
}

// Entries lists every declared setting with its value, by scope then key.
func (s *Store) Entries() []Entry {
	s.mu.RLock()
	keys := make([]string, 0, len(s.decl))
	for k := range s.decl {
		keys = append(keys, k)
	}
	s.mu.RUnlock()
	sort.Strings(keys)
	out := make([]Entry, 0, len(keys))
	for _, k := range keys {
		s.mu.RLock()
		d := s.decl[k]
		_, set := s.values[k]
		s.mu.RUnlock()
		v, _ := s.Get(k)
		out = append(out, Entry{Scope: d.scope, Setting: d.s, FullKey: k, Value: v, Set: set})
	}
	return out
}

// Scoped is one scope's view of the store. It implements module.Config.
func (s *Store) Scoped(scope string) module.Config { return scoped{s: s, scope: scope} }

type scoped struct {
	s     *Store
	scope string
}

func (c scoped) String(key string) string {
	v, _ := c.s.Get(c.scope + "." + key)
	str, _ := v.(string)
	return str
}

func (c scoped) Bool(key string) bool {
	v, _ := c.s.Get(c.scope + "." + key)
	b, _ := v.(bool)
	return b
}

func (c scoped) Int(key string) int {
	v, _ := c.s.Get(c.scope + "." + key)
	n, _ := v.(int)
	return n
}

func (c scoped) Set(ctx context.Context, key string, value any) error {
	return c.s.Set(ctx, c.scope+"."+key, value)
}

func (c scoped) Watch(key string, fn func(any)) func() { return c.s.Watch(c.scope+"."+key, fn) }

// coerce checks v against the setting's type and returns it in the type's Go form.
func coerce(st module.Setting, v any) (any, error) {
	switch st.Type {
	case module.SettingObject:
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		var object map[string]any
		if err := json.Unmarshal(raw, &object); err != nil || object == nil {
			return nil, errors.New("want an object")
		}
		return object, nil
	case module.SettingBool:
		if b, ok := v.(bool); ok {
			return b, nil
		}
		return nil, errors.New("want true or false")
	case module.SettingInt:
		switch n := v.(type) {
		case int:
			return n, nil
		case int64:
			return int(n), nil
		case float64:
			if n == float64(int(n)) {
				return int(n), nil
			}
		}
		return nil, errors.New("want a whole number")
	case module.SettingEnum:
		str, ok := v.(string)
		if !ok {
			return nil, errors.New("want one of the options")
		}
		for _, o := range st.Options {
			if o == str {
				return str, nil
			}
		}
		return nil, fmt.Errorf("%q is not one of the options", str)
	case module.SettingString, "":
		if str, ok := v.(string); ok {
			return str, nil
		}
		return nil, errors.New("want text")
	}
	return nil, fmt.Errorf("unknown setting type %q", st.Type)
}
