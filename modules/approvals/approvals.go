// Package approvals is the Approvals module: Gatecrasher's approvals in front of the owner, as
// cards, decided where they are shown.
//
// The module follows Gatecrasher's event stream as the owner (the owner key in the settings),
// shows each pending approval as a card, sends the owner's answer to Gatecrasher's decide,
// and takes the card away when the approval closes, here or anywhere else. A secret request
// is a form the page posts straight to the producer's intake: the value never reaches this
// server, its database, its log, the event bus or the agent (see cards.go and web/).
//
// Contract: gatecrasher docs/integration-guide.md and api/openapi/gate-v1.yaml.
package approvals

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hollis-labs/surface/module"
)

//go:embed migrations/*.sql
var migrations embed.FS

func init() { module.Register(&mod{}) }

type mod struct {
	module.Base
	h  module.Host
	db *sql.DB

	stop  context.CancelFunc
	done  chan struct{}
	wake  chan struct{}
	unsub []func()
	// life is the module's own context; answers run under it and are waited for at Stop.
	life context.Context
	work sync.WaitGroup

	mu    sync.Mutex
	state string // "" connected, or why not
	// cancelStream ends the open connection (a settings change reconnects at once).
	cancelStream context.CancelFunc
}

func (m *mod) Manifest() module.Manifest {
	return module.Manifest{
		ID: "approvals", Title: "Approvals", Version: "0.1.0",
		About: "Gatecrasher's approvals as cards: what an agent or a service asks the owner to allow, decided where it is shown.",
		Settings: []module.Setting{
			{Key: "url", Title: "Gatecrasher URL", About: "Gatecrasher's address (https, or http on loopback). Empty: approvals are not followed. Only the owner changes it.", Type: module.SettingString, Default: "", Owner: true},
			{Key: "key_file", Title: "Owner key file", About: "A file holding the owner's Gatecrasher key (gatecrasher.approval.read and .decide). Read when used; never logged. Only the owner changes it.", Type: module.SettingString, Default: "", Owner: true},
		},
		Store: &module.Store{Migrations: migrations, Dir: "migrations"},
		Tools: []module.Tool{{
			Name: "approvals_status", Level: module.LevelRead,
			Description: "Whether the app is following Gatecrasher's approvals, and the approvals shown lately with their status. Deciding is the owner's, on the cards; you cannot decide an approval.",
			Input:       json.RawMessage(`{"type":"object","properties":{}}`),
			Run: func(ctx context.Context, _ module.Host, _ json.RawMessage) (any, error) {
				return m.status(ctx), nil
			},
		}},
		Around: func(ctx context.Context, _ module.Host) (string, error) {
			n := m.pendingCount(ctx)
			if n == 0 {
				return "", nil
			}
			return plural(n) + " waiting on the owner in Gatecrasher (they see each as a card; only they decide).", nil
		},
	}
}

func plural(n int) string {
	if n == 1 {
		return "1 approval"
	}
	return strconv.Itoa(n) + " approvals"
}

func (m *mod) Start(_ context.Context, h module.Host) error {
	m.h, m.db = h, h.Store()
	m.wake = make(chan struct{}, 1)
	m.state = "connecting"
	m.unsub = nil
	run, cancel := context.WithCancel(context.Background())
	m.life, m.stop, m.done = run, cancel, make(chan struct{})
	m.unsub = append(m.unsub, h.Events().Subscribe("cards.changed", func(ev module.Event) {
		var c module.Card
		if json.Unmarshal(ev.Data, &c) != nil || c.Status == "" || !strings.HasPrefix(c.Subject, "approval:") {
			return
		}
		if m.life.Err() != nil {
			return
		}
		m.work.Add(1)
		go func() {
			defer m.work.Done()
			m.answered(m.life, c)
		}()
	}))
	// The secret form's intake, for the card this module showed: never read from the card.
	h.Handle("GET /form/{card}", http.HandlerFunc(m.form))
	// A change of the URL or the key reconnects at once.
	for _, k := range []string{"url", "key_file"} {
		m.unsub = append(m.unsub, h.Config().Watch(k, func(any) { m.reconnect() }))
	}
	go m.follow(run)
	return nil
}

func (m *mod) Stop(context.Context) error {
	for _, u := range m.unsub {
		u()
	}
	m.unsub = nil
	m.work.Wait() // answers in flight finish before the module's context ends
	if m.stop != nil {
		m.stop()
		<-m.done
	}
	return nil
}

