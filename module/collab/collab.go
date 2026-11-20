// Package collab is Caster's "collab" namespace as Basalisk keeps it (contract collab/v1): what
// the owner and the agent work on together, drafts (code, content, plans), artifacts under
// review and diagrams. Each is a document caster://collab/<kind>.<slug>, Markdown with a YAML
// front matter, so every save is a revision and any two can be compared, with its marks beside
// it (caster://collab/<name>.marks, JSON). A mark is something pointed at plus a note.
//
// A helper beside the contract, as tetherlink is: a module that keeps its documents in Caster
// uses this rather than its own client, so Desk and Basalisk read and write the same documents
// the same way. Ported from Basalisk (internal/collab), over module/tetherlink.
package collab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/hollis-labs/surface/module/tetherlink"
)

// Namespace is where the documents live in Caster.
const Namespace = "collab"

// Kinds of document.
const (
	KindDraft    = "draft"
	KindArtifact = "artifact"
	KindDiagram  = "diagram"
)

var nameRe = regexp.MustCompile(`^(draft|artifact|diagram)\.[a-z0-9][a-z0-9-]{0,80}$`)

// ValidName reports whether name is a document's name ("<kind>.<slug>").
func ValidName(name string) bool { return nameRe.MatchString(name) }

// Doc is one document at a revision.
type Doc struct {
	Deleted bool   `json:"deleted,omitempty"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	// Lang is what a draft is written in (markdown, go, typescript, text…).
	Lang    string `json:"lang,omitempty"`
	Project string `json:"project,omitempty"`
	// Ref is what an artifact points at: a file ("file:<root>:<path>"), a web address, a
	// fragment ("fragment:<id>").
	Ref     string `json:"ref,omitempty"`
	Created string `json:"created,omitempty"`
	Rev     int    `json:"rev"`
	Updated string `json:"updated,omitempty"`
	// Body is the text of a draft, the JSON of a diagram, the notes of an artifact.
	Body string `json:"body"`
}

// Anchor is what a mark points at. Which fields are set depends on the document.
type Anchor struct {
	// Text: the passage, with a little of what is around it, and the lines it was on.
	Quote     string `json:"quote,omitempty"`
	Prefix    string `json:"prefix,omitempty"`
	Suffix    string `json:"suffix,omitempty"`
	LineStart int    `json:"line_start,omitempty"`
	LineEnd   int    `json:"line_end,omitempty"`
	// A region of an image, a page or a mock, as fractions of its width and height.
	X    *float64 `json:"x,omitempty"`
	Y    *float64 `json:"y,omitempty"`
	W    *float64 `json:"w,omitempty"`
	H    *float64 `json:"h,omitempty"`
	Page int      `json:"page,omitempty"`
	// A time in a video or a recording, in seconds.
	Time *float64 `json:"time,omitempty"`
	// A node or an edge of a diagram, by id.
	Node string `json:"node,omitempty"`
	Edge string `json:"edge,omitempty"`
	// An element of a mock, by a CSS selector.
	Selector string `json:"selector,omitempty"`
}

// Mark is a note on something pointed at.
type Mark struct {
	ID string `json:"id"`
	// Rev is the document's revision the mark was made on.
	Rev    int    `json:"rev"`
	Anchor Anchor `json:"anchor"`
	// Kind: comment, suggestion (Replace is what to put instead of the quote), question.
	Kind    string `json:"kind"`
	Text    string `json:"text,omitempty"`
	Replace string `json:"replace,omitempty"`
	// By: "user" or "agent".
	By    string `json:"by"`
	State string `json:"state"` // open | done | declined
	At    string `json:"at"`
	Reply string `json:"reply,omitempty"`
}

// Tools are the gateway tools a Store calls: give them to tetherlink.New's Allowed. Only
// caster_put writes.
var Tools = tetherlink.Allowed{"caster_get": true, "caster_put": true, "caster_ls": true, "caster_log": true}

// Store reads and writes the documents through the Tether gateway. Nothing is kept here. Link
// is asked for the client at each call, so a module's settings are read at the moment of use.
// Source names the writer in Caster's revision log ("surface" by default): "<source>:owner"
// or "<source>:agent".
type Store struct {
	Link   func() (tetherlink.Caller, error)
	Source string
}

func (s *Store) call(ctx context.Context, tool string, args map[string]any) ([]byte, error) {
	c, err := s.Link()
	if err != nil {
		return nil, err
	}
	out, err := c.Call(ctx, tool, args)
	return []byte(out), err
}

func (s *Store) callAll(ctx context.Context, tool string, args map[string]any) ([]byte, error) {
	c, err := s.Link()
	if err != nil {
		return nil, err
	}
	out, err := tetherlink.CallAll(ctx, c, tool, args)
	return []byte(out), err
}

// ErrNotFound means there is no such document; Conflict that it moved on since it was read.
var ErrNotFound = errors.New("no such document")

// Conflict is returned by a write when the document has a newer revision than the edit began
// on, or when someone else wrote between this read and this write (Caster's put takes no
// expected revision, so the revision it answers is checked: both writes stay in its history).
type Conflict struct{ Rev int }

func (c *Conflict) Error() string { return fmt.Sprintf("the document is now at revision %d", c.Rev) }

const maxDoc = 4 << 20

type front struct {
	Deleted bool   `yaml:"deleted,omitempty"`
	Collab  string `yaml:"collab"`
	Kind    string `yaml:"kind"`
	Title   string `yaml:"title"`
	Lang    string `yaml:"lang,omitempty"`
	Project string `yaml:"project,omitempty"`
	Ref     string `yaml:"ref,omitempty"`
	Created string `yaml:"created,omitempty"`
}

func encode(d Doc) string {
	head, _ := yaml.Marshal(front{Deleted: d.Deleted, Collab: "v1", Kind: d.Kind, Title: d.Title, Lang: d.Lang, Project: d.Project, Ref: d.Ref, Created: d.Created})
	return "---\n" + string(head) + "---\n" + d.Body
}

func decode(name, content string) Doc {
	d := Doc{Name: name, Kind: strings.SplitN(name, ".", 2)[0], Body: content}
	rest, ok := strings.CutPrefix(content, "---\n")
	if !ok {
		return d
	}
	head, body, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		return d
	}
	var f front
	if yaml.Unmarshal([]byte(head), &f) != nil {
		return d
	}
	d.Deleted = f.Deleted
	d.Title, d.Lang, d.Project, d.Ref, d.Created, d.Body = f.Title, f.Lang, f.Project, f.Ref, f.Created, body
	return d
}

func (s *Store) get(ctx context.Context, name string, rev int) (content, title string, at int, ok bool, err error) {
	ref := "caster://" + Namespace + "/" + name
	if rev > 0 {
		ref += "@" + strconv.Itoa(rev)
	}
	raw, err := s.call(ctx, "caster_get", map[string]any{"ref": ref, "max_bytes": maxDoc})
	if err != nil {
		return "", "", 0, false, err
	}
	var out struct {
		Content   string `json:"content"`
		State     string `json:"state"`
		Revision  any    `json:"revision"`
		Title     string `json:"title"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", 0, false, fmt.Errorf("caster_get: %w", err)
	}
	switch {
	case out.State == "not_found":
		return "", "", 0, false, nil
	case out.State != "resolved":
		return "", "", 0, false, fmt.Errorf("caster_get %s: %s", ref, out.State)
	case out.Truncated:
		return "", "", 0, false, fmt.Errorf("%s is larger than %d MB", name, maxDoc>>20)
	}
	at, _ = strconv.Atoi(fmt.Sprint(out.Revision))
	return out.Content, out.Title, at, true, nil
}

