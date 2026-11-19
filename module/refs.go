package module

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

// RefKind is a kind of reference a module resolves: a Torque task id, a fragment, a path in
// one of the owner's places. The page finds references in any text (detection is the page's,
// web/src/cite); when it shows one as a chip, the core asks the module that declared the kind
// what it is (GET /api/cite/resolve) and, when the owner opens it, for the whole record
// (GET /api/cite/record).
//
// Resolving is reading. A Resolve or Record must never write anything, here or anywhere it
// reaches: the page asks for every reference it shows, without the owner doing anything.
type RefKind struct {
	// Kind names the kind in the app: dotted lower case, "<system>.<thing>" ("torque.task",
	// "fragments.fragment"), or one word ("path"). It is the kind id the page's detector uses.
	Kind string `json:"kind"`
	// Label is the kind as a person reads it ("Torque task").
	Label string `json:"label"`
	// ID, when set, is what an id of this kind looks like. An id that does not match is not
	// passed to Resolve or Record: it is not found.
	ID *regexp.Regexp `json:"-"`
	// Ready reports whether the kind can be resolved now (its service is configured). Nil is
	// always. Read the module's settings here, at the moment of use. It must be quick.
	Ready func(ctx context.Context, h Host) bool `json:"-"`
	// Resolve looks up a batch of ids (at most 50) and returns what it found, by id. An id
	// missing from the map was not found. An error fails the whole batch (the service is
	// down), which the page shows as an error it may retry, not as "not found".
	Resolve func(ctx context.Context, h Host, ids []string) (map[string]RefEntity, error) `json:"-"`
	// Record reads one reference whole, with its Body, for the record window. Nil means the
	// kind has no more to show than its preview. ErrRefNotFound when there is no such record.
	Record func(ctx context.Context, h Host, id string) (RefEntity, error) `json:"-"`
}

// ErrRefNotFound is a Record's answer for a reference with nothing behind it (or nothing the
// owner may read: the services do not tell the two apart).
var ErrRefNotFound = errors.New("surface: no such record")

// RefEntity is what a reference stands for, in the one shape every kind is drawn with: the
// chip's label, the preview card, and the record window.
type RefEntity struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	// Description is a short preview: a few lines, plain text.
	Description string     `json:"description,omitempty"`
	Status      *RefStatus `json:"status,omitempty"`
	Fields      []RefField `json:"fields,omitempty"`
	// UpdatedAt is when the thing last changed (RFC 3339), for "updated 3m ago".
	UpdatedAt string `json:"updatedAt,omitempty"`
	// ChipLabel is what the inline chip says once resolved ("Task: Fix the thing"). Empty:
	// what was written.
	ChipLabel string `json:"chipLabel,omitempty"`
	// URL is where the thing lives outside the app, offered as a link on the card.
	URL string `json:"url,omitempty"`
	// Open is what opening the reference does in the app: a command and its arguments ("work.open"
	// with the workspace and item). Nil: the core's record window shows it.
	Open *RefOpen `json:"open,omitempty"`
	// Body is the whole record as Markdown. Only a Record fills it.
	Body string `json:"body,omitempty"`
}

// RefStatus is a record's state as a label and a tone.
type RefStatus struct {
	Label string `json:"label"`
	// Tone is one of neutral, info, success, warning, danger.
	Tone string `json:"tone"`
}

// RefField is one line of the card: a label and a value.
type RefField struct {
	Label string `json:"label"`
	Value string `json:"value"`
	// Mono sets the value in the monospace face (an id, a path).
	Mono bool `json:"mono,omitempty"`
}

// RefOpen is a command the page runs to open a reference in the app's own window.
type RefOpen struct {
	Command string         `json:"command"`
	Args    map[string]any `json:"args,omitempty"`
}

var refKindRe = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z0-9]+)*$`)

// ValidRefKind reports whether s can name a reference kind.
func ValidRefKind(s string) bool { return len(s) <= 64 && refKindRe.MatchString(s) }

// StatusTone is the usual tone of a status word: done is success, blocked is danger, and so on.
// For a module projecting its own records onto RefStatus.
func StatusTone(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "done", "completed", "complete", "merged", "active", "approved", "saved", "published":
		return "success"
	case "doing", "in_progress", "in-progress", "running", "queued", "started", "open":
		return "info"
	case "review", "in_review", "paused", "waiting", "draft", "superseded":
		return "warning"
	case "blocked", "failed", "canceled", "deprecated", "deleted",
		"cancel" + "led": // the British spelling, as some services write it

		return "danger"
	}
	return "neutral"
}
