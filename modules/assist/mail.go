package assist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hollis-labs/surface/module/tetherlink"
)

// The owner's Tether mail, read and sent through Tether's own message tools as one of the
// owner's addresses. Ported from Basalisk (internal/inbox/tether.go).

// allowed are the only Tether tools this module calls.
var allowed = tetherlink.Allowed{
	"tether_events_history": true, "tether_events_wait": true,
	"tether_message_list": true, "tether_message_get": true, "tether_message_mark_read": true,
	"tether_message_archive": true, "tether_message_notify": true, "tether_registry_lookup": true,
}

// perAddress is how many messages are asked for per address: few enough that the gateway hands
// the answer on whole.
const perAddress = 15

// Item is one message.
type Item struct {
	ID        string    `json:"id"`
	Account   string    `json:"account"`
	From      string    `json:"from"`
	FromName  string    `json:"from_name,omitempty"`
	FromRole  string    `json:"from_role,omitempty"`
	Kind      string    `json:"kind"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	At        time.Time `json:"at"`
	Read      bool      `json:"read"`
	Thread    string    `json:"thread,omitempty"`
	InReplyTo string    `json:"in_reply_to,omitempty"`
	// Ask is set when the sender wants an answer from a person, with the options it offered.
	Ask *Ask `json:"asks_the_owner,omitempty"`
}

// Ask is a typed request: what is wanted and the options offered.
type Ask struct {
	Kind    string   `json:"kind"`
	Re      string   `json:"re,omitempty"`
	Options []Option `json:"options,omitempty"`
	Default string   `json:"default,omitempty"`
	Due     string   `json:"due,omitempty"`
}

// Option is one choice an Ask offers.
type Option struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Why         string `json:"why,omitempty"`
	Recommended bool   `json:"recommended,omitempty"`
}

func (it Item) who() string {
	if it.FromName != "" {
		return it.FromName
	}
	return it.From
}

// mailbox is the owner's mail at their addresses.
type mailbox struct {
	link      tetherlink.Caller
	addresses []string
	track     func(context.Context, string, string, string, string, string) (string, error)

	mu    *sync.Mutex
	names map[string]profile
}

type profile struct {
	name, role string
	at         time.Time
}

type envelope struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	From       string          `json:"from"`
	To         string          `json:"to"`
	Thread     string          `json:"thread_id"`
	InReplyTo  string          `json:"in_reply_to"`
	Payload    json.RawMessage `json:"payload"`
	CreatedAt  time.Time       `json:"created_at"`
	ReadAt     *time.Time      `json:"read_at"`
	ArchivedAt *time.Time      `json:"archived_at"`
	Subject    string          `json:"subject"`
	Body       string          `json:"body"`
}

const maxBody = 256 << 10

func item(e envelope) Item {
	it := Item{ID: e.ID, Account: e.To, From: e.From, Kind: e.Kind, Subject: e.Subject, Body: e.Body, At: e.CreatedAt,
		Read: e.ReadAt != nil, Thread: e.Thread, InReplyTo: e.InReplyTo}
	var p struct {
		Kind    string `json:"kind"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
		Text    string `json:"text"`
		Re      string `json:"re"`
		Options []struct {
			ID          string `json:"id"`
			Label       string `json:"label"`
			Why         string `json:"why"`
			Recommended bool   `json:"recommended"`
		} `json:"options"`
		Default struct {
			Option string `json:"option"`
			At     string `json:"at"`
		} `json:"default"`
	}
	if json.Unmarshal(e.Payload, &p) == nil {
		if it.Subject == "" {
			it.Subject = p.Subject
		}
		if it.Body == "" {
			it.Body = p.Body
		}
		if it.Body == "" {
			it.Body = p.Text
		}
		switch p.Kind {
		case "question", "decision", "approval_request":
			ask := &Ask{Kind: p.Kind, Re: p.Re, Default: p.Default.Option, Due: p.Default.At}
			for _, o := range p.Options {
				if o.ID != "" {
					ask.Options = append(ask.Options, Option{ID: o.ID, Label: o.Label, Why: o.Why, Recommended: o.Recommended})
				}
			}
			it.Ask = ask
		}
	}
	if it.Body == "" && len(e.Payload) > 0 && string(e.Payload) != "null" {
		var s string
		if json.Unmarshal(e.Payload, &s) == nil {
			it.Body = s
		} else {
			it.Body = "```json\n" + string(e.Payload) + "\n```"
		}
	}
	if len(it.Body) > maxBody {
		it.Body = it.Body[:maxBody] + "\n\n… (cut; the message is longer)"
	}
	if it.Subject == "" {
		line, _, _ := strings.Cut(strings.TrimSpace(it.Body), "\n")
		it.Subject = strings.TrimLeft(clip(line, 120), "# ")
	}
	return it
}

func (b *mailbox) mine(account string) bool {
	for _, a := range b.addresses {
		if a == account {
			return true
		}
	}
	return false
}

// first is the address the owner sends from (box() refuses a mailbox with none).
func (b *mailbox) first() string { return b.addresses[0] }

