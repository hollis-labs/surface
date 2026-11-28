package module

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// Host is what the core offers one module. Each module gets its own: its store, its
// settings and its routes are scoped to it.
type Host interface {
	// ModuleID is the id of the module this Host belongs to.
	ModuleID() string
	Log() *slog.Logger
	// Desk is the windows and layouts.
	Desk() Desk
	// Store is the module's local database, or nil when its manifest declares none.
	Store() *sql.DB
	// DataDir is a directory of the module's own inside the app's data directory, for
	// files that are not database rows. It exists.
	DataDir() string
	// Config is the module's live settings.
	Config() Config
	// Events is the app's event bus.
	Events() Events
	// Marks are the collaboration marks: notes pinned to a part of something.
	Marks() Marks
	// Cards are what the app shows the user outside a window: a notice, a question, a
	// request to approve, progress.
	Cards() Cards
	// Handle serves an HTTP handler for the module's views at /api/m/<id>/<pattern>.
	// pattern is a net/http pattern without the prefix ("GET /items/{id}"). Call it from
	// Start; routes cannot be added once the app serves.
	Handle(pattern string, handler http.Handler)
	// RunCommand runs any command in the app by id.
	RunCommand(ctx context.Context, id string, args map[string]any) (any, error)
	// Agent is the app's embedded agent, for a module to hand it an errand of its own.
	Agent() Agent
}

// Agent is the app's embedded agent as a module reaches it.
type Agent interface {
	// Ask hands the agent a turn that did not come from the owner: the module's own errand,
	// such as mail that asks for something. It runs on the agent's conversation, marked as the
	// module's, and returns its run id without waiting for it. ErrAgentBusy while a run is
	// active (try again later); ErrNoAgent when the app's agent cannot take one.
	Ask(ctx context.Context, a Ask) (string, error)
	// Gateway is the model gateway the app's agent config names, read at the moment of the
	// call: for a module that calls a model itself, and only on the owner's action (the
	// Workbench's eval runs). It carries the key file's path, never the key. Only a module
	// whose manifest declares Gateway gets it; otherwise, and when the config names no model,
	// gateway or key file, ErrNoAgent.
	Gateway(ctx context.Context) (Gateway, error)
}

// Gateway is a model gateway as the app's agent config names it: an Anthropic-compatible
// endpoint (requests go to <URL>/v1/messages with the key as a bearer token).
type Gateway struct {
	Model string `json:"model"`
	URL   string `json:"url"`
	// KeyFile is the path of the file that holds the key. Read it when a request is made;
	// never log it or show the key. It is not marshaled.
	KeyFile string `json:"-"`
}

// Ask is an errand for the agent.
type Ask struct {
	// Title names it in a line ("Mail for you: 3 requests").
	Title string `json:"title"`
	// Text is what the agent is asked to do. Write it as the module, not as the owner.
	Text string `json:"text"`
}

var (
	// ErrNoAgent: the app's agent cannot take an errand (none is configured, or it is not on
	// the conversation wire).
	ErrNoAgent = errors.New("surface: the app's agent cannot take an errand")
	// ErrAgentBusy: the agent is on a run; ask again later.
	ErrAgentBusy = errors.New("surface: the agent is busy")
)

// Window is one open window.
type Window struct {
	ID string `json:"id"`
	// View is the id of the view it shows; Module is the module that brought the view.
	View   string `json:"view"`
	Module string `json:"module"`
	Title  string `json:"title"`
	// Key identifies a window of a Multi view (a file path, say).
	Key string `json:"key,omitempty"`
	// Args are what the window was opened with, and what its body keeps there since.
	Args map[string]any `json:"args,omitempty"`
	// State is what the window's body last reported about itself (the selection, the
	// position). Small, and never trusted as more than a report from the page.
	State   map[string]any `json:"state,omitempty"`
	Focused bool           `json:"focused,omitempty"`
	// OpenedBy is who opened the window (an Actor; "" for a window from before actors were
	// kept, which counts as the owner's). Opener is the owner's window that was in front when
	// it opened, where the focus goes back to when it closes. Reason is what the opener said
	// it was for.
	OpenedBy string `json:"opened_by,omitempty"`
	Opener   string `json:"opener,omitempty"`
	Reason   string `json:"reason,omitempty"`
	// Retention is how long a delegate means the window to stay: "transient" (for the task
	// at hand) or "deliverable" (the result, to keep); "" is deliverable.
	Retention string `json:"retention,omitempty"`
	// Kept marks the owner's window: one they opened, or one they took over by working in
	// it. A delegate may not close it.
	Kept bool `json:"kept,omitempty"`
	// Dirty: the owner has unsaved work in it. Composing: the owner has unsent input in it.
	// A module about to change what such a window shows on a delegate's behalf should wait
	// or ask (docs/desk-rules.md, rule 7). Held: a delegate's change is waiting for them.
	Dirty     bool `json:"dirty,omitempty"`
	Composing bool `json:"composing,omitempty"`
	Held      bool `json:"held,omitempty"`
}

