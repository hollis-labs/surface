package scratch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/tetherlink"
)

// Caster, as Basalisk keeps scratch there: each item is also caster://scratch/<id>, a short
// Markdown text, and an edit is a new revision. The module's store is the working copy; the
// syncer writes behind it and tries again when Caster cannot be reached. What Caster has no
// place for (done, put away, the kind, the tags) travels as Caster tags. Items an agent or
// Basalisk put in the namespace are read back (Import). Off unless the sync setting is on.
// Ported from Basalisk (internal/scratch/sync.go).

// allowed are the gateway tools the syncer calls. caster_put is the only write.
var allowed = tetherlink.Allowed{"caster_put": true, "caster_ls": true, "caster_get": true}

// Ref is the Caster ref an item is kept under.
func Ref(id string) string { return "caster://scratch/" + id }

type syncer struct {
	m          *mod
	kick       chan struct{}
	stop       context.CancelFunc
	done       sync.WaitGroup
	importDue  atomic.Bool  // read Caster's items back on the next wake
	lastAsked  atomic.Int64 // when the list last asked for that (unix nanoseconds)
	lastImport time.Time    // the run loop's own
}

func (y *syncer) Kick() {
	select {
	case y.kick <- struct{}{}:
	default:
	}
}

// importSoon asks for Caster's items to be read back: when the sync is turned on, and when the
// list is opened (at most every 30 s).
func (y *syncer) importSoon(throttle bool) {
	now := time.Now().UnixNano()
	if throttle && time.Duration(now-y.lastAsked.Load()) < askEvery {
		return
	}
	y.lastAsked.Store(now)
	y.importDue.Store(true)
	y.Kick()
}

// askEvery is how often opening the list asks for a read-back.
var askEvery = 30 * time.Second

// nextBackoff is the wait after another failure: firstBackoff, doubling, at most maxBackoff.
func nextBackoff(prev time.Duration) time.Duration { return min(max(prev*2, firstBackoff), maxBackoff) }

// Waits: importEvery between read-backs; after a failure, firstBackoff doubling to maxBackoff,
// back to nothing after a pass that wrote everything.
const (
	importEvery  = 10 * time.Minute
	firstBackoff = 5 * time.Second
	maxBackoff   = 5 * time.Minute
)

// enabled reports whether items are written to Caster now: the setting is on and Tether is set.
func (m *mod) syncEnabled() bool {
	if m.h == nil || !m.h.Config().Bool("caster_sync") {
		return false
	}
	if m.link != nil {
		return true
	}
	c := m.h.Config()
	return strings.TrimSpace(c.String("tether_command")) != "" || strings.TrimSpace(c.String("tether_url")) != ""
}

func (m *mod) tether() (tetherlink.Caller, error) {
	if m.link != nil {
		return m.link, nil
	}
	cfg := m.h.Config()
	c := tetherlink.New("scratch", tetherlink.Config{URL: cfg.String("tether_url"), TokenFile: cfg.String("tether_token_file"), Command: cfg.String("tether_command")}, allowed)
	if c == nil {
		return nil, errors.New("the Caster sync needs Tether: set scratch.tether_command (or scratch.tether_url)")
	}
	return c, nil
}

