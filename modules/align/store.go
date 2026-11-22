package align

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	errNotFound = errors.New("not found")
	errBadInput = errors.New("bad input")
)

var validSubtypes = map[string]bool{
	"architecture": true,
	"triage":       true,
	"timeline":     true,
}

var validProposalStatuses = map[string]bool{
	"draft":             true,
	"proposed":          true,
	"approved":          true,
	"changes_requested": true,
	"parked":            true,
}

var validDecisions = map[string]bool{
	"approved":          true,
	"changes_requested": true,
	"parked":            true,
}

var validDropPadModes = map[string]bool{
	"collection": true,
	"file_ops":   true,
}

var validDropPadOps = map[string]bool{
	"copy":      true,
	"move":      true,
	"reference": true,
}

var validDropPadStatuses = map[string]bool{
	"active":   true,
	"executed": true,
	"archived": true,
}

// Proposal is a visual alignment proposal (architecture flow, bucket triage, or timeline).
type Proposal struct {
	ID           string            `json:"id"`
	Title        string            `json:"title"`
	Summary      string            `json:"summary"`
	Subtype      string            `json:"subtype"`
	Status       string            `json:"status"`
	Payload      json.RawMessage   `json:"payload"`
	DecisionNote string            `json:"decision_note,omitempty"`
	Rev          int               `json:"rev"`
	CreatedAt    string            `json:"created_at"`
	UpdatedAt    string            `json:"updated_at"`
	DecidedAt    string            `json:"decided_at,omitempty"`
	Revisions    []ProposalRevision `json:"revisions,omitempty"`
}

// ProposalRevision is one historical snapshot of a proposal.
type ProposalRevision struct {
	ProposalID string          `json:"proposal_id"`
	Rev        int             `json:"rev"`
	Title      string          `json:"title"`
	Summary    string          `json:"summary,omitempty"`
	Status     string          `json:"status"`
	Payload    json.RawMessage `json:"payload"`
	Note       string          `json:"note,omitempty"`
	Actor      string          `json:"actor"`
	CreatedAt  string          `json:"created_at"`
}

// DropPadItem is one item (file, ref, or note) staged in a Drop Pad.
type DropPadItem struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"` // "file" | "ref" | "note"
	RefOrPath string         `json:"ref_or_path"`
	Label     string         `json:"label"`
	DestPath  string         `json:"dest_path,omitempty"`
	Status    string         `json:"status,omitempty"` // "staged" | "done" | "skipped"
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// DropPad is a shared drag-and-drop collection or staged file-operations pad.
type DropPad struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Summary   string        `json:"summary"`
	Mode      string        `json:"mode"`                // "collection" | "file_ops"
	TargetDir string        `json:"target_dir,omitempty"`
	Operation string        `json:"operation,omitempty"` // "copy" | "move" | "reference"
	Status    string        `json:"status"`              // "active" | "executed" | "archived"
	Items     []DropPadItem `json:"items"`
	CreatedAt string        `json:"created_at"`
	UpdatedAt string        `json:"updated_at"`
}

type store struct {
	db *sql.DB
}

func stamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func newID(prefix string) string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

func normalizePayload(raw json.RawMessage, subtype string) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		switch subtype {
		case "triage":
			return json.RawMessage(`{"buckets":[],"items":[]}`), nil
		case "timeline":
			return json.RawMessage(`{"phases":[],"items":[]}`), nil
		default:
			return json.RawMessage(`{"nodes":[],"edges":[],"groups":[],"callouts":[]}`), nil
		}
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		raw = json.RawMessage(s)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, fmt.Errorf("%w: proposal payload must be a JSON object", errBadInput)
	}
	out, err := json.Marshal(obj)
	return out, err
}

// ---- Proposals ----

type proposalInput struct {
	ID      string          `json:"id"`
	Title   string          `json:"title"`
	Summary string          `json:"summary"`
	Subtype string          `json:"subtype"`
	Status  string          `json:"status"`
	Payload json.RawMessage `json:"payload"`
	Note    string          `json:"note"`
	Actor   string          `json:"actor"`
}

