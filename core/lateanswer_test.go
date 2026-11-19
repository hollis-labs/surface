package core

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// lateApp is an app on the conversation wire with the scripted runner, and a controller
// attached to its conversation.
type lateApp struct {
	t    *testing.T
	srv  *httptest.Server
	conv string
	att  string
}

func newLateApp(t *testing.T, name string) *lateApp {
	t.Helper()
	t.Setenv("SURFACE_AGENT_CONFIG", filepath.Join(t.TempDir(), "absent.json"))
	t.Setenv("SURFACE_AGENT_RUNNER", "scripted")
	t.Setenv("SURFACE_AGENT_SCRIPT_DELAY", "0s")
	ctx := context.Background()
	app, err := New(ctx, Options{Name: name, Title: name, DataDir: t.TempDir(), Modules: []string{"scratch"}})
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
	a := &lateApp{t: t, srv: srv}
	var conv struct {
		ID string `json:"conversation_id"`
	}
	do(t, srv, "POST", "/api/agent/agents/"+name+"/conversation", "{}", &conv)
	var att struct {
		ID string `json:"attachment_id"`
	}
	do(t, srv, "POST", "/api/agent/conversations/"+conv.ID+"/attachments", `{"role":"controller","stream":"committed"}`, &att)
	a.conv, a.att = conv.ID, att.ID
	return a
}

type lateCard struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Late      bool   `json:"late"`
	Delivered string `json:"delivered"`
	Outcome   *struct {
		Data map[string]any `json:"data"`
	} `json:"outcome"`
}

