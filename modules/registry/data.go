package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/hollis-labs/surface/module/tetherlink"
)

// errMissing is a record (or revision) the Registry does not have.
var errMissing = errors.New("the Registry has no such record")

// registryCodes are the Registry's error codes. A refused call is reported by its code only:
// what the far side says is not passed on (tetherlink's rule).
var registryCodes = []string{"not_found", "deleted", "validation_error", "unknown_schema", "forbidden", "unavailable", "version_conflict", "reference_unresolved"}

// call calls one read tool and returns its JSON.
func (m *mod) call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	c, err := m.link()
	if err != nil {
		return nil, err
	}
	out, err := c.Call(ctx, tool, args)
	if err != nil {
		var refused *tetherlink.RefusedError
		if errors.As(err, &refused) {
			d := strings.ToLower(refused.Detail)
			if strings.Contains(d, "not_found") || strings.Contains(d, "deleted") {
				return nil, errMissing
			}
			for _, code := range registryCodes {
				if strings.Contains(d, code) {
					return nil, fmt.Errorf("the Registry refused %s (%s)", tool, code)
				}
			}
			return nil, fmt.Errorf("the Registry refused %s", tool)
		}
		if errors.Is(err, tetherlink.ErrRefused) {
			return nil, fmt.Errorf("the Registry refused %s", tool)
		}
		return nil, err
	}
	raw := json.RawMessage(strings.TrimSpace(out))
	if !json.Valid(raw) {
		return nil, fmt.Errorf("the Registry's answer to %s could not be read", tool)
	}
	return raw, nil
}

