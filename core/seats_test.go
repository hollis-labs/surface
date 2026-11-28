package core

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A seat is another Nanite agent of the app: listed beside the seat host's in the same
// shape, opened on the wire under "seat.<id>", answering from its own folder with the app's
// context and tools.
func TestSeatsAreMoreNaniteAgentsOnTheWire(t *testing.T) {
	model := newFakeModel(t)
	naniteConfig(t, model.srv.URL)
	t.Setenv("SURFACE_AGENT_RUNNER", "")
	t.Setenv("NANITE_DEFAULT_MODEL", "test-model")
	seats := t.TempDir()
	dir := filepath.Join(seats, "researcher")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"agent.md", "host.yaml"} {
		b, err := os.ReadFile(filepath.Join("internal", "agent", "seat", f)) //nolint:gosec // fixed test inputs
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(string(b), "{{app}}", "researcher")
		if f == "agent.md" {
			text = strings.Replace(text, "name: researcher", "name: researcher\ntitle: Research Seat", 1)
			text += "\nUNIQUE-SEAT-INSTRUCTION: you are the research seat.\n"
		}
		if err := os.WriteFile(filepath.Join(dir, f), []byte(text), 0o600); err != nil { //nolint:gosec // test files in a temp dir
			t.Fatal(err)
		}
	}
	t.Setenv("SURFACE_SEATS_DIR", seats)
	app, err := New(t.Context(), Options{Name: "seatapp", Title: "Seats", DataDir: t.TempDir(), Modules: []string{"scratch"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close(); _ = app.Stop(context.Background()) })

	var list struct {
		Host  string `json:"host"`
		Seats []struct {
			Seat, Name, Shape, Error, WireBase string
			Local                              bool
		} `json:"seats"`
	}
	do(t, srv, "GET", "/api/agent/seats", "", &list)
	if len(list.Seats) != 1 || list.Seats[0].Seat != "researcher" || list.Seats[0].Name != "Research Seat" || !list.Seats[0].Local || list.Seats[0].Error != "" {
		t.Fatalf("seats = %+v", list)
	}
	var sess struct {
		ID             string `json:"id"`
		ConversationID string `json:"conversation_id"`
	}
	do(t, srv, "POST", "/api/agent/seats/researcher/sessions", "", &sess)
	if sess.ID != "seat.researcher" || sess.ConversationID == "" {
		t.Fatalf("session = %+v", sess)
	}
	var att struct {
		ID string `json:"attachment_id"`
	}
	do(t, srv, "POST", "/api/agent/conversations/"+sess.ConversationID+"/attachments", `{"role":"controller","stream":"committed"}`, &att)
	a := &lateApp{t: t, srv: srv, conv: sess.ConversationID, att: att.ID}
	if code := do(t, srv, "POST", "/api/agent/conversations/"+sess.ConversationID+"/turns", `{"attachment":"`+att.ID+`","text":"what can you do"}`, nil); code != 202 {
		t.Fatalf("turn: %d", code)
	}
	a.partsUntil("Answer 1.")
	req := model.request(1)
	if !strings.Contains(req, "UNIQUE-SEAT-INSTRUCTION") {
		t.Error("the seat did not answer from its own folder")
	}
	if !strings.Contains(req, "scratch_add") {
		t.Error("the seat does not have the app's tools")
	}
	if i := strings.Index(req, "standing_update"); i >= 0 {
		t.Errorf("standing_update in the seat's request: …%s…", req[max(0, i-300):min(len(req), i+120)])
	}
	// The embedded agent is still there, on its own conversation.
	var emb struct {
		ID string `json:"conversation_id"`
	}
	do(t, srv, "POST", "/api/agent/agents/seatapp/conversation", "{}", &emb)
	if emb.ID == "" || emb.ID == sess.ConversationID {
		t.Fatalf("embedded conversation %q (seat %q)", emb.ID, sess.ConversationID)
	}
	if code := do(t, srv, "POST", "/api/agent/seats/nobody/sessions", "", nil); code != 404 {
		t.Fatalf("unknown seat: %d", code)
	}
}