// form answers the page's secret card with where its form posts: only for a card this module
// showed for a secret request that is still pending, from what Gatecrasher said.
func (m *mod) form(w http.ResponseWriter, r *http.Request) {
	sh, ok := m.byCard(r.Context(), r.PathValue("card"))
	if !ok || sh.typ != "request_secret" || sh.status != "pending" || sh.post == "" {
		http.Error(w, "not a secret request this app is showing", http.StatusNotFound)
		return
	}
	origin, err := intakeOrigin(sh.post)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]string{"post_url": sh.post, "origin": origin, "field": sh.field})
}

func (m *mod) reconnect() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
	m.mu.Lock()
	cancel := m.cancelStream
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// gate is the configured Gatecrasher, read from the settings at the moment of use.
func (m *mod) gate() (*gate, error) {
	cfg := m.h.Config()
	return newGate(cfg.String("url"), cfg.String("key_file"))
}

func (m *mod) setState(err error) {
	s := ""
	if err != nil {
		s = err.Error()
	}
	m.mu.Lock()
	changed := m.state != s
	m.state = s
	m.mu.Unlock()
	if changed {
		if err != nil && !errors.Is(err, errNotConfigured) {
			m.h.Log().Warn("approvals: not following Gatecrasher", "err", err)
		}
		m.h.Events().Publish("approvals.state", map[string]string{"problem": s})
	}
}

// ---- store ----

func (m *mod) cursor(ctx context.Context, base string) int64 {
	var seq int64
	_ = m.db.QueryRowContext(ctx, `SELECT seq FROM cursor WHERE url=?`, base).Scan(&seq)
	return seq
}

func (m *mod) setCursor(ctx context.Context, base string, seq int64) {
	_, _ = m.db.ExecContext(ctx, `INSERT INTO cursor(url, seq) VALUES(?, ?) ON CONFLICT(url) DO UPDATE SET seq=excluded.seq WHERE excluded.seq > cursor.seq`, base, seq)
}

// shownRow is one approval as this module shows it, with what its answer is checked against.
type shownRow struct {
	id, card, typ, status, title string
	scopes                       []string
	post, field                  string
}

const shownCols = `approval_id, card_id, type, status, title, scopes, post_url, field`

func (m *mod) scanShown(row *sql.Row) (shownRow, bool) {
	var r shownRow
	var scopes string
	if err := row.Scan(&r.id, &r.card, &r.typ, &r.status, &r.title, &scopes, &r.post, &r.field); err != nil {
		return r, false
	}
	_ = json.Unmarshal([]byte(scopes), &r.scopes)
	return r, true
}

func (m *mod) shown(ctx context.Context, id string) (shownRow, bool) {
	return m.scanShown(m.db.QueryRowContext(ctx, `SELECT `+shownCols+` FROM shown WHERE approval_id=?`, id))
}

func (m *mod) byCard(ctx context.Context, card string) (shownRow, bool) {
	if card == "" {
		return shownRow{}, false
	}
	return m.scanShown(m.db.QueryRowContext(ctx, `SELECT `+shownCols+` FROM shown WHERE card_id=?`, card))
}

func (m *mod) remember(ctx context.Context, r shownRow) {
	scopes, _ := json.Marshal(r.scopes)
	if r.scopes == nil {
		scopes = []byte("[]")
	}
	_, _ = m.db.ExecContext(ctx, `INSERT INTO shown(`+shownCols+`, updated_at) VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(approval_id) DO UPDATE SET card_id=excluded.card_id, type=excluded.type, status=excluded.status, title=excluded.title,
		scopes=excluded.scopes, post_url=excluded.post_url, field=excluded.field, updated_at=excluded.updated_at`,
		r.id, r.card, r.typ, r.status, r.title, string(scopes), r.post, r.field, time.Now().UTC().Format(time.RFC3339))
}

func (m *mod) pendingCount(ctx context.Context) int {
	var n int
	_ = m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM shown WHERE status='pending'`).Scan(&n)
	return n
}

func (m *mod) status(ctx context.Context) map[string]any {
	m.mu.Lock()
	state := m.state
	m.mu.Unlock()
	if _, err := m.gate(); err != nil {
		state = err.Error()
	}
	cursor := int64(0)
	if g, err := m.gate(); err == nil {
		cursor = m.cursor(ctx, g.base)
	}
	out := map[string]any{"following": state == "", "cursor": cursor}
	if state != "" {
		out["problem"] = state
	}
	rows, err := m.db.QueryContext(ctx, `SELECT approval_id, type, status, title, updated_at FROM shown ORDER BY updated_at DESC LIMIT 20`)
	if err == nil {
		defer func() { _ = rows.Close() }()
		var list []map[string]string
		for rows.Next() {
			var id, typ, st, title, at string
			if rows.Scan(&id, &typ, &st, &title, &at) == nil {
				list = append(list, map[string]string{"approval_id": id, "type": typ, "status": st, "title": title, "updated_at": at})
			}
		}
		out["recent"] = list
	}
	return out
}
