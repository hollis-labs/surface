package scratch

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"sync"
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
	m    *mod
	kick chan struct{}
	stop context.CancelFunc
	done sync.WaitGroup
}

func (y *syncer) Kick() {
	select {
	case y.kick <- struct{}{}:
	default:
	}
}

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

// run writes behind the store until ctx ends: soon after a change, again with a growing wait
// after a failure, and every ten minutes to read back what others put in Caster.
func (y *syncer) run(ctx context.Context) {
	wait := 10 * time.Minute
	for {
		if y.m.syncEnabled() {
			if _, err := y.m.importItems(ctx); err != nil {
				slog.Warn("scratch: Caster's items were not read back", "err", err)
			}
			more, failed := y.m.pass(ctx)
			switch {
			case failed:
				wait = min(max(wait*2, 5*time.Second), 5*time.Minute)
			case more:
				continue
			default:
				wait = 10 * time.Minute
			}
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-y.kick:
			timer.Stop()
			wait = 10 * time.Minute
		case <-timer.C:
		}
	}
}

// pass writes up to 25 items Caster does not have as they are now. It reports whether more are
// waiting and whether a write failed.
func (m *mod) pass(ctx context.Context) (more, failed bool) {
	c, err := m.tether()
	if err != nil {
		return false, true
	}
	rows, err := m.st.db.QueryContext(ctx, `SELECT id FROM items WHERE synced_at <> updated_at ORDER BY updated_at LIMIT 26`)
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
		it, removed, err := m.st.anyItem(ctx, id)
		if err != nil {
			continue
		}
		// An item found in Caster keeps the name it has there.
		name := "scratch/" + it.ID
		if rest, ok := strings.CutPrefix(it.CasterRef, "caster://"); ok && rest != "" {
			name = rest
		}
		callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, err = c.Call(callCtx, "caster_put", map[string]any{
			"name": name, "content": markdown(it), "mime": "text/markdown", "title": clip(firstLine(it.Text), 120),
			"tags": strings.Join(casterTags(it, removed), " "), "source": "surface:scratch",
		})
		cancel()
		if err != nil {
			slog.Warn("scratch: an item was not written to Caster", "id", it.ID, "err", err)
			_, _ = m.st.db.ExecContext(ctx, `UPDATE items SET sync_error=? WHERE id=?`, clip(err.Error(), 300), it.ID)
			return more, true
		}
		_, _ = m.st.db.ExecContext(ctx, `UPDATE items SET caster_ref=?, synced_at=?, sync_error='' WHERE id=? AND updated_at=?`, "caster://"+name, it.Updated, it.ID, it.Updated)
	}
	return more, false
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
// and whether it is done or put away.
func casterTags(it Item, removed bool) []string {
	day := it.Created
	if len(day) >= 10 {
		day = day[:10]
	}
	tags := []string{"scratch", "kind:" + it.Kind, "day:" + day}
	for _, t := range strings.FieldsFunc(it.Tags, func(r rune) bool { return r == ',' || r == ' ' }) {
		tags = append(tags, "tag:"+t)
	}
	if it.Done {
		tags = append(tags, "done")
	}
	if removed {
		tags = append(tags, "archived")
	}
	return tags
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

// importItems reads back items in Caster's scratch namespace that are not here: ones an agent,
// Basalisk or another machine put there. Small text only (a document parked in the namespace is
// not a scratch item). They arrive as already written, so they are not written back.
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
		var have, passed int
		_ = m.st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE id=? OR caster_ref=?`, name, li.Ref).Scan(&have)
		_ = m.st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM passed WHERE ref=?`, li.Ref).Scan(&passed)
		if have > 0 || passed > 0 {
			continue
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
			_, _ = m.st.db.ExecContext(ctx, `INSERT OR IGNORE INTO passed(ref) VALUES(?)`, li.Ref)
			continue
		}
		text, kind, done := parse(item.Content)
		at := li.Updated
		if _, err := time.Parse(time.RFC3339Nano, at); err != nil {
			at = stamp()
		}
		id := name
		if !strings.HasPrefix(id, "s_") {
			id = "cst_" + name
		}
		if _, err := m.st.db.ExecContext(ctx, `INSERT OR IGNORE INTO items (id, text, kind, done, created_at, updated_at, caster_ref, synced_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, text, kind, done, at, at, li.Ref, at); err != nil {
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
