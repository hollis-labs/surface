package assist

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Classes of mail.
const (
	ClassNeeds  = "needs"  // asks the owner to decide, or escalates: a card, with its options
	ClassBas    = "bas"    // asks for something without options: an errand for the agent
	ClassDigest = "digest" // tells: held and counted
	ClassSettle = "settle" // the owner's rule says it needs nothing: marked read, on the record
	// ClassAnswer is a reply to something the assistant asked in the owner's name: it goes
	// back to the agent, which asked, not to the owner as new mail.
	ClassAnswer = "answer"
	classOld    = "old" // already waiting when the sweep first ran
)

type store struct{ db *sql.DB }

func stamp() string { return time.Now().UTC().Format(time.RFC3339) }

// Entry is one thing done in the owner's name.
type Entry struct {
	ID int64  `json:"id"`
	At string `json:"at"`
	// Kind: ack, status_ask, reply, send, settle, answer (the owner on a card), rule.
	Kind    string         `json:"kind"`
	Summary string         `json:"summary"`
	With    string         `json:"with,omitempty"`
	Ref     string         `json:"ref,omitempty"`
	Asked   bool           `json:"asked"`
	Detail  map[string]any `json:"detail,omitempty"`
}

// errNotRecorded: nothing is done in the owner's name without a record.
var errNotRecorded = errors.New("it was not done: the activity log could not be written, and nothing is sent in the owner's name without a record")

func (s *store) record(ctx context.Context, e Entry) error {
	detail := []byte("{}")
	if e.Detail != nil {
		detail, _ = json.Marshal(e.Detail)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO activity(at,kind,summary,with_whom,ref,asked,detail) VALUES(?,?,?,?,?,?,?)`,
		stamp(), e.Kind, e.Summary, e.With, e.Ref, e.Asked, string(detail)); err != nil {
		return errNotRecorded
	}
	return nil
}

// recent is the newest entries, before an id (0: the newest).
func (s *store) recent(ctx context.Context, before int64, limit int) ([]Entry, error) {
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	q, args := `SELECT id,at,kind,summary,with_whom,ref,asked,detail FROM activity`, []any{}
	if before > 0 {
		q, args = q+` WHERE id < ?`, append(args, before)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		var detail string
		if err := rows.Scan(&e.ID, &e.At, &e.Kind, &e.Summary, &e.With, &e.Ref, &e.Asked, &detail); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(detail), &e.Detail)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Rule is one of the owner's: mail that matches goes to Class. Empty fields match anything.
type Rule struct {
	ID         int64  `json:"id"`
	From       string `json:"from,omitempty"`
	Kind       string `json:"kind,omitempty"`
	SubjectHas string `json:"subject_has,omitempty"`
	Class      string `json:"class"`
	Note       string `json:"note,omitempty"`
}

func (s *store) rules(ctx context.Context) ([]Rule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,from_addr,kind,subject_has,class,note FROM rules ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Rule{}
	for rows.Next() {
		var r Rule
		if err := rows.Scan(&r.ID, &r.From, &r.Kind, &r.SubjectHas, &r.Class, &r.Note); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *store) addRule(ctx context.Context, r Rule) (Rule, error) {
	switch r.Class {
	case ClassNeeds, ClassBas, ClassDigest, ClassSettle:
	default:
		return r, errors.New("a rule sends mail to needs, bas, digest or settle")
	}
	if r.From == "" && r.Kind == "" && r.SubjectHas == "" {
		return r, errors.New("a rule must say what it matches: a sender, a kind or words in the subject")
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO rules(from_addr,kind,subject_has,class,note,created_at) VALUES(?,?,?,?,?,?)`,
		r.From, r.Kind, r.SubjectHas, r.Class, r.Note, stamp())
	if err != nil {
		return r, err
	}
	r.ID, _ = res.LastInsertId()
	return r, nil
}

func (s *store) dropRule(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM rules WHERE id=?`, id)
	return err
}

// Mail is a message the sweep sorted.
type Mail struct {
	ID       string `json:"id"`
	Account  string `json:"account"`
	From     string `json:"from"`
	FromName string `json:"from_name,omitempty"`
	Kind     string `json:"kind"`
	Subject  string `json:"subject"`
	Start    string `json:"start"`
	At       string `json:"at"`
	Class    string `json:"class"`
	Ask      *Ask   `json:"ask,omitempty"`
	// Why says which rule sorted it, when one of the owner's did.
	Why   string `json:"why,omitempty"`
	State string `json:"state"`
	Card  string `json:"card,omitempty"`
}

func (m Mail) who() string {
	if m.FromName != "" {
		return m.FromName
	}
	return m.From
}

const mailCols = `id,account,from_addr,from_name,kind,subject,start,at,class,ask,why,state,card`

func scanMail(rows *sql.Rows) ([]Mail, error) {
	defer func() { _ = rows.Close() }()
	out := []Mail{}
	for rows.Next() {
		var m Mail
		var ask string
		if err := rows.Scan(&m.ID, &m.Account, &m.From, &m.FromName, &m.Kind, &m.Subject, &m.Start, &m.At, &m.Class, &ask, &m.Why, &m.State, &m.Card); err != nil {
			return nil, err
		}
		if ask != "" {
			_ = json.Unmarshal([]byte(ask), &m.Ask)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// open is the sorted mail still waiting, of a class, newest first.
func (s *store) open(ctx context.Context, class string) ([]Mail, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+mailCols+` FROM mail WHERE state='open' AND class=? ORDER BY at DESC LIMIT 200`, class)
	if err != nil {
		return nil, err
	}
	return scanMail(rows)
}

func (s *store) mail(ctx context.Context, id string) (Mail, bool) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+mailCols+` FROM mail WHERE id=?`, id)
	if err != nil {
		return Mail{}, false
	}
	list, err := scanMail(rows)
	if err != nil || len(list) == 0 {
		return Mail{}, false
	}
	return list[0], true
}

// setState moves a sorted message on: done (answered or read), dismissed (not now), or open.
// With class set it is re-sorted too.
func (s *store) setState(ctx context.Context, id, state, class string) error {
	if class != "" {
		_, err := s.db.ExecContext(ctx, `UPDATE mail SET state=?, class=? WHERE id=?`, state, class, id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE mail SET state=? WHERE id=?`, state, id)
	return err
}

func (s *store) setCard(ctx context.Context, id, card string) {
	_, _ = s.db.ExecContext(ctx, `UPDATE mail SET card=? WHERE id=?`, card, id)
}

func (s *store) handed(ctx context.Context, ids []string) {
	for _, id := range ids {
		_, _ = s.db.ExecContext(ctx, `UPDATE mail SET handed_at=? WHERE id=?`, stamp(), id)
	}
}

// unhanded is open mail of a class not yet handed to the agent.
func (s *store) unhanded(ctx context.Context, class string) ([]Mail, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+mailCols+` FROM mail WHERE state='open' AND class=? AND handed_at='' ORDER BY at LIMIT 12`, class)
	if err != nil {
		return nil, err
	}
	return scanMail(rows)
}

func (s *store) asked(ctx context.Context, id string) (string, bool) {
	var about string
	err := s.db.QueryRowContext(ctx, `SELECT about FROM sent WHERE id=?`, id).Scan(&about)
	return about, err == nil
}