// Review is a record's review, as the Registry keeps it (tools have one).
type Review struct {
	State          string `json:"state,omitempty"`
	QueueState     string `json:"queue_state,omitempty"`
	DefinitionHash string `json:"definition_hash,omitempty"`
	ProposedBy     string `json:"proposed_by,omitempty"`
	ReviewedBy     string `json:"reviewed_by,omitempty"`
	ReviewedAt     string `json:"reviewed_at,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

// Ref is one end of a relation: {authority, kind, id}. A local one's authority is "registry"
// and its id is the record's canonical id.
type Ref struct {
	Authority string `json:"authority"`
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	URI       string `json:"uri,omitempty"`
}

// Edge is one relation as the Registry returns it.
type Edge struct {
	ID       string `json:"id"`
	Hash     string `json:"hash,omitempty"`
	Relation struct {
		Type   string   `json:"type"`
		From   Ref      `json:"from"`
		To     Ref      `json:"to"`
		Status string   `json:"status,omitempty"`
		Source string   `json:"source,omitempty"`
		Weight *float64 `json:"weight,omitempty"`
	} `json:"relation"`
	Inverse     string `json:"inverse,omitempty"`
	TargetState string `json:"target_state,omitempty"`
	ProposedBy  string `json:"proposed_by,omitempty"`
	ReviewedBy  string `json:"reviewed_by,omitempty"`
}

// brief is a record without its payload: a row of the list, and what the agent is told.
type brief struct {
	ID           string  `json:"id"`
	Kind         string  `json:"kind"`
	Schema       string  `json:"schema"`
	Key          string  `json:"key,omitempty"`
	Name         string  `json:"name"`
	Summary      string  `json:"summary,omitempty"`
	Owner        string  `json:"owner,omitempty"`
	Subtype      string  `json:"subtype,omitempty"`
	Lifecycle    string  `json:"lifecycle,omitempty"`
	Authority    string  `json:"authority,omitempty"`
	Publication  string  `json:"publication,omitempty"`
	UpdatedAt    string  `json:"updated_at,omitempty"`
	RevisionID   string  `json:"revision_id,omitempty"`
	VersionToken string  `json:"version_token,omitempty"`
	Review       *Review `json:"review,omitempty"`
	Edges        int     `json:"edges"`
	// Proposed counts the relations proposed and not yet reviewed.
	Proposed int `json:"proposed,omitempty"`
}

// rawRecord is the part of a record this module reads.
type rawRecord struct {
	brief
	Sequence    int64   `json:"sequence"`
	PublishedAt string  `json:"published_at,omitempty"`
	EdgeList    []Edge  `json:"edges"`
	Review      *Review `json:"review,omitempty"`
}

func (r rawRecord) summary() brief {
	b := r.brief
	b.Review = r.Review
	b.Edges = len(r.EdgeList)
	for _, e := range r.EdgeList {
		if e.Relation.Status == "proposed" {
			b.Proposed++
		}
	}
	if b.Name == "" {
		b.Name = b.ID
	}
	return b
}

// kindOf is the kind a schema names: "project/2" is a project.
func kindOf(schema string) string {
	k, _, _ := strings.Cut(schema, "/")
	return k
}

// Kind is one record kind: its schema versions and how many records it has.
type Kind struct {
	Kind    string   `json:"kind"`
	Schemas []string `json:"schemas"`
	Count   int      `json:"count"`
	// More: there are more records than were counted.
	More bool `json:"more,omitempty"`
}

// Overview is the left of the window: kinds with counts, the relation types, the review queue
// and the diagnostics. A part that failed has its error in Errors and the rest stands.
type Overview struct {
	Publication string            `json:"publication"`
	Kinds       []Kind            `json:"kinds"`
	Edges       json.RawMessage   `json:"edges,omitempty"`
	Reviews     []brief           `json:"reviews"`
	Diagnostics json.RawMessage   `json:"diagnostics,omitempty"`
	Total       int               `json:"total"`
	AsOf        string            `json:"as_of,omitempty"`
	Errors      map[string]string `json:"errors,omitempty"`
}

// countPages bounds the sweep that counts records per kind (200 a page).
const countPages = 12

// nonRecordKinds are schemas of values, not records: no record has them as its kind.
var nonRecordKinds = map[string]bool{"relation": true, "classification": true}

func (m *mod) overview(ctx context.Context, pub string) (*Overview, error) {
	if _, err := m.link(); err != nil {
		return nil, err
	}
	o := &Overview{Publication: pub, Reviews: []brief{}, Kinds: []Kind{}}
	var (
		mu    sync.Mutex
		wg    sync.WaitGroup
		kinds struct {
			Schemas []string        `json:"schemas"`
			Edges   json.RawMessage `json:"edges"`
		}
		counts = map[string]int{}
		more   bool
	)
	fail := func(part string, err error) {
		mu.Lock()
		defer mu.Unlock()
		if o.Errors == nil {
			o.Errors = map[string]string{}
		}
		o.Errors[part] = err.Error()
	}
	wg.Add(4)
	go func() {
		defer wg.Done()
		raw, err := m.call(ctx, "registry_kinds_list", map[string]any{})
		if err == nil {
			err = json.Unmarshal(raw, &kinds)
		}
		if err != nil {
			fail("kinds", err)
		}
	}()
	go func() {
		defer wg.Done()
		raw, err := m.call(ctx, "registry_diagnostics", map[string]any{})
		if err != nil {
			fail("diagnostics", err)
			return
		}
		o.Diagnostics = raw
	}()
	go func() {
		defer wg.Done()
		p, err := m.page(ctx, "registry_review_list", map[string]any{"limit": 200})
		if err != nil {
			fail("reviews", err)
			return
		}
		mu.Lock()
		o.Reviews = p.Records
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		cursor := ""
		for range countPages {
			q := map[string]any{"publication": pub, "limit": 200}
			if cursor != "" {
				q["cursor"] = cursor
			}
			p, err := m.page(ctx, "registry_record_list", q)
			if err != nil {
				fail("counts", err)
				return
			}
			mu.Lock()
			for _, r := range p.Records {
				counts[r.Kind]++
			}
			o.AsOf = p.AsOf
			mu.Unlock()
			if !p.HasMore || p.Cursor == "" {
				return
			}
			cursor = p.Cursor
		}
		more = true
	}()
	wg.Wait()
	o.Edges = kinds.Edges
	byKind := map[string]*Kind{}
	var order []string
	for _, s := range kinds.Schemas {
		k := kindOf(s)
		if nonRecordKinds[k] {
			continue
		}
		if byKind[k] == nil {
			byKind[k] = &Kind{Kind: k}
			order = append(order, k)
		}
		byKind[k].Schemas = append(byKind[k].Schemas, s)
	}
	// A kind with records but no schema listed still shows.
	for k := range counts {
		if byKind[k] == nil {
			byKind[k] = &Kind{Kind: k}
			order = append(order, k)
		}
	}
	sort.Strings(order)
	for _, k := range order {
		kd := byKind[k]
		kd.Count = counts[k]
		kd.More = more
		o.Total += kd.Count
		o.Kinds = append(o.Kinds, *kd)
	}
	if len(o.Errors) == 4 {
		// Nothing answered: the Registry (or the gateway) is down, not empty.
		return nil, errors.New(o.Errors["kinds"])
	}
	return o, nil
}

// Page is one page of records.
type Page struct {
	Records []brief `json:"records"`
	Cursor  string  `json:"cursor,omitempty"`
	HasMore bool    `json:"has_more"`
	AsOf    string  `json:"as_of,omitempty"`
}

func (m *mod) page(ctx context.Context, tool string, query map[string]any) (*Page, error) {
	raw, err := m.call(ctx, tool, map[string]any{"query": query})
	if err != nil {
		return nil, err
	}
	var p struct {
		Records []rawRecord `json:"records"`
		Cursor  string      `json:"cursor"`
		HasMore bool        `json:"has_more"`
		AsOf    string      `json:"as_of"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("the Registry's answer to %s could not be read", tool)
	}
	out := &Page{Records: make([]brief, 0, len(p.Records)), Cursor: p.Cursor, HasMore: p.HasMore, AsOf: p.AsOf}
	for _, r := range p.Records {
		out.Records = append(out.Records, r.summary())
	}
	return out, nil
}

