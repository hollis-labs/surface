package approvals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/hollis-labs/surface/module"
)

// An approval is shown as a card, drawn by the page:
//
//	approve         the core's approval card: what is asked for, by whom, and why; one change
//	                per requested scope, so the owner can approve some and drop the rest
//	                (Gatecrasher's narrowed scopes); a critical one asks for the phrase
//	choose          the core's choice card, with Decline
//	answer          the core's prompt card; Skip declines
//	request_secret  this module's own kind, approvals.secret: a one-field form that posts the
//	                value from the page straight to the producer's intake. The intake is not
//	                in the card: the page asks this module for it (GET form/{card}), which
//	                answers from what Gatecrasher said. The card's answer is only declined;
//	                a provided secret is reported by the producer, and the card goes then.
//
// The owner's answer counts only on the card this module showed for that approval, and is
// checked against what this module kept from Gatecrasher, never against the card's payload:
// the agent and outside clients can show cards, and a card's payload is not the module's word.

const (
	kindForm = "approvals.secret" // the secret-request card
	// declineID is the choice card's Decline. A producer's choice with this id is dropped.
	declineID = "approvals:decline"
	phrase    = "approve"
)

// risk maps a tier to the card's risk.
func risk(tier string) string {
	switch tier {
	case "critical":
		return "high"
	case "elevated":
		return "medium"
	}
	return "low"
}

// shownCard is a Gatecrasher card as a Surface card, with what the answer is checked against.
type shownCard struct {
	card   module.Card
	scopes []string
	post   string // a secret request's intake
	field  string
}

func surfaceCard(c gateCard) (shownCard, error) {
	sc := module.Card{
		Title:     c.Title,
		Subject:   "approval:" + c.ApprovalID,
		Risk:      risk(c.Tier),
		ExpiresAt: c.ExpiresAt.UTC().Format(time.RFC3339),
	}
	out := shownCard{}
	who := c.Principal.Sub
	if c.Principal.Kind != "" {
		who += " (" + c.Principal.Kind + ")"
	}
	head := fmt.Sprintf("%s · %s · from %s", c.Tier, c.Kind, c.Source)
	var payload map[string]any
	switch c.Type {
	case "approve":
		sc.Kind = "approval"
		summary := strings.TrimSpace(c.Summary)
		if summary == "" {
			summary = c.Title
		}
		var changes []map[string]string
		if r := c.Requested; r != nil {
			summary += fmt.Sprintf("\n\n%s. Audience %s, for %s.", head, r.Audience, ttl(r.TTLSeconds))
			for _, s := range r.Scopes {
				changes = append(changes, map[string]string{"label": "scope", "after": s})
			}
			out.scopes = append([]string(nil), r.Scopes...)
		} else {
			summary += "\n\n" + head + "."
		}
		if ev := evidence(c.Evidence); ev != "" {
			summary += " Evidence: " + ev + "."
		}
		payload = map[string]any{"summary": summary, "requested_by": who, "changes": changes, "per_change": len(changes) > 1}
		if c.Tier == "critical" {
			payload["phrase"] = phrase
		}
	case "choose":
		if c.Prompt == nil || len(c.Prompt.Choices) == 0 {
			return out, permanent{errors.New("a choice card without its question or choices")}
		}
		sc.Kind = "choice"
		var opts []map[string]string
		for _, ch := range c.Prompt.Choices {
			if ch.ID == declineID || ch.ID == "" {
				continue
			}
			opts = append(opts, map[string]string{"id": ch.ID, "label": ch.Label})
		}
		opts = append(opts, map[string]string{"id": declineID, "label": "Decline"})
		payload = map[string]any{"question": c.Prompt.Question, "options": opts, "body": head + " · asked by " + who}
	case "answer":
		if c.Prompt == nil {
			return out, permanent{errors.New("a question card without its question")}
		}
		sc.Kind = "prompt"
		payload = map[string]any{"question": c.Prompt.Question, "body": head + " · asked by " + who + ". Skip declines it.", "multiline": true}
	case "request_secret":
		s := c.Secret
		if s == nil {
			return out, permanent{errors.New("a secret request without its form")}
		}
		if _, err := intakeOrigin(s.PostURL); err != nil {
			return out, permanent{err}
		}
		out.post, out.field = s.PostURL, s.Field
		if out.field == "" {
			out.field = "value"
		}
		sc.Kind = kindForm
		sc.Blocking = true // a secret request is always critical
		payload = map[string]any{"summary": strings.TrimSpace(c.Summary), "head": head, "requested_by": who}
	default:
		return out, permanent{fmt.Errorf("a card of type %q", c.Type)}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return out, permanent{err}
	}
	sc.Payload = raw
	out.card = sc
	return out, nil
}

