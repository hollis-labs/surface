// Package cards keeps the cards: what the app shows the user outside a window (a notice, a
// question, a request to approve, progress) and what they answered. The state is the
// server's: a card is open until it has a status here, whatever a page believes.
package cards

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hollis-labs/surface/module"
)

// Publisher announces changes. The events bus satisfies it.
type Publisher interface {
	Publish(topic string, data any)
}

// ErrClosed is returned when a card that is no longer open is answered.
var ErrClosed = errors.New("the card is no longer open")

// Abandoned is the status of an agent's question whose run ended while it waited for the
// answer (the run was stopped, the app shut down). It is not "canceled", which is the user
// declining. An abandoned card, like an agent's question that expired, takes one late answer.
const Abandoned = "abandoned"

// LateAnswerable says whether a closed card still takes the user's answer: an agent's
// question (card_ask) the agent stopped waiting for, because its run ended (abandoned) or
// its wait ran out (expired), and that has had no late answer yet.
func LateAnswerable(c module.Card) bool {
	return c.Source == "agent" && Answerable(c.Kind) && !c.Late && (c.Status == Abandoned || c.Status == "expired")
}

// ErrNotFound is returned for an id no card has.
var ErrNotFound = errors.New("no such card")

// Store is the cards, in the core database.
type Store struct {
	db  *sql.DB
	pub Publisher
	now func() time.Time

	mu      sync.Mutex
	waiters map[string][]chan struct{}
}

// New returns the store. Its table is created by the core's migrations.
func New(db *sql.DB, pub Publisher) *Store {
	return &Store{db: db, pub: pub, now: time.Now, waiters: map[string][]chan struct{}{}}
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "cd_" + hex.EncodeToString(b)
}

const cols = `id, kind, version, title, source, subject, risk, blocking, payload, status, outcome, revision, created_at, updated_at, responded_at, expires_at, late, delivered_at`

type scanner interface{ Scan(dest ...any) error }

func scan(row scanner) (module.Card, error) {
	var c module.Card
	var payload, outcome string
	var blocking, late int
	if err := row.Scan(&c.ID, &c.Kind, &c.Version, &c.Title, &c.Source, &c.Subject, &c.Risk, &blocking, &payload,
		&c.Status, &outcome, &c.Revision, &c.Created, &c.Updated, &c.Responded, &c.ExpiresAt, &late, &c.Delivered); err != nil {
		return c, err
	}
	c.Blocking, c.Late = blocking != 0, late != 0
	c.Payload = json.RawMessage(payload)
	if outcome != "" && outcome != "null" {
		var o struct {
			module.CardOutcome
			SupersededBy string `json:"superseded_by,omitempty"`
		}
		if json.Unmarshal([]byte(outcome), &o) == nil {
			if o.Status != "" {
				out := o.CardOutcome
				c.Outcome = &out
			}
			c.SupersededBy = o.SupersededBy
		}
	}
	return c, nil
}

func (s *Store) changed(c module.Card) {
	if s.pub != nil {
		s.pub.Publish("cards.changed", c)
	}
	if c.Status == "" {
		return
	}
	s.mu.Lock()
	waiting := s.waiters[c.ID]
	delete(s.waiters, c.ID)
	s.mu.Unlock()
	for _, ch := range waiting {
		close(ch)
	}
}

// Show puts a card in front of the user.
func (s *Store) Show(ctx context.Context, c module.Card) (module.Card, error) {
	c.Kind = strings.TrimSpace(c.Kind)
	if c.Kind == "" {
		return c, errors.New("cards: a card needs a kind")
	}
	if c.Version == "" {
		c.Version = "1"
	}
	if len(c.Payload) == 0 {
		c.Payload = json.RawMessage(`{}`)
	}
	if !json.Valid(c.Payload) {
		return c, errors.New("cards: the payload is not JSON")
	}
	if err := check(c); err != nil {
		return c, err
	}
	if c.ExpiresAt != "" {
		if _, err := time.Parse(time.RFC3339, c.ExpiresAt); err != nil {
			return c, fmt.Errorf("cards: expires_at is not a time: %w", err)
		}
	}
	t := stamp(s.now())
	c.ID, c.Status, c.Outcome, c.SupersededBy, c.Revision, c.Created, c.Updated, c.Responded = newID(), "", nil, "", 1, t, t, ""

	// A card about the same thing from the same sender replaces the open one.
	var replaced []module.Card
	if c.Subject != "" {
		rows, err := s.db.QueryContext(ctx, `SELECT `+cols+` FROM cards WHERE status='' AND kind=? AND source=? AND subject=?`, c.Kind, c.Source, c.Subject)
		if err != nil {
			return c, err
		}
		for rows.Next() {
			old, err := scan(rows)
			if err != nil {
				_ = rows.Close()
				return c, err
			}
			replaced = append(replaced, old)
		}
		if err := rows.Close(); err != nil {
			return c, err
		}
	}
	c.Late, c.Delivered = false, ""
	if _, err := s.db.ExecContext(ctx, `INSERT INTO cards (`+cols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.Kind, c.Version, c.Title, c.Source, c.Subject, c.Risk, c.Blocking, string(c.Payload), "", "null", 1, t, t, "", c.ExpiresAt, 0, ""); err != nil {
		return c, err
	}
	for _, old := range replaced {
		if ended, err := s.close(ctx, old.ID, "superseded", nil, c.ID); err == nil {
			s.changed(ended)
		}
	}
	s.changed(c)
	return c, nil
}

