package ops

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Store is the working copy of Ops' documents in Caster: a mirror for quick views and for
// reading while Caster is away. Every write goes to Caster at once, each after reading the
// document again, and the new revision is kept here. Nothing lives only here.
//
// Caster's put takes no expected revision, so a write is checked by the revision it answers:
// one further on than the read plus one means someone else wrote in between. Their revision is
// then read, the change applied to it again and written once more (putOver), so neither write
// is lost; a change that cannot be applied to theirs puts theirs back and is refused.
type Store struct {
	db     *sql.DB
	caster Caster

	mu        sync.Mutex // one write or refresh at a time
	refreshed time.Time
	lastErr   string
}

// NewStore is the working copy in db over caster. caster may be nil: the copy is then read
// only and says why.
func NewStore(db *sql.DB, caster Caster) *Store { return &Store{db: db, caster: caster} }

var (
	// ErrNoCaster says there is no way to reach Caster configured.
	ErrNoCaster = errors.New("Caster is not reachable: set ops.tether_command (or ops.tether_url) in Settings")
	// ErrNotFound says there is no such workstream.
	ErrNotFound = errors.New("there is no such workstream")
)

// Conflict says the change does not stand: the document moved on, and it could not be applied
// to what is there. Rev is the revision now latest; Note, when set, says what happened.
type Conflict struct {
	Rev  int
	Note string
}

func (c *Conflict) Error() string {
	if c.Note != "" {
		return c.Note
	}
	return fmt.Sprintf("the workstream was changed meanwhile (now revision %d); read it again and redo the change", c.Rev)
}

// Taken says a create found the id taken (by someone who wrote it first).
type Taken struct{ ID string }

func (t *Taken) Error() string { return fmt.Sprintf("a workstream %s exists already", Ref(t.ID)) }

// InputError is a change refused for what it asks.
type InputError struct{ Msg string }

func (e *InputError) Error() string { return e.Msg }

func bad(format string, a ...any) error { return &InputError{Msg: fmt.Sprintf(format, a...)} }

// refreshEvery is how often the copy is read again from Caster at most, unless forced.
const refreshEvery = 15 * time.Second

// Refresh brings the copy up to date with Caster: one listing, and a read of each document
// whose revision moved. At most every refreshEvery unless forced. It returns how many changed.
func (s *Store) Refresh(ctx context.Context, force bool) (int, error) {
	if s.caster == nil {
		return 0, ErrNoCaster
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !force && time.Since(s.refreshed) < refreshEvery {
		if s.lastErr != "" {
			return 0, errors.New(s.lastErr)
		}
		return 0, nil
	}
	n, err := s.refreshLocked(ctx)
	s.refreshed, s.lastErr = time.Now(), ""
	if err != nil {
		s.lastErr = err.Error()
	}
	return n, err
}

func (s *Store) refreshLocked(ctx context.Context) (int, error) {
	heads, err := s.caster.List(ctx)
	if err != nil {
		return 0, fmt.Errorf("Caster could not be listed: %w", err)
	}
	have := map[string]int{}
	rows, err := s.db.QueryContext(ctx, `SELECT name, rev FROM docs`)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var name string
		var rev int
		if rows.Scan(&name, &rev) == nil {
			have[name] = rev
		}
	}
	_ = rows.Close()
	changed := 0
	listed := map[string]bool{}
	for _, h := range heads {
		if !wanted(h.Name) {
			continue
		}
		listed[h.Name] = true
		if have[h.Name] == h.Rev {
			continue
		}
		it, ok, err := s.caster.Get(ctx, h.Name, 0)
		if err != nil {
			return changed, err
		}
		if !ok {
			continue
		}
		if err := s.keep(ctx, it, h.Updated); err != nil {
			return changed, err
		}
		changed++
	}
	// A document no longer listed leaves the copy. An empty listing over a copy that is not
	// empty is not believed (a gateway answering with nothing is more likely than everything
	// gone): the copy is kept as it is.
	if len(listed) == 0 && len(have) > 0 {
		return changed, nil
	}
	for name := range have {
		if !listed[name] {
			if _, err := s.db.ExecContext(ctx, `DELETE FROM docs WHERE name=?`, name); err == nil {
				changed++
			}
		}
	}
	return changed, nil
}

func wanted(name string) bool {
	if name == QueueName || name == FollowupsName {
		return true
	}
	id, ok := strings.CutPrefix(name, "ws.")
	return ok && ValidID(id)
}

