package module

import (
	"context"
	"encoding/json"
	"io/fs"
)

// View is something that opens in a window.
type View struct {
	// ID is unique in the app. Use the module id for its main view and "<module>.<name>"
	// for the others.
	ID    string `json:"id"`
	Title string `json:"title"`
	// Icon is a lucide icon name ("sticky-note").
	Icon string `json:"icon,omitempty"`
	// Width and Height are the size a floating window opens at, in CSS pixels.
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
	// Menu lists the view under Apps in the launcher and gives it an "Open <title>" command.
	Menu bool `json:"menu,omitempty"`
	// Multi allows several windows of this view. Opening it again makes a new window unless
	// one was opened with the same key, which is brought forward instead.
	Multi bool `json:"multi,omitempty"`
	// DefaultKey is the key a Multi window gets when opened with none, so a plain "open"
	// brings one main window forward.
	DefaultKey string `json:"default_key,omitempty"`
	// Tiling says how multiple windows of this module behave when open together: "tile"
	// autotiles windows within the module (e.g. Terminal), while "full" (the default) opens
	// full-frame and replaces the active view.
	Tiling string `json:"tiling,omitempty"`
	// Bare means the body fills the window edge to edge and scrolls itself.
	Bare bool `json:"bare,omitempty"`
	// Render says how the window body is drawn.
	Render Render `json:"render"`
	// Opens are file name patterns ("*.md") this view opens from Files and the palette.
	Opens []string `json:"opens,omitempty"`
	// Describe adds what the module knows about one of its windows to what the desk already
	// knows (the file, the page, the record in front). It is what the agent and a slot mean
	// by "where the owner is". Optional.
	Describe func(ctx context.Context, h Host, w Window) (WindowContext, error) `json:"-"`
}

// RenderKind is how a view's body is drawn.
type RenderKind string

const (
	// RenderComponent is a frontend component compiled into the app, named by Entry: the
	// key the module's web entry exports it under. The MVP's only kind.
	RenderComponent RenderKind = "component"
	// RenderFrame is a page the module serves, shown in a frame; Entry is its path under
	// the module's routes. Reserved for run-time loaded and generated modules.
	RenderFrame RenderKind = "frame"
)

// Render names a view's body.
type Render struct {
	Kind  RenderKind `json:"kind"`
	Entry string     `json:"entry"`
}

// Component is the Render of a compiled-in frontend component.
func Component(entry string) Render { return Render{Kind: RenderComponent, Entry: entry} }

// Command is a named action. It shows in the palette, can have a shortcut, and can be run
// by a view, by the agent (through the desk's own run-command tool) or from outside.
type Command struct {
	// ID is unique in the app: "<module>.<verb>".
	ID      string `json:"id"`
	Title   string `json:"title"`
	Section string `json:"section,omitempty"`
	// Keywords are extra words the palette matches on.
	Keywords string `json:"keywords,omitempty"`
	// Keys is the default shortcut, written like "ctrl+alt+t". The user can change it.
	Keys string `json:"keys,omitempty"`
	// Hidden keeps the command out of the palette list; it can still be run.
	Hidden bool `json:"hidden,omitempty"`
	// Run does it. args is whatever the caller passed, or nil. The returned value, when not
	// nil, is given back to the caller as JSON.
	Run func(ctx context.Context, h Host, args map[string]any) (any, error) `json:"-"`
}

// Level says how much a tool call changes, and so whether the owner is asked first.
type Level string

const (
	// LevelRead looks and changes nothing. Never asks.
	LevelRead Level = "read"
	// LevelAct changes what is on screen and is undone as easily (open a window). Never asks.
	LevelAct Level = "act"
	// LevelWrite changes data or reaches outside the app. Asks; the answer can stand.
	LevelWrite Level = "write"
	// LevelConfirm is its own decision each time (deleting, running a command). Always asks.
	LevelConfirm Level = "confirm"
)

