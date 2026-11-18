package scratch

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// Item is one thing jotted down.
type Item struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	// Kind is note, todo, idea or followup.
	Kind    string `json:"kind"`
	Done    bool   `json:"done"`
	Tags    string `json:"tags,omitempty"`
	Created string `json:"created"`
	Updated string `json:"updated"`
}

var kinds = map[string]bool{"note": true, "todo": true, "idea": true, "followup": true}

var errNotFound = errors.New("no such scratch item")

type store struct{ db *sql.DB }

func stamp() string { return time.Now().UTC().Format(time.RFC3339) }

func newID() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return "s_" + hex.EncodeToString(b)
}

// kindOf reads a leading marker ("todo: ...", "? ...") off the text.
func kindOf(text, kind string) (string, string) {
	text = strings.TrimSpace(text)
	if kinds[kind] {
		return text, kind
	}
	lower := strings.ToLower(text)
	for prefix, k := range map[string]string{"todo:": "todo", "idea:": "idea", "followup:": "followup", "follow up:": "followup", "note:": "note"} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(text[len(prefix):]), k
		}
	}
	return text, "note"
}

func (s *store) add(ctx context.Context, text, kind, tags string) (Item, error) {
	text, kind = kindOf(text, kind)
	if text == "" {
		return Item{}, errors.New("nothing to keep: the text is empty")
	}
	now := stamp()
	it := Item{ID: newID(), Text: text, Kind: kind, Tags: tags, Created: now, Updated: now}
	_, err := s.db.ExecContext(ctx, `INSERT INTO items (id, text, kind, tags, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		it.ID, it.Text, it.Kind, it.Tags, now, now)
	return it, err
}

// list returns the items, newest first. q filters on the text; kind on the kind.
func (s *store) list(ctx context.Context, q, kind string, includeDone bool, limit int) ([]Item, error) {
	where, args := []string{"deleted_at IS NULL"}, []any{}
	if q != "" {
		where, args = append(where, "(text LIKE ? OR tags LIKE ?)"), append(args, "%"+q+"%", "%"+q+"%")
	}
	if kind != "" {
		where, args = append(where, "kind = ?"), append(args, kind)
	}
	if !includeDone {
		where = append(where, "done = 0")
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `SELECT id, text, kind, done, tags, created_at, updated_at FROM items WHERE `+
		strings.Join(where, " AND ")+` ORDER BY created_at DESC, id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Text, &it.Kind, &it.Done, &it.Tags, &it.Created, &it.Updated); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *store) get(ctx context.Context, id string) (Item, error) {
	var it Item
	err := s.db.QueryRowContext(ctx, `SELECT id, text, kind, done, tags, created_at, updated_at FROM items WHERE id=? AND deleted_at IS NULL`, id).
		Scan(&it.ID, &it.Text, &it.Kind, &it.Done, &it.Tags, &it.Created, &it.Updated)
	if errors.Is(err, sql.ErrNoRows) {
		return it, errNotFound
	}
	return it, err
}

// patch is the fields of an item that can change; nil leaves a field as it is.
type patch struct {
	Text *string `json:"text"`
	Kind *string `json:"kind"`
	Done *bool   `json:"done"`
	Tags *string `json:"tags"`
}

func (s *store) update(ctx context.Context, id string, p patch) (Item, error) {
	it, err := s.get(ctx, id)
	if err != nil {
		return it, err
	}
	if p.Text != nil {
		if strings.TrimSpace(*p.Text) == "" {
			return it, errors.New("the text cannot be empty; delete the item instead")
		}
		it.Text = strings.TrimSpace(*p.Text)
	}
	if p.Kind != nil {
		if !kinds[*p.Kind] {
			return it, errors.New("kind must be note, todo, idea or followup")
		}
		it.Kind = *p.Kind
	}
	if p.Done != nil {
		it.Done = *p.Done
	}
	if p.Tags != nil {
		it.Tags = strings.TrimSpace(*p.Tags)
	}
	it.Updated = stamp()
	_, err = s.db.ExecContext(ctx, `UPDATE items SET text=?, kind=?, done=?, tags=?, updated_at=? WHERE id=?`,
		it.Text, it.Kind, it.Done, it.Tags, it.Updated, id)
	return it, err
}

// remove puts an item away. The row is kept, so it can be brought back.
func (s *store) remove(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE items SET deleted_at=? WHERE id=? AND deleted_at IS NULL`, stamp(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNotFound
	}
	return nil
}

func (s *store) restore(ctx context.Context, id string) (Item, error) {
	if _, err := s.db.ExecContext(ctx, `UPDATE items SET deleted_at=NULL WHERE id=?`, id); err != nil {
		return Item{}, err
	}
	return s.get(ctx, id)
}

func (s *store) counts(ctx context.Context) (open, done int, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT coalesce(sum(done=0),0), coalesce(sum(done=1),0) FROM items WHERE deleted_at IS NULL`).Scan(&open, &done)
	return
}
