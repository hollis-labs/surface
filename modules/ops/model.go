package ops

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// The ops/v1 contract: one workstream per document, caster://ops/ws.<id>, YAML front matter
// and a Markdown body; the queue is caster://ops/queue. README.md has the whole format.

// Contract is the value of a workstream's `ops:` key.
const Contract = "v1"

// The owner's six lifecycle stages, plus the separate discarded state.
const (
	StagePlanned  = "planned"
	StageParked   = "parked"
	StageQueued   = "queued"
	StageInFlight = "in_flight"
	StageLanding  = "landing"
	StageComplete = "complete"
	StageDropped  = "dropped"
)

// Stages are the lifecycle in board order, followed by the optional discarded state.
var Stages = []string{StagePlanned, StageParked, StageQueued, StageInFlight, StageLanding, StageComplete, StageDropped}

// StageLabel is a stage as a person reads it.
var StageLabel = map[string]string{
	StagePlanned: "Planned", StageParked: "Parked", StageQueued: "Queued", StageInFlight: "In Flight",
	StageLanding: "Landing", StageComplete: "Complete", StageDropped: "Dropped",
}

// ValidStage reports whether s is a current stage accepted for new writes.
func ValidStage(s string) bool { _, ok := StageLabel[s]; return ok }

// ReadStage maps legacy ops/v1 and tracker vocabulary without changing the document.
// Unknown values remain unknown so migration callers can flag them for review.
func ReadStage(s string) string {
	switch s {
	case "planning":
		return StagePlanned
	case "flight", "in flight":
		return StageInFlight
	case "review", "in_review", "adoption":
		return StageLanding
	case "active":
		return StageComplete
	default:
		return s
	}
}

