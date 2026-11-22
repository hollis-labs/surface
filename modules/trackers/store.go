package trackers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	errNotFound = errors.New("not found")
	errBadInput = errors.New("bad input")
)

var validTrackerLifecycles = map[string]bool{
	"active":    true,
	"paused":    true,
	"completed": true,
	"archived":  true,
}

var validItemLifecycles = map[string]bool{
	"todo":        true,
	"in_progress": true,
	"blocked":     true,
	"review":      true,
	"done":        true,
	"parked":      true,
}

// Tracker is a categorized work tracker with taxonomy tags, lifecycle, metadata, and extras.
type Tracker struct {
	ID         string         `json:"id"`
	Slug       string         `json:"slug"`
	Title      string         `json:"title"`
	Summary    string         `json:"summary"`
	Category   string         `json:"category"`
	Tags       []string       `json:"tags"`
	Lifecycle  string         `json:"lifecycle"`
	Metadata   map[string]any `json:"metadata"`
	Extras     map[string]any `json:"extras"`
	ItemCounts map[string]int `json:"item_counts,omitempty"`
	Items      []TrackerItem  `json:"items,omitempty"`
	CreatedAt  string         `json:"created_at"`
	UpdatedAt  string         `json:"updated_at"`
}

// TrackerItem is one work item inside a Tracker.
type TrackerItem struct {
	ID        string         `json:"id"`
	TrackerID string         `json:"tracker_id"`
	Title     string         `json:"title"`
	Summary   string         `json:"summary"`
	Lifecycle string         `json:"lifecycle"`
	Category  string         `json:"category"`
	Tags      []string       `json:"tags"`
	Position  int            `json:"position"`
	Assignee  string         `json:"assignee"`
	Metadata  map[string]any `json:"metadata"`
	Extras    map[string]any `json:"extras"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
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

var nonSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonSlugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-")
	}
	return s
}

func normalizeTags(tags []string) []string {
	if len(tags) == 0 {
		return []string{}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

func normalizeMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func hasTag(tags []string, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return true
	}
	for _, t := range tags {
		if strings.EqualFold(t, want) {
			return true
		}
	}
	return false
}

// ---- Trackers (Lists) ----

type trackerInput struct {
	ID        string         `json:"id"`
	Slug      string         `json:"slug"`
	Title     string         `json:"title"`
	Summary   string         `json:"summary"`
	Category  string         `json:"category"`
	Tags      []string       `json:"tags"`
	Lifecycle string         `json:"lifecycle"`
	Metadata  map[string]any `json:"metadata"`
	Extras    map[string]any `json:"extras"`
}

const trackerCols = `id, slug, title, summary, category, tags, lifecycle, metadata, extras, created_at, updated_at`

func scanTracker(row interface{ Scan(...any) error }) (Tracker, error) {
	var t Tracker
	var tagsRaw, metaRaw, extrasRaw string
	if err := row.Scan(&t.ID, &t.Slug, &t.Title, &t.Summary, &t.Category, &tagsRaw, &t.Lifecycle, &metaRaw, &extrasRaw, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return t, err
	}
	_ = json.Unmarshal([]byte(tagsRaw), &t.Tags)
	if t.Tags == nil {
		t.Tags = []string{}
	}
	_ = json.Unmarshal([]byte(metaRaw), &t.Metadata)
	if t.Metadata == nil {
		t.Metadata = map[string]any{}
	}
	_ = json.Unmarshal([]byte(extrasRaw), &t.Extras)
	if t.Extras == nil {
		t.Extras = map[string]any{}
	}
	return t, nil
}

func (s *store) createTracker(ctx context.Context, in trackerInput) (Tracker, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return Tracker{}, fmt.Errorf("%w: a tracker needs a title", errBadInput)
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = newID("trk")
	}
	slug := slugify(in.Slug)
	if slug == "" {
		slug = slugify(title)
	}
	if slug == "" {
		slug = id
	}
	// Ensure slug uniqueness if needed.
	var exists int
	if s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM trackers WHERE slug = ?`, slug).Scan(&exists) == nil && exists > 0 {
		slug = slug + "-" + id[len(id)-4:]
	}
	category := strings.TrimSpace(in.Category)
	if category == "" {
		category = "general"
	}
	lifecycle := strings.TrimSpace(in.Lifecycle)
	if lifecycle == "" {
		lifecycle = "active"
	}
	if !validTrackerLifecycles[lifecycle] {
		return Tracker{}, fmt.Errorf("%w: tracker lifecycle must be active, paused, completed or archived, not %q", errBadInput, lifecycle)
	}
	tags := normalizeTags(in.Tags)
	meta := normalizeMap(in.Metadata)
	extras := normalizeMap(in.Extras)
	tagsJSON, _ := json.Marshal(tags)
	metaJSON, _ := json.Marshal(meta)
	extrasJSON, _ := json.Marshal(extras)
	now := stamp()

	t := Tracker{
		ID:         id,
		Slug:       slug,
		Title:      title,
		Summary:    strings.TrimSpace(in.Summary),
		Category:   category,
		Tags:       tags,
		Lifecycle:  lifecycle,
		Metadata:   meta,
		Extras:     extras,
		ItemCounts: map[string]int{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO trackers (`+trackerCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Slug, t.Title, t.Summary, t.Category, string(tagsJSON), t.Lifecycle, string(metaJSON), string(extrasJSON), now, now)
	return t, err
}

func (s *store) itemCountsByTracker(ctx context.Context) map[string]map[string]int {
	out := map[string]map[string]int{}
	rows, err := s.db.QueryContext(ctx, `SELECT tracker_id, lifecycle, COUNT(*) FROM tracker_items GROUP BY tracker_id, lifecycle`)
	if err != nil {
		return out
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var tid, lc string
		var cnt int
		if rows.Scan(&tid, &lc, &cnt) == nil {
			if out[tid] == nil {
				out[tid] = map[string]int{}
			}
			out[tid][lc] = cnt
			out[tid]["total"] += cnt
		}
	}
	return out
}

type trackerFilter struct {
	Category  string
	Lifecycle string
	Tag       string
	Q         string
}

func (s *store) listTrackers(ctx context.Context, f trackerFilter) ([]Tracker, error) {
	where, args := []string{"1=1"}, []any{}
	if c := strings.TrimSpace(f.Category); c != "" {
		where, args = append(where, "category = ?"), append(args, c)
	}
	if lc := strings.TrimSpace(f.Lifecycle); lc != "" {
		where, args = append(where, "lifecycle = ?"), append(args, lc)
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		where = append(where, "(title LIKE ? OR summary LIKE ? OR slug LIKE ? OR tags LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like, like, like)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+trackerCols+` FROM trackers WHERE `+ //nolint:gosec // constant fragments
		strings.Join(where, " AND ")+` ORDER BY updated_at DESC, id DESC LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	counts := s.itemCountsByTracker(ctx)
	out := []Tracker{}
	for rows.Next() {
		t, err := scanTracker(rows)
		if err != nil {
			return nil, err
		}
		if !hasTag(t.Tags, f.Tag) {
			continue
		}
		t.ItemCounts = counts[t.ID]
		if t.ItemCounts == nil {
			t.ItemCounts = map[string]int{}
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func cleanTrackerRef(id string) string {
	id = strings.TrimSpace(id)
	id = strings.TrimPrefix(id, "tracker.list:")
	id = strings.TrimPrefix(id, "tracker:")
	return id
}

func (s *store) getTracker(ctx context.Context, idOrSlug string, withItems bool) (Tracker, error) {
	idOrSlug = cleanTrackerRef(idOrSlug)
	t, err := scanTracker(s.db.QueryRowContext(ctx, `SELECT `+trackerCols+` FROM trackers WHERE id = ? OR slug = ?`, idOrSlug, idOrSlug))
	if errors.Is(err, sql.ErrNoRows) {
		return t, errNotFound
	}
	if err != nil {
		return t, err
	}
	counts := s.itemCountsByTracker(ctx)
	t.ItemCounts = counts[t.ID]
	if t.ItemCounts == nil {
		t.ItemCounts = map[string]int{}
	}
	if withItems {
		items, ierr := s.listItems(ctx, itemFilter{TrackerID: t.ID})
		if ierr != nil {
			return t, ierr
		}
		t.Items = items
	}
	return t, nil
}

type trackerPatch struct {
	Slug      *string        `json:"slug"`
	Title     *string        `json:"title"`
	Summary   *string        `json:"summary"`
	Category  *string        `json:"category"`
	Tags      *[]string      `json:"tags"`
	Lifecycle *string        `json:"lifecycle"`
	Metadata  map[string]any `json:"metadata"`
	Extras    map[string]any `json:"extras"`
}

func (s *store) updateTracker(ctx context.Context, idOrSlug string, patch trackerPatch) (Tracker, error) {
	t, err := s.getTracker(ctx, idOrSlug, false)
	if err != nil {
		return t, err
	}
	if patch.Title != nil {
		title := strings.TrimSpace(*patch.Title)
		if title == "" {
			return t, fmt.Errorf("%w: title cannot be empty", errBadInput)
		}
		t.Title = title
	}
	if patch.Slug != nil && strings.TrimSpace(*patch.Slug) != "" {
		t.Slug = slugify(*patch.Slug)
	}
	if patch.Summary != nil {
		t.Summary = strings.TrimSpace(*patch.Summary)
	}
	if patch.Category != nil && strings.TrimSpace(*patch.Category) != "" {
		t.Category = strings.TrimSpace(*patch.Category)
	}
	if patch.Tags != nil {
		t.Tags = normalizeTags(*patch.Tags)
	}
	if patch.Lifecycle != nil && strings.TrimSpace(*patch.Lifecycle) != "" {
		lc := strings.TrimSpace(*patch.Lifecycle)
		if !validTrackerLifecycles[lc] {
			return t, fmt.Errorf("%w: invalid tracker lifecycle %q", errBadInput, lc)
		}
		t.Lifecycle = lc
	}
	if patch.Metadata != nil {
		t.Metadata = normalizeMap(patch.Metadata)
	}
	if patch.Extras != nil {
		t.Extras = normalizeMap(patch.Extras)
	}
	t.UpdatedAt = stamp()
	tagsJSON, _ := json.Marshal(t.Tags)
	metaJSON, _ := json.Marshal(t.Metadata)
	extrasJSON, _ := json.Marshal(t.Extras)
	if _, err := s.db.ExecContext(ctx, `UPDATE trackers SET slug=?, title=?, summary=?, category=?, tags=?, lifecycle=?, metadata=?, extras=?, updated_at=? WHERE id=?`,
		t.Slug, t.Title, t.Summary, t.Category, string(tagsJSON), t.Lifecycle, string(metaJSON), string(extrasJSON), t.UpdatedAt, t.ID); err != nil {
		return t, err
	}
	return s.getTracker(ctx, t.ID, true)
}

func (s *store) deleteTracker(ctx context.Context, idOrSlug string) error {
	idOrSlug = cleanTrackerRef(idOrSlug)
	res, err := s.db.ExecContext(ctx, `DELETE FROM trackers WHERE id = ? OR slug = ?`, idOrSlug, idOrSlug)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNotFound
	}
	return nil
}

// ---- Tracker Items ----

const itemCols = `id, tracker_id, title, summary, lifecycle, category, tags, position, assignee, metadata, extras, created_at, updated_at`

func scanItem(row interface{ Scan(...any) error }) (TrackerItem, error) {
	var it TrackerItem
	var tagsRaw, metaRaw, extrasRaw string
	if err := row.Scan(&it.ID, &it.TrackerID, &it.Title, &it.Summary, &it.Lifecycle, &it.Category, &tagsRaw, &it.Position, &it.Assignee, &metaRaw, &extrasRaw, &it.CreatedAt, &it.UpdatedAt); err != nil {
		return it, err
	}
	_ = json.Unmarshal([]byte(tagsRaw), &it.Tags)
	if it.Tags == nil {
		it.Tags = []string{}
	}
	_ = json.Unmarshal([]byte(metaRaw), &it.Metadata)
	if it.Metadata == nil {
		it.Metadata = map[string]any{}
	}
	_ = json.Unmarshal([]byte(extrasRaw), &it.Extras)
	if it.Extras == nil {
		it.Extras = map[string]any{}
	}
	return it, nil
}

type itemInput struct {
	ID        string         `json:"id"`
	TrackerID string         `json:"tracker_id"`
	Title     string         `json:"title"`
	Summary   string         `json:"summary"`
	Lifecycle string         `json:"lifecycle"`
	Category  string         `json:"category"`
	Tags      []string       `json:"tags"`
	Position  *int           `json:"position"`
	Assignee  string         `json:"assignee"`
	Metadata  map[string]any `json:"metadata"`
	Extras    map[string]any `json:"extras"`
}

func (s *store) createItem(ctx context.Context, in itemInput) (TrackerItem, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return TrackerItem{}, fmt.Errorf("%w: a tracker item needs a title", errBadInput)
	}
	t, err := s.getTracker(ctx, in.TrackerID, false)
	if err != nil {
		return TrackerItem{}, fmt.Errorf("%w: tracker %q not found", errBadInput, in.TrackerID)
	}
	lifecycle := strings.TrimSpace(in.Lifecycle)
	if lifecycle == "" {
		lifecycle = "todo"
	}
	if !validItemLifecycles[lifecycle] {
		return TrackerItem{}, fmt.Errorf("%w: item lifecycle must be todo, in_progress, blocked, review, done or parked, not %q", errBadInput, lifecycle)
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = newID("ti")
	}
	pos := 0
	if in.Position != nil {
		pos = *in.Position
	} else {
		_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), 0) + 1 FROM tracker_items WHERE tracker_id = ?`, t.ID).Scan(&pos)
	}
	tags := normalizeTags(in.Tags)
	meta := normalizeMap(in.Metadata)
	extras := normalizeMap(in.Extras)
	tagsJSON, _ := json.Marshal(tags)
	metaJSON, _ := json.Marshal(meta)
	extrasJSON, _ := json.Marshal(extras)
	now := stamp()

	it := TrackerItem{
		ID:        id,
		TrackerID: t.ID,
		Title:     title,
		Summary:   strings.TrimSpace(in.Summary),
		Lifecycle: lifecycle,
		Category:  strings.TrimSpace(in.Category),
		Tags:      tags,
		Position:  pos,
		Assignee:  strings.TrimSpace(in.Assignee),
		Metadata:  meta,
		Extras:    extras,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO tracker_items (`+itemCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		it.ID, it.TrackerID, it.Title, it.Summary, it.Lifecycle, it.Category, string(tagsJSON), it.Position, it.Assignee, string(metaJSON), string(extrasJSON), now, now); err != nil {
		return TrackerItem{}, err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE trackers SET updated_at = ? WHERE id = ?`, now, t.ID)
	return it, nil
}

type itemFilter struct {
	TrackerID string
	Lifecycle string
	Category  string
	Tag       string
	Q         string
}

func (s *store) listItems(ctx context.Context, f itemFilter) ([]TrackerItem, error) {
	where, args := []string{"1=1"}, []any{}
	if tid := cleanTrackerRef(f.TrackerID); tid != "" {
		if t, err := s.getTracker(ctx, tid, false); err == nil {
			tid = t.ID
		}
		where, args = append(where, "tracker_id = ?"), append(args, tid)
	}
	if lc := strings.TrimSpace(f.Lifecycle); lc != "" {
		where, args = append(where, "lifecycle = ?"), append(args, lc)
	}
	if c := strings.TrimSpace(f.Category); c != "" {
		where, args = append(where, "category = ?"), append(args, c)
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		where = append(where, "(title LIKE ? OR summary LIKE ? OR tags LIKE ? OR assignee LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like, like, like)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemCols+` FROM tracker_items WHERE `+ //nolint:gosec // constant fragments
		strings.Join(where, " AND ")+` ORDER BY position ASC, created_at ASC, id ASC LIMIT 500`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []TrackerItem{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		if !hasTag(it.Tags, f.Tag) {
			continue
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func cleanItemRef(id string) string {
	id = strings.TrimSpace(id)
	id = strings.TrimPrefix(id, "tracker.item:")
	id = strings.TrimPrefix(id, "tracker:")
	return id
}

func (s *store) getItem(ctx context.Context, id string) (TrackerItem, error) {
	id = cleanItemRef(id)
	it, err := scanItem(s.db.QueryRowContext(ctx, `SELECT `+itemCols+` FROM tracker_items WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return it, errNotFound
	}
	return it, err
}

type itemPatch struct {
	TrackerID *string        `json:"tracker_id"`
	Title     *string        `json:"title"`
	Summary   *string        `json:"summary"`
	Lifecycle *string        `json:"lifecycle"`
	Category  *string        `json:"category"`
	Tags      *[]string      `json:"tags"`
	Position  *int           `json:"position"`
	Assignee  *string        `json:"assignee"`
	Metadata  map[string]any `json:"metadata"`
	Extras    map[string]any `json:"extras"`
}

func (s *store) updateItem(ctx context.Context, id string, patch itemPatch) (TrackerItem, error) {
	it, err := s.getItem(ctx, id)
	if err != nil {
		return it, err
	}
	if patch.TrackerID != nil && strings.TrimSpace(*patch.TrackerID) != "" {
		t, err := s.getTracker(ctx, *patch.TrackerID, false)
		if err != nil {
			return it, fmt.Errorf("%w: tracker %q not found", errBadInput, *patch.TrackerID)
		}
		it.TrackerID = t.ID
	}
	if patch.Title != nil {
		t := strings.TrimSpace(*patch.Title)
		if t == "" {
			return it, fmt.Errorf("%w: title cannot be empty", errBadInput)
		}
		it.Title = t
	}
	if patch.Summary != nil {
		it.Summary = strings.TrimSpace(*patch.Summary)
	}
	if patch.Lifecycle != nil && strings.TrimSpace(*patch.Lifecycle) != "" {
		lc := strings.TrimSpace(*patch.Lifecycle)
		if !validItemLifecycles[lc] {
			return it, fmt.Errorf("%w: invalid item lifecycle %q", errBadInput, lc)
		}
		it.Lifecycle = lc
	}
	if patch.Category != nil {
		it.Category = strings.TrimSpace(*patch.Category)
	}
	if patch.Tags != nil {
		it.Tags = normalizeTags(*patch.Tags)
	}
	if patch.Position != nil {
		it.Position = *patch.Position
	}
	if patch.Assignee != nil {
		it.Assignee = strings.TrimSpace(*patch.Assignee)
	}
	if patch.Metadata != nil {
		it.Metadata = normalizeMap(patch.Metadata)
	}
	if patch.Extras != nil {
		it.Extras = normalizeMap(patch.Extras)
	}
	it.UpdatedAt = stamp()
	tagsJSON, _ := json.Marshal(it.Tags)
	metaJSON, _ := json.Marshal(it.Metadata)
	extrasJSON, _ := json.Marshal(it.Extras)
	if _, err := s.db.ExecContext(ctx, `UPDATE tracker_items SET tracker_id=?, title=?, summary=?, lifecycle=?, category=?, tags=?, position=?, assignee=?, metadata=?, extras=?, updated_at=? WHERE id=?`,
		it.TrackerID, it.Title, it.Summary, it.Lifecycle, it.Category, string(tagsJSON), it.Position, it.Assignee, string(metaJSON), string(extrasJSON), it.UpdatedAt, it.ID); err != nil {
		return it, err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE trackers SET updated_at = ? WHERE id = ?`, it.UpdatedAt, it.TrackerID)
	return it, nil
}

func (s *store) deleteItem(ctx context.Context, id string) error {
	it, err := s.getItem(ctx, id)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM tracker_items WHERE id = ?`, it.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNotFound
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE trackers SET updated_at = ? WHERE id = ?`, stamp(), it.TrackerID)
	return nil
}

func (s *store) reorderItems(ctx context.Context, trackerID string, itemIDs []string) ([]TrackerItem, error) {
	t, err := s.getTracker(ctx, trackerID, false)
	if err != nil {
		return nil, err
	}
	now := stamp()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	for i, rawID := range itemIDs {
		id := cleanItemRef(rawID)
		if _, err := tx.ExecContext(ctx, `UPDATE tracker_items SET position = ?, updated_at = ? WHERE id = ? AND tracker_id = ?`, i+1, now, id, t.ID); err != nil {
			return nil, err
		}
	}
	_, _ = tx.ExecContext(ctx, `UPDATE trackers SET updated_at = ? WHERE id = ?`, now, t.ID)
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.listItems(ctx, itemFilter{TrackerID: t.ID})
}
