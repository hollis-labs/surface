package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	chatstream "github.com/hollis-labs/libs/ui/ui-go/chatstream"
	_ "modernc.org/sqlite"
)

// A run's tools come from the part's meta (as Nanite sends them) or the event's name; its
// usage from the final count; its model from the config unless the run names one.
func TestTheRunLogFollowsARun(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	l, err := openRunLog(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	l.turn("r1", "errand:assist", "messaging", "default", "/agents/example")
	c := l.begin("r1", "anthropic-opus")
	name, _ := json.Marshal("project_list")
	c.see(chatstream.Event{Verb: chatstream.VerbRunStart, Provider: "anthropic"})
	c.see(chatstream.Event{Verb: chatstream.VerbPartStart, Kind: "tool_call", Meta: map[string]json.RawMessage{"name": name}})
	c.see(chatstream.Event{Verb: chatstream.VerbPartStart, Kind: "tool_call", Name: "owner_inbox"})
	c.see(chatstream.Event{Verb: chatstream.VerbPartStart, Kind: "text"})
	c.see(chatstream.Event{Verb: chatstream.VerbRunFinish, Usage: &chatstream.Usage{Scope: chatstream.UsageFinal, UncachedInput: 6, CacheRead: 40, Output: 52}})
	l.end(c, RunFinished, "stop")
	got, err := l.recent(ctx, 5)
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %v", got, err)
	}
	r := got[0]
	if r.AgentDir != "/agents/example" || r.Origin != "errand:assist" || r.Slot != "messaging" || r.Model != "anthropic-opus" || r.Provider != "anthropic" ||
		r.Status != RunFinished || r.Output != 52 || r.CacheRead != 40 || len(r.Tools) != 2 || r.Tools[0] != "project_list" {
		t.Fatalf("%+v", r)
	}
}