// intakeOrigin is the origin the page posts a secret to: https, or http on loopback.
func intakeOrigin(post string) (string, error) {
	u, err := url.Parse(post)
	if err != nil || u.Host == "" || u.User != nil || !secure(u) {
		return "", errors.New("the secret intake is not an https address")
	}
	return u.Scheme + "://" + u.Host, nil
}

func ttl(s int64) string {
	switch {
	case s <= 0:
		return "no set time"
	case s%3600 == 0:
		return fmt.Sprintf("%d h", s/3600)
	case s%60 == 0:
		return fmt.Sprintf("%d min", s/60)
	}
	return fmt.Sprintf("%d s", s)
}

func evidence(e map[string]string) string {
	var parts []string
	for _, k := range []string{"task_id", "session_id", "policy_rule"} {
		if v := e[k]; v != "" {
			parts = append(parts, k+" "+v)
		}
	}
	return strings.Join(parts, ", ")
}

// show puts a pending approval in front of the owner, once. A permanent error means it can
// never be shown as it is.
func (m *mod) show(ctx context.Context, g *gate, id string) error {
	m.showMu.Lock()
	defer m.showMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if sh, ok := m.shown(ctx, id); ok {
		if sh.card != "" {
			if c, found, _ := m.h.Cards().Get(ctx, sh.card); found && c.Status == "" {
				return nil // already shown and open
			}
		}
		if sh.status != "pending" && sh.status != "unshowable" {
			return nil // decided here already, or closed: the stream's event says the rest
		}
	}
	c, err := g.card(ctx, id)
	if err != nil {
		return err
	}
	if c.Status != "pending" {
		m.close(ctx, id, c.Status)
		return nil
	}
	sc, err := surfaceCard(c)
	if err != nil {
		return err
	}
	shown, err := m.h.Cards().Show(ctx, sc.card)
	if err != nil {
		return permanent{fmt.Errorf("the card was refused: %w", err)}
	}
	// Show has committed: a reconnect may now cancel ctx, but must not erase the
	// association needed to recognize this card and accept its answer on replay.
	// Bound this local write independently; network reads still use the stream context.
	keep, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := m.remember(keep, shownRow{id: id, card: shown.ID, typ: c.Type, status: "pending", title: c.Title, scopes: sc.scopes, post: sc.post, field: sc.field}); err != nil {
		return fmt.Errorf("record shown approval: %w", err)
	}
	m.h.Events().Publish("approvals.changed", map[string]string{"approval_id": id, "status": "pending"})
	return nil
}

// unshowable tells the owner of an approval that cannot be shown, so it is not silently lost.
func (m *mod) unshowable(ctx context.Context, id string, err error) {
	if sh, ok := m.shown(ctx, id); ok && sh.status == "unshowable" {
		return // reported already
	}
	m.h.Log().Warn("approvals: an approval cannot be shown", "approval", id, "err", err)
	body, _ := json.Marshal(map[string]string{"body": "Approval " + id + " is waiting in Gatecrasher but cannot be shown here: " + err.Error() + ". Decide it in another surface, or ask its producer."})
	_, _ = m.h.Cards().Show(ctx, module.Card{Kind: "error", Title: "An approval cannot be shown", Subject: "approval-error:" + id, Payload: body})
	_ = m.remember(ctx, shownRow{id: id, typ: "unknown", status: "unshowable", title: id})
}

// close takes an approval's card away when Gatecrasher closed it (decided here or elsewhere,
// expired, withdrawn, a secret provided).
func (m *mod) close(ctx context.Context, id, status string) {
	sh, ok := m.shown(ctx, id)
	if !ok {
		return
	}
	if sh.card != "" {
		if c, found, _ := m.h.Cards().Get(ctx, sh.card); found && c.Status == "" {
			end := "superseded"
			if status == "expired" {
				end = "expired"
			}
			_, _ = m.h.Cards().End(ctx, sh.card, end)
		}
	}
	sh.status = status
	_ = m.remember(ctx, sh)
	m.h.Events().Publish("approvals.changed", map[string]string{"approval_id": id, "status": status})
}

