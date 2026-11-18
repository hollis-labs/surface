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
	// StatusClosed is a job the Design team closed without merging, after approval.
	StatusClosed = "closed"
)

var statuses = map[string]bool{
	StatusRequested: true, StatusInProgress: true, StatusReady: true,
	StatusChangesRequested: true, StatusApproved: true, StatusMerged: true, StatusClosed: true,
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
	PreviewURL string `json:"preview_url,omitempty"`
	BaseURL    string `json:"base_url,omitempty"`
	Status     string `json:"status"`
	TaskID     string `json:"task_id,omitempty"`
	// HeadSHA is the head the Designer last said the preview shows.
	HeadSHA string `json:"head_sha,omitempty"`
	// TaskStatus and TaskComment are the Torque task's status and its latest comment, as read
	// at TaskChecked. InboxChecked is when the Designer's replies were last looked for.
	TaskStatus   string `json:"task_status,omitempty"`
	TaskComment  string `json:"task_comment,omitempty"`
	TaskChecked  string `json:"task_checked,omitempty"`
	InboxChecked string `json:"inbox_checked,omitempty"`
	// ApprovedHead is the commit the owner approved, at ApprovedAt; the approval covers it only.
	ApprovedHead string `json:"approved_head,omitempty"`
	ApprovedAt   string `json:"approved_at,omitempty"`
	// ApprovedPR is the pull request the approval is for: only its merge completes the job.
	ApprovedPR string `json:"approved_pr,omitempty"`
	// LatestHead is the head the Designer last named, also after an approval (when HeadSHA
	// stays the approved one); HeadDiffers says it is not the approved head.
	LatestHead  string `json:"latest_head,omitempty"`
	HeadDiffers bool   `json:"head_differs"`
	// UpdateReceived says work came back (a head or a pull request) with no preview to review
	// yet; ready for review still needs a preview.
	UpdateReceived bool `json:"update_received"`
	// MergeWarning says a pull request was merged on GitHub that is not the approved commit.
	MergeWarning string `json:"merge_warning,omitempty"`
	// approvalCard is the open approval card; approvalHead and approvalPR what it shows.
	approvalCard, approvalHead, approvalPR string
	Rounds                                 []Round `json:"rounds"`
	Replies                                []Reply `json:"replies"`
	Created                                string  `json:"created"`
	Updated                                string  `json:"updated"`
	// announced: the review window was opened and the owner told when the preview first came.
	announced bool
}

// Reply is one message from the Designer on a job's thread. What it set on the job is in its
// fields; Ignored says which labeled lines were refused and why. The body is shown as text,
// never acted on.
type Reply struct {
	MessageID  string `json:"message_id"`
	From       string `json:"from"`
	Subject    string `json:"subject,omitempty"`
	Body       string `json:"body"`
	Sent       string `json:"sent,omitempty"`
	Received   string `json:"received"`
	PreviewURL string `json:"preview_url,omitempty"`
	BaseURL    string `json:"base_url,omitempty"`
	PRURL      string `json:"pr_url,omitempty"`
	HeadSHA    string `json:"head_sha,omitempty"`
	Ignored    string `json:"ignored,omitempty"`
	// Note says what the reply did to a settled job, or why its merged:/closed: line was not taken.
	Note string `json:"note,omitempty"`
	// Merged and Closed are its merged:/closed: lines, read only once the job is approved.
	// OtherMerged names another pull request GitHub shows merged at that commit, if any.
	Merged      string `json:"-"`
	Closed      string `json:"-"`
	OtherMerged string `json:"-"`
}

