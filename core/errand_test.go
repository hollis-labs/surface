package core

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hollis-labs/surface/module"
)

// errandMod is a module that says something around every message and hands the agent errands.
type errandMod struct {
	module.Base
	h module.Host
}

func (m *errandMod) Manifest() module.Manifest {
	return module.Manifest{ID: "errandtest", Title: "Errand test", Version: "0.0.1",
		Around: func(context.Context, module.Host) (string, error) { return "2 answers came back", nil }}
}
func (m *errandMod) Start(_ context.Context, h module.Host) error { m.h = h; return nil }

var theErrandMod = &errandMod{}

func init() { module.Register(theErrandMod) }

// A module's Around is in every message's context, and its errand runs as a turn on the
// agent's conversation, marked as the module's and kept out of the context trace.
func TestAModulesAroundAndErrand(t *testing.T) {
	t.Setenv("SURFACE_AGENT_CONFIG", filepath.Join(t.TempDir(), "absent.json"))
	t.Setenv("SURFACE_AGENT_RUNNER", "scripted")
	t.Setenv("SURFACE_AGENT_SCRIPT_DELAY", "0s")
	ctx := context.Background()
	app, err := New(ctx, Options{Name: "errands", Title: "Errands", DataDir: t.TempDir(), Modules: []string{"scratch", "errandtest"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
		_ = app.Stop(context.Background())
	})

	var asm struct {
		Block  string `json:"block"`
		Around []struct {
			Module, Text string
		} `json:"around"`
	}
	do(t, srv, "GET", "/api/agent/context", "", &asm)
	if !strings.Contains(asm.Block, "Around (errandtest):\n2 answers came back") || len(asm.Around) != 1 {
		t.Fatalf("the context: %+v", asm)
	}

	run, err := theErrandMod.h.Agent().Ask(ctx, module.Ask{Title: "Mail for you", Text: "Answer the two requests."})
	if err != nil || run == "" {
		t.Fatalf("errand: %q %v", run, err)
	}
	// While it runs, another errand is told the agent is busy.
	if _, again := theErrandMod.h.Agent().Ask(ctx, module.Ask{Text: "again"}); again != nil && !errors.Is(again, module.ErrAgentBusy) {
		t.Fatalf("a second errand: %v", again)
	}
	var conv struct {
		ID string `json:"conversation_id"`
	}
	do(t, srv, "POST", "/api/agent/agents/errands/conversation", "{}", &conv)
	var att struct {
		ID string `json:"attachment_id"`
	}
	do(t, srv, "POST", "/api/agent/conversations/"+conv.ID+"/attachments", `{"role":"viewer","stream":"committed"}`, &att)
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(rctx, "GET", srv.URL+"/api/agent/conversations/"+conv.ID+"/events?attachment="+att.ID+"&after=0", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<20)
	found := false
	for sc.Scan() {
		if strings.Contains(sc.Text(), "An errand from the errandtest module, not a message from the owner: Mail for you]") {
			found = true
		}
		if found && strings.Contains(sc.Text(), `"run.finish"`) {
			break
		}
	}
	if !found {
		t.Fatal("the errand's turn is not on the conversation")
	}
	var trace struct {
		Turns []map[string]any `json:"turns"`
	}
	do(t, srv, "GET", "/api/agent/trace?limit=5", "", &trace)
	if len(trace.Turns) != 0 {
		t.Fatalf("an errand was traced as the owner's turn: %v", trace.Turns)
	}
}

// Off the conversation wire the agent takes no errands, and says so.
func TestNoErrandsOffTheWire(t *testing.T) {
	t.Setenv("SURFACE_AGENT_CONFIG", filepath.Join(t.TempDir(), "absent.json"))
	t.Setenv("SURFACE_AGENT_RUNNER", "")
	ctx := context.Background()
	app, err := New(ctx, Options{Name: "noerrands", Title: "No errands", DataDir: t.TempDir(), Modules: []string{"errandtest"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
	if _, err := theErrandMod.h.Agent().Ask(ctx, module.Ask{Text: "x"}); !errors.Is(err, module.ErrNoAgent) {
		t.Fatalf("%v", err)
	}
}