type listQuery struct {
	Publication, Kind, Lifecycle, Owner, ReviewState, Search, Cursor string
}

var (
	kindRe   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,40}$`)
	wordRe   = regexp.MustCompile(`^[A-Za-z0-9*_.:/@-]{1,200}$`)
	cursorRe = regexp.MustCompile(`^[A-Za-z0-9_.:=+/-]+$`)
)

// records lists one page of records: of a kind, filtered by lifecycle, owner and review state,
// or found by a search (registry_record_search).
func (m *mod) records(ctx context.Context, lq listQuery) (*Page, error) {
	q := map[string]any{"publication": lq.Publication, "limit": 100}
	for k, v := range map[string]string{"kind": lq.Kind, "lifecycle": lq.Lifecycle, "owner": lq.Owner, "review_state": lq.ReviewState, "cursor": lq.Cursor} {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		re := wordRe
		switch k {
		case "kind":
			re = kindRe
		case "cursor":
			re = cursorRe
		}
		if len(v) > 4000 || !re.MatchString(v) {
			return nil, fmt.Errorf("%w: %s", errBadInput, k)
		}
		q[k] = v
	}
	tool := "registry_record_list"
	if s := strings.TrimSpace(lq.Search); s != "" {
		if len(s) > 200 {
			return nil, fmt.Errorf("%w: the search is too long", errBadInput)
		}
		q["search"] = s
		tool = "registry_record_search"
	}
	return m.page(ctx, tool, q)
}

// idRe is what a Registry canonical id looks like: kind, a colon, the kind's own id.
var idRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,40}:[A-Za-z0-9][A-Za-z0-9._/:@+-]{0,400}$`)