func stageRank(s string) int {
	for i, x := range Stages {
		if x == s {
			return i
		}
	}
	return -1
}

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]$`)

// ValidID reports whether id can name a workstream: a slug of 3 to 64 letters, digits and
// dashes, not ending in a dash (the page's Cite detector drops a trailing dash as
// punctuation). It is also the Registry workstream key (1:1), so it is never re-keyed.
func ValidID(id string) bool { return idRe.MatchString(id) }

// maxDepth bounds nesting: a concept, its workstreams, theirs, and so on.
const maxDepth = 6

// Held marks a workstream blocked, in any lane. It is a flag, not a stage.
type Held struct {
	Reason string `yaml:"reason" json:"reason"`
	Since  string `yaml:"since,omitempty" json:"since,omitempty"`
}

// Check is one line of a workstream's definition of "active".
type Check struct {
	Text string `yaml:"text" json:"text"`
	Done bool   `yaml:"done" json:"done"`
	Ref  string `yaml:"ref,omitempty" json:"ref,omitempty"`
}

// Link is a typed link: Kind is a Cite kind, so every link is a chip.
type Link struct {
	Kind  string `yaml:"kind" json:"kind"`
	ID    string `yaml:"id" json:"id"`
	Role  string `yaml:"role,omitempty" json:"role,omitempty"`
	Label string `yaml:"label,omitempty" json:"label,omitempty"`
}

// Correlation is how a workstream answers to things that do not link to it.
type Correlation struct {
	Labels            []string `yaml:"labels,omitempty" json:"labels,omitempty"`
	TetherWorkstreams []string `yaml:"tether_workstreams,omitempty" json:"tether_workstreams,omitempty"`
}

// Transition is one lifecycle move.
type Transition struct {
	From string `yaml:"from" json:"from"`
	To   string `yaml:"to" json:"to"`
	At   string `yaml:"at" json:"at"`
	By   string `yaml:"by,omitempty" json:"by,omitempty"`
	Why  string `yaml:"why,omitempty" json:"why,omitempty"`
}

// maxTransitions is how many moves a document keeps; Caster's log is the full record.
const maxTransitions = 50

// Workstream is one ops/v1 document as the app reads it.
type Workstream struct {
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Owner       string       `json:"owner,omitempty"`
	Project     string       `json:"project,omitempty"`
	StatusNS    string       `json:"status_ns,omitempty"`
	Stage       string       `json:"stage"`
	StageSince  string       `json:"stage_since,omitempty"`
	Parent      string       `json:"parent,omitempty"`
	Held        *Held        `json:"held,omitempty"`
	ActiveWhen  []Check      `json:"active_when"`
	Links       []Link       `json:"links"`
	Correlation Correlation  `json:"correlation"`
	Transitions []Transition `json:"transitions"`
	Purpose     string       `json:"purpose,omitempty"`
	ActiveMeans string       `json:"active_means,omitempty"`
	Notes       string       `json:"notes,omitempty"`

	Rev     int    `json:"rev"`
	Updated string `json:"updated,omitempty"`
	// Children are the workstreams whose parent this is (from the copy, not the document).
	Children []string `json:"children"`
	// Problem says why the document could not be read whole.
	Problem string `json:"problem,omitempty"`
}

// WSName is a workstream's document name in the namespace.
func WSName(id string) string { return "ws." + id }

// QueueName is the queue's document name.
const QueueName = "queue"

// Ref is a workstream as text: ops:<id>, the Cite form.
func Ref(id string) string { return "ops:" + id }

// Body sections, in the order they are written.
var bodyOrder = []string{"Purpose", "Active means", "Notes"}

// parseWorkstream reads a document. A document that is not ops/v1 still reads, with a problem.
func parseWorkstream(id, content string, rev int) Workstream {
	w := Workstream{ID: id, Rev: rev, ActiveWhen: []Check{}, Links: []Link{}, Transitions: []Transition{}, Children: []string{}}
	d, err := ParseDoc(content)
	if err != nil {
		w.Title, w.Stage, w.Problem = id, StagePlanned, err.Error()
		return w
	}
	fillFrom(&w, d)
	return w
}

func fillFrom(w *Workstream, d *Doc) {
	if v := d.Str("ops"); v != Contract {
		w.Problem = fmt.Sprintf("not an ops/v1 document (ops: %q)", v)
	}
	w.Title = d.Str("title")
	if w.Title == "" {
		w.Title = w.ID
	}
	w.Owner, w.Project, w.StatusNS = d.Str("owner"), d.Str("project"), d.Str("status_ns")
	w.Stage, w.StageSince, w.Parent = ReadStage(d.Str("stage")), d.Str("stage_since"), d.Str("parent")
	if !ValidStage(w.Stage) {
		w.Problem = strings.TrimSpace(w.Problem + fmt.Sprintf(" unknown stage %q", w.Stage))
	}
	var h Held
	if d.Decode("held", &h) == nil && strings.TrimSpace(h.Reason) != "" {
		w.Held = &h
		if raw := d.Str("stage"); raw == "planning" || raw == StagePlanned {
			w.Stage = StageParked
		}
	}
	_ = d.Decode("active_when", &w.ActiveWhen)
	_ = d.Decode("links", &w.Links)
	_ = d.Decode("correlation", &w.Correlation)
	_ = d.Decode("transitions", &w.Transitions)
	if w.ActiveWhen == nil {
		w.ActiveWhen = []Check{}
	}
	if w.Links == nil {
		w.Links = []Link{}
	}
	if w.Transitions == nil {
		w.Transitions = []Transition{}
	}
	b := ParseBody(d.Body)
	w.Purpose, w.ActiveMeans, w.Notes = b.Get("Purpose"), b.Get("Active means"), b.Get("Notes")
}

// newDoc is a fresh document for w.
func newDoc(w Workstream) (*Doc, error) {
	d, _ := ParseDoc("")
	if err := d.Set("ops", Contract); err != nil {
		return nil, err
	}
	if err := d.Set("id", w.ID); err != nil {
		return nil, err
	}
	for _, kv := range []struct {
		k string
		v any
	}{
		{"title", w.Title}, {"owner", w.Owner}, {"project", w.Project}, {"status_ns", w.StatusNS},
		{"parent", w.Parent}, {"stage", w.Stage}, {"stage_since", w.StageSince},
	} {
		if s, _ := kv.v.(string); s != "" {
			if err := d.Set(kv.k, s); err != nil {
				return nil, err
			}
		}
	}
	if w.Held != nil {
		_ = d.Set("held", w.Held)
	}
	_ = d.Set("active_when", w.ActiveWhen)
	_ = d.Set("links", w.Links)
	if len(w.Correlation.Labels)+len(w.Correlation.TetherWorkstreams) > 0 {
		_ = d.Set("correlation", w.Correlation)
	}
	_ = d.Set("transitions", w.Transitions)
	var b Body
	for _, s := range []struct{ h, t string }{{"Purpose", w.Purpose}, {"Active means", w.ActiveMeans}, {"Notes", w.Notes}} {
		if strings.TrimSpace(s.t) != "" {
			b.Put(s.h, s.t, bodyOrder)
		}
	}
	d.Body = b.String()
	return d, nil
}

// setStr writes a key, or takes it out when v is "".
func setStr(d *Doc, key, v string) error {
	if strings.TrimSpace(v) == "" {
		d.Del(key)
		return nil
	}
	return d.Set(key, v)
}

// putSection sets a body section, keeping the others.
func putSection(d *Doc, heading, text string) {
	b := ParseBody(d.Body)
	b.Put(heading, text, bodyOrder)
	d.Body = b.String()
}

// addTransition records a move on the document (the last maxTransitions are kept).
func addTransition(d *Doc, t Transition) error {
	var ts []Transition
	_ = d.Decode("transitions", &ts)
	ts = append(ts, t)
	if len(ts) > maxTransitions {
		ts = ts[len(ts)-maxTransitions:]
	}
	return d.Set("transitions", ts)
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

// The link kinds a workstream may hold: the Cite kinds, and two that show as text until
// they have a resolver (tether.team, tether.session).
var linkKinds = map[string]*regexp.Regexp{
	"ops.workstream":  idRe,
	"work.item":       regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}/[a-z0-9]{8}$`),
	"torque.task":     regexp.MustCompile(`^CW-\d{8}-\d{4}$`),
	"torque.epic":     regexp.MustCompile(`^EP-\d{8}-\d{4}$`),
	"torque.plan":     regexp.MustCompile(`^(CW|PL)-\d{8}-\d{4}$`),
	"torque.project":  regexp.MustCompile(`^PRJ-\d{8}-\d{4}$`),
	"torque.sprint":   regexp.MustCompile(`^SP-\d{8}-\d{4}$`),
	"registry.record": regexp.MustCompile(`^[a-z][a-z0-9_]{0,40}:[A-Za-z0-9][A-Za-z0-9._/:@+-]{0,400}$`),
	"caster.doc":      regexp.MustCompile(`^caster://[A-Za-z0-9_.-]{1,128}/[A-Za-z0-9_.:@-]{1,256}$`),
	"tesseract.item":  regexp.MustCompile(`^[0-7][0-9A-HJKMNP-TV-Z]{25}$`),
	"tesseract.key":   regexp.MustCompile(`^.{3,400}$`),
	"tether.agent":    regexp.MustCompile(`^msg://[a-z][a-z0-9-]{0,31}/[A-Za-z0-9_.@:/-]{1,300}$`),
	"tether.message":  regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`),
	"tether.team":     regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`),
	"tether.session":  regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`),
	"url":             regexp.MustCompile(`^https?://\S{1,1000}$`),
}

var nsRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}(/[a-z0-9][a-z0-9._-]{0,63}){1,7}$`)

// validNS reports whether s can be a status namespace ("" is none): slash-separated lower-case
// segments, as Tesseract names them (project/<slug>/event/status).
func validNS(s string) bool { return s == "" || nsRe.MatchString(s) }

var roleRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// checkLink says why a link cannot be kept, or nil.
func checkLink(l Link) error {
	re, ok := linkKinds[l.Kind]
	if !ok {
		return fmt.Errorf("%q is not a link kind Ops keeps", l.Kind)
	}
	if !re.MatchString(l.ID) {
		return fmt.Errorf("%q is not a %s id", l.ID, l.Kind)
	}
	if l.Role != "" && !roleRe.MatchString(l.Role) {
		return fmt.Errorf("%q is not a role (lower case, like scope or design)", l.Role)
	}
	if len(l.Label) > 200 {
		return errors.New("a link's label is at most 200 characters")
	}
	return nil
}