// WindowContext is what a window is showing, as far as its module can say. It is small and
// safe to put in front of the agent.
type WindowContext struct {
	Window string `json:"window"`
	View   string `json:"view"`
	Module string `json:"module"`
	Title  string `json:"title"`
	// Kind is the kind of thing in front: "document", "url", "file", "folder", "data",
	// "task", "project", "workspace".
	Kind string `json:"kind,omitempty"`
	// Ref names the thing in front in the module's own terms ("notes:ideas/surface.md").
	// It is also what a Mark is pinned to.
	Ref string `json:"ref,omitempty"`
	URL string `json:"url,omitempty"`
	// Selection is the selected text, when there is one.
	Selection string `json:"selection,omitempty"`
	// Extra is anything else worth knowing. Keep it small.
	Extra map[string]any `json:"extra,omitempty"`
}

// OpenOptions say how a window opens.
type OpenOptions struct {
	Args  map[string]any `json:"args,omitempty"`
	Key   string         `json:"key,omitempty"`
	Title string         `json:"title,omitempty"`
	// Background opens the window without giving it the focus.
	Background bool `json:"background,omitempty"`
	// Reason says what the window is for, for the owner and for whoever reads the desk later.
	Reason string `json:"reason,omitempty"`
	// Retention is "transient" or "deliverable" (the default); see Window.
	Retention string `json:"retention,omitempty"`
}

// Layout is an arrangement of windows the user can come back to: which views are open,
// with what, and how they are placed. Saved under a name, it is a saved view.
type Layout struct {
	Name  string `json:"name"`
	About string `json:"about,omitempty"`
	// Mode is "tile" (the first window on the left, the rest stacked on the right) or
	// "float" (every window floating).
	Mode string `json:"mode"`
	// Ratio is the first window's share of the width when tiling (0.2 to 0.8). Zero keeps it.
	Ratio   float64        `json:"ratio,omitempty"`
	Windows []LayoutWindow `json:"windows"`
	// Setup is whatever goes with the arrangement: the user's mode, the reflexes that are
	// on while it is in use. The core reads the keys it knows ("mode", "reflexes",
	// "instructions"); a module may keep its own under its id.
	Setup map[string]any `json:"setup,omitempty"`
	// Tree is the exact arrangement as the desk captured it (how the windows are split,
	// tabbed together and sized), naming each window by its place in Windows. It is the
	// desk's to read and write; leave it out and the desk arranges Windows by Mode and
	// Ratio, which is all a module composing a layout needs to give.
	Tree json.RawMessage `json:"tree,omitempty"`
}

// LayoutWindow is one window of a Layout.
type LayoutWindow struct {
	View  string         `json:"view"`
	Key   string         `json:"key,omitempty"`
	Title string         `json:"title,omitempty"`
	Args  map[string]any `json:"args,omitempty"`
	// Rect is where a floating window sits, in CSS pixels; nil lets the desk place it.
	Rect *Rect `json:"rect,omitempty"`
	// Floating takes the window out of the tiling. Focus gives it the focus.
	Floating bool `json:"floating,omitempty"`
	Focus    bool `json:"focus,omitempty"`
}

