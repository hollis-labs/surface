package drafts

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Draft is one document the owner and the agent work on together.
type Draft struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Lang is markdown, text or a programming language.
	Lang    string `json:"lang"`
	Body    string `json:"body,omitempty"`
	Rev     int    `json:"rev"`
	Created string `json:"created"`
	Updated string `json:"updated"`
	// Lines is the body's line count, in lists.
	Lines int `json:"lines,omitempty"`
}

// Revision is one saved state of a draft.
type Revision struct {
	Rev     int    `json:"rev"`
	Body    string `json:"body,omitempty"`
	Author  string `json:"author"`
	Summary string `json:"summary,omitempty"`
	Created string `json:"created"`
}

var (
	errNotFound = errors.New("no such draft")
	// errStale is a write made against a revision that is no longer the latest.
	errStale = errors.New("the draft has changed since that revision; read it again and redo the change on the latest")
)

type store struct{ db *sql.DB }

func stamp() string { return time.Now().UTC().Format(time.RFC3339) }

func newID() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return "d_" + hex.EncodeToString(b)
}

func lines(s string) int {
	return len(split(s))
}

func (s *store) create(ctx context.Context, title, lang, body, author string) (Draft, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Untitled"
	}
	if lang == "" {
		lang = "markdown"
	}
	now := stamp()
	d := Draft{ID: newID(), Title: title, Lang: lang, Body: body, Rev: 1, Created: now, Updated: now}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return d, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO drafts (id, title, lang, body, rev, created_at, updated_at) VALUES (?, ?, ?, ?, 1, ?, ?)`,
		d.ID, d.Title, d.Lang, body, now, now); err != nil {
		return d, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO revisions (draft_id, rev, body, author, summary, created_at) VALUES (?, 1, ?, ?, 'created', ?)`,
		d.ID, body, author, now); err != nil {
		return d, err
	}
	return d, tx.Commit()
}

func (s *store) list(ctx context.Context, q string) ([]Draft, error) {
	where, args := "deleted_at IS NULL", []any{}
	if q != "" {
		where += " AND (title LIKE ? OR body LIKE ?)"
		args = append(args, "%"+q+"%", "%"+q+"%")
	}
	// where is built from constants; the values are bound.
	query := `SELECT id, title, lang, body, rev, created_at, updated_at FROM drafts WHERE ` + where + ` ORDER BY updated_at DESC LIMIT 200` //nolint:gosec // where is constants only
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Draft{}
	for rows.Next() {
		var d Draft
		if err := rows.Scan(&d.ID, &d.Title, &d.Lang, &d.Body, &d.Rev, &d.Created, &d.Updated); err != nil {
			return nil, err
		}
		d.Lines, d.Body = lines(d.Body), ""
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *store) get(ctx context.Context, id string) (Draft, error) {
	var d Draft
	err := s.db.QueryRowContext(ctx, `SELECT id, title, lang, body, rev, created_at, updated_at FROM drafts WHERE id = ? AND deleted_at IS NULL`, id).
		Scan(&d.ID, &d.Title, &d.Lang, &d.Body, &d.Rev, &d.Created, &d.Updated)
	if errors.Is(err, sql.ErrNoRows) {
		return d, errNotFound
	}
	d.Lines = lines(d.Body)
	return d, err
}

func (s *store) revision(ctx context.Context, id string, rev int) (Revision, error) {
	var r Revision
	err := s.db.QueryRowContext(ctx, `SELECT rev, body, author, summary, created_at FROM revisions WHERE draft_id = ? AND rev = ?`, id, rev).
		Scan(&r.Rev, &r.Body, &r.Author, &r.Summary, &r.Created)
	if errors.Is(err, sql.ErrNoRows) {
		return r, fmt.Errorf("draft %s has no revision %d", id, rev)
	}
	return r, err
}

func (s *store) revisions(ctx context.Context, id string) ([]Revision, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT rev, author, summary, created_at FROM revisions WHERE draft_id = ? ORDER BY rev DESC`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Revision{}
	for rows.Next() {
		var r Revision
		if err := rows.Scan(&r.Rev, &r.Author, &r.Summary, &r.Created); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// write saves a new revision. expect, when not 0, must be the latest revision: a change made
// against an older one is refused rather than silently overwriting.
func (s *store) write(ctx context.Context, id, body, author, summary string, expect int, title *string) (Draft, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Draft{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var rev int
	var cur, curTitle string
	err = tx.QueryRowContext(ctx, `SELECT rev, body, title FROM drafts WHERE id = ? AND deleted_at IS NULL`, id).Scan(&rev, &cur, &curTitle)
	if errors.Is(err, sql.ErrNoRows) {
		return Draft{}, errNotFound
	}
	if err != nil {
		return Draft{}, err
	}
	if expect != 0 && expect != rev {
		return Draft{}, errStale
	}
	newTitle := curTitle
	if title != nil && strings.TrimSpace(*title) != "" {
		newTitle = strings.TrimSpace(*title)
	}
	if body == cur && newTitle == curTitle {
		_ = tx.Rollback()
		return s.get(ctx, id)
	}
	now := stamp()
	if body != cur {
		rev++
		if _, err := tx.ExecContext(ctx, `INSERT INTO revisions (draft_id, rev, body, author, summary, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			id, rev, body, author, summary, now); err != nil {
			return Draft{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE drafts SET body = ?, rev = ?, title = ?, updated_at = ? WHERE id = ?`, body, rev, newTitle, now, id); err != nil {
		return Draft{}, err
	}
	if err := tx.Commit(); err != nil {
		return Draft{}, err
	}
	return s.get(ctx, id)
}

func (s *store) setLang(ctx context.Context, id, lang string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE drafts SET lang = ?, updated_at = ? WHERE id = ?`, lang, stamp(), id)
	return err
}

func (s *store) remove(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE drafts SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`, stamp(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNotFound
	}
	return nil
}
