package core

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hollis-labs/surface/module"
)

// readyMod is a module that answers readiness with whatever the test sets.
type readyMod struct {
	mu sync.Mutex
	r  module.Readiness
}

func (m *readyMod) Manifest() module.Manifest {
	return module.Manifest{ID: "readyprobe", Title: "Ready probe"}
}
func (m *readyMod) Start(context.Context, module.Host) error { return nil }
func (m *readyMod) Stop(context.Context) error               { return nil }
func (m *readyMod) Ready(context.Context) module.Readiness {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.r
}
func (m *readyMod) set(status string, why ...string) {
	m.mu.Lock()
	m.r = module.Readiness{Status: status, Reasons: why}
	m.mu.Unlock()
}

var theReadyMod = &readyMod{r: module.Readiness{Status: module.ReadyOK}}

func init() { module.Register(theReadyMod) }

func TestReadyzIsTheWorstModuleAndStatusOnly(t *testing.T) {
	t.Setenv("SURFACE_AGENT_CONFIG", filepath.Join(t.TempDir(), "absent.json"))
	ctx := context.Background()
	app, err := New(ctx, Options{Name: "readytest", Title: "Ready", DataDir: t.TempDir(), Modules: []string{"scratch", "readyprobe"}})
	if err != nil {
		t.Fatal(err)
	}
	// Before the modules start, the app is not ready.
	if r := app.k.Ready(ctx); r.Status != module.ReadyDown || !strings.Contains(strings.Join(r.Reasons, ","), "not started") {
		t.Fatalf("before start: %+v", r)
	}
	if err = app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	t.Cleanup(func() {
		srv.Close()
		_ = app.Stop(context.Background())
	})

	for _, c := range []struct {
		status string
		why    []string
		code   int
		want   string
	}{
		{module.ReadyOK, nil, 200, "ok"},
		{module.ReadyDegraded, []string{"a machine is offline"}, 200, "degraded"},
		{module.ReadyDown, []string{"the store does not answer"}, 503, "down"},
		{"", nil, 200, "degraded"}, // no answer is not ok
	} {
		theReadyMod.set(c.status, c.why...)
		var got map[string]any
		if code := do(t, srv, "GET", "/readyz", "", &got); code != c.code || got["status"] != c.want || len(got) != 1 {
			t.Errorf("%q: %d %v", c.status, code, got)
		}
	}
	theReadyMod.set(module.ReadyDegraded, "a machine is offline")
	if r := app.k.Ready(ctx); len(r.Reasons) != 1 || r.Reasons[0] != "readyprobe: a machine is offline" {
		t.Fatalf("reasons %v", r.Reasons)
	}
	theReadyMod.set(module.ReadyOK)
}

func TestIdentityEventsStayInTheProcess(t *testing.T) {
	t.Setenv("SURFACE_AGENT_CONFIG", filepath.Join(t.TempDir(), "absent.json"))
	ctx := context.Background()
	app, err := New(ctx, Options{Name: "eventstest", Title: "Events", DataDir: t.TempDir(), Modules: []string{"scratch"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Handler())
	t.Cleanup(func() {
		srv.Close()
		_ = app.Stop(context.Background())
	})
	// A module sees identity.key.* on the bus.
	seen := make(chan string, 4)
	stop := app.k.Events().Subscribe(module.KeyEventTopic, func(ev module.Event) { seen <- ev.Topic })
	defer stop()

	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(c, "GET", srv.URL+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data: ") {
				lines <- sc.Text()
			}
		}
		close(lines)
	}()
	<-lines // the desk's state, first

	app.Publish(module.KeyEventTopic+".created", module.KeyEvent{Action: "created", ID: "k_1", Name: "laptop"})
	app.Publish("page.marker", map[string]string{"x": "y"})
	if got := <-seen; got != "identity.key.created" {
		t.Fatalf("bus: %s", got)
	}
	for l := range lines {
		if strings.Contains(l, "identity") {
			t.Fatalf("streamed to the page: %s", l)
		}
		if strings.Contains(l, `"topic":"page.marker"`) {
			return // published after the key event: the key event was skipped
		}
	}
	t.Fatal("the stream ended")
}
