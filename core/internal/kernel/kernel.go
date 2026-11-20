// Package kernel is what the core's own services (the agent, slots, the router) are given:
// the app's shared state, already assembled. It is an interface so those services can be
// built and tested without the whole app.
package kernel

import (
	"context"
	"database/sql"
	"encoding/json"
	"io/fs"
	"log/slog"

	"github.com/hollis-labs/surface/module"
)

// Kernel is the assembled app, as a core service sees it.
type Kernel interface {
	AppName() string
	// DataDir is the app's data directory. It exists.
	DataDir() string
	// DB is the core database. A service adds its own tables with storage.Migrate under
	// its own migration set.
	DB() *sql.DB
	Log() *slog.Logger

	Desk() module.Desk
	Events() module.Events
	Marks() module.Marks
	Cards() module.Cards
	// Config is the live settings of one scope ("agent", or a module id).
	Config(scope string) module.Config

	// Tools is every tool in the app, bound to the Host of the module that brought it:
	// each module's own, and the desk's (desk_*, window_context, marks_*, card_*).
	Tools() []BoundTool
	// Slots is every module's context slots, bound likewise.
	Slots() []BoundSlot
	// Arounds is every module's Around, bound likewise, in module order.
	Arounds() []BoundAround
	// Profiles is every module's agent profile (Manifest.Profile), in module order.
	Profiles() []BoundProfile
	// Situation is what is happening now: the window in front, the open windows, the
	// user's mode and the layout in use.
	Situation(ctx context.Context) module.Situation

	RunCommand(ctx context.Context, id string, args map[string]any) (any, error)
	Commands() []module.Command
}

// BoundTool is a tool with the module that brought it and a call already bound to that
// module's Host.
type BoundTool struct {
	module.Tool
	Module string
	Call   func(ctx context.Context, input json.RawMessage) (any, error)
}

// BoundProfile is a module's agent profile: the module and its title, and the profile's
// files (a Nanite agent directory without host.yaml or .mcp.json).
type BoundProfile struct {
	Module string
	Title  string
	Files  fs.FS
}

// BoundAround is a module's Around bound to its Host.
type BoundAround struct {
	Module string
	Read   func(ctx context.Context) (string, error)
}

// Asker is what takes the modules' errands for the agent (the agent service, once it runs).
type Asker interface {
	AskFrom(ctx context.Context, module string, a module.Ask) (string, error)
}

// BoundSlot is a slot with the module that brought it and its Knowledge bound to that
// module's Host. Knowledge is nil when the slot has none.
type BoundSlot struct {
	module.Slot
	Module    string
	Knowledge func(ctx context.Context, s module.Situation) (string, error)
}
