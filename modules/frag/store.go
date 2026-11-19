package frag

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// The states a queued capture is in.
const (
	stateQueued    = "queued"
	stateAttention = "needs_attention"
	stateSaved     = "saved"
	stateDiscarded = "discarded"
)

// Limits, as the Fragments Engine capture contract and Basalisk's queue have them.
const (
	maxContent    = 64 * 1024
	maxTags       = 32
	maxTag        = 100
	maxContext    = 16 * 1024
	maxPending    = 500
	maxRows       = 10000
	leaseSeconds  = 120
	minBackoff    = 5
	maxBackoff    = 300
	timestampForm = "2006-01-02T15:04:05.000Z07:00"
)

var (
	errNotFound = errors.New("no such capture")
	// errConflict is a capture id already used for other text, or a change refused because
	// delivery is active or the capture is saved.
	errConflict = errors.New("conflict")
	errInvalid  = errors.New("invalid capture")
	// errIDTaken is errConflict for a capture id already queued with other text.
	errIDTaken = fmt.Errorf("%w: this capture id was already queued with different text; keep the draft and try again with a new id", errConflict)
)

// conflictf and invalidf wrap errConflict and errInvalid with a message for the owner.
func conflictf(msg string) error { return fmt.Errorf("%w: %s", errConflict, msg) }
func invalidf(msg string) error  { return fmt.Errorf("%w: %s", errInvalid, msg) }

