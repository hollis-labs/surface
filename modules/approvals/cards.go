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
//	answer          the core's prompt card (an empty answer declines)
//	request_secret  this module's own kind, gatecrasher.secret: a one-field form that posts
//	                the value from the page straight to the producer's intake. The card's
//	                answer says only provided or declined; the value is never in it.
//
// The owner's answer on the card is the owner deciding: the module sends it to Gatecrasher.

const (
	kindForm = "gatecrasher.secret" // the secret-request card
	declineID  = "_decline"
	phrase     = "approve"
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

// surfaceCard is a Gatecrasher card as a Surface card.
func surfaceCard(c gateCard) (module.Card, error) {
	sc := module.Card{
		Title:     c.Title,
		Subject:   "approval:" + c.ApprovalID,
		Risk:      risk(c.Tier),
		ExpiresAt: c.ExpiresAt.UTC().Format(time.RFC3339),
	}
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
		} else {
			summary += "\n\n" + head
		}
		if ev := evidence(c.Evidence); ev != "" {
			summary += " Evidence: " + ev + "."
		}
		payload = map[string]any{"summary": summary, "requested_by": who, "changes": changes}
		if c.Tier == "critical" {
			payload["phrase"] = phrase
		}
	case "choose":
		if c.Prompt == nil {
			return sc, errors.New("a choice card without its question")
		}
		sc.Kind = "choice"
		var opts []map[string]string
		for _, ch := range c.Prompt.Choices {
			opts = append(opts, map[string]string{"id": ch.ID, "label": ch.Label})
		}
		opts = append(opts, map[string]string{"id": declineID, "label": "Decline"})
		payload = map[string]any{"question": c.Prompt.Question, "options": opts, "body": head + " · asked by " + who}
	case "answer":
		if c.Prompt == nil {
			return sc, errors.New("a question card without its question")
		}
		sc.Kind = "prompt"
		payload = map[string]any{"question": c.Prompt.Question, "body": head + " · asked by " + who + ". Leave it empty to decline.", "multiline": true}
	case "request_secret":
		s := c.Secret
		if s == nil {
			return sc, errors.New("a request_secret card without its form")
		}
		origin, err := intakeOrigin(s.PostURL)
		if err != nil {
			return sc, err
		}
		field := s.Field
		if field == "" {
			field = "value"
		}
		sc.Kind = kindForm
		sc.Blocking = true // a secret request is always critical
		payload = map[string]any{
			"summary": strings.TrimSpace(c.Summary), "head": head, "requested_by": who,
			"post_url": s.PostURL, "origin": origin, "field": field,
			"intake_id": s.IntakeID, "expires_at": s.ExpiresAt.UTC().Format(time.RFC3339),
		}
	default:
		return sc, fmt.Errorf("a card of type %q", c.Type)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return sc, err
	}
	sc.Payload = raw
	return sc, nil
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

// show puts a pending approval in front of the owner, once.
func (m *mod) show(ctx context.Context, g *gate, id string) error {
	if sh, ok := m.shown(ctx, id); ok {
		if c, found, _ := m.h.Cards().Get(ctx, sh.card); found && c.Status == "" {
			return nil // already shown and open
		}
		if sh.status != "pending" {
			return nil // decided here already; the stream's event will close it
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
	shown, err := m.h.Cards().Show(ctx, sc)
	if err != nil {
		return err
	}
	m.remember(ctx, id, shown.ID, c.Type, "pending", c.Title)
	m.h.Events().Publish("approvals.changed", map[string]string{"approval_id": id, "status": "pending"})
	return nil
}

// close takes an approval's card away when Gatecrasher closed it (decided here or elsewhere,
// expired, withdrawn, a secret provided).
func (m *mod) close(ctx context.Context, id, status string) {
	sh, ok := m.shown(ctx, id)
	if !ok {
		return
	}
	if c, found, _ := m.h.Cards().Get(ctx, sh.card); found && c.Status == "" {
		end := "superseded"
		if status == "expired" {
			end = "expired"
		}
		_, _ = m.h.Cards().End(ctx, sh.card, end)
	}
	m.remember(ctx, id, sh.card, sh.typ, status, sh.title)
	m.h.Events().Publish("approvals.changed", map[string]string{"approval_id": id, "status": status})
}

// answered sends the owner's answer on a card to Gatecrasher.
func (m *mod) answered(ctx context.Context, c module.Card) {
	id, ok := strings.CutPrefix(c.Subject, "approval:")
	if !ok || c.Status != "submitted" || c.Outcome == nil {
		return
	}
	sh, ok := m.shown(ctx, id)
	if !ok || sh.status != "pending" {
		return
	}
	d, ok := decision(c)
	if !ok {
		return // a secret provided: the producer reports it, and the stream closes the card
	}
	g, err := m.gate()
	if err != nil {
		m.h.Log().Warn("approvals: the owner's answer was not sent", "approval", id, "err", err)
		return
	}
	switch err := g.decide(ctx, id, d); {
	case err == nil, errors.Is(err, errTerminal):
		m.remember(ctx, id, sh.card, sh.typ, d.Outcome, sh.title)
		m.h.Events().Publish("approvals.changed", map[string]string{"approval_id": id, "status": d.Outcome})
	default:
		m.h.Log().Warn("approvals: the owner's answer was not sent", "approval", id, "err", err)
	}
}

// decision is the decide call an answered card makes; false for a card that makes none.
func decision(c module.Card) (decideRequest, bool) {
	o := c.Outcome
	switch c.Kind {
	case "approval":
		approved, all := 0, true
		for _, d := range o.Decisions {
			if d.ItemID == "approval" {
				if d.Action == "approve" {
					return decideRequest{Outcome: "approved"}, true
				}
				return decideRequest{Outcome: "denied"}, true
			}
			if d.Action == "approve" {
				approved++
			} else {
				all = false
			}
		}
		switch {
		case approved == 0:
			return decideRequest{Outcome: "denied"}, true
		case all:
			return decideRequest{Outcome: "approved"}, true
		}
		return decideRequest{Outcome: "approved", Narrowed: &narrowed{Scopes: scopes(c)}}, true
	case "choice":
		if len(o.Answers) == 0 {
			return decideRequest{}, false
		}
		v, _ := o.Answers[0].Value.(string)
		if v == "" || v == declineID {
			return decideRequest{Outcome: "denied", Note: o.Answers[0].Note}, true
		}
		return decideRequest{Outcome: "approved", Choice: v}, true
	case "prompt":
		if len(o.Answers) == 0 {
			return decideRequest{}, false
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
		return decideRequest{}, false
	}
	return decideRequest{}, false
}

// scopes are the approved changes' scopes, read from the card's payload by the position in
// each decision's item id ("change:<i>").
func scopes(c module.Card) []string {
	var p struct {
		Changes []struct {
			After string `json:"after"`
		} `json:"changes"`
	}
	_ = json.Unmarshal(c.Payload, &p)
	var out []string
	for _, d := range c.Outcome.Decisions {
		var i int
		if _, err := fmt.Sscanf(d.ItemID, "change:%d", &i); err != nil || d.Action != "approve" || i < 0 || i >= len(p.Changes) {
			continue
		}
		out = append(out, p.Changes[i].After)
	}
	return out
}
