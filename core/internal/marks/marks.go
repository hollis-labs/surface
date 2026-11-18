// Package marks keeps the collaboration marks: notes pinned to a part of something, by the
// owner or the agent, with replies, until resolved.
package marks

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/hollis-labs/surface/module"
)

// Publisher announces changes. The events bus satisfies it.
type Publisher interface {
	Publish(topic string, data any)
}

// Store is the marks, in the core database.
type Store struct {
	db  *sql.DB
	pub Publisher
}

// New returns the store. Its table is created by the core's migrations.
func New(db *sql.DB, pub Publisher) *Store { return &Store{db: db, pub: pub} }

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "mk_" + hex.EncodeToString(b)
}

func author(a string) string {
	if a == "agent" {
		return "agent"
	}
	return "owner"
}

// Add pins a new mark.
func (s *Store) Add(ctx context.Context, m module.Mark) (module.Mark, error) {
	if strings.TrimSpace(m.Ref) == "" {
		return m, errors.New("marks: a mark needs a ref (what it is on)")
	}
	if strings.TrimSpace(m.Body) == "" {
		return m, errors.New("marks: a mark needs a note")
	}
	m.ID, m.Status, m.Created, m.Author, m.Replies = newID(), "open", now(), author(m.Author), nil
	anchor := string(m.Anchor)
	if anchor == "" {
		anchor = "null"
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO marks (id, ref, anchor, quote, body, author, status, created_at, replies)
		VALUES (?, ?, ?, ?, ?, ?, 'open', ?, '[]')`, m.ID, m.Ref, anchor, m.Quote, m.Body, m.Author, m.Created); err != nil {
		return m, err
	}
	s.changed(m.Ref)
	return m, nil
}

// List returns the marks on a ref, oldest first; every open mark when ref is "".
func (s *Store) List(ctx context.Context, ref string) ([]module.Mark, error) {
	q, args := `SELECT id, ref, anchor, quote, body, author, status, created_at, replies FROM marks WHERE status='open' ORDER BY created_at, id`, []any{}
	if ref != "" {
		q, args = `SELECT id, ref, anchor, quote, body, author, status, created_at, replies FROM marks WHERE ref=? ORDER BY created_at, id`, []any{ref}
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []module.Mark{}
	for rows.Next() {
		m, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

type scanner interface{ Scan(...any) error }

func scan(r scanner) (module.Mark, error) {
	var m module.Mark
	var anchor, replies string
	if err := r.Scan(&m.ID, &m.Ref, &anchor, &m.Quote, &m.Body, &m.Author, &m.Status, &m.Created, &replies); err != nil {
		return m, err
	}
	if anchor != "" && anchor != "null" {
		m.Anchor = json.RawMessage(anchor)
	}
	_ = json.Unmarshal([]byte(replies), &m.Replies)
	return m, nil
}

func (s *Store) get(ctx context.Context, id string) (module.Mark, error) {
	m, err := scan(s.db.QueryRowContext(ctx, `SELECT id, ref, anchor, quote, body, author, status, created_at, replies FROM marks WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return m, errors.New("marks: no mark " + id)
	}
	return m, err
}

// Reply adds a note to a mark.
func (s *Store) Reply(ctx context.Context, id string, r module.MarkReply) (module.Mark, error) {
	if strings.TrimSpace(r.Body) == "" {
		return module.Mark{}, errors.New("marks: a reply needs text")
	}
	m, err := s.get(ctx, id)
	if err != nil {
		return m, err
	}
	r.Author, r.Created = author(r.Author), now()
	m.Replies = append(m.Replies, r)
	raw, _ := json.Marshal(m.Replies)
	if _, err := s.db.ExecContext(ctx, `UPDATE marks SET replies=? WHERE id=?`, string(raw), id); err != nil {
		return m, err
	}
	s.changed(m.Ref)
	return m, nil
}

// Resolve closes a mark.
func (s *Store) Resolve(ctx context.Context, id string) error {
	m, err := s.get(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE marks SET status='resolved' WHERE id=?`, id); err != nil {
		return err
	}
	s.changed(m.Ref)
	return nil
}

func (s *Store) changed(ref string) {
	if s.pub != nil {
		s.pub.Publish("marks.changed", map[string]any{"ref": ref})
	}
}

var _ module.Marks = (*Store)(nil)
