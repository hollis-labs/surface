package assist

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
)

const (
	me    = "msg://user/local/operator"
	chief = "msg://agent/agent-mux/agt_chief00001"
	other = "msg://agent/agent-mux/agt_someone99"
)

// fake is Tether: the owner's mailbox, a registry, and a record of what was sent.
type fake struct {
	mu    sync.Mutex
	box   []map[string]any
	sent  []map[string]any
	reads []string
}

func (f *fake) Call(_ context.Context, tool string, args map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch tool {
	case "tether_message_list":
		var out []map[string]any
		for _, e := range f.box {
			if args["to"] != me || (args["unread_only"] == true && e["read_at"] != nil) {
				continue
			}
			out = append(out, e)
		}
		b, _ := json.Marshal(map[string]any{"messages": out})
		return string(b), nil
	case "tether_message_get":
		for _, e := range f.box {
			if e["id"] == args["message_id"] {
				b, _ := json.Marshal(map[string]any{"message": e})
				return string(b), nil
			}
		}
		return "", nil
	case "tether_message_mark_read":
		f.reads = append(f.reads, args["message_id"].(string))
		for _, e := range f.box {
			if e["id"] == args["message_id"] {
				e["read_at"] = "2026-11-19T12:00:00Z"
			}
		}
		return `{"ok":true}`, nil
	case "tether_message_notify":
		f.sent = append(f.sent, args)
		return fmt.Sprintf(`{"id":"out-%d"}`, len(f.sent)), nil
	case "tether_registry_lookup":
		return `{"display_name":"Someone"}`, nil
	}
	return `{}`, nil
}

func (f *fake) add(e map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e["to"] = me
	if e["created_at"] == nil {
		e["created_at"] = "2026-11-19T11:00:00Z"
	}
	f.box = append(f.box, e)
}

func start(t *testing.T) (*moduletest.Host, *mod, *fake) {
	t.Helper()
	f := &fake{}
	m := &mod{link: f}
	h := moduletest.Start(t, m)
	ctx := context.Background()
	if err := h.Settings.Set(ctx, "addresses", me); err != nil {
		t.Fatal(err)
	}
	if err := h.Settings.Set(ctx, "roster", `{"chief":"`+chief+`"}`); err != nil {
		t.Fatal(err)
	}
	return h, m, f
}

func TestClassify(t *testing.T) {
	rules := []Rule{{ID: 2, From: other, Kind: "notice", Class: ClassSettle}}
	cases := []struct {
		it   Item
		want string
	}{
		{Item{From: other, Kind: "notice"}, ClassSettle},
		{Item{From: chief, Kind: "notice"}, ClassDigest},
		{Item{From: chief, Kind: "request"}, ClassBas},
		{Item{From: chief, Kind: "escalation"}, ClassNeeds},
		{Item{From: chief, Kind: "request", Ask: &Ask{Kind: "decision"}}, ClassNeeds},
	}
	for _, c := range cases {
		if got, _ := classify(c.it, rules); got != c.want {
			t.Errorf("%+v: %s, want %s", c.it, got, c.want)
		}
	}
}

