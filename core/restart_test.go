package core

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// A page held the input when the app went away (killed, restarted for a deploy) and never
// got to let go. The app that starts next must not keep that lease for a window that is gone:
// its start bumps the conversation's generation, so the lease is free and the old attachment
// is stale (a live page attaches again on its own).
func TestARestartFreesTheInputAPageHeld(t *testing.T) {
	t.Setenv("SURFACE_AGENT_CONFIG", filepath.Join(t.TempDir(), "absent.json"))
	t.Setenv("SURFACE_AGENT_RUNNER", "scripted")
	t.Setenv("SURFACE_AGENT_SCRIPT_DELAY", "0s")
	data := t.TempDir()
	ctx := context.Background()
	open := func() (*App, *httptest.Server) {
		app, err := New(ctx, Options{Name: "restart", Title: "Restart", DataDir: data, Modules: []string{"scratch"}})
		if err != nil {
			t.Fatal(err)
		}
		if err = app.Start(ctx); err != nil {
			t.Fatal(err)
		}
		return app, httptest.NewServer(app.Handler())
	}
	type conv struct {
		ID         string `json:"conversation_id"`
		Generation int64  `json:"generation"`
		Lease      *struct {
			Attachment string `json:"attachment"`
		} `json:"lease"`
	}

	app, srv := open()
	var c1 conv
	do(t, srv, "POST", "/api/agent/agents/restart/conversation", "{}", &c1)
	var att struct {
		ID string `json:"attachment_id"`
	}
	do(t, srv, "POST", "/api/agent/conversations/"+c1.ID+"/attachments", `{"role":"controller","stream":"committed"}`, &att)
	if code := do(t, srv, "POST", "/api/agent/conversations/"+c1.ID+"/lease", `{"attachment":"`+att.ID+`"}`, nil); code != 200 {
		t.Fatalf("lease: %d", code)
	}
	do(t, srv, "POST", "/api/agent/agents/restart/conversation", "{}", &c1)
	if c1.Lease == nil || c1.Lease.Attachment != att.ID {
		t.Fatalf("the page does not hold the input: %+v", c1)
	}
	// The app goes away; the page never detaches.
	srv.Close()
	_ = app.Stop(context.Background())

	app2, srv2 := open()
	t.Cleanup(func() {
		srv2.Close()
		_ = app2.Stop(context.Background())
	})
	var c2 conv
	deadline := time.Now().Add(3 * time.Second)
	for {
		do(t, srv2, "POST", "/api/agent/agents/restart/conversation", "{}", &c2)
		if c2.Lease == nil && c2.Generation > c1.Generation {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after a restart the lease is still held by a gone page: %+v (before %+v)", c2, c1)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if c2.ID != c1.ID {
		t.Fatalf("the conversation changed: %s %s", c1.ID, c2.ID)
	}
	// The old attachment is stale: the page that held it attaches again.
	if code := do(t, srv2, "POST", "/api/agent/conversations/"+c2.ID+"/lease", `{"attachment":"`+att.ID+`"}`, nil); code != 409 {
		t.Fatalf("the old attachment still works: %d", code)
	}
}