// Update replaces an open card's payload, and its title when one is given.
func (s *Store) Update(ctx context.Context, id, title string, payload json.RawMessage) (module.Card, error) {
	c, ok, err := s.Get(ctx, id)
	if err != nil {
		return c, err
	}
	if !ok {
		return c, ErrNotFound
	}
	if c.Status != "" {
		return c, ErrClosed
	}
	if len(payload) > 0 {
		if !json.Valid(payload) {
			return c, errors.New("cards: the payload is not JSON")
		}
		c.Payload = payload
	}
	if title != "" {
		c.Title = title
	}
	if err = check(c); err != nil {
		return c, err
	}
	c.Revision++
	c.Updated = stamp(s.now())
	res, err := s.db.ExecContext(ctx, `UPDATE cards SET title=?, payload=?, revision=?, updated_at=? WHERE id=? AND status=''`,
		c.Title, string(c.Payload), c.Revision, c.Updated, id)
	if err != nil {
		return c, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return c, ErrClosed
	}
	s.changed(c)
	return c, nil
}

// close gives an open card its final status. Only one closer wins; the rest get ErrClosed.
func (s *Store) close(ctx context.Context, id, status string, o *module.CardOutcome, supersededBy string) (module.Card, error) {
	stored := struct {
		*module.CardOutcome
		SupersededBy string `json:"superseded_by,omitempty"`
	}{o, supersededBy}
	raw := []byte("null")
	if o != nil || supersededBy != "" {
		var err error
		if raw, err = json.Marshal(stored); err != nil {
			return module.Card{}, err
		}
	}
	t := stamp(s.now())
	res, err := s.db.ExecContext(ctx, `UPDATE cards SET status=?, outcome=?, revision=revision+1, updated_at=?, responded_at=? WHERE id=? AND status=''`,
		status, string(raw), t, t, id)
	if err != nil {
		return module.Card{}, err
	}
	c, ok, err := s.get(ctx, id)
	if err != nil {
		return c, err
	}
	if !ok {
		return c, ErrNotFound
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return c, ErrClosed
	}
	return c, nil
}

// End closes a card from the sender's side.
func (s *Store) End(ctx context.Context, id, status string) (module.Card, error) {
	switch status {
	case "superseded", "expired", "canceled", "failed", Abandoned:
	default:
		return module.Card{}, fmt.Errorf("cards: a card cannot end as %q", status)
	}
	c, err := s.close(ctx, id, status, nil, "")
	if errors.Is(err, ErrClosed) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	s.changed(c)
	return c, nil
}

// Respond records the user's answer. An answer to a card that is no longer open is refused,
// whatever the page still showed, except a late answer to an agent's question the agent
// stopped waiting for (LateAnswerable): that is taken once and marked late. A submitted late
// answer is handed to the agent as a message (LateAnswers); a canceled one only dismisses
// the card.
func (s *Store) Respond(ctx context.Context, id string, o module.CardOutcome) (module.Card, error) {
	if o.Status != "submitted" && o.Status != "canceled" {
		return module.Card{}, fmt.Errorf("cards: an answer is submitted or canceled, not %q", o.Status)
	}
	// Lapse it first if its time has passed, so the answer meets the card as it stands.
	if _, _, err := s.Get(ctx, id); err != nil {
		return module.Card{}, err
	}
	c, err := s.close(ctx, id, o.Status, &o, "")
	if errors.Is(err, ErrClosed) && LateAnswerable(c) {
		c, err = s.closeLate(ctx, id, o)
	}
	if err != nil {
		return c, err
	}
	s.changed(c)
	return c, nil
}

// closeLate records a late answer. The update matches only a card still waiting for one, so
// of two late answers only the first is taken.
func (s *Store) closeLate(ctx context.Context, id string, o module.CardOutcome) (module.Card, error) {
	raw, err := json.Marshal(o)
	if err != nil {
		return module.Card{}, err
	}
	t := stamp(s.now())
	res, err := s.db.ExecContext(ctx, `UPDATE cards SET status=?, outcome=?, late=1, revision=revision+1, updated_at=?, responded_at=?
		WHERE id=? AND late=0 AND source='agent' AND status IN (?, 'expired')`, o.Status, string(raw), t, t, id, Abandoned)
	if err != nil {
		return module.Card{}, err
	}
	c, ok, err := s.get(ctx, id)
	if err != nil {
		return c, err
	}
	if !ok {
		return c, ErrNotFound
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return c, ErrClosed
	}
	return c, nil
}