func TestTheSweepSortsNewMail(t *testing.T) {
	h, m, f := start(t)
	ctx := context.Background()
	f.add(map[string]any{"id": "old1", "from": other, "kind": "request", "payload": map[string]any{"subject": "Waiting already"}})
	if _, err := m.sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(h.AgentState.Taken()); n != 0 {
		t.Fatalf("what was waiting at the first sweep was handed on: %d", n)
	}
	f.add(map[string]any{"id": "d1", "from": chief, "kind": "request", "payload": map[string]any{"kind": "decision", "subject": "Ship it?",
		"body": "Ready to ship.", "options": []map[string]any{{"id": "yes", "label": "Ship"}, {"id": "no", "label": "Hold"}}}})
	f.add(map[string]any{"id": "r1", "from": chief, "kind": "request", "payload": map[string]any{"subject": "Please review"}})
	f.add(map[string]any{"id": "n1", "from": other, "kind": "notice", "payload": map[string]any{"subject": "FYI"}})
	n, err := m.sweep(ctx)
	if err != nil || n != 3 {
		t.Fatalf("%d %v", n, err)
	}
	cards, _ := h.CardStore.List(ctx, false)
	if len(cards) != 1 || cards[0].Subject != "mail:d1" || cards[0].Kind != "choice" {
		t.Fatalf("cards: %+v", cards)
	}
	if !strings.Contains(string(cards[0].Payload), `"id":"yes"`) || !strings.Contains(string(cards[0].Payload), optNoise) {
		t.Fatalf("payload: %s", cards[0].Payload)
	}
	asks := h.AgentState.Taken()
	if len(asks) != 1 || !strings.Contains(asks[0].Text, "id r1") || strings.Contains(asks[0].Text, "id d1") {
		t.Fatalf("errand: %+v", asks)
	}
	around, _ := h.Around()
	for _, want := range []string{"Waiting on the owner to decide (1)", "Requests handed to you: 1", "Held, not shown to the owner: 1"} {
		if !strings.Contains(around, want) {
			t.Errorf("around lacks %q:\n%s", want, around)
		}
	}
	// A second sweep shows no second card and hands nothing twice.
	if _, err := m.sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if cards, _ = h.CardStore.List(ctx, false); len(cards) != 1 || len(h.AgentState.Taken()) != 1 {
		t.Fatalf("again: %d cards, %d errands", len(cards), len(h.AgentState.Taken()))
	}
	// Read elsewhere: it leaves the lists and its card goes.
	_, _ = f.Call(ctx, "tether_message_mark_read", map[string]any{"message_id": "d1"})
	if _, err := m.sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if cards, _ = h.CardStore.List(ctx, false); len(cards) != 0 {
		t.Fatalf("the card stayed: %+v", cards)
	}
}

func TestABusyAgentGetsTheMailLater(t *testing.T) {
	h, m, f := start(t)
	ctx := context.Background()
	_, _ = m.sweep(ctx)
	h.AgentState.Err = module.ErrAgentBusy
	f.add(map[string]any{"id": "r1", "from": chief, "kind": "request", "payload": map[string]any{"subject": "Please review"}})
	_, _ = m.sweep(ctx)
	h.AgentState.Err = nil
	_, _ = m.sweep(ctx)
	if asks := h.AgentState.Taken(); len(asks) != 1 {
		t.Fatalf("errands: %+v", asks)
	}
}

func TestNoAgentShowsRequestsToTheOwner(t *testing.T) {
	h, m, f := start(t)
	ctx := context.Background()
	_, _ = m.sweep(ctx)
	h.AgentState.Err = module.ErrNoAgent
	f.add(map[string]any{"id": "r1", "from": chief, "kind": "request", "payload": map[string]any{"subject": "Please review"}})
	_, _ = m.sweep(ctx)
	if cards, _ := h.CardStore.List(ctx, false); len(cards) != 1 || cards[0].Subject != "mail:r1" {
		t.Fatalf("cards: %+v", cards)
	}
}

func TestTheOwnersAnswerOnACardGoesFromThem(t *testing.T) {
	h, m, f := start(t)
	ctx := context.Background()
	_, _ = m.sweep(ctx)
	f.add(map[string]any{"id": "d1", "from": chief, "kind": "request", "thread_id": "th1", "payload": map[string]any{"kind": "decision", "subject": "Ship it?",
		"options": []map[string]any{{"id": "yes", "label": "Ship"}}}})
	_, _ = m.sweep(ctx)
	cards, _ := h.CardStore.List(ctx, false)
	c := cards[0]
	c.Status = "submitted"
	c.Outcome = &module.CardOutcome{Status: "submitted", Answers: []module.CardAnswer{{QuestionID: "choice", Value: "yes"}}}
	m.answered(ctx, c)
	if len(f.sent) != 1 {
		t.Fatalf("sent: %v", f.sent)
	}
	s := f.sent[0]
	var payload map[string]any
	_ = json.Unmarshal([]byte(s["payload_json"].(string)), &payload)
	if s["from"] != me || s["to"] != chief || s["in_reply_to"] != "d1" || s["thread_id"] != "th1" || payload["choice"] != "yes" || payload["choice_label"] != "Ship" {
		t.Fatalf("reply: %v %v", s, payload)
	}
	if _, marked := payload["sent_by"]; marked {
		t.Fatal("the owner's own answer carries the assistant's marker")
	}
	acts, _ := m.st.recent(ctx, 0, 10)
	if len(acts) == 0 || acts[0].Kind != "answer" {
		t.Fatalf("activity: %+v", acts)
	}
}

