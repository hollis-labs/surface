package core

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	smodule "github.com/hollis-labs/substrate/module"
	"github.com/hollis-labs/substrate/modules/httphost"
)

func TestFrontDeskDecisionsSurviveRestart(t *testing.T) {
	for _, action := range []string{"accept", "decline"} {
		t.Run(action, func(t *testing.T) {
			model := newFakeModel(t)
			naniteConfig(t, model.srv.URL)
			path := os.Getenv("SURFACE_AGENT_CONFIG")
			raw, err := os.ReadFile(path) //nolint:gosec // config created by naniteConfig
			if err != nil {
				t.Fatal(err)
			}
			var cfg map[string]any
			if err = json.Unmarshal(raw, &cfg); err != nil {
				t.Fatal(err)
			}
			cfg["front_desk"] = map[string]any{"mode": "suggest"}
			cfg["builtins"] = []string{"subtasks", "background"}
			cfg["auto_approve"] = true
			raw, _ = json.Marshal(cfg)
			path = filepath.Join(t.TempDir(), "agent.json")
			t.Setenv("SURFACE_AGENT_CONFIG", path)
			if err = os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			} //nolint:gosec // test config
			data := t.TempDir()
			open := func() (*App, *httptest.Server, *lateApp) {
				a, openErr := New(t.Context(), Options{Name: "plans", DataDir: data, Modules: []string{"scratch"}})
				if openErr != nil {
					t.Fatal(openErr)
				}
				if openErr = a.Start(t.Context()); openErr != nil {
					t.Fatal(openErr)
				}
				srv := httptest.NewServer(a.Handler())
				var conv struct {
					ID string `json:"conversation_id"`
				}
				do(t, srv, "POST", "/api/agent/agents/plans/conversation", "{}", &conv)
				var att struct {
					ID string `json:"attachment_id"`
				}
				do(t, srv, "POST", "/api/agent/conversations/"+conv.ID+"/attachments", `{"role":"controller","stream":"deltas"}`, &att)
				return a, srv, &lateApp{t: t, srv: srv, conv: conv.ID, att: att.ID}
			}
			a, srv, wire := open()
			closed := false
			defer func() {
				if !closed {
					srv.CloseClientConnections()
					srv.Close()
					_ = a.Stop(context.Background())
				}
			}()
			turn, _ := json.Marshal(map[string]string{"attachment": wire.att, "text": "1. Research apples\n2. Research oranges"})
			if code := do(t, srv, "POST", "/api/agent/conversations/"+wire.conv+"/turns", string(turn), nil); code != 202 {
				t.Fatalf("turn %d", code)
			}
			wire.waitIdle()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/agent/conversations/"+wire.conv+"/events?attachment="+wire.att+"&after=0", nil)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			scan := bufio.NewScanner(res.Body)
			plan := ""
			for scan.Scan() {
				line, ok := strings.CutPrefix(scan.Text(), "data: ")
				if !ok {
					continue
				}
				var e struct {
					Kind  string `json:"kind"`
					Value struct {
						ID string `json:"id"`
					} `json:"value"`
				}
				if json.Unmarshal([]byte(line), &e) == nil && e.Kind == "nanite.desk.plan" {
					plan = e.Value.ID
					break
				}
			}
			_ = res.Body.Close()
			if plan == "" {
				t.Fatalf("no plan: %v", scan.Err())
			}
			route := "/api/agent/front-desk/plans/" + plan
			if code := answerPlan(t, srv, route+"/"+action); code != 200 {
				t.Fatalf("answer %s: %d", action, code)
			}
			srv.CloseClientConnections()
			srv.Close()
			_ = a.Stop(context.Background())
			closed = true
			a, srv, _ = open()
			closed = false
			var decision struct {
				Status string `json:"status"`
			}
			if code := do(t, srv, "GET", route, "", &decision); code != 200 || decision.Status != action {
				t.Fatalf("reopened: %d %+v", code, decision)
			}
			if code := answerPlan(t, srv, route+"/"+action); code != 409 {
				t.Fatalf("repeat answer: %d", code)
			}
		})
	}
}

// Exercise Substrate's actual stopping signal: request contexts stay live during
// its grace period. Both streams an attached panel opens must end before that.
func TestPanelStreamsDrainOnServerShutdown(t *testing.T) {
	t.Setenv("SURFACE_AGENT_CONFIG", filepath.Join(t.TempDir(), "absent.json"))
	t.Setenv("SURFACE_AGENT_RUNNER", "scripted")
	app, err := New(t.Context(), Options{Name: "drain", DataDir: t.TempDir(), Modules: []string{"scratch"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Stop(context.Background()) }()
	if err = app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() {
		done <- httphost.Serve(ctx, &smodule.App{}, app.Handler(), httphost.ServeOptions{Addr: "127.0.0.1:0", GracePeriod: 2 * time.Second, Ready: func(a net.Addr) { ready <- a }})
	}()
	var addr net.Addr
	select {
	case addr = <-ready:
	case err = <-done:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not listen")
	}
	srv := &httptest.Server{URL: "http://" + addr.String()}
	var conv struct {
		ID string `json:"conversation_id"`
	}
	var att struct {
		ID string `json:"attachment_id"`
	}
	do(t, srv, "POST", "/api/agent/agents/drain/conversation", "{}", &conv)
	do(t, srv, "POST", "/api/agent/conversations/"+conv.ID+"/attachments", `{"role":"viewer","stream":"deltas"}`, &att)
	ended := make(chan error, 2)
	for _, path := range []string{"/api/events", "/api/agent/conversations/" + conv.ID + "/events?attachment=" + att.ID + "&after=0"} {
		res, streamErr := http.Get(srv.URL + path) //nolint:gosec // disposable loopback server
		if streamErr != nil {
			t.Fatal(streamErr)
		}
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != 200 {
			t.Fatalf("stream %s: %d", path, res.StatusCode)
		}
		go func() { _, copyErr := io.Copy(io.Discard, res.Body); ended <- copyErr }()
	}
	start := time.Now()
	cancel()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("streams exceeded 1s shutdown allowance (2s grace)")
	}
	for range 2 {
		select {
		case err = <-ended:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("reader did not close")
		}
	}
	t.Logf("attached panel shutdown: %s", time.Since(start))
}

func answerPlan(t *testing.T, srv *httptest.Server, route string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+route, strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", srv.URL)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	return res.StatusCode
}