// list is the owner's mail at every address, newest first per address. A failed address is
// an error: a partial listing would close mail that is still unread.
func (b *mailbox) list(ctx context.Context, unreadOnly bool) ([]Item, error) {
	var items []Item
	for _, account := range b.addresses {
		raw, err := b.link.Call(ctx, "tether_message_list", map[string]any{"to": account, "limit": perAddress, "unread_only": unreadOnly})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", account, err)
		}
		var out struct {
			Messages []envelope `json:"messages"`
		}
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			return nil, fmt.Errorf("%s: the mail list could not be read", account)
		}
		for _, e := range out.Messages {
			if e.To == "" {
				e.To = account
			}
			items = append(items, item(e))
		}
	}
	b.name(ctx, items)
	return items, nil
}

func (b *mailbox) get(ctx context.Context, account, id string) (Item, error) {
	if !b.mine(account) {
		return Item{}, errors.New("that is not one of the owner's addresses; use the account owner_inbox gave")
	}
	raw, err := b.link.Call(ctx, "tether_message_get", map[string]any{"message_id": id, "as": account})
	if err != nil {
		return Item{}, fmt.Errorf("that message could not be read: %w", err)
	}
	var got struct {
		Message *envelope `json:"message"`
		envelope
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		return Item{}, errors.New("that message could not be read")
	}
	e := got.envelope
	if got.Message != nil {
		e = *got.Message
	}
	if e.ID == "" || e.From == "" {
		return Item{}, errors.New("that message could not be read")
	}
	if e.To != "" && e.To != account {
		return Item{}, errors.New("that message is not to that address")
	}
	if e.To == "" {
		e.To = account
	}
	items := []Item{item(e)}
	b.name(ctx, items)
	return items[0], nil
}

func (b *mailbox) markRead(ctx context.Context, account, id string) error {
	_, err := b.link.Call(ctx, "tether_message_mark_read", map[string]any{"message_id": id, "as": account})
	return err
}

func (b *mailbox) archive(ctx context.Context, account, id string) error {
	_, err := b.link.Call(ctx, "tether_message_archive", map[string]any{"message_id": id, "as": account})
	return err
}

// reply answers a message from the address it came to, on its thread. extra goes into the
// payload (who sent it on the owner's behalf); choice is an option the message offered.
func (b *mailbox) reply(ctx context.Context, orig Item, body, choice string, extra map[string]any) error {
	subject := orig.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "re: " + subject
	}
	payload := map[string]any{"subject": subject, "body": body}
	for k, v := range extra {
		payload[k] = v
	}
	if choice != "" {
		payload["choice"] = choice
		if orig.Ask != nil {
			for _, o := range orig.Ask.Options {
				if o.ID == choice {
					payload["choice_label"] = o.Label
				}
			}
		}
	}
	raw, _ := json.Marshal(payload)
	args := map[string]any{"from": orig.Account, "to": orig.From, "kind": "response", "in_reply_to": orig.ID, "payload_json": string(raw)}
	if orig.Thread != "" {
		args["thread_id"] = orig.Thread
	}
	out, err := b.link.Call(ctx, "tether_message_notify", args)
	if err == nil && b.track != nil {
		_, err = b.track(ctx, out, orig.Account, "response", subject, orig.From)
	}
	return err
}

// outgoing is a new message from one of the owner's addresses.
type outgoing struct {
	To, Kind, Subject, Body string
	Extra                   map[string]any
}

func (b *mailbox) send(ctx context.Context, account string, m outgoing) (string, error) {
	if !strings.HasPrefix(m.To, "msg://") {
		return "", errors.New("the recipient must be a Tether address (msg://…)")
	}
	switch m.Kind {
	case "":
		m.Kind = "notice"
	case "request", "response", "notice", "status_update", "handoff", "escalation":
	default:
		return "", fmt.Errorf("%q is not a kind of Tether message", m.Kind)
	}
	payload := map[string]any{"subject": m.Subject, "body": m.Body}
	for k, v := range m.Extra {
		payload[k] = v
	}
	raw, _ := json.Marshal(payload)
	out, err := b.link.Call(ctx, "tether_message_notify", map[string]any{"from": account, "to": m.To, "kind": m.Kind, "payload_json": string(raw)})
	if err != nil {
		return "", err
	}
	if b.track != nil {
		return b.track(ctx, out, account, m.Kind, m.Subject, m.To)
	}
	receipt, err := parseReceipt(out)
	return receipt.ID, err
}

// name fills in who sent each message, from Tether's registry, kept for half an hour.
func (b *mailbox) name(ctx context.Context, items []Item) {
	const keep = 30 * time.Minute
	asked := 0
	for i := range items {
		urn := items[i].From
		if urn == "" {
			continue
		}
		b.mu.Lock()
		p, ok := b.names[urn]
		b.mu.Unlock()
		if (!ok || time.Since(p.at) > keep) && asked < 20 {
			asked++
			p = profile{at: time.Now()}
			c, cancel := context.WithTimeout(ctx, 5*time.Second)
			raw, err := b.link.Call(c, "tether_registry_lookup", map[string]any{"urn": urn})
			cancel()
			if err == nil {
				var out struct {
					Name    string `json:"display_name"`
					Role    string `json:"role"`
					Profile *struct {
						Name string `json:"display_name"`
						Role string `json:"role"`
					} `json:"profile"`
				}
				if json.Unmarshal([]byte(raw), &out) == nil {
					p.name, p.role = out.Name, out.Role
					if out.Profile != nil && p.name == "" {
						p.name, p.role = out.Profile.Name, out.Profile.Role
					}
				}
			}
			b.mu.Lock()
			b.names[urn] = p
			b.mu.Unlock()
		}
		items[i].FromName, items[i].FromRole = p.name, p.role
	}
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
