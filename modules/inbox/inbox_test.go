package inbox

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/hollis-labs/surface/module/moduletest"
	"github.com/hollis-labs/surface/module/tetherlink"
)

const (
	me    = "msg://user/local/operator"
	agent = "msg://agent/agent-mux/agt_example001"
	other = "msg://agent/agent-mux/agt_someone99"
)

// fake is Tether: two mailboxes, a registry, and a record of what was sent.
type fake struct {
	mu    sync.Mutex
	boxes map[string][]map[string]any
	sent  []map[string]any
	calls []string
}

func (f *fake) Call(_ context.Context, tool string, args map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, tool)
	switch tool {
	case "tether_message_list":
		var out []map[string]any
		for _, e := range f.boxes[args["to"].(string)] {
			if args["unread_only"] == true && e["read_at"] != nil {
				continue
			}
			out = append(out, e)
		}
		b, _ := json.Marshal(map[string]any{"messages": out})
		return string(b), nil
	case "tether_message_get":
		for _, e := range f.boxes[args["as"].(string)] {
			if e["id"] == args["message_id"] {
				b, _ := json.Marshal(map[string]any{"message": e})
				return string(b), nil
			}
		}
		return "", tetherlink.ErrRefused
	case "tether_registry_lookup":
		return `{"profile":{"display_name":"Someone"}}`, nil
	case "tether_message_send":
		f.sent = append(f.sent, args)
		return `{"message":{"id":"m-new"}}`, nil
	}
	return `{"ok":true}`, nil
}

func start(t *testing.T) (*moduletest.Host, *mod, *fake) {
	t.Helper()
	f := &fake{boxes: map[string][]map[string]any{
		me: {
			{"id": "m1", "from": other, "to": me, "kind": "notice", "thread_id": "t1", "created_at": "2026-11-18T10:00:00Z", "payload": map[string]any{"subject": "Hello", "body": "first"}},
			{"id": "m2", "from": other, "to": me, "kind": "request", "created_at": "2026-11-18T12:00:00Z", "read_at": "2026-11-18T12:05:00Z", "payload": map[string]any{"subject": "Old", "body": "already read"}},
		},
		agent: {
			{"id": "m3", "from": other, "to": agent, "kind": "notice", "created_at": "2026-11-18T11:00:00Z", "payload": map[string]any{"subject": "For the agent", "body": "x"}},
		},
	}}
	m := &mod{caller: f}
	h := moduletest.Start(t, m)
	if err := h.Settings.Set(context.Background(), "addresses", me+", "+agent); err != nil {
		t.Fatal(err)
	}
	return h, m, f
}

func TestListAcrossAddresses(t *testing.T) {
	_, m, _ := start(t)
	ctx := context.Background()
	unread, err := m.list(ctx, BoxUnread, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(unread) != 2 || unread[0].ID != "m3" || unread[1].ID != "m1" || unread[0].Account != agent || unread[1].FromName != "Someone" {
		t.Fatalf("unread: %+v", unread)
	}
	all, _ := m.list(ctx, BoxAll, 50)
	if len(all) != 3 || all[0].ID != "m2" || !all[0].Read || all[0].Subject != "Old" {
		t.Fatalf("all: %+v", all)
	}
}

func TestOnlyTheOwnersAddresses(t *testing.T) {
	_, m, f := start(t)
	ctx := context.Background()
	if _, err := m.get(ctx, "m1", other); err == nil {
		t.Fatal("read as an address that is not the owner's")
	}
	if err := m.archive(ctx, "m1", other, true); err == nil {
		t.Fatal("archived as an address that is not the owner's")
	}
	if _, err := m.send(ctx, Outgoing{From: other, To: me, Body: "x"}); err == nil {
		t.Fatal("sent as an address that is not the owner's")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == "tether_message_get" || c == "tether_message_archive" || c == "tether_message_send" {
			t.Fatalf("Tether was called (%s) for an address that is not the owner's", c)
		}
	}
}

func TestReplyGoesBackOnTheThread(t *testing.T) {
	h, _, f := start(t)
	out, err := h.Tool("inbox_reply", `{"id":"m1","account":"`+me+`","body":"thanks"}`)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if out.(map[string]string)["message_id"] != "m-new" || len(f.sent) != 1 {
		t.Fatalf("reply: %v %v", out, f.sent)
	}
	s := f.sent[0]
	if s["from"] != me || s["to"] != other || s["thread_id"] != "t1" || s["in_reply_to"] != "m1" || s["kind"] != "response" || !strings.Contains(s["payload_json"].(string), `"subject":"Re: Hello"`) {
		t.Fatalf("sent: %v", s)
	}
}

func TestOnlyItsOwnTools(t *testing.T) {
	c := tetherlink.New("inbox", tetherlink.Config{Command: "/bin/true"}, allowed)
	if _, err := c.Call(context.Background(), "torque_task_create", nil); err == nil || !strings.Contains(err.Error(), "not a tool the inbox module calls") {
		t.Fatalf("an unlisted tool was reachable: %v", err)
	}
	if _, err := c.Call(context.Background(), "tether_message_purge", nil); err == nil {
		t.Fatal("purge was reachable")
	}
}

// Review finding: a message id from another mailbox, asked for with the owner's address, is
// refused even when Tether would hand it over; reading, archiving and replying all check.
func TestMessageMustBeToTheAddress(t *testing.T) {
	_, m, f := start(t)
	ctx := context.Background()
	f.mu.Lock()
	// Tether answering for any mailbox: m3 (sent to the agent address) listed under the owner's.
	f.boxes[me] = append(f.boxes[me], f.boxes[agent][0])
	f.mu.Unlock()
	if _, err := m.get(ctx, "m3", me); !errors.Is(err, errNotYours) {
		t.Fatalf("read another mailbox's message: %v", err)
	}
	if err := m.archive(ctx, "m3", me, true); !errors.Is(err, errNotYours) {
		t.Fatalf("archived another mailbox's message: %v", err)
	}
	if err := m.markRead(ctx, "m3", me); !errors.Is(err, errNotYours) {
		t.Fatalf("marked another mailbox's message: %v", err)
	}
	if _, err := m.reply(ctx, "m3", me, "hi"); !errors.Is(err, errNotYours) {
		t.Fatalf("replied to another mailbox's message: %v", err)
	}
	if _, err := m.get(ctx, "m3", agent); err != nil {
		t.Fatalf("its own mailbox: %v", err)
	}
}
