package design

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

// The statuses a job moves through, in order. "merged" is read from the pull request.
const (
	StatusRequested        = "requested"
	StatusInProgress       = "in progress"
	StatusReady            = "ready for review"
	StatusChangesRequested = "changes requested"
	StatusApproved         = "approved"
	StatusMerged           = "merged"
)

var statuses = map[string]bool{
	StatusRequested: true, StatusInProgress: true, StatusReady: true,
	StatusChangesRequested: true, StatusApproved: true, StatusMerged: true,
}

// Job is one piece of design work handed to the Design team.
type Job struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Brief string `json:"brief"`
	// Refs are references the work should follow (links, screenshots, components), one per line.
	Refs   string `json:"refs,omitempty"`
	Branch string `json:"branch"`
	PRURL  string `json:"pr_url,omitempty"`
	// PreviewURL serves the gallery from the branch; BaseURL from main. Both are given per job.
	PreviewURL string  `json:"preview_url,omitempty"`
	BaseURL    string  `json:"base_url,omitempty"`
	Status     string  `json:"status"`
	TaskID     string  `json:"task_id,omitempty"`
	Rounds     []Round `json:"rounds"`
	Created    string  `json:"created"`
	Updated    string  `json:"updated"`
}

// Round is one ask of the Design team: the first is the brief, later ones are feedback.
type Round struct {
	N     int    `json:"n"`
	Asked string `json:"asked"`
	// Sent says the message reached Tether; false means it is waiting to be sent by hand.
	Sent    bool   `json:"sent"`
	Created string `json:"created"`
}

var errNotFound = errors.New("no such design job")

type store struct{ db *sql.DB }

func stamp() string { return time.Now().UTC().Format(time.RFC3339) }

func newID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "d_" + hex.EncodeToString(b)
}

// branchFor is the branch the Design team is asked to use: design/<id>-<slug of the title>.
func branchFor(id, title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		switch {
		case ok:
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	// At most 40 characters, cut between words.
	if len(slug) > 40 {
		cut := slug[:40]
		if slug[40] != '-' {
			if i := strings.LastIndex(cut, "-"); i > 0 {
				cut = cut[:i]
			}
		}
		slug = strings.Trim(cut, "-")
	}
	if slug == "" {
		return "design/" + strings.TrimPrefix(id, "d_")
	}
	return "design/" + strings.TrimPrefix(id, "d_") + "-" + slug
}

func (s *store) create(ctx context.Context, title, brief, refs string) (Job, error) {
	title, brief = strings.TrimSpace(title), strings.TrimSpace(brief)
	if title == "" || brief == "" {
		return Job{}, errors.New("a design job needs a title and a brief")
	}
	now := stamp()
	j := Job{ID: newID(), Title: title, Brief: brief, Refs: strings.TrimSpace(refs), Status: StatusRequested, Created: now, Updated: now}
	j.Branch = branchFor(j.ID, title)
	_, err := s.db.ExecContext(ctx, `INSERT INTO jobs (id, title, brief, refs, branch, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		j.ID, j.Title, j.Brief, j.Refs, j.Branch, j.Status, now, now)
	if err != nil {
		return Job{}, err
	}
	return j, nil
}

const jobCols = `id, title, brief, refs, branch, pr_url, preview_url, base_url, status, task_id, created_at, updated_at`

func scanJob(sc interface{ Scan(...any) error }) (Job, error) {
	var j Job
	err := sc.Scan(&j.ID, &j.Title, &j.Brief, &j.Refs, &j.Branch, &j.PRURL, &j.PreviewURL, &j.BaseURL, &j.Status, &j.TaskID, &j.Created, &j.Updated)
	return j, err
}

func (s *store) get(ctx context.Context, id string) (Job, error) {
	j, err := scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobCols+` FROM jobs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, fmt.Errorf("%w: %s", errNotFound, id)
	}
	if err != nil {
		return Job{}, err
	}
	j.Rounds, err = s.rounds(ctx, id)
	return j, err
}

// list returns the jobs, the most recently changed first. With open, merged jobs are left out.
func (s *store) list(ctx context.Context, open bool) ([]Job, error) {
	q := `SELECT ` + jobCols + ` FROM jobs`
	if open {
		q += ` WHERE status <> 'merged'`
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY updated_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		j.Rounds = []Round{}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *store) rounds(ctx context.Context, id string) ([]Round, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT n, asked, sent, created_at FROM rounds WHERE job_id = ? ORDER BY n`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Round{}
	for rows.Next() {
		var r Round
		var sent int
		if err := rows.Scan(&r.N, &r.Asked, &sent, &r.Created); err != nil {
			return nil, err
		}
		r.Sent = sent == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *store) addRound(ctx context.Context, id, asked string, sent bool) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(n), 0) + 1 FROM rounds WHERE job_id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO rounds (job_id, n, asked, sent, created_at) VALUES (?, ?, ?, ?, ?)`, id, n, asked, boolInt(sent), stamp()); err != nil {
		return err
	}
	return s.touch(ctx, id)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *store) touch(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET updated_at = ? WHERE id = ?`, stamp(), id)
	return err
}

// Change is what may be set on a job; nil fields stay as they are.
type Change struct {
	Branch     *string `json:"branch,omitempty"`
	PRURL      *string `json:"pr_url,omitempty"`
	PreviewURL *string `json:"preview_url,omitempty"`
	BaseURL    *string `json:"base_url,omitempty"`
	Status     *string `json:"status,omitempty"`
	TaskID     *string `json:"task_id,omitempty"`
}

func (s *store) update(ctx context.Context, id string, c Change) (Job, error) {
	j, err := s.get(ctx, id)
	if err != nil {
		return Job{}, err
	}
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = strings.TrimSpace(*v)
		}
	}
	set(&j.Branch, c.Branch)
	set(&j.PRURL, c.PRURL)
	set(&j.PreviewURL, c.PreviewURL)
	set(&j.BaseURL, c.BaseURL)
	set(&j.TaskID, c.TaskID)
	if c.Status != nil {
		if !statuses[*c.Status] {
			return Job{}, fmt.Errorf("status is one of requested, in progress, ready for review, changes requested, approved, merged; not %q", *c.Status)
		}
		j.Status = *c.Status
	}
	for _, u := range []string{j.PRURL, j.PreviewURL, j.BaseURL} {
		if u != "" && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			return Job{}, fmt.Errorf("%q is not an http(s) URL", u)
		}
	}
	j.Updated = stamp()
	_, err = s.db.ExecContext(ctx, `UPDATE jobs SET branch = ?, pr_url = ?, preview_url = ?, base_url = ?, status = ?, task_id = ?, updated_at = ? WHERE id = ?`,
		j.Branch, j.PRURL, j.PreviewURL, j.BaseURL, j.Status, j.TaskID, j.Updated, id)
	return j, err
}