// Detail is a record whole, with its relations both ways and its history.
type Detail struct {
	Record      json.RawMessage   `json:"record"`
	Brief       brief             `json:"brief"`
	Publication string            `json:"publication"`
	Outgoing    []Edge            `json:"outgoing"`
	Incoming    []Edge            `json:"incoming"`
	Revisions   []Revision        `json:"revisions"`
	MoreHistory bool              `json:"more_history,omitempty"`
	Errors      map[string]string `json:"errors,omitempty"`
}

// Revision is one entry of a record's history.
type Revision struct {
	RevisionID   string `json:"revision_id"`
	VersionToken string `json:"version_token,omitempty"`
	Sequence     int64  `json:"sequence"`
	UpdatedAt    string `json:"updated_at,omitempty"`
	Lifecycle    string `json:"lifecycle,omitempty"`
	Publication  string `json:"publication,omitempty"`
	PublishedAt  string `json:"published_at,omitempty"`
	Name         string `json:"name,omitempty"`
}

func (m *mod) record(ctx context.Context, id, pub string) (*Detail, error) {
	id = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(id), citePrefix))
	if !idRe.MatchString(id) {
		return nil, fmt.Errorf("%w: %q is not a Registry record id", errBadInput, id)
	}
	raw, err := m.call(ctx, "registry_record_get", map[string]any{"id": id, "publication": pub})
	if errors.Is(err, errMissing) && pub == "published" {
		// Not published yet: the working copy is what there is.
		pub = "draft"
		raw, err = m.call(ctx, "registry_record_get", map[string]any{"id": id, "publication": pub})
	}
	if err != nil {
		return nil, err
	}
	var rec rawRecord
	if err := json.Unmarshal(raw, &rec); err != nil || rec.ID == "" {
		return nil, errors.New("the Registry's record could not be read")
	}
	d := &Detail{Record: raw, Brief: rec.summary(), Publication: pub, Outgoing: rec.EdgeList, Incoming: []Edge{}, Revisions: []Revision{}}
	if d.Outgoing == nil {
		d.Outgoing = []Edge{}
	}
	m.remember(d.Brief)
	var wg sync.WaitGroup
	var mu sync.Mutex
	fail := func(part string, err error) {
		mu.Lock()
		defer mu.Unlock()
		if d.Errors == nil {
			d.Errors = map[string]string{}
		}
		d.Errors[part] = err.Error()
	}
	wg.Add(2)
	go func() {
		defer wg.Done()
		raw, err := m.call(ctx, "registry_relation_list", map[string]any{"id": id, "direction": "incoming", "publication": pub})
		if err != nil {
			fail("incoming", err)
			return
		}
		var rel struct {
			Edges []Edge `json:"edges"`
		}
		if err := json.Unmarshal(raw, &rel); err != nil {
			fail("incoming", errors.New("the relations could not be read"))
			return
		}
		mu.Lock()
		if rel.Edges != nil {
			d.Incoming = rel.Edges
		}
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		raw, err := m.call(ctx, "registry_revision_list", map[string]any{"id": id, "query": map[string]any{"limit": 50}})
		if err != nil {
			fail("revisions", err)
			return
		}
		var h struct {
			Revisions []Revision `json:"revisions"`
			HasMore   bool       `json:"has_more"`
		}
		if err := json.Unmarshal(raw, &h); err != nil {
			fail("revisions", errors.New("the history could not be read"))
			return
		}
		mu.Lock()
		if h.Revisions != nil {
			d.Revisions = h.Revisions
		}
		d.MoreHistory = h.HasMore
		mu.Unlock()
	}()
	wg.Wait()
	return d, nil
}

// remember keeps what the window read of a record, for the agent's Knowledge. Bounded.
func (m *mod) remember(b brief) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.seen) >= 200 {
		clear(m.seen)
	}
	m.seen[b.ID] = b
}