// Tags are how a document is marked in Caster's listing, so a list needs no read of each
// document: "collab", its kind and language, when it was made, and "deleted" when hidden.
// Caster keeps the last tags given when a put gives none, so every put here gives them all.
func tags(d Doc) string {
	t := []string{"collab", "kind:" + d.Kind}
	if d.Lang != "" {
		t = append(t, "lang:"+d.Lang)
	}
	if d.Created != "" && !strings.ContainsAny(d.Created, " \t\n") {
		t = append(t, "created:"+d.Created)
	}
	if d.Deleted {
		t = append(t, "deleted")
	}
	return strings.Join(t, " ")
}

// actor is who a write is recorded as: "agent", or "owner" for anyone else.
func actor(by string) string {
	if by == "agent" {
		return "agent"
	}
	return "owner"
}

func (s *Store) put(ctx context.Context, name, content, title, mime, tags, by string) (int, error) {
	source := s.Source
	if source == "" {
		source = "surface"
	}
	args := map[string]any{
		"name": Namespace + "/" + name, "content": content, "mime": mime, "title": title, "source": source + ":" + actor(by),
	}
	if tags != "" {
		args["tags"] = tags
	}
	raw, err := s.call(ctx, "caster_put", args)
	if err != nil {
		return 0, err
	}
	var out struct {
		Ref string `json:"ref"`
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return 0, fmt.Errorf("caster_put: %w", err)
	}
	_, at, ok := strings.Cut(out.Ref, "@")
	rev, err := strconv.Atoi(at)
	if !ok || err != nil {
		return 0, errors.New("caster_put did not say the new revision")
	}
	return rev, nil
}