// Rect is a rectangle in CSS pixels.
type Rect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// Desk is the windows and layouts, as a module may use them.
type Desk interface {
	// Open opens a window of a view, or brings the matching one forward.
	Open(ctx context.Context, view string, opts OpenOptions) (Window, error)
	Close(ctx context.Context, window string) error
	Focus(ctx context.Context, window string) error
	SetTitle(ctx context.Context, window, title string) error
	// SetArgs replaces what the window was opened with (the page a browser window is on).
	SetArgs(ctx context.Context, window string, args map[string]any) error
	Windows(ctx context.Context) []Window
	// Focused is the window in front.
	Focused(ctx context.Context) (Window, bool)
	// Context is what a window is showing; the focused one when window is "".
	Context(ctx context.Context, window string) (WindowContext, bool)

	// Arrange replaces the open windows with a layout's. Windows holding unsaved work
	// are kept.
	Arrange(ctx context.Context, l Layout) error
	// Capture returns the present arrangement as a Layout.
	Capture(ctx context.Context) Layout
	// SaveLayout keeps a layout under its name, replacing one of the same name.
	SaveLayout(ctx context.Context, l Layout) error
	Layouts(ctx context.Context) ([]Layout, error)
	Layout(ctx context.Context, name string) (Layout, bool, error)
	DeleteLayout(ctx context.Context, name string) error
}

// Config is a module's live settings. A read always returns the current value: the user's
// when one is set, the manifest's default otherwise. Nothing is cached by the caller, so a
// change takes effect on the next read; Watch is for a module that must act on the change.
type Config interface {
	String(key string) string
	Bool(key string) bool
	Int(key string) int
	// Set stores a value. It is validated against the setting's type.
	Set(ctx context.Context, key string, value any) error
	// Watch calls fn after the setting changes, with its new value. The returned function
	// stops the watch.
	Watch(key string, fn func(value any)) (stop func())
}

// Event is something that happened.
type Event struct {
	// Topic is dotted: "notes.saved", "desk.focus". A module's own topics start with its id.
	Topic string `json:"topic"`
	// Data is small and JSON.
	Data json.RawMessage `json:"data,omitempty"`
}

// Events is the app's event bus. Events also reach the page, where a view can listen for
// them and refresh; that is how a view learns that the agent changed its data.
type Events interface {
	// Publish sends an event. data is marshaled to JSON.
	Publish(topic string, data any)
	// Subscribe calls fn for each event whose topic is prefix or starts with prefix + ".".
	// An empty prefix takes every event. fn must not block.
	Subscribe(prefix string, fn func(Event)) (stop func())
}

// Mark is a note pinned to a part of something: a passage, a line range, a region, a node.
// Marks are how the owner and the agent point at the same thing and talk about it.
type Mark struct {
	ID string `json:"id"`
	// Ref is what the mark is on, as WindowContext.Ref names it.
	Ref string `json:"ref"`
	// Anchor is the part pointed at, in the module's own terms.
	Anchor json.RawMessage `json:"anchor,omitempty"`
	// Quote is the text pointed at, when the anchor is a passage.
	Quote string `json:"quote,omitempty"`
	Body  string `json:"body"`
	// Author is "owner" or "agent".
	Author string `json:"author"`
	// Status is "open" or "resolved".
	Status  string `json:"status"`
	Created string `json:"created"`
	// Replies are the notes added since, oldest first.
	Replies []MarkReply `json:"replies,omitempty"`
}

// MarkReply is one reply on a mark.
type MarkReply struct {
	Body    string `json:"body"`
	Author  string `json:"author"`
	Created string `json:"created"`
}

// Marks are the collaboration marks. They live in the core, so every module's documents
// are marked the same way and one panel can list them all.
type Marks interface {
	Add(ctx context.Context, m Mark) (Mark, error)
	// List returns the marks on a ref, oldest first. An empty ref lists every open mark.
	List(ctx context.Context, ref string) ([]Mark, error)
	Reply(ctx context.Context, id string, r MarkReply) (Mark, error)
	Resolve(ctx context.Context, id string) error
}