// run writes behind the store until ctx ends: soon after a change; again after a failure, from
// 5 s doubling to 5 min; and reads Caster's items back every ten minutes, when the sync is
// turned on, and when the list is opened.
func (y *syncer) run(ctx context.Context) {
	backoff := time.Duration(0)
	for {
		wait := importEvery
		if y.m.syncEnabled() {
			failed := false
			if y.importDue.Swap(false) || time.Since(y.lastImport) >= importEvery {
				y.lastImport = time.Now()
				if _, err := y.m.importItems(ctx); err != nil {
					slog.Warn("scratch: Caster's items were not read back", "err", err)
					failed = true
				}
			}
			more, pfailed := y.m.pass(ctx)
			failed = failed || pfailed
			switch {
			case failed:
				backoff = nextBackoff(backoff)
				wait = backoff
			case more:
				backoff = 0
				continue
			default:
				backoff = 0
				wait = max(importEvery-time.Since(y.lastImport), time.Second)
			}
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-y.kick:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// managed reports whether a Caster tag is one this module writes from the item itself; any
// other (a Basalisk link "task:CW-…", its own kinds such as "kind:command") is kept as it is.
func managed(tag string) bool {
	switch {
	case tag == "scratch", tag == "done", tag == "archived",
		strings.HasPrefix(tag, "day:"), strings.HasPrefix(tag, "tag:"):
		return true
	case strings.HasPrefix(tag, "kind:"):
		return kinds[strings.TrimPrefix(tag, "kind:")]
	}
	return false
}

// pass writes up to 25 items Caster does not have as they are now (one in conflict is left
// alone). One that fails is noted and the rest are still written. It reports whether more are
// waiting and whether any write failed.
func (m *mod) pass(ctx context.Context) (more, failed bool) {
	c, err := m.tether()
	if err != nil {
		return false, true
	}
	rows, err := m.st.db.QueryContext(ctx, `SELECT id FROM items WHERE synced_at <> updated_at AND conflict_rev = 0 ORDER BY sync_error <> '', updated_at LIMIT 26`)
	if err != nil {
		return false, true
	}
	var todo []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			todo = append(todo, id)
		}
	}
	_ = rows.Close()
	if len(todo) > 25 {
		more, todo = true, todo[:25]
	}
	for _, id := range todo {
		if !m.write(ctx, c, id) {
			failed = true
		}
	}
	return more, failed
}

// write puts one item in Caster and checks the revision Caster answers against the one this
// item was at: a later one means someone wrote in between (both are in Caster's history), which
// is flagged as a conflict rather than taken as ours.
func (m *mod) write(ctx context.Context, c tetherlink.Caller, id string) bool {
	var it Item
	var removed bool
	var rev int
	var keep string
	var deleted sql.NullString
	err := m.st.db.QueryRowContext(ctx, `SELECT id, text, kind, done, tags, created_at, updated_at, caster_ref, deleted_at, caster_rev, caster_tags FROM items WHERE id=?`, id).
		Scan(&it.ID, &it.Text, &it.Kind, &it.Done, &it.Tags, &it.Created, &it.Updated, &it.CasterRef, &deleted, &rev, &keep)
	if err != nil {
		return true
	}
	removed = deleted.Valid
	// An item found in Caster keeps the name it has there.
	name := "scratch/" + it.ID
	if rest, ok := strings.CutPrefix(it.CasterRef, "caster://"); ok && rest != "" {
		name = rest
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	raw, err := c.Call(callCtx, "caster_put", map[string]any{
		"name": name, "content": markdown(it), "mime": "text/markdown", "title": clip(firstLine(it.Text), 120),
		"tags": strings.Join(casterTags(it, removed, strings.Fields(keep)), " "), "source": "surface:scratch",
	})
	cancel()
	var out struct {
		Ref string `json:"ref"`
	}
	var got int
	if err == nil {
		if err = json.Unmarshal([]byte(raw), &out); err == nil {
			_, at, _ := strings.Cut(out.Ref, "@")
			if got, err = strconv.Atoi(at); err != nil {
				err = errors.New("caster_put did not say the new revision")
			}
		}
	}
	if err != nil {
		slog.Warn("scratch: an item was not written to Caster", "id", it.ID, "err", err)
		_, _ = m.st.db.ExecContext(ctx, `UPDATE items SET sync_error=? WHERE id=?`, clip(err.Error(), 300), it.ID)
		return false
	}
	if rev > 0 && got != rev && got != rev+1 {
		_, _ = m.st.db.ExecContext(ctx, `UPDATE items SET caster_ref=?, conflict_rev=?, sync_error=? WHERE id=?`, "caster://"+name, got,
			fmt.Sprintf("conflict: Caster had a change of its own (revision %d) under this write; both are in its history", got-1), it.ID)
		return true
	}
	_, _ = m.st.db.ExecContext(ctx, `UPDATE items SET caster_ref=?, caster_rev=?, synced_at=?, sync_error='' WHERE id=? AND updated_at=?`, "caster://"+name, got, it.Updated, it.ID, it.Updated)
	return true
}

// markdown is an item as Basalisk writes one: a note as it is, a to-do as a task-list line,
// anything else as "::<kind> text".
func markdown(it Item) string {
	switch it.Kind {
	case "note":
		return it.Text + "\n"
	case "todo":
		box := "[ ] "
		if it.Done {
			box = "[x] "
		}
		return box + it.Text + "\n"
	}
	s := "::" + it.Kind + " " + it.Text
	if it.Done {
		s += "  (done)"
	}
	return s + "\n"
}

// casterTags are an item's tags in Caster: that it is scratch, its kind, its day, its own tags,
// whether it is done or put away, and the tags it had there that this module does not manage
// (keep). A kept kind (Basalisk's "kind:command") stands for the item's kind.
func casterTags(it Item, removed bool, keep []string) []string {
	day := it.Created
	if len(day) >= 10 {
		day = day[:10]
	}
	tags := []string{"scratch"}
	ownKind := false
	for _, t := range keep {
		ownKind = ownKind || strings.HasPrefix(t, "kind:")
	}
	if !ownKind {
		tags = append(tags, "kind:"+it.Kind)
	}
	tags = append(tags, "day:"+day)
	for _, t := range strings.FieldsFunc(it.Tags, func(r rune) bool { return r == ',' || r == ' ' }) {
		tags = append(tags, "tag:"+t)
	}
	if it.Done {
		tags = append(tags, "done")
	}
	if removed {
		tags = append(tags, "archived")
	}
	return append(tags, keep...)
}

// fromTags reads what Caster's tags say of an item: done, put away, its own tags, its kind, and
// the tags this module does not manage.
func fromTags(tags string) (done, archived bool, own []string, kind string, keep []string) {
	for _, t := range strings.Fields(tags) {
		switch {
		case t == "done":
			done = true
		case t == "archived":
			archived = true
		case strings.HasPrefix(t, "tag:"):
			own = append(own, strings.TrimPrefix(t, "tag:"))
		case strings.HasPrefix(t, "kind:") && kinds[strings.TrimPrefix(t, "kind:")]:
			kind = strings.TrimPrefix(t, "kind:")
		case !managed(t):
			keep = append(keep, t)
		}
	}
	return
}

// maxImport is the largest text read back from Caster as a scratch item.
const maxImport = 8 << 10

var kindLine = regexp.MustCompile(`^::([a-z]+)\s+`)

// parse reads an item back from its Caster text: the inverse of markdown.
func parse(content string) (text, kind string, done bool) {
	s := strings.TrimSpace(content)
	switch {
	case strings.HasPrefix(s, "[ ] "):
		return strings.TrimSpace(s[4:]), "todo", false
	case strings.HasPrefix(strings.ToLower(s), "[x] "):
		return strings.TrimSpace(s[4:]), "todo", true
	}
	if m := kindLine.FindStringSubmatch(s); m != nil && kinds[m[1]] {
		rest := strings.TrimSpace(s[len(m[0]):])
		done = strings.HasSuffix(rest, "(done)")
		return strings.TrimSpace(strings.TrimSuffix(rest, "(done)")), m[1], done
	}
	return s, "note", false
}

// importItems reads Caster's scratch namespace back: items an agent, Basalisk or another
// machine put there that are not here (small text only: a document parked in the namespace is
// not a scratch item), and changes made there to items this module holds. A change made there
// is taken when the item has no change of its own waiting; otherwise the item is flagged as in
// conflict and neither side is written over. Done, put away, the kind and the item's own tags
// come from Caster's tags.
func (m *mod) importItems(ctx context.Context) (int, error) {
	c, err := m.tether()
	if err != nil {
		return 0, err
	}
	raw, err := tetherlink.CallAll(ctx, c, "caster_ls", map[string]any{"ns": "scratch"})
	if err != nil {
		return 0, err
	}
	var list struct {
		Items []struct {
			Ref     string `json:"ref"`
			Rev     int    `json:"rev"`
			Tags    string `json:"tags"`
			Updated string `json:"updated"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return 0, err
	}
	n := 0
	for _, li := range list.Items {
		name, ok := strings.CutPrefix(li.Ref, "caster://scratch/")
		if !ok || name == "" {
			continue
		}
		var id, synced, updated string
		var rev, conflict int
		err := m.st.db.QueryRowContext(ctx, `SELECT id, caster_rev, synced_at, updated_at, conflict_rev FROM items WHERE caster_ref=? OR id=?`, li.Ref, name).
			Scan(&id, &rev, &synced, &updated, &conflict)
		known := err == nil
		if known && (li.Rev <= rev || conflict >= li.Rev) {
			continue // Caster has nothing newer (or the conflict is already flagged)
		}
		if known && synced != updated {
			_, _ = m.st.db.ExecContext(ctx, `UPDATE items SET conflict_rev=?, sync_error=? WHERE id=?`, li.Rev,
				fmt.Sprintf("conflict: changed in Caster (revision %d) and here; neither is written over", li.Rev), id)
			n++
			continue
		}
		if !known {
			var passed int
			_ = m.st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM passed WHERE ref=?`, li.Ref).Scan(&passed)
			if passed > 0 {
				continue
			}
		}
		got, err := c.Call(ctx, "caster_get", map[string]any{"ref": li.Ref, "max_bytes": maxImport + 1})
		if err != nil {
			return n, err
		}
		var item struct {
			Content   string `json:"content"`
			State     string `json:"state"`
			Truncated bool   `json:"truncated"`
		}
		if json.Unmarshal([]byte(got), &item) != nil || item.State != "resolved" || item.Truncated || len(item.Content) > maxImport || strings.TrimSpace(item.Content) == "" {
			if !known {
				_, _ = m.st.db.ExecContext(ctx, `INSERT OR IGNORE INTO passed(ref) VALUES(?)`, li.Ref)
			}
			continue
		}
		text, kind, done := parse(item.Content)
		tdone, archived, own, tkind, keep := fromTags(li.Tags)
		if tkind != "" {
			kind = tkind
		}
		done = done || tdone
		at := li.Updated
		if _, perr := time.Parse(time.RFC3339Nano, at); perr != nil {
			at = stamp()
		}
		var deletedAt any
		if archived {
			deletedAt = at
		}
		if known {
			now := stamp()
			_, err = m.st.db.ExecContext(ctx, `UPDATE items SET text=?, kind=?, done=?, tags=?, deleted_at=?, caster_rev=?, caster_tags=?, updated_at=?, synced_at=?, sync_error='' WHERE id=?`,
				text, kind, done, strings.Join(own, ", "), deletedAt, li.Rev, strings.Join(keep, " "), now, now, id)
		} else {
			if id = name; !strings.HasPrefix(id, "s_") {
				id = "cst_" + name
			}
			_, err = m.st.db.ExecContext(ctx, `INSERT OR IGNORE INTO items (id, text, kind, done, tags, created_at, updated_at, deleted_at, caster_ref, caster_rev, caster_tags, synced_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				id, text, kind, done, strings.Join(own, ", "), at, at, deletedAt, li.Ref, li.Rev, strings.Join(keep, " "), at)
		}
		if err != nil {
			return n, err
		}
		n++
	}
	if n > 0 {
		m.h.Events().Publish("scratch.changed", map[string]any{"imported": n})
	}
	return n, nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

// syncSettings turn the sync on and say how Tether is reached.
var syncSettings = []module.Setting{
	{Key: "caster_sync", Title: "Keep items in Caster", About: "Write each item to caster://scratch/<id> as Basalisk does, and read back items others put there.", Type: module.SettingBool, Default: false},
	{Key: "tether_command", Title: "Tether command", About: "A command that speaks MCP over stdio to Tether, through which Caster is reached.", Type: module.SettingString, Default: ""},
	{Key: "tether_url", Title: "Tether MCP URL", About: "Instead of a command: Tether's MCP endpoint over HTTP.", Type: module.SettingString, Default: ""},
	{Key: "tether_token_file", Title: "Tether token file", About: "The file holding the token for the MCP URL.", Type: module.SettingString, Default: ""},
}
