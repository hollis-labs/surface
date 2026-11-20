package assist

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// wireEvent accepts both history rows and the environment event envelope.
type wireEvent struct {
	ID          string          `json:"id"`
	Seq         int64           `json:"seq"`
	Kind        string          `json:"kind"`
	At          string          `json:"at"`
	Time        string          `json:"time"`
	Payload     json.RawMessage `json:"payload"`
	PayloadJSON string          `json:"payload_json"`
}

type lifecyclePayload struct {
	At        string          `json:"at"`
	MessageID string          `json:"message_id"`
	From      string          `json:"from"`
	To        json.RawMessage `json:"to"`
	Target    string          `json:"target"`
	MsgKind   string          `json:"msg_kind"`
	Subject   string          `json:"subject"`
	InReplyTo string          `json:"in_reply_to"`
	ParentID  string          `json:"parent_id"`
	ReplyID   string          `json:"reply_id"`
	TaskID    string          `json:"task_id"`
	Requester string          `json:"requester"`
	Recipient string          `json:"recipient"`
	State     string          `json:"state"`
	Status    string          `json:"status"`
	Reason    string          `json:"reason"`
	Summary   string          `json:"summary"`
	Delivered bool            `json:"delivered"`
}

func (e wireEvent) decode() (lifecyclePayload, error) {
	var p lifecyclePayload
	raw := e.Payload
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte(e.PayloadJSON)
	}
	err := json.Unmarshal(raw, &p)
	return p, err
}
func (p lifecyclePayload) recipients() []string {
	var a []string
	if json.Unmarshal(p.To, &a) == nil {
		return a
	}
	var s string
	if json.Unmarshal(p.To, &s) == nil && s != "" {
		return []string{s}
	}
	return nil
}

const lifecycleKinds = "message.sent,message.queued,message.delivered,message.read,message.consumed,wake.outcome,routing.reply_delivered,routing.reply_undeliverable,request.submitted,request.acknowledged,request.state_changed,request.unacknowledged_alert,request.closed"

