package diagram

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Diagram is one drawing: its current body and how many revisions it has.
type Diagram struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Rev     int    `json:"rev"`
	Body    string `json:"body,omitempty"`
	Created string `json:"created"`
	Updated string `json:"updated"`
}

// Revision is one save of a diagram.
type Revision struct {
	N       int    `json:"n"`
	Title   string `json:"title"`
	Body    string `json:"body,omitempty"`
	Created string `json:"created"`
}

var (
	errNotFound = errors.New("no such diagram")
	// errConflict: the save was made from an older revision than the current one.
	errConflict = errors.New("the diagram changed since that revision was read: read it again before saving")
)

type store struct{ db *sql.DB }

func stamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func newID() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return "dg_" + hex.EncodeToString(b)
}

// maxBody bounds a diagram's JSON.
const maxBody = 1 << 20

// check reads a body as the Diagram window does (model.ts parse), strictly enough to refuse what
// would not draw: it must be a JSON object; nodes, edges and groups are lists when given; a node
// has an id or a label; an edge's from and to name nodes in it. It returns the body as stored.
func check(body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		body = `{"diagram":"v1","nodes":[],"edges":[],"groups":[]}`
	}
	if len(body) > maxBody {
		return "", fmt.Errorf("the diagram is %d bytes; at most %d", len(body), maxBody)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return "", fmt.Errorf("the body is not a JSON object: %w", err)
	}
	if v, ok := raw["diagram"]; ok {
		var s string
		if json.Unmarshal(v, &s) != nil || s != "v1" {
			return "", errors.New(`"diagram" is "v1" when given`)
		}
	}
	list := func(key string) ([]map[string]any, error) {
		v, ok := raw[key]
		if !ok || string(v) == "null" {
			return nil, nil
		}
		var out []map[string]any
		if err := json.Unmarshal(v, &out); err != nil {
			return nil, fmt.Errorf("%q is a list of objects: %w", key, err)
		}
		return out, nil
	}
	nodes, err := list("nodes")
	if err != nil {
		return "", err
	}
	edges, err := list("edges")
	if err != nil {
		return "", err
	}
	if _, err = list("groups"); err != nil {
		return "", err
	}
	ids := map[string]bool{}
	for i, n := range nodes {
		id := strings.TrimSpace(fmt.Sprint(firstOf(n, "id", "label")))
		if id == "" || id == "<nil>" {
			return "", fmt.Errorf("node %d has no id or label", i)
		}
		ids[id] = true
	}
	var bad []string
	for i, e := range edges {
		from, to := fmt.Sprint(e["from"]), fmt.Sprint(e["to"])
		if !ids[from] || !ids[to] {
			bad = append(bad, fmt.Sprintf("edge %d (%s -> %s)", i, from, to))
		}
	}
	if len(bad) > 0 {
		return "", fmt.Errorf("edges must join nodes in the diagram: %s", strings.Join(bad, ", "))
	}
	return body, nil
}

func firstOf(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil && fmt.Sprint(v) != "" {
			return v
		}
	}
	return nil
}

func (s *store) list(ctx context.Context) ([]Diagram, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, title, rev, created_at, updated_at FROM diagrams ORDER BY updated_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Diagram{}
	for rows.Next() {
		var d Diagram
		if err = rows.Scan(&d.ID, &d.Title, &d.Rev, &d.Created, &d.Updated); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *store) get(ctx context.Context, id string) (Diagram, error) {
	var d Diagram
	err := s.db.QueryRowContext(ctx, `SELECT id, title, rev, body, created_at, updated_at FROM diagrams WHERE id = ?`, id).
		Scan(&d.ID, &d.Title, &d.Rev, &d.Body, &d.Created, &d.Updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Diagram{}, fmt.Errorf("%w: %s", errNotFound, id)
	}
	return d, err
}

// revisions lists a diagram's saves, newest first, without their bodies.
func (s *store) revisions(ctx context.Context, id string) ([]Revision, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT n, title, created_at FROM revisions WHERE diagram_id = ? ORDER BY n DESC`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Revision{}
	for rows.Next() {
		var r Revision
		if err = rows.Scan(&r.N, &r.Title, &r.Created); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *store) revision(ctx context.Context, id string, n int) (Revision, error) {
	r := Revision{N: n}
	err := s.db.QueryRowContext(ctx, `SELECT title, body, created_at FROM revisions WHERE diagram_id = ? AND n = ?`, id, n).
		Scan(&r.Title, &r.Body, &r.Created)
	if errors.Is(err, sql.ErrNoRows) {
		return Revision{}, fmt.Errorf("%w: %s revision %d", errNotFound, id, n)
	}
	return r, err
}

func (s *store) create(ctx context.Context, title, body string) (Diagram, error) {
	body, err := check(body)
	if err != nil {
		return Diagram{}, err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Untitled diagram"
	}
	now := stamp()
	d := Diagram{ID: newID(), Title: title, Rev: 1, Body: body, Created: now, Updated: now}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Diagram{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `INSERT INTO diagrams (id, title, rev, body, created_at, updated_at) VALUES (?, ?, 1, ?, ?, ?)`, d.ID, d.Title, d.Body, now, now); err != nil {
		return Diagram{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO revisions (diagram_id, n, title, body, created_at) VALUES (?, 1, ?, ?, ?)`, d.ID, d.Title, d.Body, now); err != nil {
		return Diagram{}, err
	}
	return d, tx.Commit()
}

// save keeps body (and title, when given) as the next revision. base is the revision the change
// was made from: 0 saves over whatever is current; otherwise a newer revision refuses it.
func (s *store) save(ctx context.Context, id, body, title string, base int) (Diagram, error) {
	body, err := check(body)
	if err != nil {
		return Diagram{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Diagram{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var d Diagram
	err = tx.QueryRowContext(ctx, `SELECT id, title, rev, created_at FROM diagrams WHERE id = ?`, id).Scan(&d.ID, &d.Title, &d.Rev, &d.Created)
	if errors.Is(err, sql.ErrNoRows) {
		return Diagram{}, fmt.Errorf("%w: %s", errNotFound, id)
	}
	if err != nil {
		return Diagram{}, err
	}
	if base > 0 && base != d.Rev {
		return Diagram{}, fmt.Errorf("%w (made from revision %d; it is at %d)", errConflict, base, d.Rev)
	}
	if t := strings.TrimSpace(title); t != "" {
		d.Title = t
	}
	d.Rev++
	d.Body = body
	d.Updated = stamp()
	if _, err = tx.ExecContext(ctx, `UPDATE diagrams SET title = ?, rev = ?, body = ?, updated_at = ? WHERE id = ?`, d.Title, d.Rev, d.Body, d.Updated, id); err != nil {
		return Diagram{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO revisions (diagram_id, n, title, body, created_at) VALUES (?, ?, ?, ?, ?)`, id, d.Rev, d.Title, d.Body, d.Updated); err != nil {
		return Diagram{}, err
	}
	return d, tx.Commit()
}

// rename changes the title without a new revision.
func (s *store) rename(ctx context.Context, id, title string) (Diagram, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Diagram{}, errors.New("a title is needed")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE diagrams SET title = ?, updated_at = ? WHERE id = ?`, title, stamp(), id)
	if err != nil {
		return Diagram{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Diagram{}, fmt.Errorf("%w: %s", errNotFound, id)
	}
	return s.get(ctx, id)
}

func (s *store) remove(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `DELETE FROM revisions WHERE diagram_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM diagrams WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: %s", errNotFound, id)
	}
	return tx.Commit()
}