var revRe = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,200}$`)

func (m *mod) revision(ctx context.Context, id, rev string) (json.RawMessage, error) {
	id = strings.TrimPrefix(strings.TrimSpace(id), citePrefix)
	if !idRe.MatchString(id) || !revRe.MatchString(rev) {
		return nil, fmt.Errorf("%w: a record id and a revision", errBadInput)
	}
	return m.call(ctx, "registry_revision_get", map[string]any{"id": id, "revision": rev})
}

// Change is one field that differs between two revisions, by its path in the record.
type Change struct {
	Path string `json:"path"`
	// Op is added, removed or changed.
	Op     string `json:"op"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

// noise are record fields every revision changes: they say nothing about the change itself.
var noise = map[string]bool{"revision_id": true, "version_token": true, "sequence": true, "updated_at": true}

// diff compares two revisions of a record field by field (from is the older).
func (m *mod) diff(ctx context.Context, id, from, to string) ([]Change, error) {
	var (
		a, b   json.RawMessage
		ea, eb error
		wg     sync.WaitGroup
	)
	wg.Add(2)
	go func() { defer wg.Done(); a, ea = m.revision(ctx, id, from) }()
	go func() { defer wg.Done(); b, eb = m.revision(ctx, id, to) }()
	wg.Wait()
	if err := errors.Join(ea, eb); err != nil {
		return nil, err
	}
	return diffJSON(a, b)
}

func diffJSON(a, b json.RawMessage) ([]Change, error) {
	var va, vb any
	if err := json.Unmarshal(a, &va); err != nil {
		return nil, errors.New("the older revision could not be read")
	}
	if err := json.Unmarshal(b, &vb); err != nil {
		return nil, errors.New("the newer revision could not be read")
	}
	fa, fb := map[string]string{}, map[string]string{}
	flatten("", va, fa)
	flatten("", vb, fb)
	paths := map[string]bool{}
	for p := range fa {
		paths[p] = true
	}
	for p := range fb {
		paths[p] = true
	}
	out := []Change{}
	for p := range paths {
		top, _, _ := strings.Cut(p, ".")
		top, _, _ = strings.Cut(top, "[")
		if noise[top] {
			continue
		}
		x, inA := fa[p]
		y, inB := fb[p]
		switch {
		case inA && !inB:
			out = append(out, Change{Path: p, Op: "removed", Before: x})
		case !inA && inB:
			out = append(out, Change{Path: p, Op: "added", After: y})
		case x != y:
			out = append(out, Change{Path: p, Op: "changed", Before: x, After: y})
		}
	}
	slices.SortFunc(out, func(x, y Change) int { return strings.Compare(x.Path, y.Path) })
	return out, nil
}

// flatten writes each leaf of v under its path ("payload.interfaces[0].type"). An empty object
// or list is a leaf of its own, so a field that became empty still shows.
func flatten(prefix string, v any, out map[string]string) {
	switch t := v.(type) {
	case map[string]any:
		if len(t) == 0 && prefix != "" {
			out[prefix] = "{}"
			return
		}
		for k, x := range t {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			flatten(p, x, out)
		}
	case []any:
		if len(t) == 0 {
			out[prefix] = "[]"
			return
		}
		for i, x := range t {
			flatten(fmt.Sprintf("%s[%d]", prefix, i), x, out)
		}
	case string:
		out[prefix] = t
	case nil:
		out[prefix] = "null"
	default:
		b, _ := json.Marshal(t)
		out[prefix] = string(b)
	}
}

var schemaRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,40}/[0-9]{1,4}$`)

func (m *mod) schema(ctx context.Context, schema string) (json.RawMessage, error) {
	if !schemaRe.MatchString(schema) {
		return nil, fmt.Errorf("%w: a schema such as project/2", errBadInput)
	}
	return m.call(ctx, "registry_schema_get", map[string]any{"schema": schema})
}