// ask runs card_ask as a run would, and stops the run while the card is up: it returns the
// card, abandoned.
func (a *lateApp) askAndStop(title string) lateCard {
	t := a.t
	t.Helper()
	rctx, cancel := context.WithCancel(context.Background())
	body := `{"kind":"form","title":"` + title + `","payload":{"schema":{"type":"object","properties":{"name":{"type":"string"},"size":{"type":"integer"}}}}}`
	req, _ := http.NewRequestWithContext(rctx, "POST", a.srv.URL+"/api/tools/card_ask", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	done := make(chan struct{})
	go func() {
		defer close(done)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}()
	id := ""
	a.until("the card is shown", func() bool {
		var open []lateCard
		do(t, a.srv, "GET", "/api/cards", "", &open)
		if len(open) == 1 {
			id = open[0].ID
		}
		return id != ""
	})
	cancel() // the run that asked is stopped
	<-done
	var c lateCard
	a.until("the card is abandoned", func() bool {
		do(t, a.srv, "GET", "/api/cards/"+id, "", &c)
		return c.Status != ""
	})
	return c
}

func (a *lateApp) until(what string, ok func() bool) {
	a.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			a.t.Fatalf("timed out waiting: %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// userTexts reads the conversation's stream from the start until want is in a part, and
// returns the texts of every part seen.
func (a *lateApp) partsUntil(want string) []string {
	t := a.t
	t.Helper()
	rctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(rctx, "GET", a.srv.URL+"/api/agent/conversations/"+a.conv+"/events?attachment="+a.att+"&after=0", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var parts []string
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<20)
	for sc.Scan() {
		line, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok {
			continue
		}
		var ev struct {
			Verb  string `json:"verb"`
			Final struct {
				Text string `json:"text"`
			} `json:"final"`
		}
		_ = json.Unmarshal([]byte(line), &ev)
		if ev.Verb == "part.end" && ev.Final.Text != "" {
			parts = append(parts, ev.Final.Text)
			if strings.Contains(ev.Final.Text, want) {
				return parts
			}
		}
	}
	t.Fatalf("the stream ended before %q; saw %q", want, parts)
	return nil
}

type lateRun struct {
	RunID  string `json:"run_id"`
	Origin string `json:"origin"`
	Status string `json:"status"`
}

func (a *lateApp) runs() []lateRun {
	var runs []lateRun
	do(a.t, a.srv, "GET", "/api/agent/runs", "", &runs)
	return runs
}

func countOrigin(runs []lateRun, origin string) int {
	n := 0
	for _, r := range runs {
		if r.Origin == origin {
			n++
		}
	}
	return n
}

const lateHead = `[The owner's answer to your card "`

// The bug: the agent asked with card_ask, its run ended while the owner was answering, and
// the answer was lost. Now the card is abandoned, the owner can still answer it once, and the
// answer reaches the agent's conversation as a turn of its own, out of the context trace.
func TestALateAnswerReachesTheAgent(t *testing.T) {
	a := newLateApp(t, "lateanswer")

	c := a.askAndStop("Project details")
	if c.Status != "abandoned" {
		t.Fatalf("a card_ask whose run was stopped: %q, want abandoned (not canceled)", c.Status)
	}

	var late lateCard
	if code := do(t, a.srv, "POST", "/api/cards/"+c.ID+"/response", `{"status":"submitted","data":{"name":"Atlas","size":3}}`, &late); code != 200 {
		t.Fatalf("a late answer: %d", code)
	}
	if late.Status != "submitted" || !late.Late {
		t.Fatalf("the late answer: %+v", late)
	}
	if code := do(t, a.srv, "POST", "/api/cards/"+c.ID+"/response", `{"status":"submitted","data":{"name":"Again"}}`, nil); code != 409 {
		t.Errorf("a second late answer: %d, want 409", code)
	}

	parts := a.partsUntil(`{"name":"Atlas","size":3}`)
	got := parts[len(parts)-1]
	if !strings.HasPrefix(got, lateHead+`Project details"`) || !strings.Contains(got, "given after the run that asked it ended") {
		t.Errorf("the late answer's turn: %q", got)
	}
	a.until("the card says it was delivered", func() bool {
		do(t, a.srv, "GET", "/api/cards/"+c.ID, "", &late)
		return late.Delivered != ""
	})

	// The run log has it as the late-answer errand; the trace does not have it.
	a.until("the run log has the late answer", func() bool {
		runs := a.runs()
		return len(runs) == 1 && runs[0].Status != ""
	})
	if runs := a.runs(); runs[0].Origin != "errand:late-answer" {
		t.Errorf("the run log: %+v", runs)
	}
	var trace struct {
		Turns []map[string]any `json:"turns"`
	}
	do(t, a.srv, "GET", "/api/agent/trace?limit=5", "", &trace)
	if len(trace.Turns) != 0 {
		t.Errorf("the late answer was traced as the owner's turn: %v", trace.Turns)
	}

	// A canceled late answer only dismisses the card: nothing goes to the agent.
	d := a.askAndStop("Dismiss me")
	var gone lateCard
	if code := do(t, a.srv, "POST", "/api/cards/"+d.ID+"/response", `{"status":"canceled"}`, &gone); code != 200 || gone.Status != "canceled" || !gone.Late {
		t.Fatalf("dismissed: %d %+v", code, gone)
	}
	time.Sleep(200 * time.Millisecond)
	if n := countOrigin(a.runs(), "errand:late-answer"); n != 1 {
		t.Errorf("%d late-answer runs after a dismissal, want 1", n)
	}
}

// While the agent is busy, a late answer waits, and is delivered once when the run ends.
func TestALateAnswerWaitsForABusyAgent(t *testing.T) {
	a := newLateApp(t, "latebusy")
	c := a.askAndStop("While busy")

	var hold struct {
		RunID string `json:"run_id"`
	}
	if code := do(t, a.srv, "POST", "/api/agent/conversations/"+a.conv+"/turns", `{"attachment":"`+a.att+`","text":"hold"}`, &hold); code != 202 || hold.RunID == "" {
		t.Fatalf("the holding turn: %d %+v", code, hold)
	}
	if code := do(t, a.srv, "POST", "/api/cards/"+c.ID+"/response", `{"status":"submitted","data":{"name":"Queued"}}`, nil); code != 200 {
		t.Fatalf("a late answer: %d", code)
	}
	// It waits: the agent is busy past a retry.
	time.Sleep(2500 * time.Millisecond)
	var card lateCard
	do(t, a.srv, "GET", "/api/cards/"+c.ID, "", &card)
	if card.Delivered != "" || countOrigin(a.runs(), "errand:late-answer") != 0 {
		t.Fatalf("delivered while the agent was busy: %+v", card)
	}

	if code := do(t, a.srv, "POST", "/api/agent/conversations/"+a.conv+"/runs/"+hold.RunID+"/cancel", `{"attachment":"`+a.att+`"}`, nil); code != 202 {
		t.Fatalf("canceling the holding run: %d", code)
	}
	a.partsUntil(`{"name":"Queued"}`)
	a.until("delivered", func() bool {
		do(t, a.srv, "GET", "/api/cards/"+c.ID, "", &card)
		return card.Delivered != ""
	})
	// Exactly once: a later turn, and still one late-answer run and one late-answer part.
	a.until("the late answer's run ended", func() bool {
		for _, r := range a.runs() {
			if r.Origin == "errand:late-answer" && r.Status != "" {
				return true
			}
		}
		return false
	})
	time.Sleep(300 * time.Millisecond)
	if code := do(t, a.srv, "POST", "/api/agent/conversations/"+a.conv+"/turns", `{"attachment":"`+a.att+`","text":"marker"}`, nil); code != 202 {
		t.Fatalf("the marker turn: %d", code)
	}
	parts := a.partsUntil("You said: marker")
	n := 0
	for _, p := range parts {
		if strings.HasPrefix(p, lateHead) {
			n++
		}
	}
	if n != 1 || countOrigin(a.runs(), "errand:late-answer") != 1 {
		t.Errorf("the late answer was delivered %d times (runs %+v), want once", n, a.runs())
	}
}

// An app that stopped with the agent's question still open (killed before card_ask could
// close it) abandons it at the next start, so it can still be answered late.
func TestAnAgentsQuestionLeftOpenIsAbandonedAtStart(t *testing.T) {
	t.Setenv("SURFACE_AGENT_CONFIG", filepath.Join(t.TempDir(), "absent.json"))
	t.Setenv("SURFACE_AGENT_RUNNER", "")
	ctx := context.Background()
	dir := t.TempDir()
	open := func() (*App, *httptest.Server) {
		app, err := New(ctx, Options{Name: "lateorphan", Title: "Orphans", DataDir: dir, Modules: []string{"scratch"}})
		if err != nil {
			t.Fatal(err)
		}
		if err = app.Start(ctx); err != nil {
			t.Fatal(err)
		}
		return app, httptest.NewServer(app.Handler())
	}
	app, srv := open()
	var c lateCard
	do(t, srv, "POST", "/api/cards", `{"kind":"confirm","source":"agent","blocking":true,"payload":{"body":"Go?"}}`, &c)
	srv.Close()
	_ = app.Stop(ctx)

	app, srv = open()
	defer func() { srv.Close(); _ = app.Stop(ctx) }()
	do(t, srv, "GET", "/api/cards/"+c.ID, "", &c)
	if c.Status != "abandoned" {
		t.Fatalf("after a restart: %q, want abandoned", c.Status)
	}
	// Off the conversation wire the late answer is taken and kept on the card.
	if code := do(t, srv, "POST", "/api/cards/"+c.ID+"/response", `{"status":"submitted"}`, &c); code != 200 || !c.Late || c.Delivered != "" {
		t.Errorf("a late answer off the wire: %d %+v", code, c)
	}
}