func (s *store) createProposal(ctx context.Context, in proposalInput) (Proposal, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return Proposal{}, fmt.Errorf("%w: a proposal needs a title", errBadInput)
	}
	subtype := strings.TrimSpace(in.Subtype)
	if subtype == "" {
		subtype = "architecture"
	}
	if !validSubtypes[subtype] {
		return Proposal{}, fmt.Errorf("%w: subtype must be architecture, triage or timeline, not %q", errBadInput, subtype)
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = "proposed"
	}
	if !validProposalStatuses[status] {
		return Proposal{}, fmt.Errorf("%w: invalid proposal status %q", errBadInput, status)
	}
	payload, err := normalizePayload(in.Payload, subtype)
	if err != nil {
		return Proposal{}, err
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = newID("ap")
	}
	actor := strings.TrimSpace(in.Actor)
	if actor == "" {
		actor = "agent"
	}
	now := stamp()
	p := Proposal{
		ID:        id,
		Title:     title,
		Summary:   strings.TrimSpace(in.Summary),
		Subtype:   subtype,
		Status:    status,
		Payload:   payload,
		Rev:       1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Proposal{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO align_proposals (id, title, summary, subtype, status, payload, decision_note, rev, created_at, updated_at, decided_at)
		VALUES (?, ?, ?, ?, ?, ?, '', 1, ?, ?, '')`,
		p.ID, p.Title, p.Summary, p.Subtype, p.Status, string(p.Payload), now, now); err != nil {
		return Proposal{}, err
	}
	rev := ProposalRevision{
		ProposalID: p.ID,
		Rev:        1,
		Title:      p.Title,
		Summary:    p.Summary,
		Status:     p.Status,
		Payload:    p.Payload,
		Note:       strings.TrimSpace(in.Note),
		Actor:      actor,
		CreatedAt:  now,
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO align_proposal_revisions (proposal_id, rev, title, summary, status, payload, note, actor, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rev.ProposalID, rev.Rev, rev.Title, rev.Summary, rev.Status, string(rev.Payload), rev.Note, rev.Actor, rev.CreatedAt); err != nil {
		return Proposal{}, err
	}
	if err := tx.Commit(); err != nil {
		return Proposal{}, err
	}
	p.Revisions = []ProposalRevision{rev}
	return p, nil
}

func scanProposal(row interface{ Scan(...any) error }) (Proposal, error) {
	var p Proposal
	var payload string
	if err := row.Scan(&p.ID, &p.Title, &p.Summary, &p.Subtype, &p.Status, &payload, &p.DecisionNote, &p.Rev, &p.CreatedAt, &p.UpdatedAt, &p.DecidedAt); err != nil {
		return p, err
	}
	p.Payload = json.RawMessage(payload)
	return p, nil
}

const proposalCols = `id, title, summary, subtype, status, payload, decision_note, rev, created_at, updated_at, decided_at`

func (s *store) listProposals(ctx context.Context, subtype, status string) ([]Proposal, error) {
	where, args := []string{"1=1"}, []any{}
	if subtype = strings.TrimSpace(subtype); subtype != "" {
		where, args = append(where, "subtype = ?"), append(args, subtype)
	}
	if status = strings.TrimSpace(status); status != "" {
		where, args = append(where, "status = ?"), append(args, status)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+proposalCols+` FROM align_proposals WHERE `+ //nolint:gosec // constant fragments
		strings.Join(where, " AND ")+` ORDER BY updated_at DESC, id DESC LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Proposal{}
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *store) getProposal(ctx context.Context, id string, withRevs bool) (Proposal, error) {
	id = strings.TrimPrefix(strings.TrimSpace(id), "align:")
	p, err := scanProposal(s.db.QueryRowContext(ctx, `SELECT `+proposalCols+` FROM align_proposals WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, errNotFound
	}
	if err != nil || !withRevs {
		return p, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT proposal_id, rev, title, summary, status, payload, note, actor, created_at
		FROM align_proposal_revisions WHERE proposal_id = ? ORDER BY rev DESC`, id)
	if err != nil {
		return p, err
	}
	defer func() { _ = rows.Close() }()
	revs := []ProposalRevision{}
	for rows.Next() {
		var r ProposalRevision
		var payload string
		if err := rows.Scan(&r.ProposalID, &r.Rev, &r.Title, &r.Summary, &r.Status, &payload, &r.Note, &r.Actor, &r.CreatedAt); err != nil {
			return p, err
		}
		r.Payload = json.RawMessage(payload)
		revs = append(revs, r)
	}
	p.Revisions = revs
	return p, rows.Err()
}

type proposalPatch struct {
	Title   *string         `json:"title"`
	Summary *string         `json:"summary"`
	Subtype *string         `json:"subtype"`
	Status  *string         `json:"status"`
	Payload json.RawMessage `json:"payload"`
	Note    string          `json:"note"`
	Actor   string          `json:"actor"`
}

func (s *store) updateProposal(ctx context.Context, id string, patch proposalPatch) (Proposal, error) {
	id = strings.TrimPrefix(strings.TrimSpace(id), "align:")
	p, err := s.getProposal(ctx, id, false)
	if err != nil {
		return p, err
	}
	if patch.Title != nil {
		t := strings.TrimSpace(*patch.Title)
		if t == "" {
			return p, fmt.Errorf("%w: title cannot be empty", errBadInput)
		}
		p.Title = t
	}
	if patch.Summary != nil {
		p.Summary = strings.TrimSpace(*patch.Summary)
	}
	if patch.Subtype != nil && strings.TrimSpace(*patch.Subtype) != "" {
		st := strings.TrimSpace(*patch.Subtype)
		if !validSubtypes[st] {
			return p, fmt.Errorf("%w: invalid subtype %q", errBadInput, st)
		}
		p.Subtype = st
	}
	if patch.Status != nil && strings.TrimSpace(*patch.Status) != "" {
		st := strings.TrimSpace(*patch.Status)
		if !validProposalStatuses[st] {
			return p, fmt.Errorf("%w: invalid status %q", errBadInput, st)
		}
		p.Status = st
	}
	if len(patch.Payload) > 0 && string(patch.Payload) != "null" {
		norm, err := normalizePayload(patch.Payload, p.Subtype)
		if err != nil {
			return p, err
		}
		p.Payload = norm
	}
	p.Rev++
	p.UpdatedAt = stamp()
	actor := strings.TrimSpace(patch.Actor)
	if actor == "" {
		actor = "owner"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE align_proposals SET title=?, summary=?, subtype=?, status=?, payload=?, rev=?, updated_at=? WHERE id=?`,
		p.Title, p.Summary, p.Subtype, p.Status, string(p.Payload), p.Rev, p.UpdatedAt, p.ID); err != nil {
		return p, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO align_proposal_revisions (proposal_id, rev, title, summary, status, payload, note, actor, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Rev, p.Title, p.Summary, p.Status, string(p.Payload), strings.TrimSpace(patch.Note), actor, p.UpdatedAt); err != nil {
		return p, err
	}
	if err := tx.Commit(); err != nil {
		return p, err
	}
	return s.getProposal(ctx, p.ID, true)
}

type decideInput struct {
	Decision string          `json:"decision"`
	Note     string          `json:"note"`
	Payload  json.RawMessage `json:"payload"`
	Actor    string          `json:"actor"`
}

func (s *store) decideProposal(ctx context.Context, id string, in decideInput) (Proposal, error) {
	id = strings.TrimPrefix(strings.TrimSpace(id), "align:")
	decision := strings.TrimSpace(in.Decision)
	if !validDecisions[decision] {
		return Proposal{}, fmt.Errorf("%w: decision must be approved, changes_requested or parked, not %q", errBadInput, decision)
	}
	p, err := s.getProposal(ctx, id, false)
	if err != nil {
		return p, err
	}
	if len(in.Payload) > 0 && string(in.Payload) != "null" {
		norm, err := normalizePayload(in.Payload, p.Subtype)
		if err != nil {
			return p, err
		}
		p.Payload = norm
	}
	now := stamp()
	p.Status = decision
	p.DecisionNote = strings.TrimSpace(in.Note)
	p.Rev++
	p.UpdatedAt = now
	p.DecidedAt = now
	actor := strings.TrimSpace(in.Actor)
	if actor == "" {
		actor = "owner"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE align_proposals SET status=?, payload=?, decision_note=?, rev=?, updated_at=?, decided_at=? WHERE id=?`,
		p.Status, string(p.Payload), p.DecisionNote, p.Rev, p.UpdatedAt, p.DecidedAt, p.ID); err != nil {
		return p, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO align_proposal_revisions (proposal_id, rev, title, summary, status, payload, note, actor, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Rev, p.Title, p.Summary, p.Status, string(p.Payload), p.DecisionNote, actor, now); err != nil {
		return p, err
	}
	if err := tx.Commit(); err != nil {
		return p, err
	}
	return s.getProposal(ctx, p.ID, true)
}

func (s *store) deleteProposal(ctx context.Context, id string) error {
	id = strings.TrimPrefix(strings.TrimSpace(id), "align:")
	res, err := s.db.ExecContext(ctx, `DELETE FROM align_proposals WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNotFound
	}
	return nil
}

// ---- Drop Pads ----

func normalizeDropPadItems(items []DropPadItem) []DropPadItem {
	if items == nil {
		return []DropPadItem{}
	}
	out := make([]DropPadItem, 0, len(items))
	for i, it := range items {
		it.RefOrPath = strings.TrimSpace(it.RefOrPath)
		it.Label = strings.TrimSpace(it.Label)
		if it.ID == "" {
			it.ID = fmt.Sprintf("dpi_%d_%s", i+1, hex.EncodeToString([]byte(fmt.Sprint(i+1))))
		}
		if it.Kind == "" {
			if strings.HasPrefix(it.RefOrPath, "/") || strings.HasPrefix(it.RefOrPath, "~/") || strings.HasPrefix(it.RefOrPath, "./") {
				it.Kind = "file"
			} else if strings.Contains(it.RefOrPath, ":") {
				it.Kind = "ref"
			} else {
				it.Kind = "note"
			}
		}
		if it.Label == "" {
			if it.Kind == "file" && it.RefOrPath != "" {
				it.Label = filepath.Base(it.RefOrPath)
			} else {
				it.Label = it.RefOrPath
			}
		}
		if it.Status == "" {
			it.Status = "staged"
		}
		out = append(out, it)
	}
	return out
}

type dropPadInput struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Summary   string        `json:"summary"`
	Mode      string        `json:"mode"`
	TargetDir string        `json:"target_dir"`
	Operation string        `json:"operation"`
	Status    string        `json:"status"`
	Items     []DropPadItem `json:"items"`
}

func (s *store) createDropPad(ctx context.Context, in dropPadInput) (DropPad, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return DropPad{}, fmt.Errorf("%w: a drop pad needs a title", errBadInput)
	}
	mode := strings.TrimSpace(in.Mode)
	if mode == "" {
		mode = "collection"
	}
	if !validDropPadModes[mode] {
		return DropPad{}, fmt.Errorf("%w: mode must be collection or file_ops, not %q", errBadInput, mode)
	}
	op := strings.TrimSpace(in.Operation)
	if op == "" {
		if mode == "file_ops" {
			op = "copy"
		} else {
			op = "reference"
		}
	}
	if !validDropPadOps[op] {
		return DropPad{}, fmt.Errorf("%w: operation must be copy, move or reference, not %q", errBadInput, op)
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = "active"
	}
	if !validDropPadStatuses[status] {
		return DropPad{}, fmt.Errorf("%w: invalid drop pad status %q", errBadInput, status)
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = newID("dp")
	}
	now := stamp()
	items := normalizeDropPadItems(in.Items)
	itemsRaw, _ := json.Marshal(items)
	dp := DropPad{
		ID:        id,
		Title:     title,
		Summary:   strings.TrimSpace(in.Summary),
		Mode:      mode,
		TargetDir: strings.TrimSpace(in.TargetDir),
		Operation: op,
		Status:    status,
		Items:     items,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO align_droppads (id, title, summary, mode, target_dir, operation, status, items, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		dp.ID, dp.Title, dp.Summary, dp.Mode, dp.TargetDir, dp.Operation, dp.Status, string(itemsRaw), now, now)
	return dp, err
}

const dropPadCols = `id, title, summary, mode, target_dir, operation, status, items, created_at, updated_at`

func scanDropPad(row interface{ Scan(...any) error }) (DropPad, error) {
	var dp DropPad
	var itemsRaw string
	if err := row.Scan(&dp.ID, &dp.Title, &dp.Summary, &dp.Mode, &dp.TargetDir, &dp.Operation, &dp.Status, &itemsRaw, &dp.CreatedAt, &dp.UpdatedAt); err != nil {
		return dp, err
	}
	_ = json.Unmarshal([]byte(itemsRaw), &dp.Items)
	if dp.Items == nil {
		dp.Items = []DropPadItem{}
	}
	return dp, nil
}

func (s *store) listDropPads(ctx context.Context, mode, status string) ([]DropPad, error) {
	where, args := []string{"1=1"}, []any{}
	if mode = strings.TrimSpace(mode); mode != "" {
		where, args = append(where, "mode = ?"), append(args, mode)
	}
	if status = strings.TrimSpace(status); status != "" {
		where, args = append(where, "status = ?"), append(args, status)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+dropPadCols+` FROM align_droppads WHERE `+ //nolint:gosec // constant fragments
		strings.Join(where, " AND ")+` ORDER BY updated_at DESC, id DESC LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []DropPad{}
	for rows.Next() {
		dp, err := scanDropPad(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, dp)
	}
	return out, rows.Err()
}

func (s *store) getDropPad(ctx context.Context, id string) (DropPad, error) {
	id = strings.TrimPrefix(strings.TrimSpace(id), "droppad:")
	dp, err := scanDropPad(s.db.QueryRowContext(ctx, `SELECT `+dropPadCols+` FROM align_droppads WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return dp, errNotFound
	}
	return dp, err
}

type dropPadPatch struct {
	Title     *string        `json:"title"`
	Summary   *string        `json:"summary"`
	Mode      *string        `json:"mode"`
	TargetDir *string        `json:"target_dir"`
	Operation *string        `json:"operation"`
	Status    *string        `json:"status"`
	Items     *[]DropPadItem `json:"items"`
}

func (s *store) updateDropPad(ctx context.Context, id string, patch dropPadPatch) (DropPad, error) {
	id = strings.TrimPrefix(strings.TrimSpace(id), "droppad:")
	dp, err := s.getDropPad(ctx, id)
	if err != nil {
		return dp, err
	}
	if patch.Title != nil {
		t := strings.TrimSpace(*patch.Title)
		if t == "" {
			return dp, fmt.Errorf("%w: title cannot be empty", errBadInput)
		}
		dp.Title = t
	}
	if patch.Summary != nil {
		dp.Summary = strings.TrimSpace(*patch.Summary)
	}
	if patch.Mode != nil && strings.TrimSpace(*patch.Mode) != "" {
		m := strings.TrimSpace(*patch.Mode)
		if !validDropPadModes[m] {
			return dp, fmt.Errorf("%w: invalid mode %q", errBadInput, m)
		}
		dp.Mode = m
	}
	if patch.TargetDir != nil {
		dp.TargetDir = strings.TrimSpace(*patch.TargetDir)
	}
	if patch.Operation != nil && strings.TrimSpace(*patch.Operation) != "" {
		op := strings.TrimSpace(*patch.Operation)
		if !validDropPadOps[op] {
			return dp, fmt.Errorf("%w: invalid operation %q", errBadInput, op)
		}
		dp.Operation = op
	}
	if patch.Status != nil && strings.TrimSpace(*patch.Status) != "" {
		st := strings.TrimSpace(*patch.Status)
		if !validDropPadStatuses[st] {
			return dp, fmt.Errorf("%w: invalid status %q", errBadInput, st)
		}
		dp.Status = st
	}
	if patch.Items != nil {
		dp.Items = normalizeDropPadItems(*patch.Items)
	}
	dp.UpdatedAt = stamp()
	itemsRaw, _ := json.Marshal(dp.Items)
	_, err = s.db.ExecContext(ctx, `UPDATE align_droppads SET title=?, summary=?, mode=?, target_dir=?, operation=?, status=?, items=?, updated_at=? WHERE id=?`,
		dp.Title, dp.Summary, dp.Mode, dp.TargetDir, dp.Operation, dp.Status, string(itemsRaw), dp.UpdatedAt, dp.ID)
	return dp, err
}

func (s *store) stageDropPadItems(ctx context.Context, id, targetDir, operation string, addItems []DropPadItem) (DropPad, error) {
	id = strings.TrimPrefix(strings.TrimSpace(id), "droppad:")
	dp, err := s.getDropPad(ctx, id)
	if err != nil {
		return dp, err
	}
	if targetDir = strings.TrimSpace(targetDir); targetDir != "" {
		dp.TargetDir = targetDir
	}
	if operation = strings.TrimSpace(operation); operation != "" {
		if !validDropPadOps[operation] {
			return dp, fmt.Errorf("%w: invalid operation %q", errBadInput, operation)
		}
		dp.Operation = operation
	}
	if dp.Operation == "copy" || dp.Operation == "move" {
		dp.Mode = "file_ops"
	}
	normalized := normalizeDropPadItems(addItems)
	byID := map[string]int{}
	for idx, existing := range dp.Items {
		byID[existing.ID] = idx
	}
	for _, it := range normalized {
		if idx, exists := byID[it.ID]; exists {
			dp.Items[idx] = it
		} else {
			if it.ID == "" || byID[it.ID] != 0 {
				it.ID = newID("dpi")
			}
			dp.Items = append(dp.Items, it)
		}
	}
	dp.UpdatedAt = stamp()
	itemsRaw, _ := json.Marshal(dp.Items)
	_, err = s.db.ExecContext(ctx, `UPDATE align_droppads SET mode=?, target_dir=?, operation=?, items=?, updated_at=? WHERE id=?`,
		dp.Mode, dp.TargetDir, dp.Operation, string(itemsRaw), dp.UpdatedAt, dp.ID)
	return dp, err
}

func (s *store) deleteDropPad(ctx context.Context, id string) error {
	id = strings.TrimPrefix(strings.TrimSpace(id), "droppad:")
	res, err := s.db.ExecContext(ctx, `DELETE FROM align_droppads WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNotFound
	}
	return nil
}

func expandLocalPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("%w: empty path", errBadInput)
	}
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, p[2:])
	}
	return filepath.Clean(p), nil
}

func (s *store) executeDropPad(ctx context.Context, id string) (DropPad, error) {
	id = strings.TrimPrefix(strings.TrimSpace(id), "droppad:")
	dp, err := s.getDropPad(ctx, id)
	if err != nil {
		return dp, err
	}
	if dp.Mode != "file_ops" || dp.Operation == "reference" {
		for i := range dp.Items {
			if dp.Items[i].Status == "" || dp.Items[i].Status == "staged" {
				dp.Items[i].Status = "done"
			}
		}
		dp.Status = "executed"
		dp.UpdatedAt = stamp()
		itemsRaw, _ := json.Marshal(dp.Items)
		_, err = s.db.ExecContext(ctx, `UPDATE align_droppads SET status=?, items=?, updated_at=? WHERE id=?`,
			dp.Status, string(itemsRaw), dp.UpdatedAt, dp.ID)
		return dp, err
	}
	targetRoot := ""
	if dp.TargetDir != "" {
		targetRoot, err = expandLocalPath(dp.TargetDir)
		if err != nil {
			return dp, err
		}
		if err := os.MkdirAll(targetRoot, 0o750); err != nil {
			return dp, err
		}
	}
	for i := range dp.Items {
		it := &dp.Items[i]
		if it.Status == "skipped" || it.Status == "done" {
			continue
		}
		if it.Kind != "file" {
			it.Status = "done"
			continue
		}
		src, err := expandLocalPath(it.RefOrPath)
		if err != nil {
			return dp, fmt.Errorf("item %s source: %w", it.ID, err)
		}
		dest := strings.TrimSpace(it.DestPath)
		if dest == "" {
			if targetRoot == "" {
				return dp, fmt.Errorf("%w: item %s needs dest_path or drop pad target_dir", errBadInput, it.ID)
			}
			dest = filepath.Join(targetRoot, filepath.Base(src))
		} else {
			if !filepath.IsAbs(dest) && !strings.HasPrefix(dest, "~/") && targetRoot != "" {
				dest = filepath.Join(targetRoot, dest)
			} else {
				dest, err = expandLocalPath(dest)
				if err != nil {
					return dp, fmt.Errorf("item %s dest: %w", it.ID, err)
				}
			}
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
			return dp, err
		}
		switch dp.Operation {
		case "copy":
			if err := copyFileSafe(src, dest); err != nil {
				return dp, err
			}
		case "move":
			if err := moveFileSafe(src, dest); err != nil {
				return dp, err
			}
		}
		it.DestPath = dest
		it.Status = "done"
	}
	dp.Status = "executed"
	dp.UpdatedAt = stamp()
	itemsRaw, _ := json.Marshal(dp.Items)
	_, err = s.db.ExecContext(ctx, `UPDATE align_droppads SET status=?, items=?, updated_at=? WHERE id=?`,
		dp.Status, string(itemsRaw), dp.UpdatedAt, dp.ID)
	return dp, err
}

func copyFileSafe(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // user-triggered local file staging operation
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return fmt.Errorf("%w: cannot copy directory %s directly", errBadInput, src)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fi.Mode().Perm()) //nolint:gosec // user-triggered local file staging operation
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func moveFileSafe(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyFileSafe(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}