// putOver writes a document over the revision cur was read at and checks what Caster answered:
// the same revision is an unchanged text (Caster keeps no new revision for it), the next one is
// this write; anything else means someone else wrote in between, and both are in its history.
func (s *Store) putOver(ctx context.Context, cur int, d Doc, by string) (Doc, error) {
	rev, err := s.put(ctx, d.Name, encode(d), d.Title, "text/markdown", tags(d), by)
	if err != nil {
		return Doc{}, err
	}
	d.Rev = rev
	if rev != cur && rev != cur+1 {
		return d, &Conflict{Rev: rev}
	}
	return d, nil
}

// probeBytes is how much of a document is read to see its front matter, for one listed without
// this package's tags (written by an older Basalisk, say).
const probeBytes = 1024

// List is the documents of a kind ("" for all), most recently changed first, without bodies.
// It is one caster_ls: a document written here carries its kind, language, creation and
// whether it is hidden as tags. One without them has its front matter read (a small read); one
// that cannot be read is left out and logged, and the rest are listed.
func (s *Store) List(ctx context.Context, kind string) ([]Doc, error) {
	raw, err := s.callAll(ctx, "caster_ls", map[string]any{"ns": Namespace})
	if err != nil {
		return nil, err
	}
	var out struct {
		Items []struct {
			Ref     string `json:"ref"`
			Rev     int    `json:"rev"`
			Title   string `json:"title"`
			Tags    any    `json:"tags"`
			Updated string `json:"updated"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("caster_ls: %w", err)
	}
	docs := []Doc{}
	for _, it := range out.Items {
		name, ok := strings.CutPrefix(it.Ref, "caster://"+Namespace+"/")
		if !ok || !ValidName(name) {
			continue
		}
		k := strings.SplitN(name, ".", 2)[0]
		if kind != "" && k != kind {
			continue
		}
		d := Doc{Name: name, Kind: k, Title: it.Title, Rev: it.Rev, Updated: it.Updated}
		t := tagSet(it.Tags)
		if t["collab"] {
			d.Deleted = t["deleted"]
			for tag := range t {
				if v, ok := strings.CutPrefix(tag, "lang:"); ok {
					d.Lang = v
				}
				if v, ok := strings.CutPrefix(tag, "created:"); ok {
					d.Created = v
				}
			}
		} else {
			head, perr := s.probe(ctx, name)
			if perr != nil {
				slog.Warn("collab: a document could not be read for the list; it is left out", "name", name, "err", perr)
				continue
			}
			d.Deleted, d.Lang, d.Created = head.Deleted, head.Lang, head.Created
			if head.Title != "" {
				d.Title = head.Title
			}
		}
		if !d.Deleted {
			docs = append(docs, d)
		}
	}
	sort.SliceStable(docs, func(i, j int) bool { return docs[i].Updated > docs[j].Updated })
	return docs, nil
}

// tagSet reads caster_ls's tags: a space-separated string (or a list).
func tagSet(v any) map[string]bool {
	out := map[string]bool{}
	switch t := v.(type) {
	case string:
		for _, w := range strings.Fields(t) {
			out[w] = true
		}
	case []any:
		for _, w := range t {
			if s, ok := w.(string); ok {
				out[s] = true
			}
		}
	}
	return out
}

// probe reads a document's front matter only.
func (s *Store) probe(ctx context.Context, name string) (Doc, error) {
	raw, err := s.call(ctx, "caster_get", map[string]any{"ref": "caster://" + Namespace + "/" + name, "max_bytes": probeBytes})
	if err != nil {
		return Doc{}, err
	}
	var out struct {
		Content string `json:"content"`
		State   string `json:"state"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.State != "resolved" {
		return Doc{}, fmt.Errorf("caster_get %s: %s", name, out.State)
	}
	return decode(name, out.Content), nil
}

// read is a document at a revision (0 = latest), without when it was written.
func (s *Store) read(ctx context.Context, name string, rev int) (Doc, error) {
	if !ValidName(name) {
		return Doc{}, ErrNotFound
	}
	content, title, at, ok, err := s.get(ctx, name, rev)
	if err != nil {
		return Doc{}, err
	}
	if !ok {
		return Doc{}, ErrNotFound
	}
	d := decode(name, content)
	if d.Title == "" {
		d.Title = title
	}
	d.Rev = at
	return d, nil
}

// Get is a document at a revision (0 = latest), with when that revision was written (from
// caster_log, which caster_get does not say).
func (s *Store) Get(ctx context.Context, name string, rev int) (Doc, error) {
	d, err := s.read(ctx, name, rev)
	if err != nil {
		return Doc{}, err
	}
	if log, lerr := s.Log(ctx, name); lerr == nil {
		for _, r := range log {
			if r.Rev == d.Rev {
				d.Updated = r.At
				break
			}
		}
	}
	return d, nil
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// Create makes a new document and returns it. The name is made from the title: the slug, then
// slug-2 … slug-200 when it is taken. A name someone else took between the check and the write
// (Caster then answers a later revision than 1) is given up for the next.
func (s *Store) Create(ctx context.Context, d Doc, by string) (Doc, error) {
	if d.Kind != KindDraft && d.Kind != KindArtifact && d.Kind != KindDiagram {
		return Doc{}, errors.New("a document is a draft, an artifact or a diagram")
	}
	d.Title = strings.TrimSpace(d.Title)
	if d.Title == "" {
		d.Title = "Untitled"
	}
	slug := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(d.Title), "-"), "-")
	if len(slug) > 48 {
		slug = strings.Trim(slug[:48], "-")
	}
	if slug == "" {
		slug = d.Kind
	}
	d.Created = time.Now().UTC().Format(time.RFC3339)
	base := d.Kind + "." + slug
	if !ValidName(base) {
		return Doc{}, errors.New("that title does not make a usable name")
	}
	for n := 1; n <= maxNames; n++ {
		d.Name = base
		if n > 1 {
			d.Name = base + "-" + strconv.Itoa(n)
		}
		_, _, _, taken, err := s.get(ctx, d.Name, 0)
		if err != nil {
			return Doc{}, err
		}
		if taken {
			continue
		}
		made, err := s.putOver(ctx, 0, d, by)
		var conflict *Conflict
		if errors.As(err, &conflict) {
			continue // taken meanwhile: its own text is revision 1, ours a later one
		}
		return made, err
	}
	return Doc{}, fmt.Errorf("%s and %d more names like it are taken", base, maxNames-1)
}

// maxNames is how many names Create tries for one title.
const maxNames = 200

// Save writes a new revision of a document's body (and title, when given). base is the
// revision the edit began on: when the document has moved on since, nothing is written and
// a *Conflict says where it is now. base 0 is the revision read just before the write; a
// write by someone else between the two is still caught (putOver).
func (s *Store) Save(ctx context.Context, name string, base int, title *string, body string, by string) (Doc, error) {
	return s.Update(ctx, name, base, func(d *Doc) {
		if title != nil && strings.TrimSpace(*title) != "" {
			d.Title = strings.TrimSpace(*title)
		}
		d.Body = body
	}, by)
}

// Revision is one entry of a document's history.
type Revision struct {
	Rev   int    `json:"rev"`
	At    string `json:"at"`
	Title string `json:"title"`
}

// Log is a document's revisions, newest first.
func (s *Store) Log(ctx context.Context, name string) ([]Revision, error) {
	if !ValidName(name) {
		return nil, ErrNotFound
	}
	raw, err := s.callAll(ctx, "caster_log", map[string]any{"name": Namespace + "/" + name})
	if err != nil {
		return nil, err
	}
	var out struct {
		Revisions []Revision `json:"revisions"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("caster_log: %w", err)
	}
	return out.Revisions, nil
}

type markFile struct {
	Collab string `json:"collab"`
	Marks  []Mark `json:"marks"`
}

// Marks is a document's marks, oldest first.
func (s *Store) Marks(ctx context.Context, name string) ([]Mark, error) {
	if !ValidName(name) {
		return nil, ErrNotFound
	}
	content, _, _, ok, err := s.get(ctx, name+".marks", 0)
	if err != nil || !ok {
		return []Mark{}, err
	}
	var f markFile
	if json.Unmarshal([]byte(content), &f) != nil || f.Marks == nil {
		return []Mark{}, nil //nolint:nilerr // a marks file that does not read is no marks, as in Basalisk
	}
	return f.Marks, nil
}

// MarkOp changes a document's marks: "add" (the id and time are given here), "update" (the
// fields set in m replace those of the mark with its id) or "remove". The marks are read
// again immediately before writing, so two writers do not undo each other's.
func (s *Store) MarkOp(ctx context.Context, name, op string, m Mark) ([]Mark, error) {
	marks, err := s.Marks(ctx, name)
	if err != nil {
		return nil, err
	}
	switch op {
	case "add":
		if m.By != "agent" {
			m.By = "user"
		}
		switch m.Kind {
		case "comment", "suggestion", "question":
		default:
			m.Kind = "comment"
		}
		if strings.TrimSpace(m.Text) == "" && m.Kind != "suggestion" {
			return nil, errors.New("a mark needs a note")
		}
		m.ID = fmt.Sprintf("m%x", time.Now().UnixNano()/1000)
		m.At, m.State, m.Reply = time.Now().UTC().Format(time.RFC3339), "open", ""
		marks = append(marks, m)
	case "update":
		found := false
		for i := range marks {
			if marks[i].ID != m.ID {
				continue
			}
			found = true
			switch m.State {
			case "open", "done", "declined":
				marks[i].State = m.State
			}
			if m.Text != "" {
				marks[i].Text = m.Text
			}
			if m.Reply != "" {
				marks[i].Reply = m.Reply
			}
			if m.Replace != "" {
				marks[i].Replace = m.Replace
			}
		}
		if !found {
			return nil, ErrNotFound
		}
	case "remove":
		kept := marks[:0]
		for _, x := range marks {
			if x.ID != m.ID {
				kept = append(kept, x)
			}
		}
		marks = kept
	default:
		return nil, errors.New("a mark is added, updated or removed")
	}
	data, _ := json.MarshalIndent(markFile{Collab: "v1", Marks: marks}, "", " ")
	if _, err := s.put(ctx, name+".marks", string(data), "Marks on "+name, "application/json", "collab marks", m.By); err != nil {
		return nil, err
	}
	return marks, nil
}

// SetDeleted hides or restores a document without erasing revisions or marks: in its front
// matter, as Basalisk does, and as the tag "deleted", so a list sees it without reading it. by
// is who did it.
func (s *Store) SetDeleted(ctx context.Context, name string, base int, deleted bool, by string) (Doc, error) {
	if base < 1 {
		return Doc{}, errors.New("base_rev is required")
	}
	return s.Update(ctx, name, base, func(d *Doc) { d.Deleted = deleted }, by)
}

// Update writes a new revision of a document after change has edited it (its title, its
// language, its body). base is the revision the edit began on, as for Save.
func (s *Store) Update(ctx context.Context, name string, base int, change func(*Doc), by string) (Doc, error) {
	cur, err := s.read(ctx, name, 0)
	if err != nil {
		return Doc{}, err
	}
	if base > 0 && cur.Rev != base {
		return cur, &Conflict{Rev: cur.Rev}
	}
	read := cur.Rev
	change(&cur)
	return s.putOver(ctx, read, cur, by)
}