// Tool is something the agent can call. The same tools are offered to outside agents
// through the app's MCP endpoint.
type Tool struct {
	// Name is unique in the app: "<module>_<verb>".
	Name        string `json:"name"`
	Description string `json:"description"`
	// Input is the JSON Schema of the tool's input object.
	Input json.RawMessage `json:"input_schema"`
	// Level defaults to LevelWrite when empty or unknown.
	Level Level `json:"level"`
	// Describe says what one call will do in a plain line, for the permission question and
	// the call's row in the thread. Optional.
	Describe func(input json.RawMessage) string `json:"-"`
	// Run does it. The returned value is what the model is told: a string as is, anything
	// else as JSON. An error is reported to the model as a failed call.
	Run func(ctx context.Context, h Host, input json.RawMessage) (any, error) `json:"-"`
}

// Store declares a module's local store: a SQLite database of its own in the app's data
// directory, opened and migrated by the core before Start and reached with Host.Store.
//
// Remote stores with a cache come later; they will be a second kind here, not a change to
// this one.
type Store struct {
	// Migrations holds numbered SQL files ("0001_init.sql", "0002_tags.sql") in Dir. Each
	// runs once, in name order, inside a transaction.
	Migrations fs.FS `json:"-"`
	// Dir is the directory inside Migrations. Empty means its root.
	Dir string `json:"-"`
}

// SettingType is the type of a setting's value.
type SettingType string

// The setting types.
const (
	SettingString SettingType = "string"
	SettingBool   SettingType = "bool"
	SettingInt    SettingType = "int"
	SettingEnum   SettingType = "enum"
	SettingObject SettingType = "object"
)

// Setting is one live setting of a module. Its full key in the app is "<module>.<Key>".
type Setting struct {
	// Validate checks a coerced value before persistence. It must not read settings.
	Validate func(any) error `json:"-"`
	Key      string          `json:"key"`
	Title    string          `json:"title"`
	About    string          `json:"about,omitempty"`
	Type     SettingType     `json:"type"`
	// Default is the value until the user sets one. It must match Type.
	Default any `json:"default"`
	// Options are the allowed values of a SettingEnum.
	Options []string `json:"options,omitempty"`
	// Owner marks a setting only the owner changes (the page, the CLI, the HTTP API): the
	// agent's settings_set refuses it. For what decides where a credential goes, or which.
	Owner bool `json:"owner,omitempty"`
}

// Slot is what a module feeds the embedded agent when one kind of work is in front, so one
// agent does not have to hold everything at once. The core decides which slots are active
// for a turn (today from the view in front; later from a classifier over input, activity
// and workspace) and assembles the agent's context from them.
type Slot struct {
	// Name is unique in the app: usually the module id.
	Name string `json:"name"`
	// About is the kind of work, as the agent is told it.
	About string `json:"about"`
	// Views are the views whose window in front selects this slot.
	Views []string `json:"views,omitempty"`
	// Modes are user modes that select this slot whatever is in front. Reserved for the
	// classifier; the core matches them when a mode is set.
	Modes []string `json:"modes,omitempty"`
	// Tools are top of mind here, by name. The agent's other tools stay available.
	Tools []string `json:"tools,omitempty"`
	// Instructions are standing guidance for this kind of work.
	Instructions string `json:"instructions,omitempty"`
	// Where says where the state of this kind of work is kept, for finding what a note lacks.
	Where string `json:"where,omitempty"`
	// Knowledge returns what the agent should know right now (the open document's outline,
	// today's items). It is called when the slot becomes active and must be quick. Optional.
	Knowledge func(ctx context.Context, h Host, s Situation) (string, error) `json:"-"`
}

// Situation is what is happening when slots are chosen and filled.
type Situation struct {
	// Focused is the window in front, when there is one.
	Focused *WindowContext `json:"focused,omitempty"`
	// Windows are all the open windows.
	Windows []Window `json:"windows,omitempty"`
	// Mode is the user's mode, when one is set ("writing", "triage").
	Mode string `json:"mode,omitempty"`
	// Layout is the saved layout in use, when there is one.
	Layout string `json:"layout,omitempty"`
	// Input is the user's message, when a turn is being assembled.
	Input string `json:"input,omitempty"`
}
