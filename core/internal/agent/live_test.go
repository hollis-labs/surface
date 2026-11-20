package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hollis-labs/libs/agent/chat"
)

// TestLive runs one real turn against the gateway in SURFACE_LIVE_CONFIG (an agent.json).
// Skipped unless that is set.
func TestLive(t *testing.T) {
	path := os.Getenv("SURFACE_LIVE_CONFIG")
	if path == "" {
		t.Skip("SURFACE_LIVE_CONFIG not set")
	}
	s, _, _, calls := setup(t, Options{})
	t.Setenv("SURFACE_AGENT_CONFIG", path)
	cfg, err := LoadConfig("x")
	if err != nil {
		t.Fatal(err)
	}
	s.wire.close(context.Background())
	s.cfg = cfg
	s.wire, err = s.openWire(context.Background(), RunnerNanite)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Mount(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	view, err := s.wire.svc.EnsureConversation(t.Context(), s.wire.agent)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.wire.svc.Submit(t.Context(), view.ConversationID, chat.Turn{Text: "Which slot are you in, and which tools are top of mind? Then read the note in front with notes_read. Answer in two short lines."})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := http.Post(srv.URL+"/api/agent/conversations/"+view.ConversationID+"/attachments", "application/json", strings.NewReader(`{"role":"viewer","stream":"committed"}`))
	if err != nil {
		t.Fatal(err)
	}
	var attachment struct {
		ID string `json:"attachment_id"`
	}
	_ = json.NewDecoder(raw.Body).Decode(&attachment)
	_ = raw.Body.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/agent/conversations/"+view.ConversationID+"/events?attachment="+attachment.ID+"&after=0", nil)
	ev, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer ev.Body.Close()
	var text strings.Builder
	end := ""
	sc := bufio.NewScanner(ev.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() && end == "" {
		line, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok {
			continue
		}
		var e struct {
			Verb, Text, Message, Reason string
		}
		_ = json.Unmarshal([]byte(line), &e)
		switch e.Verb {
		case "part.delta":
			text.WriteString(e.Text)
		case "run.finish", "run.error", "run.abort":
			end = e.Verb + " " + e.Message + e.Reason
		}
	}
	t.Logf("end: %s\nanswer: %s\ntool calls: %v", end, text.String(), *calls)
	if !strings.HasPrefix(end, "run.finish") {
		t.Fatalf("run ended %q", end)
	}
	if len(*calls) == 0 {
		t.Errorf("the model did not call notes_read")
	}
}