// Round is one ask of the Design team: the first is the brief, later ones are feedback.
type Round struct {
	N     int    `json:"n"`
	Asked string `json:"asked"`
	// Sent says the message reached Tether; false means it is waiting to be sent by hand.
	Sent bool `json:"sent"`
	// MessageID is the Tether message that carried it, when it was sent.
	MessageID string `json:"message_id,omitempty"`
	Created   string `json:"created"`
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

const jobCols = `id, title, brief, refs, branch, pr_url, preview_url, base_url, status, task_id, head_sha, task_status, task_comment, task_checked_at, inbox_checked_at, preview_announced, approval_card, approval_head, approved_head, approved_at, update_received, merge_warning, approval_pr, approved_pr, created_at, updated_at`

func scanJob(sc interface{ Scan(...any) error }) (Job, error) {
	var j Job
	var announced, update int
	err := sc.Scan(&j.ID, &j.Title, &j.Brief, &j.Refs, &j.Branch, &j.PRURL, &j.PreviewURL, &j.BaseURL, &j.Status, &j.TaskID,
		&j.HeadSHA, &j.TaskStatus, &j.TaskComment, &j.TaskChecked, &j.InboxChecked, &announced,
		&j.approvalCard, &j.approvalHead, &j.ApprovedHead, &j.ApprovedAt, &update, &j.MergeWarning, &j.approvalPR, &j.ApprovedPR, &j.Created, &j.Updated)
	j.announced = announced == 1
	j.UpdateReceived = update == 1
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
	if j.Rounds, err = s.rounds(ctx, id); err != nil {
		return Job{}, err
	}
	if j.Replies, err = s.replies(ctx, id, 0); err != nil {
		return Job{}, err
	}
	j.heads()
	return j, nil
}

// heads fills LatestHead and HeadDiffers from the job and its replies.
func (j *Job) heads() {
	j.LatestHead = j.HeadSHA
	for _, r := range j.Replies { // oldest first: the last naming a head wins
		if r.HeadSHA != "" {
			j.LatestHead = r.HeadSHA
		}
	}
	j.HeadDiffers = j.ApprovedHead != "" && j.LatestHead != j.ApprovedHead
}

// list returns the jobs, the most recently changed first. With open, merged jobs are left out.
func (s *store) list(ctx context.Context, open bool) ([]Job, error) {
	q := `SELECT ` + jobCols + ` FROM jobs`
	if open {
		q += ` WHERE status NOT IN ('merged', 'closed')`
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY updated_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Job{}
	for rows.Next() {
		j, scanErr := scanJob(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		j.Rounds = []Round{}
		out = append(out, j)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		// A list carries each job's replies with their bodies cut short; design_job has them whole.
		if out[i].Replies, err = s.replies(ctx, out[i].ID, 600); err != nil {
			return nil, err
		}
		out[i].heads()
	}
	return out, nil
}

// replies are a job's replies, oldest first. With cut > 0 each body is cut to that many bytes.
func (s *store) replies(ctx context.Context, id string, cut int) ([]Reply, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT message_id, from_urn, subject, body, sent_at, received_at, preview_url, base_url, pr_url, head_sha, ignored, note
		FROM replies WHERE job_id = ? ORDER BY sent_at, received_at, message_id`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Reply{}
	for rows.Next() {
		var r Reply
		if err := rows.Scan(&r.MessageID, &r.From, &r.Subject, &r.Body, &r.Sent, &r.Received, &r.PreviewURL, &r.BaseURL, &r.PRURL, &r.HeadSHA, &r.Ignored, &r.Note); err != nil {
			return nil, err
		}
		if cut > 0 && len(r.Body) > cut {
			r.Body = strings.ToValidUTF8(r.Body[:cut], "") + "…"
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// open are the jobs still being worked: neither approved nor merged.
func (s *store) open(ctx context.Context) ([]Job, error) {
	jobs, err := s.list(ctx, true)
	if err != nil {
		return nil, err
	}
	out := jobs[:0]
	for _, j := range jobs {
		switch j.Status {
		case StatusMerged, StatusClosed:
		case StatusApproved:
			// Read on until the Design team says merged or closed, for a week at most.
			if at, err := time.Parse(time.RFC3339, j.ApprovedAt); err == nil && time.Since(at) < approvedWatch {
				out = append(out, j)
			}
		default:
			out = append(out, j)
		}
	}
	return out, nil
}

// approvedWatch is how long an approved job's thread is still read for the merge.
const approvedWatch = 7 * 24 * time.Hour

func (s *store) rounds(ctx context.Context, id string) ([]Round, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT n, asked, sent, message_id, created_at FROM rounds WHERE job_id = ? ORDER BY n`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Round{}
	for rows.Next() {
		var r Round
		var sent int
		if err := rows.Scan(&r.N, &r.Asked, &sent, &r.MessageID, &r.Created); err != nil {
			return nil, err
		}
		r.Sent = sent == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *store) addRound(ctx context.Context, id, asked string, out Outbox) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(n), 0) + 1 FROM rounds WHERE job_id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO rounds (job_id, n, asked, sent, message_id, created_at) VALUES (?, ?, ?, ?, ?, ?)`, id, n, asked, boolInt(out.Sent), out.MessageID, stamp()); err != nil {
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
	HeadSHA    *string `json:"head_sha,omitempty"`
	// What the reader records; not settable from a tool or the page.
	TaskStatus   *string `json:"-"`
	TaskComment  *string `json:"-"`
	TaskChecked  *string `json:"-"`
	InboxChecked *string `json:"-"`
	Announced    *bool   `json:"-"`
}

func (c Change) empty() bool { return c == Change{} }

// update writes only the fields the change names, so two writers that change different fields
// (the reader, a tool, the page) never undo each other.
func (s *store) update(ctx context.Context, id string, c Change) (Job, error) {
	if _, err := s.get(ctx, id); err != nil {
		return Job{}, err
	}
	var cols []string
	var args []any
	text := func(col string, v *string) {
		if v != nil {
			cols = append(cols, col+" = ?")
			args = append(args, strings.TrimSpace(*v))
		}
	}
	for _, u := range []*string{c.PRURL, c.PreviewURL, c.BaseURL} {
		if u != nil {
			if v := strings.TrimSpace(*u); v != "" && !strings.HasPrefix(v, "http://") && !strings.HasPrefix(v, "https://") {
				return Job{}, fmt.Errorf("%q is not an http(s) URL", v)
			}
		}
	}
	if c.Status != nil && !statuses[*c.Status] {
		return Job{}, fmt.Errorf("status is one of requested, in progress, ready for review, changes requested, approved, merged, closed; not %q", *c.Status)
	}
	text("branch", c.Branch)
	text("pr_url", c.PRURL)
	text("preview_url", c.PreviewURL)
	text("base_url", c.BaseURL)
	text("status", c.Status)
	text("task_id", c.TaskID)
	text("head_sha", c.HeadSHA)
	text("task_status", c.TaskStatus)
	text("task_comment", c.TaskComment)
	text("task_checked_at", c.TaskChecked)
	text("inbox_checked_at", c.InboxChecked)
	if c.Announced != nil {
		cols = append(cols, "preview_announced = ?")
		args = append(args, boolInt(*c.Announced))
	}
	if len(cols) > 0 {
		// Looking (the checked times) is not a change: it does not move the job up the list.
		looked := Change{TaskChecked: c.TaskChecked, InboxChecked: c.InboxChecked}
		if c != looked {
			cols = append(cols, "updated_at = ?")
			args = append(args, stamp())
		}
		args = append(args, id)
		if _, err := s.db.ExecContext(ctx, `UPDATE jobs SET `+strings.Join(cols, ", ")+` WHERE id = ?`, args...); err != nil { //nolint:gosec // the columns are this function's own names
			return Job{}, err
		}
	}
	return s.get(ctx, id)
}

// settled are the statuses nothing automatic may change: the owner's decision, or done.
func settled(status string) bool {
	return status == StatusApproved || status == StatusMerged || status == StatusClosed
}

// errMoved means the job changed under a write that was computed from an older read; the write
// was not made, and the next pass reads afresh.
var errMoved = errors.New("the job changed while it was being checked; it is checked again next time")

// moveStatus sets a job's status only if it is still from: what a reader computed from one read
// never overwrites what someone else (the owner approving) wrote since. It says whether it moved.
func (s *store) moveStatus(ctx context.Context, id, from, to string) (bool, error) {
	if !statuses[to] {
		return false, fmt.Errorf("unknown status %q", to)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE jobs SET status = ?, updated_at = ? WHERE id = ? AND status = ?`, to, stamp(), id, from)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// fillPR sets a job's pull request from GitHub only while none is recorded, so it never
// replaces one the owner or the Designer gave.
func (s *store) fillPR(ctx context.Context, id, pr string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE jobs SET pr_url = ?, updated_at = ? WHERE id = ? AND pr_url = '' AND status NOT IN ('approved', 'merged')`, pr, stamp(), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ingest stores a reply and applies it in one transaction: the reply, the fields its labeled
// lines set and the status it moves the job to are written together or not at all. The caller
// marks the message read only after this returns without error, so a failure leaves the message
// unread and the next pass takes it again.
//
// A field is set only when no stored reply sent later already set it (a later reply wins, in
// whatever order they are read). A job that is approved or merged keeps its fields and status:
// the reply is stored and shown, nothing more. The status moves only forward (derive), and only
// from the status read in this transaction.
func (s *store) ingest(ctx context.Context, jobID string, r Reply) (isNew bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO replies (message_id, job_id, from_urn, subject, body, sent_at, received_at, preview_url, base_url, pr_url, head_sha, ignored)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.MessageID, jobID, r.From, r.Subject, r.Body, r.Sent, r.Received, r.PreviewURL, r.BaseURL, r.PRURL, r.HeadSHA, r.Ignored)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 { // stored and applied before; only the read mark was missing
		return false, tx.Commit()
	}
	var status, preview, approved, approvedPR string
	if err = tx.QueryRowContext(ctx, `SELECT status, preview_url, approved_head, approved_pr FROM jobs WHERE id = ?`, jobID).Scan(&status, &preview, &approved, &approvedPR); err != nil {
		return false, err
	}
	var notes []string
	note := func() error {
		if len(notes) == 0 {
			return nil
		}
		_, noteErr := tx.ExecContext(ctx, `UPDATE replies SET note = ? WHERE message_id = ?`, strings.Join(notes, "; "), r.MessageID)
		return noteErr
	}
	if settled(status) {
		// Stored and shown. On an approved job only merged: (of the approved commit) and closed:
		// move it; the approved head, the preview and the fields are never changed, and the job
		// is not reopened.
		if r.HeadSHA != "" && approved != "" && r.HeadSHA != approved {
			notes = append(notes, noteAfterApproval)
		}
		if status == StatusApproved {
			to := ""
			warn := ""
			switch {
			case r.Merged != "" && r.Merged != approved:
				warn = fmt.Sprintf("the Designer says %s was merged, but the approved commit is %s (%s)", r.Merged, approved, or(approvedPR, "no pull request"))
			case r.Merged != "" && r.PRURL != "" && !samePR(r.PRURL, approvedPR):
				warn = fmt.Sprintf("the Designer says %s was merged at %s, but the approval is for %s at %s", r.PRURL, r.Merged, or(approvedPR, "no pull request"), approved)
			case r.Merged != "" && r.OtherMerged != "":
				warn = fmt.Sprintf("GitHub shows %s merged at %s, but the approval is for %s", r.OtherMerged, r.Merged, or(approvedPR, "no pull request"))
			case r.Merged != "":
				to = StatusMerged
				notes = append(notes, "merged at the approved commit, "+approvedPR)
			case r.Closed != "":
				to = StatusClosed
				notes = append(notes, "closed: "+r.Closed)
			}
			if warn != "" {
				warn = "merged on GitHub without approval of this commit: " + warn
				notes = append(notes, warn+"; not taken")
				if _, err = tx.ExecContext(ctx, `UPDATE jobs SET merge_warning = ? WHERE id = ?`, warn, jobID); err != nil {
					return false, err
				}
			}
			if to != "" {
				if _, err = tx.ExecContext(ctx, `UPDATE jobs SET status = ?, updated_at = ? WHERE id = ? AND status = ?`, to, stamp(), jobID, StatusApproved); err != nil {
					return false, err
				}
			}
		}
		if err = note(); err != nil {
			return false, err
		}
		return true, tx.Commit()
	}
	if r.Merged != "" || r.Closed != "" {
		notes = append(notes, "merged:/closed: are taken only after approval")
	}
	if err = note(); err != nil {
		return false, err
	}
	var cols []string
	var args []any
	for _, f := range []struct{ col, v string }{{"preview_url", r.PreviewURL}, {"base_url", r.BaseURL}, {"pr_url", r.PRURL}, {"head_sha", r.HeadSHA}} {
		if f.v == "" {
			continue
		}
		var later int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM replies WHERE job_id = ? AND message_id <> ? AND `+f.col+` <> '' AND sent_at > ?`, jobID, r.MessageID, r.Sent).Scan(&later); err != nil { //nolint:gosec // the column is this function's own name
			return false, err
		}
		if later > 0 {
			continue
		}
		cols = append(cols, f.col+" = ?")
		args = append(args, f.v)
		if f.col == "preview_url" {
			preview = f.v
		}
	}
	// Work came back without a preview: say so until a preview comes.
	switch {
	case r.PreviewURL != "":
		cols = append(cols, "update_received = 0")
	case r.HeadSHA != "" || r.PRURL != "":
		cols = append(cols, "update_received = 1")
	}
	if next := derive(status, "", preview, true, r.PreviewURL != ""); next != status {
		cols = append(cols, "status = ?")
		args = append(args, next)
	}
	cols = append(cols, "updated_at = ?")
	args = append(args, stamp(), jobID, status)
	res, err = tx.ExecContext(ctx, `UPDATE jobs SET `+strings.Join(cols, ", ")+` WHERE id = ? AND status = ?`, args...) //nolint:gosec // the columns are this function's own names
	if err != nil {
		return false, err
	}
	if n, err = res.RowsAffected(); err != nil {
		return false, err
	}
	if n != 1 {
		err = errMoved
		return false, err
	}
	return true, tx.Commit()
}

