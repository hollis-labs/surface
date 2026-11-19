// Package module is the contract between the Surface core and a module.
//
// A Surface app is the core plus modules, compiled into one binary. A module is primitives
// plus data plus AI: it brings views (what opens in a window), commands (what the palette,
// a shortcut or an agent can run) and agent tools, and may declare a local store, live
// settings and context slots for the embedded agent.
//
// This package is the only part of Surface a module imports. The core lives under
// core/internal and cannot be reached from a module; whatever a module needs from the core
// it asks of its Host. That is the seam: a module that compiles against this package alone
// can later be loaded at run time or served from another process, because a Manifest is
// plain data apart from its handlers.
//
// A module registers itself from an init function, like a database/sql driver:
//
//	func init() { module.Register(New()) }
//
// and an app selects modules by importing them (the generated surface_wiring.go).
package module

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"sync"
)

// Module is one unit of a Surface app.
type Module interface {
	// Manifest says what the module brings. It is read once, at boot, and must not change.
	Manifest() Manifest
	// Start is called once, after the module's store is open and before the app serves.
	// The Host is the module's for its whole life and is safe to keep.
	Start(ctx context.Context, h Host) error
	// Stop is called once at shutdown, in reverse start order.
	Stop(ctx context.Context) error
}

// Manifest is what a module brings to the app.
//
// The MVP contract is Views, Commands and Tools. Store, Settings and Slots are optional.
// Planned additions (not fields yet, so nothing depends on a guess at their shape): data and
// service dependencies, skills, reflexes and triggers, and permissions (what the agent may
// do in the module without asking, put to the user at install).
type Manifest struct {
	// ID names the module: lower case letters, digits and dashes. It prefixes the module's
	// routes (/api/m/<id>/), its settings and its events.
	ID string `json:"id"`
	// Title is the module's name as a person reads it.
	Title string `json:"title"`
	// About is one line on what the module is for.
	About   string `json:"about,omitempty"`
	Version string `json:"version,omitempty"`

	Views    []View    `json:"views,omitempty"`
	Commands []Command `json:"commands,omitempty"`
	Tools    []Tool    `json:"tools,omitempty"`

	// Store, when set, gives the module a local SQLite database of its own.
	Store *Store `json:"store,omitempty"`
	// Settings are the module's live settings. They are kept in the app's database and a
	// change reaches the module without a restart (Host.Config).
	Settings []Setting `json:"settings,omitempty"`
	// Slots are what the module feeds the embedded agent's context when its work is in front.
	Slots []Slot `json:"slots,omitempty"`
	// Around is a few lines the agent carries with every message, whatever the slot: what
	// waits on the owner, what came back while they were away. "" adds nothing. It runs on
	// every message, so it reads what the module already holds; the core bounds its time and
	// caps it at 1500 characters. Optional.
	Around func(ctx context.Context, h Host) (string, error) `json:"-"`
}

// Base gives a module no-op Start and Stop, for one that needs neither.
type Base struct{}

// Start does nothing.
func (Base) Start(context.Context, Host) error { return nil }

// Stop does nothing.
func (Base) Stop(context.Context) error { return nil }

var idRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

// ValidID reports whether id can name a module.
func ValidID(id string) bool { return idRe.MatchString(id) }

var (
	regMu    sync.RWMutex
	registry = map[string]Module{}
)

// Register adds a module to the binary. It panics on an invalid or repeated id: both are
// programming errors that must stop the build's first run, not surface later.
func Register(m Module) {
	id := m.Manifest().ID
	if !ValidID(id) {
		panic(fmt.Sprintf("surface/module: invalid module id %q", id))
	}
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := registry[id]; dup {
		panic(fmt.Sprintf("surface/module: module %q registered twice", id))
	}
	registry[id] = m
}

// Lookup returns the registered module with this id.
func Lookup(id string) (Module, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	m, ok := registry[id]
	return m, ok
}

// Registered lists the ids of every module compiled into the binary, sorted.
func Registered() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	ids := make([]string, 0, len(registry))
	for id := range registry {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
