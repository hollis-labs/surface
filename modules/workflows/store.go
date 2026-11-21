package workflows

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

// Brief describes the shared workflow, before implementation or design review.
type Brief struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Outcome     string `json:"outcome"`
	UserSteps   string `json:"user_steps"`
	AgentSteps  string `json:"agent_steps"`
	SystemSteps string `json:"system_steps"`
	Acceptance  string `json:"acceptance"`
	Stage       string `json:"stage"`
	Revision    int    `json:"revision"`
	Updated     string `json:"updated"`
}

var (
	errMissing  = errors.New("workflow brief not found")
	errConflict = errors.New("the brief changed; reload it before saving")
	errWorking  = errors.New("the owner is editing this brief; wait or ask before changing it")
)

type store struct{ db *sql.DB }

const columns = `id,title,outcome,user_steps,agent_steps,system_steps,acceptance,stage,revision,updated`

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Brief, error) {
	var b Brief
	err := row.Scan(&b.ID, &b.Title, &b.Outcome, &b.UserSteps, &b.AgentSteps, &b.SystemSteps, &b.Acceptance, &b.Stage, &b.Revision, &b.Updated)
	if errors.Is(err, sql.ErrNoRows) {
		err = errMissing
	}
	return b, err
}
func (s *store) get(ctx context.Context, id string) (Brief, error) {
	return scan(s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM briefs WHERE id=?`, id))
}
func (s *store) list(ctx context.Context) ([]Brief, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM briefs ORDER BY updated DESC,id LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Brief{}
	for rows.Next() {
		b, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func validate(b Brief) error {
	if strings.TrimSpace(b.Title) == "" || strings.TrimSpace(b.Outcome) == "" {
		return errors.New("title and intended outcome are required")
	}
	if len(b.Title) > 200 || len(b.Outcome)+len(b.UserSteps)+len(b.AgentSteps)+len(b.SystemSteps)+len(b.Acceptance) > 64000 {
		return errors.New("brief is too large (title: 200 bytes; content: 64000 bytes)")
	}
	switch b.Stage {
	case "draft":
	case "review", "ready":
		if strings.TrimSpace(b.UserSteps) == "" || strings.TrimSpace(b.AgentSteps) == "" || strings.TrimSpace(b.SystemSteps) == "" || strings.TrimSpace(b.Acceptance) == "" {
			return errors.New("review needs user, agent and system steps plus acceptance checks")
		}
	default:
		return errors.New("stage must be draft, review or ready")
	}
	return nil
}
func (s *store) save(ctx context.Context, b Brief) (Brief, error) {
	if b.Stage == "" {
		b.Stage = "draft"
	}
	if err := validate(b); err != nil {
		return Brief{}, err
	}
	b.Title = strings.TrimSpace(b.Title)
	b.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	if b.ID == "" {
		if b.Stage != "draft" {
			return Brief{}, errors.New("create a draft before requesting review")
		}
		raw := make([]byte, 8)
		if _, err := rand.Read(raw); err != nil {
			return Brief{}, err
		}
		b.ID = hex.EncodeToString(raw)
		b.Revision = 1
		_, err := s.db.ExecContext(ctx, `INSERT INTO briefs (`+columns+`) VALUES (?,?,?,?,?,?,?,?,?,?)`, b.ID, b.Title, b.Outcome, b.UserSteps, b.AgentSteps, b.SystemSteps, b.Acceptance, b.Stage, b.Revision, b.Updated)
		return b, err
	}
	previous, err := s.get(ctx, b.ID)
	if err != nil {
		return Brief{}, err
	}
	if previous.Revision != b.Revision {
		return Brief{}, errConflict
	}
	if b.Stage == "ready" && previous.Stage != "review" && previous.Stage != "ready" {
		return Brief{}, errors.New("request review before marking ready")
	}
	// Editing an agreed brief starts another draft; a previous review cannot approve new text.
	changed := b.Title != previous.Title || b.Outcome != previous.Outcome || b.UserSteps != previous.UserSteps || b.AgentSteps != previous.AgentSteps || b.SystemSteps != previous.SystemSteps || b.Acceptance != previous.Acceptance
	if changed && b.Stage != "draft" {
		return Brief{}, errors.New("save changed content as a draft before review")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE briefs SET title=?,outcome=?,user_steps=?,agent_steps=?,system_steps=?,acceptance=?,stage=?,revision=revision+1,updated=? WHERE id=? AND revision=?`, b.Title, b.Outcome, b.UserSteps, b.AgentSteps, b.SystemSteps, b.Acceptance, b.Stage, b.Updated, b.ID, b.Revision)
	if err != nil {
		return Brief{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Brief{}, err
	}
	if n == 0 {
		return Brief{}, errConflict
	}
	b.Revision++
	return b, nil
}
func handoff(b Brief) string {
	return fmt.Sprintf("# %s\n\nWorkflow: workflows:%s · revision %d · %s\n\n## Outcome\n%s\n\n## User actions\n%s\n\n## Agent actions\n%s\n\n## System behavior\n%s\n\n## Acceptance checks\n%s\n\nReview the interaction and its state transitions before implementation.\n", b.Title, b.ID, b.Revision, b.Stage, b.Outcome, b.UserSteps, b.AgentSteps, b.SystemSteps, b.Acceptance)
}