// ingest commits the cursor with the projection input: a crash replays safely.
// Only owner-originated mail (or events linked to that mail) is retained.
func (s *store) ingest(ctx context.Context, source string, cursor int64, events []wireEvent, addresses []string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	sort.SliceStable(events, func(i, j int) bool { return events[i].Seq < events[j].Seq })
	changed := false
	for _, e := range events {
		if !contains(strings.Split(lifecycleKinds, ","), e.Kind) {
			continue
		}
		p, decodeErr := e.decode()
		if decodeErr != nil {
			return false, fmt.Errorf("decode %s: %w", e.Kind, decodeErr)
		}
		at := p.At
		if at == "" {
			at = e.Time
		}
		if at == "" {
			at = e.At
		}
		if at == "" {
			at = stamp()
		}
		if parsed, parseErr := time.Parse(time.RFC3339Nano, at); parseErr == nil {
			at = parsed.UTC().Format("2006-01-02T15:04:05.000000000Z")
		}
		id := p.MessageID
		if id == "" && p.TaskID != "" {
			err = tx.QueryRowContext(ctx, `SELECT id FROM sent WHERE task_id=? LIMIT 1`, p.TaskID).Scan(&id)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return false, err
			}
		}
		incomingReply := p.InReplyTo != "" && p.From != "" && !contains(addresses, p.From)
		if incomingReply {
			id = p.InReplyTo
		}
		if strings.HasPrefix(e.Kind, "routing.reply_") {
			id = p.ParentID
		}
		if id == "" {
			continue
		}
		var account string
		err = tx.QueryRowContext(ctx, `SELECT account FROM sent WHERE id=?`, id).Scan(&account)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
		if errors.Is(err, sql.ErrNoRows) {
			from := p.From
			if from == "" {
				from = p.Requester
			}
			if incomingReply || !contains(addresses, from) {
				continue
			}
			to := strings.Join(p.recipients(), ", ")
			if to == "" {
				to = p.Recipient
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO sent(id,kind,about,to_addr,at,account,task_id) VALUES(?,?,?,?,?,?,?)`, id, p.MsgKind, p.Subject, to, at, from, p.TaskID); err != nil {
				return false, err
			}
			account = from
		}
		if account != "" && !contains(addresses, account) {
			continue
		}
		if p.TaskID != "" {
			if _, err = tx.ExecContext(ctx, `UPDATE sent SET task_id=? WHERE id=?`, p.TaskID, id); err != nil {
				return false, err
			}
		}
		state, detail := "", p.Reason
		switch e.Kind {
		case "message.sent":
			state = "sent"
		case "message.queued":
			state = "queued"
		case "message.delivered":
			state = "delivered"
		case "message.read":
			state = "read"
		case "message.consumed":
			state = "consumed"
		case "wake.outcome":
			if p.Delivered {
				state = "delivered"
			} else if p.Reason == "offline" || p.Reason == "offline-race" || p.Reason == "busy" {
				state = "queued"
			}
		case "routing.reply_delivered":
			state = "replied"
		case "request.submitted":
			state = "submitted"
		case "request.acknowledged":
			state = "acknowledged"
		case "request.state_changed", "request.closed":
			state = p.State
			if state == "" {
				state = p.Status
			}
			if state == "doing" || state == "in_progress" {
				state = "in progress"
			}
			if state == "cancelled" || state == "canceled" { //nolint:misspell // Torque wire status.
				state = "declined"
			}
			if !contains([]string{"submitted", "acknowledged", "in progress", "done", "declined", "blocked"}, state) {
				state = ""
			}
		}
		if incomingReply {
			// Only an incoming stored reply is evidence of a reply; its read event
			// must never advance the original message's recipient-read state.
			if e.Kind != "message.sent" && e.Kind != "message.delivered" {
				continue
			}
			state = "replied"
		}
		if p.Summary != "" {
			detail = p.Summary
		}
		key := fmt.Sprintf("%s:%d", source, e.Seq)
		if e.Seq == 0 && e.ID != "" {
			key = source + ":" + e.ID
		}
		res, insertErr := tx.ExecContext(ctx, `INSERT OR IGNORE INTO sent_events(event_key,message_id,at,seq,kind,state,detail,task_id) VALUES(?,?,?,?,?,?,?,?)`, key, id, at, e.Seq, e.Kind, state, detail, p.TaskID)
		if insertErr != nil {
			return false, insertErr
		}
		n, _ := res.RowsAffected()
		changed = changed || n > 0
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO lifecycle_cursor(source,seq) VALUES(?,?) ON CONFLICT(source) DO UPDATE SET seq=excluded.seq`, source, cursor); err != nil {
		return false, err
	}
	return changed, tx.Commit()
}

type sendReceipt struct {
	TaskID string
	ID     string
	At     string
	Wake   *struct {
		Reason    string `json:"reason"`
		Delivered bool   `json:"delivered"`
	}
}

func parseReceipt(raw string) (sendReceipt, error) {
	// notify wraps MessageNotifyResult under result; send returns message directly.
	var wrapper struct {
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal([]byte(raw), &wrapper) == nil && len(wrapper.Result) > 0 && string(wrapper.Result) != "null" {
		raw = string(wrapper.Result)
	}
	var r struct {
		ID            string          `json:"id"`
		TaskID        string          `json:"task_id"`
		WakeReason    string          `json:"wake_reason"`
		WakeDelivered bool            `json:"wake_delivered"`
		MessageID     string          `json:"message_id"`
		Wake          json.RawMessage `json:"wake_outcome"`
		Message       struct {
			ID       string            `json:"id"`
			At       string            `json:"created_at"`
			Metadata map[string]string `json:"metadata"`
			TaskID   string            `json:"task_id"`
			Payload  struct {
				TaskID string `json:"task_id"`
			} `json:"payload"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return sendReceipt{}, fmt.Errorf("message was sent but its receipt could not be decoded; do not resend: %w", err)
	}
	out := sendReceipt{ID: r.ID, At: r.Message.At, TaskID: r.TaskID}
	if out.TaskID == "" {
		out.TaskID = r.Message.TaskID
	}
	if out.TaskID == "" {
		out.TaskID = r.Message.Payload.TaskID
	}
	if out.TaskID == "" {
		out.TaskID = r.Message.Metadata["task_id"]
	}
	if out.ID == "" {
		out.ID = r.Message.ID
	}
	if out.ID == "" {
		out.ID = r.MessageID
	}
	if out.ID == "" {
		return out, fmt.Errorf("message was sent but Tether returned no message id; do not resend")
	}
	if out.At == "" {
		out.At = stamp()
	}
	wake := r.Wake
	if len(wake) == 0 {
		wake = []byte(r.Message.Metadata["tether.wake_outcome"])
	}
	if len(wake) > 0 {
		var encoded string
		if json.Unmarshal(wake, &encoded) == nil {
			wake = []byte(encoded)
		}
		_ = json.Unmarshal(wake, &out.Wake)
	}
	if out.Wake == nil && (r.WakeReason != "" || r.WakeDelivered) {
		data, _ := json.Marshal(map[string]any{"reason": r.WakeReason, "delivered": r.WakeDelivered})
		_ = json.Unmarshal(data, &out.Wake)
	}
	return out, nil
}