// noteAfterApproval marks a reply naming a commit other than the approved one.
const noteAfterApproval = "new commit after approval"

// pendApproval records the approval card just raised and the head and pull request it shows.
func (s *store) pendApproval(ctx context.Context, id, card, head, pr string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET approval_card = ?, approval_head = ?, approval_pr = ? WHERE id = ?`, card, head, pr, id)
	return err
}

// approve records the owner's approval of the head on card, in one compare-and-set: only while
// that card is still the job's open approval, the job's head is still the card's, and the job
// is not approved or merged already. It says whether it was recorded.
func (s *store) approve(ctx context.Context, id, card, head, pr string) (bool, error) {
	if card == "" || head == "" || pr == "" {
		return false, nil
	}
	res, err := s.db.ExecContext(ctx, `UPDATE jobs SET status = ?, approved_head = ?, approved_pr = ?, approved_at = ?, approval_card = '', approval_head = '', approval_pr = '', update_received = 0, updated_at = ?
		WHERE id = ? AND approval_card = ? AND approval_head = ? AND head_sha = ? AND approval_pr = ? AND pr_url = ? AND status IN ('requested', 'in progress', 'ready for review', 'changes requested')`,
		StatusApproved, head, pr, stamp(), stamp(), id, card, head, head, pr, pr)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// roundSent clears the work-came-back flag: a new round asks for new work.
func (s *store) roundSent(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET update_received = 0 WHERE id = ?`, id)
	return err
}

// mergeApproved moves an approved job to merged, only if it is still approved at head.
func (s *store) mergeApproved(ctx context.Context, id, head, pr string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE jobs SET status = ?, updated_at = ? WHERE id = ? AND status = ? AND approved_head = ? AND approved_head <> '' AND approved_pr = ? AND approved_pr <> ''`,
		StatusMerged, stamp(), id, StatusApproved, head, pr)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// warnMerge records a merge warning once. It says whether it is new.
func (s *store) warnMerge(ctx context.Context, id, warning string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE jobs SET merge_warning = ? WHERE id = ? AND merge_warning <> ?`, warning, id, warning)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// dropApproval clears the open approval card if it is still card.
func (s *store) dropApproval(ctx context.Context, id, card string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET approval_card = '', approval_head = '', approval_pr = '' WHERE id = ? AND approval_card = ?`, id, card)
	return err
}