// keep puts a revision in the copy (never an older one over a newer).
func (s *Store) keep(ctx context.Context, it Item, updated string) error {
	if updated == "" {
		updated = now()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO docs(name, rev, title, content, updated, fetched_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(name) DO UPDATE SET rev=excluded.rev, title=excluded.title, content=excluded.content, updated=excluded.updated, fetched_at=excluded.fetched_at
		WHERE excluded.rev >= docs.rev`, it.Name, it.Rev, it.Title, it.Content, updated, now())
	return err
}

// LastError is what went wrong on the last refresh, if anything.
func (s *Store) LastError() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
}

// ---- Reading the copy ----

// Board is every workstream in the copy and the queue order.
type Board struct {
	Workstreams []Workstream `json:"workstreams"`
	// Queue is the Queued band in launch order: every workstream at stage queued, in the
	// order the queue document gives, then any it does not name yet (oldest first).
	Queue []string `json:"queue"`
}

// All reads the copy. A workstream whose parent is missing, makes a loop or nests too deep
// is shown at the top level, with a problem saying why, so nothing drops off the board.
func (s *Store) All(ctx context.Context) (Board, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, rev, content, updated FROM docs`)
	if err != nil {
		return Board{}, err
	}
	defer func() { _ = rows.Close() }()
	var b Board
	var order []string
	for rows.Next() {
		var name, content, updated string
		var rev int
		if err := rows.Scan(&name, &rev, &content, &updated); err != nil {
			return Board{}, err
		}
		switch name {
		case QueueName:
			order = parseQueue(content)
			continue
		case FollowupsName:
			continue
		}
		w := parseWorkstream(strings.TrimPrefix(name, "ws."), content, rev)
		w.Updated = updated
		b.Workstreams = append(b.Workstreams, w)
	}
	if err := rows.Err(); err != nil {
		return Board{}, err
	}
	sort.Slice(b.Workstreams, func(i, j int) bool { return b.Workstreams[i].ID < b.Workstreams[j].ID })
	sanitizeParents(b.Workstreams)
	idx := map[string]int{}
	for i, w := range b.Workstreams {
		idx[w.ID] = i
	}
	for i, w := range b.Workstreams {
		if p, ok := idx[w.Parent]; ok {
			b.Workstreams[p].Children = append(b.Workstreams[p].Children, b.Workstreams[i].ID)
		}
	}
	b.Queue = effectiveQueue(order, b.Workstreams)
	return b, nil
}

// sanitizeParents puts at the top level (with a problem saying why) only the workstreams that
// cannot sit where their document says: one whose own parent is not in Ops, one on a loop of
// parents, and the topmost one of a chain that runs deeper than maxDepth. Everything under
// such a workstream stays under it.
func sanitizeParents(all []Workstream) {
	idx := map[string]int{}
	for i, w := range all {
		idx[w.ID] = i
	}
	lift := func(i int, why string) {
		all[i].Problem = joinProblem(all[i].Problem, why+"; shown at the top level (parent "+Ref(all[i].Parent)+")")
		all[i].Parent = ""
	}
	// A parent that is not there.
	for i, w := range all {
		if _, ok := idx[w.Parent]; w.Parent != "" && !ok {
			lift(i, fmt.Sprintf("its parent %s is not in Ops", Ref(w.Parent)))
		}
	}
	// Loops: a workstream whose chain of parents comes back to itself.
	for i := range all {
		if all[i].Parent == "" {
			continue
		}
		for p, n := all[i].Parent, 0; p != "" && n <= len(all); n++ {
			if p == all[i].ID {
				lift(i, fmt.Sprintf("its parents make a loop back to it (through %s)", Ref(all[i].Parent)))
				break
			}
			p = all[idx[p]].Parent
		}
	}
	// Depth: lift the topmost of a chain that runs too deep, and look again.
	for round := 0; round < len(all); round++ {
		lifted := false
		for i := range all {
			nest := 0
			for p := all[i].Parent; p != ""; p = all[idx[p]].Parent {
				nest++
			}
			if nest == maxDepth+1 {
				lift(i, fmt.Sprintf("it would sit %d deep, past the %d allowed", nest, maxDepth))
				lifted = true
			}
		}
		if !lifted {
			return
		}
	}
}

func joinProblem(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

// effectiveQueue is the queue order reconciled with the stages: the queue document can lag a
// move (two writes), so the stage decides membership and the document decides order.
func effectiveQueue(order []string, all []Workstream) []string {
	queued := map[string]Workstream{}
	for _, w := range all {
		if w.Stage == StageQueued {
			queued[w.ID] = w
		}
	}
	out := []string{}
	seen := map[string]bool{}
	for _, id := range order {
		if _, ok := queued[id]; ok && !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	var rest []Workstream
	for id, w := range queued {
		if !seen[id] {
			rest = append(rest, w)
		}
	}
	sort.Slice(rest, func(i, j int) bool {
		if rest[i].StageSince != rest[j].StageSince {
			return rest[i].StageSince < rest[j].StageSince
		}
		return rest[i].ID < rest[j].ID
	})
	for _, w := range rest {
		out = append(out, w.ID)
	}
	return out
}

func parseQueue(content string) []string {
	d, err := ParsePlain(content)
	if err != nil {
		return nil
	}
	return d.Strings("order")
}

// Get is one workstream from the copy, with its children.
func (s *Store) Get(ctx context.Context, id string) (Workstream, bool, error) {
	b, err := s.All(ctx)
	if err != nil {
		return Workstream{}, false, err
	}
	w, ok := b.Find(id)
	return w, ok, nil
}

// Descendants are a workstream's children, theirs, and so on (not itself), from b. All has
// already broken loops and over-deep chains, so every level is walked.
func (b Board) Descendants(id string) []Workstream {
	byID := map[string]Workstream{}
	for _, w := range b.Workstreams {
		byID[w.ID] = w
	}
	var out []Workstream
	seen := map[string]bool{id: true}
	next := []string{id}
	for len(next) > 0 {
		var more []string
		for _, p := range next {
			for _, c := range byID[p].Children {
				if !seen[c] {
					seen[c] = true
					out = append(out, byID[c])
					more = append(more, c)
				}
			}
		}
		next = more
	}
	return out
}

// height is how many levels a workstream's subtree has (1: no children).
func (b Board) height(id string) int {
	byID := map[string]Workstream{}
	for _, w := range b.Workstreams {
		byID[w.ID] = w
	}
	h := 0
	seen := map[string]bool{}
	next := []string{id}
	for len(next) > 0 {
		h++
		var more []string
		for _, p := range next {
			if seen[p] {
				continue
			}
			seen[p] = true
			more = append(more, byID[p].Children...)
		}
		next = more
	}
	return h
}

// Find is a workstream in b.
func (b Board) Find(id string) (Workstream, bool) {
	for _, w := range b.Workstreams {
		if w.ID == id {
			return w, true
		}
	}
	return Workstream{}, false
}

// ---- Writes ----

func (s *Store) fresh(ctx context.Context, name string) (Item, bool, error) {
	it, ok, err := s.caster.Get(ctx, name, 0)
	if err != nil {
		return Item{}, false, fmt.Errorf("Caster could not be read: %w", err)
	}
	if ok {
		_ = s.keep(ctx, it, "")
	}
	return it, ok, nil
}

// maxRetries is how many times a write is applied again over someone else's.
const maxRetries = 2

// apply makes a document's next text from the one it is applied to (exists false: there is
// none yet). Its error refuses the change.
type apply func(cur Item, exists bool) (content, title string, err error)

// putOver writes name by applying fn to cur (the revision just read).
//
// Caster's put takes no expected revision, so a write is checked by the revision it answers.
// One further on than the revision this write follows means someone else wrote in between, and
// this write now sits over theirs. Their revision (the one just before) is read at once, fn is
// applied to it, and the result is written again, so the window in which the latest revision
// lacks their change is one read and one put long. That is repeated up to maxRetries times.
//
// The result reported always matches what Caster holds:
//   - nil: this change is the latest revision, built on everyone else's;
//   - fn's error, or *Conflict: this change does not stand. The latest foreign revision seen was
//     put back over it, or someone wrote after it (and theirs is left alone);
//   - any other error says what stands in its message.
//
// A race can still lose a write: a writer who builds on this write's first revision, before it
// is re-applied, builds without the change it overwrote. Caster keeps every revision, so nothing
// is gone from its history.
func (s *Store) putOver(ctx context.Context, name, mime string, cur Item, exists bool, fn apply) error {
	base, baseExists := cur, exists
	// last is the revision this write follows: the one read, then this write's own previous
	// revision (which a retry follows, over theirs).
	last := cur.Rev
	ours := 0 // this write's latest revision in Caster, 0 before the first put
	for attempt := 0; ; attempt++ {
		content, title, err := fn(base, baseExists)
		if err != nil {
			if ours == 0 {
				return err
			}
			return s.restore(ctx, name, mime, base, ours, func(r int) error {
				var c *Conflict
				if errors.As(err, &c) {
					return &Conflict{Rev: r, Note: c.Note}
				}
				return err
			})
		}
		rev, err := s.put(ctx, name, content, title, mime)
		if err != nil {
			if ours == 0 {
				return err
			}
			return s.restore(ctx, name, mime, base, ours, func(int) error {
				return fmt.Errorf("the change was not written again over someone else's: %w", err)
			})
		}
		ours = rev
		if rev <= last+1 {
			return nil
		}
		theirs, ok, gerr := s.caster.Get(ctx, name, rev-1)
		if gerr != nil || !ok {
			return fmt.Errorf("written as revision %d, but the revision just before it (someone else's) could not be read to keep their change; Caster's history has both", rev)
		}
		if attempt == maxRetries {
			return s.restore(ctx, name, mime, theirs, ours, func(r int) error {
				return &Conflict{Rev: r, Note: fmt.Sprintf("others kept writing while this was written; their latest is back as revision %d and this change was not kept: read it again and redo the change", r)}
			})
		}
		base, baseExists, last = theirs, true, rev
	}
}

// restore puts theirs back over this write's revision ours, so a refused change does not stand,
// and returns refused (with the revision now latest). The restore is checked like any write:
//   - when someone wrote after ours, theirs is not restored over it, and the conflict says so;
//   - when a write lands between that check and the restore's put, theirs (now over it) is put
//     back once more on top.
//
// Whatever it cannot settle is said in the error.
func (s *Store) restore(ctx context.Context, name, mime string, theirs Item, ours int, refused func(rev int) error) error {
	latest, ok, err := s.caster.Get(ctx, name, 0)
	if err != nil || !ok {
		return fmt.Errorf("%w; and Caster could not be read to put the earlier revision back: this change may stand as revision %d", refused(ours), ours)
	}
	if latest.Rev != ours {
		_ = s.keep(ctx, latest, "")
		return &Conflict{Rev: latest.Rev, Note: fmt.Sprintf("someone else wrote after this change (now revision %d); it was left as they wrote it: read it to see what stands", latest.Rev)}
	}
	rr, err := s.put(ctx, name, theirs.Content, theirs.Title, mime)
	if err != nil {
		return fmt.Errorf("%w; and the earlier revision could not be put back: this change stands as revision %d", refused(ours), ours)
	}
	if rr <= ours+1 {
		return refused(rr)
	}
	// A write landed between the check and the restore, and the restore sits over it: put
	// theirs back on top, once.
	newcomer, ok, err := s.caster.Get(ctx, name, rr-1)
	if err == nil && ok {
		if r3, perr := s.put(ctx, name, newcomer.Content, newcomer.Title, mime); perr == nil && r3 <= rr+1 {
			return &Conflict{Rev: r3, Note: fmt.Sprintf("someone else wrote while this change was being undone; theirs is the latest (revision %d) and this change was not kept: read it again", r3)}
		}
	}
	return &Conflict{Rev: rr, Note: fmt.Sprintf("several writers raced on this document (revisions %d to %d); it could not be settled: read it and check what stands, Caster's history has every revision", ours, rr)}
}

// put writes one revision and keeps it in the copy.
func (s *Store) put(ctx context.Context, name, content, title, mime string) (int, error) {
	rev, err := s.caster.Put(ctx, name, content, title, mime)
	if err != nil {
		return 0, fmt.Errorf("Caster did not take the change: %w", err)
	}
	return rev, s.keep(ctx, Item{Name: name, Rev: rev, Title: title, Content: content}, now())
}

// NewWorkstream is what a create takes.
type NewWorkstream struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Owner       string      `json:"owner,omitempty"`
	Project     string      `json:"project,omitempty"`
	StatusNS    string      `json:"status_ns,omitempty"`
	Stage       string      `json:"stage,omitempty"`
	Parent      string      `json:"parent,omitempty"`
	Links       []Link      `json:"links,omitempty"`
	ActiveWhen  []Check     `json:"active_when,omitempty"`
	Correlation Correlation `json:"correlation,omitempty"`
	Purpose     string      `json:"purpose,omitempty"`
	Notes       string      `json:"notes,omitempty"`
	Why         string      `json:"why,omitempty"`
}

// Create writes a new workstream (refused when the id is taken, even by a write that lands
// between the check and this one: theirs is put back) and, at stage queued, puts it at the
// end of the queue.
func (s *Store) Create(ctx context.Context, in NewWorkstream, by string) (Workstream, error) {
	if s.caster == nil {
		return Workstream{}, ErrNoCaster
	}
	in.ID, in.Title = strings.TrimSpace(in.ID), strings.TrimSpace(in.Title)
	if !ValidID(in.ID) {
		return Workstream{}, bad("%q is not a workstream id: 3 to 64 lower-case letters, digits and dashes, not ending in a dash", in.ID)
	}
	if in.Title == "" {
		return Workstream{}, bad("a workstream needs a title")
	}
	if in.Stage == "" {
		in.Stage = StagePlanned
	}
	if !ValidStage(in.Stage) {
		return Workstream{}, bad("%q is not a stage", in.Stage)
	}
	if !validNS(in.StatusNS) {
		return Workstream{}, bad("%q is not a status namespace (project/<slug>/event/status)", in.StatusNS)
	}
	for _, l := range in.Links {
		if err := checkLink(l); err != nil {
			return Workstream{}, &InputError{Msg: err.Error()}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, exists, err := s.fresh(ctx, WSName(in.ID))
	if err != nil {
		return Workstream{}, err
	}
	if exists {
		return Workstream{}, &Taken{ID: in.ID}
	}
	if in.Parent != "" {
		if err = s.checkParent(ctx, in.ID, in.Parent); err != nil {
			return Workstream{}, err
		}
	}
	at := now()
	why := in.Why
	if why == "" {
		why = "created"
	}
	w := Workstream{
		ID: in.ID, Title: in.Title, Owner: in.Owner, Project: in.Project, StatusNS: in.StatusNS,
		Stage: in.Stage, StageSince: at, Parent: in.Parent, ActiveWhen: in.ActiveWhen, Links: in.Links,
		Correlation: in.Correlation, Purpose: in.Purpose, Notes: in.Notes,
		Transitions: []Transition{{From: "", To: in.Stage, At: at, By: by, Why: why}},
	}
	if w.ActiveWhen == nil {
		w.ActiveWhen = []Check{}
	}
	if w.Links == nil {
		w.Links = []Link{}
	}
	d, err := newDoc(w)
	if err != nil {
		return Workstream{}, err
	}
	text := d.String()
	err = s.putOver(ctx, WSName(in.ID), "text/markdown", cur, false, func(_ Item, there bool) (string, string, error) {
		if there {
			return "", "", &Taken{ID: in.ID}
		}
		return text, in.Title, nil
	})
	if err != nil {
		return Workstream{}, err
	}
	if in.Stage == StageQueued {
		if err = s.queueLocked(ctx, func(order []string) ([]string, error) { return append(without(order, in.ID), in.ID), nil }); err != nil {
			return Workstream{}, fmt.Errorf("the workstream was made, but the queue could not take it: %w", err)
		}
	}
	got, _, err := s.Get(ctx, in.ID)
	return got, err
}

// checkParent refuses a parent that is not there, is the workstream itself or one of its own
// descendants, or would put the deepest of the workstream's own subtree past maxDepth.
func (s *Store) checkParent(ctx context.Context, id, parent string) error {
	if parent == id {
		return bad("a workstream cannot be its own parent")
	}
	if !ValidID(parent) {
		return bad("%q is not a workstream id", parent)
	}
	if _, ok, err := s.fresh(ctx, WSName(parent)); err != nil {
		return err
	} else if !ok {
		return bad("there is no workstream %s to nest under", Ref(parent))
	}
	b, err := s.All(ctx)
	if err != nil {
		return err
	}
	// nest is how deep the parent sits (0: top level).
	nest := 0
	for p := parent; ; nest++ {
		if p == id {
			return bad("%s is inside %s already: nesting it there would make a loop", Ref(parent), Ref(id))
		}
		w, ok := b.Find(p)
		if !ok || w.Parent == "" {
			break
		}
		p = w.Parent
	}
	if deepest := nest + b.height(id); deepest > maxDepth {
		return bad("workstreams nest at most %d deep; there it would reach %d (with what is inside it)", maxDepth, deepest)
	}
	return nil
}

// change re-reads a workstream and applies fn to it as it is now. baseRev, when not 0, is
// the revision the caller read: a document that moved on since is not changed (*Conflict).
func (s *Store) change(ctx context.Context, id string, baseRev int, fn func(d *Doc, w Workstream) error) (Workstream, error) {
	if s.caster == nil {
		return Workstream{}, ErrNoCaster
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.changeLocked(ctx, id, baseRev, fn)
}

func (s *Store) changeLocked(ctx context.Context, id string, baseRev int, fn func(d *Doc, w Workstream) error) (Workstream, error) {
	if !ValidID(id) {
		return Workstream{}, ErrNotFound
	}
	it, ok, err := s.fresh(ctx, WSName(id))
	if err != nil {
		return Workstream{}, err
	}
	if !ok {
		return Workstream{}, ErrNotFound
	}
	if baseRev > 0 && it.Rev != baseRev {
		return Workstream{}, &Conflict{Rev: it.Rev}
	}
	err = s.putOver(ctx, WSName(id), "text/markdown", it, true, func(cur Item, there bool) (string, string, error) {
		if !there {
			return "", "", ErrNotFound
		}
		d, perr := ParseDoc(cur.Content)
		if perr != nil {
			return "", "", fmt.Errorf("%s cannot be read: %w", Ref(id), perr)
		}
		if ferr := fn(d, parseWorkstream(id, cur.Content, cur.Rev)); ferr != nil {
			return "", "", ferr
		}
		if !d.Has("ops") {
			_ = d.Set("ops", Contract)
		}
		title := d.Str("title")
		if title == "" {
			title = id
		}
		return d.String(), title, nil
	})
	if err != nil {
		return Workstream{}, err
	}
	got, _, err := s.Get(ctx, id)
	return got, err
}

// Patch is what an update may change. A nil field is left as it is.
type Patch struct {
	Title       *string `json:"title,omitempty"`
	Owner       *string `json:"owner,omitempty"`
	Project     *string `json:"project,omitempty"`
	StatusNS    *string `json:"status_ns,omitempty"`
	Parent      *string `json:"parent,omitempty"`
	Purpose     *string `json:"purpose,omitempty"`
	ActiveMeans *string `json:"active_means,omitempty"`
	Notes       *string `json:"notes,omitempty"`
	// AppendNote adds a paragraph to the notes, dated.
	AppendNote *string `json:"append_note,omitempty"`
	// Held sets the held flag with a reason; "" clears it.
	Held *string `json:"held,omitempty"`
	// ActiveWhen replaces the checklist; Tick sets one line's done by its index; AddCheck adds one.
	ActiveWhen *[]Check  `json:"active_when,omitempty"`
	Tick       *Tick     `json:"tick,omitempty"`
	AddCheck   *Check    `json:"add_check,omitempty"`
	Labels     *[]string `json:"labels,omitempty"`
}

// replaces says whether the patch replaces something written against what the caller read
// (a section, the checklist, the labels, or a line of the checklist by its index): such a
// patch needs base_rev, and is refused over a newer revision rather than applied to it.
func (p Patch) replaces() bool {
	return p.Notes != nil || p.Purpose != nil || p.ActiveMeans != nil || p.ActiveWhen != nil || p.Labels != nil || p.Tick != nil
}

// Tick sets one active-when line's done.
type Tick struct {
	Index int  `json:"index"`
	Done  bool `json:"done"`
}

// Update applies a patch over baseRev (0: whatever is there now, for a patch that replaces
// nothing the caller read).
func (s *Store) Update(ctx context.Context, id string, baseRev int, p Patch) (Workstream, error) {
	if p.Title != nil && strings.TrimSpace(*p.Title) == "" {
		return Workstream{}, bad("a workstream needs a title")
	}
	if p.replaces() && baseRev <= 0 {
		return Workstream{}, bad("give base_rev (the rev you read): notes, purpose, active_means, active_when, labels and tick replace what you read")
	}
	if p.StatusNS != nil && !validNS(strings.TrimSpace(*p.StatusNS)) {
		return Workstream{}, bad("%q is not a status namespace (project/<slug>/event/status)", *p.StatusNS)
	}
	if s.caster == nil {
		return Workstream{}, ErrNoCaster
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.Parent != nil && *p.Parent != "" {
		if err := s.checkParent(ctx, id, *p.Parent); err != nil {
			return Workstream{}, err
		}
	}
	return s.changeLocked(ctx, id, baseRev, func(d *Doc, w Workstream) error {
		if p.replaces() && w.Rev != baseRev {
			// Someone wrote between the read and the write: what the caller replaces was
			// written against an older text.
			return &Conflict{Rev: w.Rev}
		}
		for _, kv := range []struct {
			k string
			v *string
		}{{"title", p.Title}, {"owner", p.Owner}, {"project", p.Project}, {"status_ns", p.StatusNS}, {"parent", p.Parent}} {
			if kv.v != nil {
				if err := setStr(d, kv.k, strings.TrimSpace(*kv.v)); err != nil {
					return err
				}
			}
		}
		if p.Purpose != nil {
			putSection(d, "Purpose", *p.Purpose)
		}
		if p.ActiveMeans != nil {
			putSection(d, "Active means", *p.ActiveMeans)
		}
		if p.Notes != nil {
			putSection(d, "Notes", *p.Notes)
		}
		if p.AppendNote != nil && strings.TrimSpace(*p.AppendNote) != "" {
			notes := strings.TrimSpace(w.Notes)
			add := fmt.Sprintf("- %s: %s", time.Now().UTC().Format("2006-01-02"), strings.TrimSpace(*p.AppendNote))
			if notes != "" {
				notes += "\n"
			}
			putSection(d, "Notes", notes+add)
		}
		if p.Held != nil {
			if r := strings.TrimSpace(*p.Held); r == "" {
				d.Del("held")
			} else if err := d.Set("held", Held{Reason: r, Since: now()}); err != nil {
				return err
			}
		}
		checks := w.ActiveWhen
		changed := false
		if p.ActiveWhen != nil {
			checks, changed = *p.ActiveWhen, true
		}
		if p.Tick != nil {
			if p.Tick.Index < 0 || p.Tick.Index >= len(checks) {
				return bad("there is no active-when line %d", p.Tick.Index)
			}
			checks[p.Tick.Index].Done, changed = p.Tick.Done, true
		}
		if p.AddCheck != nil {
			if strings.TrimSpace(p.AddCheck.Text) == "" {
				return bad("an active-when line needs text")
			}
			checks, changed = append(checks, *p.AddCheck), true
		}
		if changed {
			if checks == nil {
				checks = []Check{}
			}
			if err := d.Set("active_when", checks); err != nil {
				return err
			}
		}
		if p.Labels != nil {
			c := w.Correlation
			c.Labels = *p.Labels
			if err := d.Set("correlation", c); err != nil {
				return err
			}
		}
		return nil
	})
}

// Move changes a workstream's stage, records the move, and keeps the queue: moving into
// Queued puts it at the end of the queue, moving out takes it off.
func (s *Store) Move(ctx context.Context, id, to, why, by string) (Workstream, error) {
	if !ValidStage(to) {
		return Workstream{}, bad("%q is not a stage (%s)", to, strings.Join(Stages, ", "))
	}
	if s.caster == nil {
		return Workstream{}, ErrNoCaster
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var from string
	w, err := s.changeLocked(ctx, id, 0, func(d *Doc, w Workstream) error {
		from = w.Stage
		if from == to {
			return nil
		}
		at := now()
		if err := d.Set("stage", to); err != nil {
			return err
		}
		if err := d.Set("stage_since", at); err != nil {
			return err
		}
		return addTransition(d, Transition{From: from, To: to, At: at, By: by, Why: strings.TrimSpace(why)})
	})
	if err != nil || from == to {
		return w, err
	}
	switch {
	case to == StageQueued:
		err = s.queueLocked(ctx, func(order []string) ([]string, error) { return append(without(order, id), id), nil })
	case from == StageQueued:
		err = s.queueLocked(ctx, func(order []string) ([]string, error) { return without(order, id), nil })
	}
	if err != nil {
		return w, fmt.Errorf("the move was made, but the queue could not follow it (it shows in order anyway): %w", err)
	}
	return w, nil
}

// Link adds a typed link (the same kind and id again replaces its role and label).
func (s *Store) Link(ctx context.Context, id string, l Link) (Workstream, error) {
	l.Kind, l.ID, l.Role = strings.TrimSpace(l.Kind), strings.TrimSpace(l.ID), strings.TrimSpace(l.Role)
	if err := checkLink(l); err != nil {
		return Workstream{}, &InputError{Msg: err.Error()}
	}
	return s.change(ctx, id, 0, func(d *Doc, w Workstream) error {
		links := slices.DeleteFunc(w.Links, func(x Link) bool { return x.Kind == l.Kind && x.ID == l.ID })
		return d.Set("links", append(links, l))
	})
}

// Unlink takes a link out. One that is not there is not an error.
func (s *Store) Unlink(ctx context.Context, id, kind, ref string) (Workstream, error) {
	return s.change(ctx, id, 0, func(d *Doc, w Workstream) error {
		return d.Set("links", slices.DeleteFunc(w.Links, func(x Link) bool { return x.Kind == kind && x.ID == ref }))
	})
}

// ---- The queue ----

// queueLocked rewrites the queue document's order with fn, applied to the queue as it is in
// Caster (and again to someone else's, should theirs land between the read and the write).
func (s *Store) queueLocked(ctx context.Context, fn func([]string) ([]string, error)) error {
	it, ok, err := s.fresh(ctx, QueueName)
	if err != nil {
		return err
	}
	err = s.putOver(ctx, QueueName, "application/yaml", it, ok, func(cur Item, there bool) (string, string, error) {
		var d *Doc
		if there {
			var perr error
			if d, perr = ParsePlain(cur.Content); perr != nil {
				return "", "", fmt.Errorf("the queue cannot be read: %w", perr)
			}
		} else {
			d, _ = ParsePlain("")
			_ = d.Set("ops", Contract)
		}
		order, ferr := fn(d.Strings("order"))
		if ferr != nil {
			return "", "", ferr
		}
		if order == nil {
			order = []string{}
		}
		if serr := d.Set("order", order); serr != nil {
			return "", "", serr
		}
		return d.String(), "Ops queue", nil
	})
	return err
}

// Reorder moves a queued workstream within the Queued band, among its siblings only (the
// queued workstreams with the same parent): before or after another, or one place up or down.
// The move is worked out on the queue as it is in Caster at the write, not as the copy last
// saw it.
func (s *Store) Reorder(ctx context.Context, id, before, after, dir string) ([]string, error) {
	if s.caster == nil {
		return nil, ErrNoCaster
	}
	if before == "" && after == "" && dir != "up" && dir != "down" {
		return nil, bad("say where: before, after, or dir up or down")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.refreshLocked(ctx); err != nil {
		return nil, err
	}
	b, err := s.All(ctx)
	if err != nil {
		return nil, err
	}
	w, ok := b.Find(id)
	if !ok {
		return nil, ErrNotFound
	}
	if w.Stage != StageQueued {
		return nil, bad("%s is not queued: only the Queued band is ordered", Ref(id))
	}
	var result []string
	err = s.queueLocked(ctx, func(order []string) ([]string, error) {
		out, rerr := reorder(effectiveQueue(order, b.Workstreams), b, w, before, after, dir)
		result = out
		return out, rerr
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// reorder moves w within queue among its queued siblings.
func reorder(queue []string, b Board, w Workstream, before, after, dir string) ([]string, error) {
	var sibs []string
	for _, q := range queue {
		if x, _ := b.Find(q); x.Parent == w.Parent {
			sibs = append(sibs, q)
		}
	}
	at := slices.Index(sibs, w.ID)
	target, place := "", ""
	switch {
	case before != "":
		target, place = before, "before"
	case after != "":
		target, place = after, "after"
	case dir == "up":
		if at <= 0 {
			return queue, nil
		}
		target, place = sibs[at-1], "before"
	default:
		if at < 0 || at == len(sibs)-1 {
			return queue, nil
		}
		target, place = sibs[at+1], "after"
	}
	if target == w.ID {
		return queue, nil
	}
	if !slices.Contains(sibs, target) {
		return nil, bad("%s is not queued beside %s: the queue is reordered among siblings (same parent) only", Ref(target), Ref(w.ID))
	}
	order := without(queue, w.ID)
	i := slices.Index(order, target)
	if place == "after" {
		i++
	}
	return slices.Insert(order, i, w.ID), nil
}

func without(list []string, id string) []string {
	return slices.DeleteFunc(slices.Clone(list), func(x string) bool { return x == id })
}

// ---- History ----

// Log is a workstream's revisions in Caster, newest first.
func (s *Store) Log(ctx context.Context, id string) ([]Revision, error) {
	if s.caster == nil {
		return nil, ErrNoCaster
	}
	return s.caster.Log(ctx, WSName(id))
}

// At is a workstream's document at a revision.
func (s *Store) At(ctx context.Context, id string, rev int) (Item, bool, error) {
	if s.caster == nil {
		return Item{}, false, ErrNoCaster
	}
	return s.caster.Get(ctx, WSName(id), rev)
}