func now() time.Time { return time.Now().UTC() }
func stamp() string  { return now().Format(timestampForm) }
func digest(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// newCaptureID is a fresh id for a draft's text.
func newCaptureID() string { return "frag-" + randomHex(12) }

// Payload is what a capture's row keeps for delivery: exactly what FE receives, and when.
type Payload struct {
	CaptureID   string   `json:"capture_id"`
	Content     string   `json:"content"`
	Tags        []string `json:"tags"`
	DraftAt     string   `json:"draft_at"`
	SubmittedAt string   `json:"submitted_at"`
}

// Window is the owner's window at a moment: what it showed, as the desk knows it.
type Window struct {
	View   string `json:"view,omitempty"`
	Module string `json:"module,omitempty"`
	Title  string `json:"title,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Ref    string `json:"ref,omitempty"`
	URL    string `json:"url,omitempty"`
}

// Moment is what a capture remembers about when it was drafted or saved.
type Moment struct {
	Window *Window `json:"window,omitempty"`
}

// OwnerContext is kept with the row and never sent to FE.
type OwnerContext struct {
	Draft *Moment `json:"draft,omitempty"`
	Save  *Moment `json:"save,omitempty"`
}

// Capture is one row of the queue, as the outbox shows it.
type Capture struct {
	Sequence           int64           `json:"sequence"`
	CaptureID          string          `json:"capture_id"`
	State              string          `json:"state"`
	Code               string          `json:"code"`
	Content            string          `json:"content"`
	Tags               []string        `json:"tags"`
	Context            json.RawMessage `json:"context,omitempty"`
	CreatedAt          string          `json:"created_at"`
	UpdatedAt          string          `json:"updated_at"`
	Attempts           int             `json:"attempts"`
	NextAttempt        int64           `json:"next_attempt,omitempty"`
	MayHaveSent        bool            `json:"may_have_sent"`
	Delivering         bool            `json:"delivering"`
	FragmentID         string          `json:"fragment_id,omitempty"`
	FragmentRevisionID string          `json:"fragment_revision_id,omitempty"`
	FragmentURL        string          `json:"fragment_url,omitempty"`
}

type store struct{ db *sql.DB }

// ParseTags splits "a, b,,c" into [a b c].
func ParseTags(s string) []string {
	out := []string{}
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func validCaptureID(id string) bool {
	if len(id) < 16 || len(id) > 100 {
		return false
	}
	for _, c := range id {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

func validTag(t string) bool {
	if strings.TrimSpace(t) == "" || len(t) > maxTag || !utf8.ValidString(t) {
		return false
	}
	for _, r := range t {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// check validates a capture before it is queued.
func (p *Payload) check() error {
	if p.Tags == nil {
		p.Tags = []string{}
	}
	if !validCaptureID(p.CaptureID) {
		return invalidf("the capture id is 16 to 100 letters, digits, - or _")
	}
	if strings.TrimSpace(p.Content) == "" {
		return invalidf("there is nothing to capture: the text is empty")
	}
	if len(p.Content) > maxContent || !utf8.ValidString(p.Content) || strings.ContainsRune(p.Content, 0) {
		return invalidf("the text must be at most 64 KiB of UTF-8")
	}
	if len(p.Tags) > maxTags {
		return invalidf("at most 32 tags")
	}
	for _, t := range p.Tags {
		if !validTag(t) {
			return invalidf("a tag is at most 100 characters, with no control characters")
		}
	}
	draft, e1 := time.Parse(time.RFC3339Nano, p.DraftAt)
	submitted, e2 := time.Parse(time.RFC3339Nano, p.SubmittedAt)
	if e1 != nil || e2 != nil || draft.After(submitted) || submitted.After(time.Now().Add(30*time.Second)) {
		return invalidf("the draft and submit times are not valid")
	}
	return nil
}

// hash covers what FE receives (content and tags); the context and the times are ours.
func (p Payload) hash() string {
	raw, _ := json.Marshal(struct {
		Content string   `json:"content"`
		Tags    []string `json:"tags"`
	}{p.Content, p.Tags})
	return digest(string(raw))
}

// enqueue queues one capture. The same id with the same text answers with the existing row
// (fresh false); the same id with other text is refused with errConflict.
func (s *store) enqueue(ctx context.Context, p Payload, oc *OwnerContext) (c Capture, fresh bool, err error) {
	if bad := p.check(); bad != nil {
		return c, false, bad
	}
	var ocRaw []byte
	if oc != nil {
		ocRaw, _ = json.Marshal(oc)
		if len(ocRaw) > maxContext {
			ocRaw = nil
		}
	}
	hash := p.hash()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return c, false, err
	}
	defer func() { _ = tx.Rollback() }()
	var old string
	err = tx.QueryRowContext(ctx, `SELECT payload_hash FROM submissions WHERE capture_id=?`, p.CaptureID).Scan(&old)
	if err == nil {
		if old != hash {
			return c, false, errIDTaken
		}
		c, err = scanCapture(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM submissions WHERE capture_id=?`, p.CaptureID))
		return c, false, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return c, false, err
	}
	var total, pending int
	if err = tx.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(state NOT IN ('saved','discarded')),0) FROM submissions`).Scan(&total, &pending); err != nil {
		return c, false, err
	}
	if total >= maxRows || pending >= maxPending {
		return c, false, conflictf("the capture queue is full; keep the draft and review the outbox")
	}
	raw, _ := json.Marshal(p)
	at := stamp()
	res, err := tx.ExecContext(ctx, `INSERT INTO submissions(capture_id,payload_hash,payload,owner_context,state,code,created_at,updated_at) VALUES(?,?,?,?,'queued','delivery_unavailable',?,?)`,
		p.CaptureID, hash, string(raw), string(ocRaw), at, at)
	if err != nil {
		return c, false, err
	}
	seq, err := res.LastInsertId()
	if err != nil {
		return c, false, err
	}
	if err = tx.Commit(); err != nil {
		return c, false, err
	}
	c, err = s.get(ctx, seq)
	return c, true, err
}

const columns = `sequence,capture_id,state,code,payload,owner_context,created_at,updated_at,attempts,next_attempt,may_have_sent,lease_until,fragment_id,fragment_revision_id,delivery_origin`

func scanCapture(row interface{ Scan(...any) error }) (Capture, error) {
	var c Capture
	var payload, oc, origin string
	var leaseUntil int64
	err := row.Scan(&c.Sequence, &c.CaptureID, &c.State, &c.Code, &payload, &oc, &c.CreatedAt, &c.UpdatedAt, &c.Attempts, &c.NextAttempt, &c.MayHaveSent, &leaseUntil, &c.FragmentID, &c.FragmentRevisionID, &origin)
	if errors.Is(err, sql.ErrNoRows) {
		return c, errNotFound
	}
	if err != nil {
		return c, err
	}
	c.Tags = []string{}
	var p Payload
	if payload != "" && json.Unmarshal([]byte(payload), &p) == nil { // a discarded row's payload is cleared
		c.Content = p.Content
		if p.Tags != nil {
			c.Tags = p.Tags
		}
	}
	if oc != "" && json.Valid([]byte(oc)) {
		c.Context = json.RawMessage(oc)
	}
	c.Delivering = leaseUntil > time.Now().Unix()
	if c.State == stateSaved && c.FragmentID != "" && origin != "" {
		c.FragmentURL = origin + "/sysop/reader/" + c.FragmentID
	}
	return c, nil
}

func (s *store) get(ctx context.Context, seq int64) (Capture, error) {
	return scanCapture(s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM submissions WHERE sequence=?`, seq))
}