// answered sends the owner's answer on a card to Gatecrasher. It acts only on the card this
// module showed for that approval while it is still pending here.
func (m *mod) answered(ctx context.Context, c module.Card) {
	id, ok := strings.CutPrefix(c.Subject, "approval:")
	if !ok {
		return
	}
	sh, ok := m.shown(ctx, id)
	if !ok || sh.status != "pending" || sh.card == "" || c.ID != sh.card {
		return
	}
	d, ok := decision(c, sh)
	if !ok {
		return
	}
	g, err := m.gate()
	if err != nil {
		m.notDelivered(ctx, sh, err)
		return
	}
	switch err := g.decide(ctx, id, d); {
	case err == nil:
		sh.status = d.Outcome
		_ = m.remember(ctx, sh)
		m.h.Events().Publish("approvals.changed", map[string]string{"approval_id": id, "status": d.Outcome})
	case errors.Is(err, errTerminal):
		// Closed elsewhere first: keep what Gatecrasher says, not what was answered here.
		status := "closed"
		if gc, cerr := g.card(ctx, id); cerr == nil {
			status = gc.Status
		}
		m.close(ctx, id, status)
	default:
		m.notDelivered(ctx, sh, err)
	}
}

// notDelivered tells the owner their answer did not reach Gatecrasher, and shows the approval
// again: it is still waiting.
func (m *mod) notDelivered(ctx context.Context, sh shownRow, err error) {
	m.h.Log().Warn("approvals: the owner's answer did not reach Gatecrasher", "approval", sh.id, "err", err)
	body, _ := json.Marshal(map[string]string{"body": "Your answer on \"" + sh.title + "\" did not reach Gatecrasher (" + err.Error() + "). The approval is still waiting; it is shown again as soon as Gatecrasher answers."})
	_, _ = m.h.Cards().Show(ctx, module.Card{Kind: "error", Title: "An approval answer did not go through", Subject: "approval-error:" + sh.id, Payload: body})
	sh.card = ""
	_ = m.remember(ctx, sh)
	if g, gerr := m.gate(); gerr == nil && m.show(ctx, g, sh.id) == nil {
		return
	}
	// Gatecrasher did not answer: the next connection shows it again (reshow).
	m.reconnect()
}

// reshow shows again each approval still waiting here without a card (one whose answer did
// not go through, or that could not be shown): on every connection.
func (m *mod) reshow(ctx context.Context, g *gate) {
	rows, err := m.db.QueryContext(ctx, `SELECT approval_id FROM shown WHERE (status='pending' AND card_id='') OR status='unshowable'`)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	_ = rows.Close()
	for _, id := range ids {
		if err := m.show(ctx, g, id); err != nil {
			m.h.Log().Debug("approvals: not shown again yet", "approval", id, "err", err)
		}
	}
}

// decision is the decide call an answered card makes; false for none. Dismissing a card
// (canceled) declines it: an approval left unanswered in Gatecrasher would wait unseen.
func decision(c module.Card, sh shownRow) (decideRequest, bool) {
	if c.Status == "canceled" {
		return decideRequest{Outcome: "denied", Note: "dismissed by the owner"}, true
	}
	o := c.Outcome
	if c.Status != "submitted" || o == nil {
		return decideRequest{}, false // expired, superseded: Gatecrasher closes it on its own
	}
	switch c.Kind {
	case "approval":
		approved := map[int]bool{}
		for _, d := range o.Decisions {
			if d.ItemID == "approval" {
				if d.Action == "approve" {
					return decideRequest{Outcome: "approved"}, true
				}
				return decideRequest{Outcome: "denied"}, true
			}
			var i int
			if _, err := fmt.Sscanf(d.ItemID, "change:%d", &i); err == nil && d.Action == "approve" && i >= 0 && i < len(sh.scopes) {
				approved[i] = true
			}
		}
		var keep []string
		for i, s := range sh.scopes {
			if approved[i] {
				keep = append(keep, s)
			}
		}
		switch {
		case len(keep) == 0:
			return decideRequest{Outcome: "denied"}, true
		case len(keep) == len(sh.scopes):
			return decideRequest{Outcome: "approved"}, true
		}
		return decideRequest{Outcome: "approved", Narrowed: &narrowed{Scopes: keep}}, true
	case "choice":
		if len(o.Answers) == 0 {
			return decideRequest{Outcome: "denied"}, true
		}
		v, _ := o.Answers[0].Value.(string)
		if v == "" || v == declineID {
			return decideRequest{Outcome: "denied", Note: o.Answers[0].Note}, true
		}
		return decideRequest{Outcome: "approved", Choice: v}, true
	case "prompt":
		if len(o.Answers) == 0 {
			return decideRequest{Outcome: "denied"}, true
		}
		v, _ := o.Answers[0].Value.(string)
		if strings.TrimSpace(v) == "" {
			return decideRequest{Outcome: "denied"}, true
		}
		return decideRequest{Outcome: "approved", Answer: v}, true
	case kindForm:
		if declined, _ := o.Data["declined"].(bool); declined {
			return decideRequest{Outcome: "denied"}, true
		}
	}
	return decideRequest{}, false
}