// LateAnswers returns the late answers submitted but not yet handed to the agent, oldest
// first. It is the delivery queue: kept in the database, so a restart does not lose it.
func (s *Store) LateAnswers(ctx context.Context) ([]module.Card, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols+` FROM cards WHERE late=1 AND status='submitted' AND delivered_at='' ORDER BY responded_at, id`)
	if err != nil {
		return nil, err
	}
	out := []module.Card{}
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Close()
}

// MarkDelivered records that a late answer reached the agent. It reports false when it had
// been marked already.
func (s *Store) MarkDelivered(ctx context.Context, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE cards SET delivered_at=?, revision=revision+1 WHERE id=? AND late=1 AND delivered_at=''`,
		stamp(s.now()), id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return false, nil
	}
	if c, ok, err := s.get(ctx, id); err == nil && ok {
		s.changed(c)
	}
	return true, nil
}

// LateWindow is how long after the agent stopped waiting a card is still offered for a late
// answer (LateOpen). Respond takes one later too; the window only keeps old ones off the page.
const LateWindow = 7 * 24 * time.Hour

// LateOpen returns the cards that take a late answer (LateAnswerable) and stopped waiting
// within LateWindow, oldest first.
func (s *Store) LateOpen(ctx context.Context) ([]module.Card, error) {
	since := stamp(s.now().Add(-LateWindow))
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols+` FROM cards WHERE source='agent' AND late=0 AND status IN (?, 'expired') AND responded_at>=?
		ORDER BY created_at, id`, Abandoned, since)
	if err != nil {
		return nil, err
	}
	out := []module.Card{}
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		if LateAnswerable(c) {
			out = append(out, c)
		}
	}
	return out, rows.Close()
}

// AbandonOrphans ends the agent's questions left open by an app that stopped without
// closing them (it was killed): after a start no run can be waiting for them. They become
// abandoned, so the user can still answer them late.
func (s *Store) AbandonOrphans(ctx context.Context) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind FROM cards WHERE status='' AND source='agent'`)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id, kind string
		if err := rows.Scan(&id, &kind); err != nil {
			_ = rows.Close()
			return 0, err
		}
		if Answerable(kind) {
			ids = append(ids, id)
		}
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		if c, err := s.close(ctx, id, Abandoned, nil, ""); err == nil {
			s.changed(c)
			n++
		}
	}
	return n, nil
}

func (s *Store) get(ctx context.Context, id string) (module.Card, bool, error) {
	c, err := scan(s.db.QueryRowContext(ctx, `SELECT `+cols+` FROM cards WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	return c, err == nil, err
}

// lapse closes a card whose time has passed. The server decides this, never a page's clock.
func (s *Store) lapse(ctx context.Context, c module.Card) module.Card {
	if c.Status != "" || c.ExpiresAt == "" {
		return c
	}
	at, err := time.Parse(time.RFC3339, c.ExpiresAt)
	if err != nil || s.now().Before(at) {
		return c
	}
	ended, err := s.close(ctx, c.ID, "expired", nil, "")
	if err == nil {
		s.changed(ended)
		return ended
	}
	if errors.Is(err, ErrClosed) {
		return ended
	}
	return c
}

// Get returns one card.
func (s *Store) Get(ctx context.Context, id string) (module.Card, bool, error) {
	c, ok, err := s.get(ctx, id)
	if err != nil || !ok {
		return c, ok, err
	}
	return s.lapse(ctx, c), true, nil
}

// List returns the open cards, oldest first; with all, the closed ones as well.
func (s *Store) List(ctx context.Context, all bool) ([]module.Card, error) {
	q := `SELECT ` + cols + ` FROM cards WHERE status='' ORDER BY created_at, id`
	if all {
		q = `SELECT ` + cols + ` FROM (SELECT * FROM cards ORDER BY created_at DESC, id DESC LIMIT 200) ORDER BY created_at, id`
	}
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	var found []module.Card
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		found = append(found, c)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	out := []module.Card{}
	for _, c := range found {
		c = s.lapse(ctx, c)
		if all || c.Status == "" {
			out = append(out, c)
		}
	}
	return out, nil
}

// Await returns the card once it is closed, or when ctx ends. A card with an expiry is
// closed as expired when its time comes, so a waiter is not left past it.
func (s *Store) Await(ctx context.Context, id string) (module.Card, error) {
	for {
		ch := make(chan struct{})
		s.mu.Lock()
		s.waiters[id] = append(s.waiters[id], ch)
		s.mu.Unlock()
		// Read after registering, so a close between the two is not missed.
		c, ok, err := s.Get(ctx, id)
		if err != nil {
			return c, err
		}
		if !ok {
			return c, ErrNotFound
		}
		if c.Status != "" {
			return c, nil
		}
		var lapse <-chan time.Time
		if at, err := time.Parse(time.RFC3339, c.ExpiresAt); err == nil {
			timer := time.NewTimer(max(at.Sub(s.now()), 0) + 50*time.Millisecond)
			defer timer.Stop()
			lapse = timer.C
		}
		select {
		case <-ch:
		case <-lapse:
		case <-ctx.Done():
			return c, ctx.Err()
		}
	}
}

var _ module.Cards = (*Store)(nil)