func TestNoiseMakesARule(t *testing.T) {
	h, m, f := start(t)
	ctx := context.Background()
	_, _ = m.sweep(ctx)
	f.add(map[string]any{"id": "e1", "from": other, "kind": "escalation", "payload": map[string]any{"subject": "Disk at 80%"}})
	_, _ = m.sweep(ctx)
	cards, _ := h.CardStore.List(ctx, false)
	c := cards[0]
	c.Status = "submitted"
	c.Outcome = &module.CardOutcome{Status: "submitted", Answers: []module.CardAnswer{{QuestionID: "choice", Value: optNoise}}}
	m.answered(ctx, c)
	rules, _ := m.st.rules(ctx)
	if len(rules) != 1 || rules[0].From != other || rules[0].Kind != "escalation" || rules[0].Class != ClassDigest {
		t.Fatalf("rules: %+v", rules)
	}
	f.add(map[string]any{"id": "e2", "from": other, "kind": "escalation", "payload": map[string]any{"subject": "Disk at 81%"}})
	_, _ = m.sweep(ctx)
	if open, _ := m.st.open(ctx, ClassDigest); len(open) != 1 || open[0].ID != "e2" {
		t.Fatalf("held: %+v", open)
	}
}

func TestAnAnswerToWhatWasAskedGoesBackToTheAgent(t *testing.T) {
	h, m, f := start(t)
	ctx := context.Background()
	_, _ = m.sweep(ctx)
	out, err := h.Tool("owner_status_ask", `{"to":"chief","about":"CW-1"}`)
	if err != nil {
		t.Fatal(err)
	}
	sentID := out.(map[string]any)["sent"].(string)
	var payload map[string]any
	_ = json.Unmarshal([]byte(f.sent[0]["payload_json"].(string)), &payload)
	if f.sent[0]["to"] != chief || payload["sent_by"] != "assistant" || payload["on_behalf_of"] != me {
		t.Fatalf("status ask: %v %v", f.sent[0], payload)
	}
	f.add(map[string]any{"id": "a1", "from": chief, "kind": "response", "in_reply_to": sentID, "payload": map[string]any{"subject": "re: Status", "body": "Done."}})
	_, _ = m.sweep(ctx)
	if open, _ := m.st.open(ctx, ClassAnswer); len(open) != 1 {
		t.Fatalf("answers: %+v", open)
	}
	if asks := h.AgentState.Taken(); len(asks) != 1 || !strings.Contains(asks[0].Text, "Answers came back") {
		t.Fatalf("errand: %+v", asks)
	}
	around, _ := h.Around()
	if !strings.Contains(around, "Answers to what you asked for the owner (1)") {
		t.Fatalf("around: %s", around)
	}
}

func TestReceiptsAndSettling(t *testing.T) {
	h, m, f := start(t)
	ctx := context.Background()
	_, _ = m.sweep(ctx)
	f.add(map[string]any{"id": "r1", "from": chief, "kind": "request", "payload": map[string]any{"subject": "Look at this"}})
	_, _ = m.sweep(ctx)
	if _, err := h.Tool("owner_ack", `{"account":"`+me+`","id":"r1","receipt":"will_reply"}`); err != nil {
		t.Fatal(err)
	}
	if cards, _ := h.CardStore.List(ctx, false); len(cards) != 1 {
		t.Fatalf("will_reply shows the owner a card: %+v", cards)
	}
	if len(f.reads) != 0 {
		t.Fatal("will_reply left it unread")
	}
	if _, err := h.Tool("owner_mail_settle", `{"account":"`+me+`","id":"r1","reason":"done already"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Tool("owner_mail_read", `{"account":"msg://user/local/someone-else","id":"r1"}`); err == nil {
		t.Fatal("read another's mail")
	}
	if _, err := h.Tool("owner_send", `{"to":"nobody","kind":"request","subject":"x","body":"y"}`); err == nil || !strings.Contains(err.Error(), "roster") {
		t.Fatalf("an unknown seat: %v", err)
	}
	acts, _ := m.st.recent(ctx, 0, 10)
	if len(acts) != 2 || acts[0].Kind != "settle" || acts[1].Kind != "ack" {
		t.Fatalf("activity: %+v", acts)
	}
}
