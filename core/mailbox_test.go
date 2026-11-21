package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeTether is Tether's registry and messages API as Nanite's mailbox uses it: one agent,
// its unread mail, and which messages were marked read, with the bearer it was given.
type fakeTether struct {
	srv    *httptest.Server
	urn    string
	mu     sync.Mutex
	unread []map[string]any
	read   map[string]int
	bad    int // requests without the agent's credential
}

func newFakeTether(t *testing.T, urn, credential string) *fakeTether {
	f := &fakeTether{urn: urn, read: map[string]int{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+credential {
			f.bad++
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/registry/agents/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"urn": f.urn, "display_name": "Bas"})
		case r.Method == "GET" && r.URL.Path == "/messages/list":
			if r.URL.Query().Get("to") != f.urn || r.URL.Query().Get("unread_only") != "true" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"messages": f.unread, "total": len(f.unread)})
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/read"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/messages/"), "/read")
			f.read[id]++
			kept := f.unread[:0]
			for _, m := range f.unread {
				if m["id"] != id {
					kept = append(kept, m)
				}
			}
			f.unread = kept
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeTether) send(id, subject string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unread = append(f.unread, map[string]any{"id": id, "from": "msg://agent/agent-mux/sender", "to": f.urn, "kind": "request",
		"subject": subject, "body": "Please look at " + subject, "created_at": time.Now().UTC().Format(time.RFC3339Nano)})
}

func (f *fakeTether) readCount(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.read[id]
}

// Mail to the agent's own address arrives as a notice turn, shown in the conversation in
// front as Tether mail, is marked read after it is handled, and is handled once.
func TestAgentMailboxDeliversMailOnceAsANoticeTurn(t *testing.T) {
	const urn = "msg://agent/agent-mux/agt_mailtest1"
	model := newFakeModel(t)
	naniteConfig(t, model.srv.URL)
	dir := filepath.Dir(os.Getenv("SURFACE_AGENT_CONFIG"))
	credential := filepath.Join(dir, "tether.credential")
	if err := os.WriteFile(credential, []byte("bas-credential\n"), 0o600); err != nil { //nolint:gosec // the test's own temporary directory
		t.Fatal(err)
	}
	tether := newFakeTether(t, urn, "bas-credential")
	raw, _ := json.Marshal(map[string]any{"model": "test-model", "gateway_url": model.srv.URL, "key_file": filepath.Join(dir, "key"),
		"mailbox": map[string]any{"url": tether.srv.URL, "credential_file": credential, "poll_seconds": 1}})
	if err := os.WriteFile(os.Getenv("SURFACE_AGENT_CONFIG"), raw, 0o600); err != nil { //nolint:gosec // the test's own temporary directory
		t.Fatal(err)
	}
	agentsDir := t.TempDir()
	t.Setenv("SURFACE_AGENTS_DIR", agentsDir)
	seat := filepath.Join(agentsDir, "mailtest")
	if err := os.MkdirAll(seat, 0o700); err != nil {
		t.Fatal(err)
	}
	starter, err := os.ReadFile("internal/agent/seat/agent.md")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(seat, "agent.md"), []byte(strings.ReplaceAll(string(starter), "{{app}}", "mailtest")), 0o600); err != nil { //nolint:gosec // the test's own temporary directory
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(seat, "host.yaml"), []byte("kind: embedded\nlifetime: application\napplication_id: mailtest\nidentity:\n  tether: "+urn+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tether.send("mail-1", "MAILBOX-SUBJECT-ONE")

	app, err := New(t.Context(), Options{Name: "mailtest", DataDir: t.TempDir(), Modules: []string{"scratch"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	t.Cleanup(func() { srv.CloseClientConnections(); srv.Close(); _ = app.Stop(context.Background()) })
	var conv struct {
		ID string `json:"conversation_id"`
	}
	do(t, srv, "POST", "/api/agent/agents/mailtest/conversation", "{}", &conv)
	var att struct {
		ID string `json:"attachment_id"`
	}
	do(t, srv, "POST", "/api/agent/conversations/"+conv.ID+"/attachments", `{"role":"viewer","stream":"committed"}`, &att)
	a := &lateApp{t: t, srv: srv, conv: conv.ID, att: att.ID}

	deadline := time.Now().Add(15 * time.Second)
	for tether.readCount("mail-1") == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the mail was never marked read; model requests: %d", model.count())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if model.sentWith("MAILBOX-SUBJECT-ONE") == "" {
		t.Fatal("the agent never saw the mail")
	}
	// Shown in the conversation in front, labeled.
	lines := a.streamUntil(`"auto":"Tether mail"`)
	if countWith(lines, `"auto":"Tether mail"`) == 0 {
		t.Fatal("the mail turn is not labeled in the conversation")
	}
	a.waitIdle()

	// Handled once: further polls neither ask the model again nor mark it read again.
	asked := model.count()
	time.Sleep(2500 * time.Millisecond)
	if n := tether.readCount("mail-1"); n != 1 {
		t.Fatalf("marked read %d times", n)
	}
	mailTurns := 0
	for i := 1; i <= model.count(); i++ {
		if strings.Contains(model.request(i), "MAILBOX-SUBJECT-ONE") && i > asked {
			mailTurns++
		}
	}
	if mailTurns != 0 {
		t.Fatalf("the mail reached the model again after it was handled (%d requests)", mailTurns)
	}
	if tether.bad != 0 {
		t.Fatalf("%d requests to Tether without the agent's credential", tether.bad)
	}
}

// A seat that names a Tether identity without a mailbox in the agent config says what is
// missing instead of failing inside Nanite.
func TestIdentityWithoutMailboxConfigSaysWhatIsMissing(t *testing.T) {
	model := newFakeModel(t)
	naniteConfig(t, model.srv.URL)
	agentsDir := t.TempDir()
	t.Setenv("SURFACE_AGENTS_DIR", agentsDir)
	seat := filepath.Join(agentsDir, "mailmissing")
	if err := os.MkdirAll(seat, 0o700); err != nil {
		t.Fatal(err)
	}
	starter, err := os.ReadFile("internal/agent/seat/agent.md")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(seat, "agent.md"), []byte(strings.ReplaceAll(string(starter), "{{app}}", "mailmissing")), 0o600); err != nil { //nolint:gosec // the test's own temporary directory
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(seat, "host.yaml"), []byte("kind: embedded\nlifetime: application\napplication_id: mailmissing\nidentity:\n  tether: msg://agent/agent-mux/agt_mailtest2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := New(t.Context(), Options{Name: "mailmissing", DataDir: t.TempDir(), Modules: []string{"scratch"}})
	if err == nil {
		err = app.Start(t.Context())
		t.Cleanup(func() { _ = app.Stop(context.Background()) })
	}
	if err == nil || !strings.Contains(err.Error(), "mailbox.credential_file") || strings.Contains(err.Error(), ".staged") {
		t.Fatalf("want an error naming the missing mailbox config and the seat, got %v", err)
	}
}