// list is the outbox, newest first. state filters on one state.
func (s *store) list(ctx context.Context, state string, limit int) ([]Capture, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q, args := `SELECT `+columns+` FROM submissions`, []any{}
	if state != "" {
		q, args = q+` WHERE state=?`, append(args, state)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY sequence DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Capture{}
	for rows.Next() {
		c, err := scanCapture(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// counts is how many captures are in each state.
func (s *store) counts(ctx context.Context) (map[string]int, error) {
	out := map[string]int{stateQueued: 0, stateAttention: 0, stateSaved: 0, stateDiscarded: 0}
	rows, err := s.db.QueryContext(ctx, `SELECT state, count(*) FROM submissions GROUP BY state`)
	if err != nil {
		return out, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return out, err
		}
		out[st] = n
	}
	return out, rows.Err()
}

// retry puts a capture that waits (queued or needing attention) back in line now. A capture
// FE refused as invalid was never created there, so its frozen manifest is dropped and built
// again; so is one stopped because the FE address changed before anything was sent. Any
// other failure keeps the manifest, which is what makes a resend idempotent.
func (s *store) retry(ctx context.Context, seq int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE submissions SET
		manifest=CASE WHEN code='capture_rejected' OR (code='connection_changed' AND may_have_sent=0) THEN '' ELSE manifest END,
		upstream_capture_id=CASE WHEN code='capture_rejected' OR (code='connection_changed' AND may_have_sent=0) THEN '' ELSE upstream_capture_id END,
		delivery_origin=CASE WHEN code='capture_rejected' OR (code='connection_changed' AND may_have_sent=0) THEN '' ELSE delivery_origin END,
		state='queued',code='retry_requested',next_attempt=0,lease_token='',lease_until=0,updated_at=?
		WHERE sequence=? AND state IN ('queued','needs_attention') AND lease_until<=?`, stamp(), seq, time.Now().Unix())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		if _, e := s.get(ctx, seq); errors.Is(e, errNotFound) {
			return errNotFound
		}
		return conflictf("the capture is being delivered, saved or discarded; refresh before retrying")
	}
	return nil
}

// discard deletes a capture explicitly. Before any send it is discarded; after a possible send
// it keeps a tombstone (owner_stopped_delivery), because FE may already hold a copy. Either
// way the row's text and context are cleared and its id and hash kept, so a repeat of the
// same capture cannot bring it back. It never deletes anything in FE, and it is refused while
// a delivery is active or once the capture is saved.
func (s *store) discard(ctx context.Context, seq int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE submissions SET state='discarded',
		code=CASE WHEN may_have_sent=1 THEN 'owner_stopped_delivery' ELSE 'owner_discarded' END,
		payload='',owner_context='',manifest='',receipt='',updated_at=?
		WHERE sequence=? AND state IN ('queued','needs_attention','discarded') AND lease_until<=?`, stamp(), seq, time.Now().Unix())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, e := s.get(ctx, seq); errors.Is(e, errNotFound) {
			return errNotFound
		}
		return conflictf("delivery is active or the capture is already saved; refresh before trying again")
	}
	return nil
}

// Draft is the capture being written. It is kept until it is committed.
type Draft struct {
	Content   string          `json:"content"`
	Tags      string          `json:"tags"`
	DraftAt   string          `json:"draft_at,omitempty"`
	CaptureID string          `json:"capture_id,omitempty"`
	Context   json.RawMessage `json:"context,omitempty"`
	idFor     string
}

func (d Draft) empty() bool { return d.Content == "" && d.Tags == "" }

// key is what a capture id is made for: the text and the tags as they will be sent.
func (d Draft) key() string { return d.Content + "\x00" + strings.Join(ParseTags(d.Tags), ",") }

func (s *store) draft(ctx context.Context) (Draft, error) {
	var d Draft
	var c string
	err := s.db.QueryRowContext(ctx, `SELECT content,tags,draft_at,capture_id,id_for,context FROM draft WHERE id=1`).Scan(&d.Content, &d.Tags, &d.DraftAt, &d.CaptureID, &d.idFor, &c)
	if errors.Is(err, sql.ErrNoRows) {
		return Draft{}, nil
	}
	if c != "" && json.Valid([]byte(c)) {
		d.Context = json.RawMessage(c)
	}
	return d, err
}

func (s *store) saveDraft(ctx context.Context, d Draft) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO draft(id,content,tags,draft_at,capture_id,id_for,context,updated_at) VALUES(1,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET content=excluded.content,tags=excluded.tags,draft_at=excluded.draft_at,capture_id=excluded.capture_id,id_for=excluded.id_for,context=excluded.context,updated_at=excluded.updated_at`,
		d.Content, d.Tags, d.DraftAt, d.CaptureID, d.idFor, string(d.Context), stamp())
	return err
}

func (s *store) clearDraft(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM draft WHERE id=1`)
	return err
}

// withID gives the draft the capture id for its current text: the same one while the text
// and tags stay the same, a new one when they changed.
func (d Draft) withID() Draft {
	if d.CaptureID != "" && d.idFor == d.key() {
		return d
	}
	d.CaptureID, d.idFor = newCaptureID(), d.key()
	return d
}
