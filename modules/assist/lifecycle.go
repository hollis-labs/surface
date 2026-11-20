package assist

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hollis-labs/surface/module"
)

// LifecycleEvent records transport receipts separately from request acknowledgement.
// Consumed mail is not evidence that work was claimed or completed.
type LifecycleEvent struct {
	ID     string `json:"id"`
	At     string `json:"at"`
	Kind   string `json:"kind"`
	State  string `json:"state,omitempty"`
	Detail string `json:"detail,omitempty"`
	TaskID string `json:"task_id,omitempty"`
}

type SentMessage struct {
	ID       string           `json:"id"`
	Kind     string           `json:"kind"`
	Subject  string           `json:"subject"`
	To       string           `json:"to"`
	Account  string           `json:"account"`
	At       string           `json:"at"`
	TaskID   string           `json:"task_id,omitempty"`
	Delivery string           `json:"delivery"`
	Request  string           `json:"request,omitempty"`
	Alert    string           `json:"alert,omitempty"`
	Events   []LifecycleEvent `json:"events"`
}

func (s *store) sentMessages(ctx context.Context, addresses []string) ([]SentMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,kind,about,to_addr,account,at,task_id FROM sent ORDER BY at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	out := []SentMessage{}
	for rows.Next() {
		var m SentMessage
		if err = rows.Scan(&m.ID, &m.Kind, &m.Subject, &m.To, &m.Account, &m.At, &m.TaskID); err != nil {
			break
		}
		// Legacy records have no account. Keep them visible as pre-tracking history.
		if m.Account != "" && !contains(addresses, m.Account) {
			continue
		}
		m.Events = []LifecycleEvent{}
		m.Delivery = "sent"
		out = append(out, m)
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range out {
		m := &out[i]
		events, err := s.db.QueryContext(ctx, `SELECT event_key,at,kind,state,detail,task_id FROM sent_events WHERE message_id=? ORDER BY at,seq,event_key`, m.ID)
		if err != nil {
			return nil, err
		}
		for events.Next() {
			var e LifecycleEvent
			if err = events.Scan(&e.ID, &e.At, &e.Kind, &e.State, &e.Detail, &e.TaskID); err != nil {
				break
			}
			m.Events = append(m.Events, e)
		}
		if err == nil {
			err = events.Err()
		}
		_ = events.Close()
		if err != nil {
			return nil, err
		}
		projectLifecycle(m)
	}
	return out, nil
}

func contains(a []string, s string) bool {
	for _, v := range a {
		if v == s {
			return true
		}
	}
	return false
}

func projectLifecycle(m *SentMessage) {
	rank := map[string]int{"sent": 0, "queued": 1, "delivered": 2, "read": 3, "consumed": 4, "replied": 5}
	offline, unack, replyFailed := false, false, false
	for _, e := range m.Events {
		if e.TaskID != "" {
			m.TaskID = e.TaskID
		}
		if r, ok := rank[e.State]; ok && r >= rank[m.Delivery] {
			m.Delivery = e.State
		}
		switch e.Kind {
		case "wake.outcome":
			if rank[m.Delivery] < 2 {
				offline = (e.Detail == "offline" || e.Detail == "offline-race")
			}
		case "request.unacknowledged_alert":
			unack = true
		case "routing.reply_undeliverable":
			replyFailed = true
		case "routing.reply_delivered":
			replyFailed = false
		}
		if strings.HasPrefix(e.Kind, "request.") && e.Kind != "request.unacknowledged_alert" && e.State != "" {
			m.Request = e.State
		}
	}
	if rank[m.Delivery] >= 2 {
		offline = false
	}
	if m.Request != "" && m.Request != "submitted" {
		unack = false
		offline = false
	}
	if offline {
		m.Alert = "Queued — recipient offline"
	}
	if unack {
		m.Alert = "Request has not been acknowledged"
	}
	if replyFailed {
		m.Alert = "Reply stored, but could not be delivered to your session"
	}
}

func (m *mod) lifecycleSource() string {
	cfg := m.h.Config()
	return fmt.Sprintf("%x", sha256.Sum256([]byte(cfg.String("tether_command")+"\n"+cfg.String("tether_url")+"\n"+cfg.String("addresses"))))
}

// A dedicated long-poll subscriber is independent of automatic mail sorting.
// Settings are re-read at every reconnect. The browser refreshes on assist.sent.
func (m *mod) lifecycleLoop(ctx context.Context) {
	defer close(m.lifecycleDone)
	for ctx.Err() == nil {
		c, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := m.syncLifecycle(c)
		cancel()
		if err != nil && ctx.Err() == nil {
			m.setLifecycleWarning("Delivery tracking is unavailable; showing the last recorded receipts.")
		}
		if err != nil && ctx.Err() == nil {
			m.h.Log().Debug("message lifecycle unavailable", "err", err)
		}
		// Also avoids a tight loop with older gateways returning an empty result.
		delay := time.Second
		if err != nil {
			delay = 5 * time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func (m *mod) syncLifecycle(ctx context.Context) error {
	box, err := m.box()
	if err != nil {
		return err
	}
	source := m.lifecycleSource()
	var cursor int64
	err = m.st.db.QueryRowContext(ctx, `SELECT seq FROM lifecycle_cursor WHERE source=?`, source).Scan(&cursor)
	var raw string
	bootstrap := errors.Is(err, sql.ErrNoRows)
	if err != nil && !bootstrap {
		return err
	}
	if bootstrap {
		// Seed with a bounded recent history; thereafter replay every retained event
		// from the durable environment cursor, including across app restarts.
		raw, err = box.link.Call(ctx, "tether_events_history", map[string]any{"scope": "daemon,session,broker", "kind": lifecycleKinds, "limit": 25, "full_payload": true})
	} else {
		raw, err = box.link.Call(ctx, "tether_events_wait", map[string]any{"after_seq": cursor, "kind": lifecycleKinds, "max_events": 10, "wait_ms": 5000})
	}
	if err != nil {
		return err
	}
	var batch struct {
		OK     bool        `json:"ok"`
		Events []wireEvent `json:"events"`
		Next   int64       `json:"next_cursor"`
		Gap    *struct {
			Reason   string `json:"reason"`
			Earliest int64  `json:"earliest_available"`
			High     int64  `json:"high_water_seq"`
		} `json:"gap"`
	}
	if err = json.Unmarshal([]byte(raw), &batch); err != nil {
		return err
	}
	if !batch.OK {
		return errors.New("tether did not return an event batch")
	}
	if batch.Gap != nil {
		if batch.Gap.Reason == "backfill_in_progress" {
			return errors.New("tether event backfill is in progress")
		}
		// The environment API refuses oversized catch-up and interior retention
		// holes as well as purged prefixes. Seeking to its floor can repeat the
		// same gap forever. Re-seed from history and explicitly report the gap.
		raw, err = box.link.Call(ctx, "tether_events_history", map[string]any{"scope": "daemon,session,broker", "kind": lifecycleKinds, "limit": 25, "full_payload": true})
		if err != nil {
			return err
		}
		high := batch.Gap.High
		batch.Events = nil
		batch.Gap = nil
		batch.OK = false
		if err = json.Unmarshal([]byte(raw), &batch); err != nil {
			return err
		}
		if !batch.OK {
			return errors.New("tether did not return recovery history")
		}
		cursor = high
		if _, err = m.st.db.ExecContext(ctx, `UPDATE lifecycle_cursor SET warning=? WHERE source=?`, "Delivery history has a gap; showing saved receipts and recent events.", source); err != nil {
			return err
		}
	} else if !bootstrap {
		cursor = max(cursor, batch.Next)
	}
	var warning string
	err = m.st.db.QueryRowContext(ctx, `SELECT warning FROM lifecycle_cursor WHERE source=?`, source).Scan(&warning)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	m.setLifecycleWarning(warning)
	for _, e := range batch.Events {
		cursor = max(cursor, e.Seq)
	}
	changed, err := m.st.ingest(ctx, source, cursor, batch.Events, box.addresses)
	if err == nil && changed {
		m.h.Events().Publish("assist.sent", nil)
	}
	if err == nil {
		err = m.lifecycleAlerts(ctx, box.addresses)
	}
	return err
}

func (m *mod) setLifecycleWarning(warning string) {
	m.lifecycleMu.Lock()
	changed := m.lifecycleWarning != warning
	m.lifecycleWarning = warning
	m.lifecycleMu.Unlock()
	if changed {
		m.h.Events().Publish("assist.tracking", nil)
	}
}

func (m *mod) trackSend(ctx context.Context, raw, account, kind, subject, to string) (string, error) {
	r, err := parseReceipt(raw)
	if err != nil {
		return "", err
	}
	_, err = m.st.db.ExecContext(ctx, `INSERT INTO sent(id,kind,about,to_addr,at,account) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET about=excluded.about,account=excluded.account`, r.ID, kind, subject, to, r.At, account)
	if err != nil {
		return r.ID, fmt.Errorf("message %s was sent but could not be tracked; do not resend: %w", r.ID, err)
	}
	p := map[string]any{"message_id": r.ID, "from": account, "to": []string{to}, "msg_kind": kind, "task_id": r.TaskID}
	data, _ := json.Marshal(p)
	events := []wireEvent{{ID: r.ID + ":sent", Kind: "message.sent", At: r.At, Payload: data}}
	if r.Wake != nil {
		p["reason"] = r.Wake.Reason
		p["delivered"] = r.Wake.Delivered
		data, _ = json.Marshal(p)
		events = append(events, wireEvent{ID: r.ID + ":wake", Kind: "wake.outcome", At: r.At, Payload: data})
	}
	_, err = m.st.ingest(ctx, "send", 0, events, []string{account})
	m.h.Events().Publish("assist.sent", nil)
	if err != nil {
		return r.ID, fmt.Errorf("message %s was sent but its receipt could not be saved; do not resend: %w", r.ID, err)
	}
	if err = m.lifecycleAlerts(ctx, []string{account}); err != nil {
		m.h.Log().Warn("delivery alert was not shown", "err", err)
	}
	return r.ID, nil
}

// Cards make offline and unacknowledged requests visible even with this view closed.
// Remember the last alert so dismissing a card survives reconnects and restarts.
func (m *mod) lifecycleAlerts(ctx context.Context, addresses []string) error {
	m.alertMu.Lock()
	defer m.alertMu.Unlock()
	messages, err := m.st.sentMessages(ctx, addresses)
	if err != nil {
		return err
	}
	for _, msg := range messages {
		var previous, card string
		if err = m.st.db.QueryRowContext(ctx, `SELECT alert_text,alert_card FROM sent WHERE id=?`, msg.ID).Scan(&previous, &card); err != nil {
			return err
		}
		if msg.Alert == previous {
			continue
		}
		if card != "" {
			if _, err = m.h.Cards().End(ctx, card, "canceled"); err != nil {
				return err
			}
		}
		card = ""
		if msg.Alert != "" {
			payload, _ := json.Marshal(map[string]any{"body": msg.Subject + "\n\nTo " + msg.To + ". Open Done for you → Sent for the timeline.", "tone": "warning"})
			c, showErr := m.h.Cards().Show(ctx, module.Card{Kind: "info", Title: msg.Alert, Source: "assist", Subject: "sent:" + msg.ID, Payload: payload})
			if showErr != nil {
				return showErr
			}
			card = c.ID
		}
		if _, err = m.st.db.ExecContext(ctx, `UPDATE sent SET alert_text=?,alert_card=? WHERE id=?`, msg.Alert, card, msg.ID); err != nil {
			return err
		}
	}
	return nil
}