// Card is something shown to the user outside a window: a notice, a question, a request to
// approve, progress on work. The sender says what it is (Kind and Payload); the page picks
// how it is drawn and where. What the user answers comes back as its Outcome. The server
// holds the card's state: a card is open until it has a Status.
type Card struct {
	ID string `json:"id"`
	// Kind names the card's shape. The core's own are "info", "error", "confirm", "prompt",
	// "choice", "form", "approval", "progress" and "plan"; a module may show a kind of its
	// own, which the page draws with the renderer registered for it.
	Kind string `json:"kind"`
	// Version is the version of the kind's payload. Empty means "1".
	Version string `json:"version,omitempty"`
	Title   string `json:"title,omitempty"`
	// Source is who showed it: a module's id, or "agent".
	Source string `json:"source,omitempty"`
	// Agent names the outside agent that showed it (a seat, over MCP), for the owner to see
	// who asked; empty for the embedded agent and for modules.
	Agent string `json:"agent,omitempty"`
	// ReplyTo is that agent's handle for routing a late answer back (a seat host's session
	// id). A card with an Agent is not answered to the embedded agent: its host fetches the
	// answer and marks it delivered (POST /api/cards/{id}/delivered).
	ReplyTo string `json:"reply_to,omitempty"`
	// Subject is what the card is about (a run, a file). A new card with the same kind,
	// source and subject as an open one replaces it.
	Subject string `json:"subject,omitempty"`
	// Risk is "low", "medium" or "high", for a card that asks to approve something.
	Risk string `json:"risk,omitempty"`
	// Blocking says the sender is waiting on the answer.
	Blocking bool `json:"blocking,omitempty"`
	// Payload is the kind's own content.
	Payload json.RawMessage `json:"payload,omitempty"`
	// Status is empty while the card is open, then "submitted", "canceled", "expired",
	// "superseded", "failed" or "abandoned" (the agent's run that asked it ended while it
	// waited).
	Status string `json:"status,omitempty"`
	// Late is set when the user answered an agent's question after the agent stopped
	// waiting for it (the card was abandoned or expired). A late answer is taken once; a
	// submitted one is handed to the agent as a message.
	Late bool `json:"late,omitempty"`
	// Delivered is when a late answer reached the agent (RFC 3339). Empty: not yet.
	Delivered string `json:"delivered,omitempty"`
	// Outcome is what the user answered, once they have.
	Outcome *CardOutcome `json:"outcome,omitempty"`
	// Revision counts changes, so a page can tell a newer card from an older one.
	Revision int `json:"revision,omitempty"`
	// SupersededBy is the id of the card that replaced this one.
	SupersededBy string `json:"superseded_by,omitempty"`
	Created      string `json:"created,omitempty"`
	Updated      string `json:"updated,omitempty"`
	Responded    string `json:"responded,omitempty"`
	// ExpiresAt is when an unanswered card lapses (RFC 3339). Empty: it does not.
	ExpiresAt string `json:"expires_at,omitempty"`
}

// CardOutcome is what the user did with a card: answered it ("submitted") or declined
// ("canceled"). The three channels are kept apart because they are different things:
// free-form data (a form's values), answers to questions, and decisions about items.
type CardOutcome struct {
	Status    string         `json:"status"`
	Data      map[string]any `json:"data,omitempty"`
	Answers   []CardAnswer   `json:"answers,omitempty"`
	Decisions []CardDecision `json:"decisions,omitempty"`
}

// CardAnswer is one answer to one question.
type CardAnswer struct {
	QuestionID string `json:"questionId"`
	Value      any    `json:"value"`
	Note       string `json:"note,omitempty"`
}

// CardDecision is one decision about one item ("approve", "reject", "skip").
type CardDecision struct {
	ItemID string `json:"itemId"`
	Action string `json:"action"`
	Note   string `json:"note,omitempty"`
}

// Cards shows cards to the user and reads what they answered.
type Cards interface {
	// Show puts a card in front of the user and returns it with its id.
	Show(ctx context.Context, c Card) (Card, error)
	// Update replaces an open card's payload (progress moving on), and its title when one
	// is given.
	Update(ctx context.Context, id string, title string, payload json.RawMessage) (Card, error)
	// End closes a card from the sender's side: status is "superseded", "expired",
	// "canceled", "failed" or "abandoned". A card already closed is returned as it is.
	End(ctx context.Context, id, status string) (Card, error)
	// Respond records the user's answer. A card that is no longer open refuses it, except
	// an agent's question that was abandoned or expired: that takes one late answer.
	Respond(ctx context.Context, id string, o CardOutcome) (Card, error)
	// Await returns the card once it is closed, or when ctx ends.
	Await(ctx context.Context, id string) (Card, error)
	Get(ctx context.Context, id string) (Card, bool, error)
	// List returns the open cards, oldest first; with all, the closed ones as well
	// (newest 200).
	List(ctx context.Context, all bool) ([]Card, error)
}
