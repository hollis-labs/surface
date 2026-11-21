// Package terminal is the Terminal module: local shells in pseudo-terminals, drawn by xterm.js in
// a window of tabs. A shell outlives its window: closing or reloading the page leaves it running,
// and the next attach replays its recent output. The shells are this machine's, as the user that
// runs the app: including the module in an app is the decision to hand one out.
package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/coder/websocket"
	"github.com/hollis-labs/substrate/modules/httphost"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/modules/terminal/shell"
)

func init() { module.Register(&mod{}) }

type mod struct {
	module.Base
	sh *shell.Manager
}

const viewID = "terminal"

func (m *mod) Manifest() module.Manifest {
	return module.Manifest{
		ID: "terminal", Title: "Terminal", Version: "0.1.0",
		About: "Shells on this machine, in tabs. A shell keeps running when its window closes or the page reloads.",
		Views: []module.View{{
			ID: viewID, Title: "Terminal", Icon: "square-terminal", Width: 900, Height: 560,
			Menu: true, Multi: true, Bare: true, DefaultKey: "main", Tiling: "tile",
			Render: module.Component("terminal/main"),
			Describe: func(_ context.Context, _ module.Host, _ module.Window) (module.WindowContext, error) {
				return module.WindowContext{Kind: "data", Ref: "terminal:sessions", Extra: map[string]any{"sessions": len(m.sh.List())}}, nil
			},
		}},
		Commands: []module.Command{
			{ID: "terminal.open", Title: "Terminal: open", Section: "Terminal", Keywords: "shell console command line tty", Keys: "ctrl+`",
				Run: func(ctx context.Context, h module.Host, _ map[string]any) (any, error) {
					return h.Desk().Open(ctx, viewID, module.OpenOptions{Key: "main"})
				}},
		},
		Tools: []module.Tool{
			{Name: "terminal_list", Level: module.LevelRead,
				Description: "List the open terminal sessions: id, title, shell, starting folder, whether the shell has exited, and how many windows show it.",
				Input:       json.RawMessage(`{"type":"object","properties":{}}`),
				Run: func(context.Context, module.Host, json.RawMessage) (any, error) {
					return m.sh.List(), nil
				}},
			{Name: "terminal_status", Level: module.LevelRead,
				Description: "What one terminal session is doing: whether a program holds its foreground (busy), which one, and the folder it is in.",
				Input:       json.RawMessage(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}`),
				Run: func(_ context.Context, _ module.Host, in json.RawMessage) (any, error) {
					var a struct {
						ID string `json:"id"`
					}
					_ = json.Unmarshal(in, &a)
					return m.sh.Status(a.ID)
				}},
		},
	}
}

func (m *mod) Start(_ context.Context, h module.Host) error {
	m.sh = shell.NewManager()
	h.Handle("GET /sessions", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reply(w, 200, map[string]any{"sessions": m.sh.List(), "max_sessions": shell.MaxSessions})
	}))
	h.Handle("POST /sessions", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Cwd   string `json:"cwd"`
			Title string `json:"title"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		info, err := m.sh.Create(shell.Options{Cwd: in.Cwd, Title: in.Title})
		switch {
		case errors.Is(err, shell.ErrTooMany):
			fail(w, 429, err.Error())
		case err != nil:
			fail(w, 400, err.Error())
		default:
			h.Events().Publish("terminal.changed", map[string]any{"created": info.ID})
			reply(w, 201, info)
		}
	}))
	h.Handle("PUT /sessions/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Title string `json:"title"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		info, err := m.sh.Rename(r.PathValue("id"), in.Title)
		switch {
		case errors.Is(err, shell.ErrNotFound):
			fail(w, 404, "no such terminal session")
		case err != nil:
			fail(w, 400, err.Error())
		default:
			h.Events().Publish("terminal.changed", map[string]any{"renamed": info.ID})
			reply(w, 200, info)
		}
	}))
	h.Handle("DELETE /sessions/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := m.sh.Close(r.PathValue("id")); err != nil {
			fail(w, 404, "no such terminal session")
			return
		}
		h.Events().Publish("terminal.changed", map[string]any{"closed": r.PathValue("id")})
		w.WriteHeader(204)
	}))
	h.Handle("GET /sessions/{id}/status", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st, err := m.sh.Status(r.PathValue("id"))
		if err != nil {
			fail(w, 404, "no such terminal session")
			return
		}
		reply(w, 200, st)
	}))
	h.Handle("GET /sessions/{id}/ws", http.HandlerFunc(m.attach))
	return nil
}

func (m *mod) Stop(context.Context) error {
	if m.sh != nil {
		m.sh.CloseAll()
	}
	return nil
}

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	reply(w, code, map[string]string{"error": msg})
}

// The socket. Binary frames are raw bytes both ways; text frames are JSON control messages:
// resize from the browser; state and exit to it. Basalisk's and Tachyon's wire protocol.
type control struct {
	Type string `json:"type"` // resize
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

type event struct {
	Type  string `json:"type"` // state | exit
	State string `json:"state,omitempty"`
	Code  *int   `json:"code,omitempty"`
}

// sameOrigin refuses a socket opened by another site: the upgrade is a GET, so the app's guard
// (which checks the Origin of unsafe methods) does not, and a shell is the most powerful thing
// the app hands out. The guard also refuses a Host a page could rebind to this machine, so an
// Origin that matches the Host is this app's own page.
func sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // not a browser
	}
	u, err := url.Parse(origin)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.User == nil && u.Host == r.Host
}

func (m *mod) attach(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		fail(w, 403, "cross-origin terminal connections are refused")
		return
	}
	id := r.PathValue("id")
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = c.CloseNow() }()
	c.SetReadLimit(1 << 20)
	ctx := r.Context()

	// The browser sends its size first, so the shell starts at the right size. A socket that
	// says nothing for a few seconds is dropped (a canceled read closes it anyway).
	cols, rows := uint16(100), uint16(30)
	var pending []byte
	rctx, rcancel := context.WithTimeout(ctx, 5*time.Second)
	typ, data, err := c.Read(rctx)
	rcancel()
	if err != nil {
		return
	}
	if typ == websocket.MessageText {
		var ctl control
		if json.Unmarshal(data, &ctl) == nil && ctl.Type == "resize" {
			cols, rows = ctl.Cols, ctl.Rows
		}
	} else {
		pending = data
	}

	att, err := m.sh.Attach(id, cols, rows)
	if err != nil {
		_ = c.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}
	defer att.Detach()
	att.Resize(cols, rows)

	send := func(ev event) {
		b, _ := json.Marshal(ev)
		_ = c.Write(ctx, websocket.MessageText, b)
	}
	if len(att.Replay) > 0 && c.Write(ctx, websocket.MessageBinary, att.Replay) != nil {
		return
	}
	if len(pending) > 0 {
		_ = att.Input(pending)
	}
	send(event{Type: "state", State: "open"})

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageBinary {
				if att.Input(data) != nil {
					return
				}
				continue
			}
			var ctl control
			if json.Unmarshal(data, &ctl) == nil && ctl.Type == "resize" {
				att.Resize(ctl.Cols, ctl.Rows)
			}
		}
	}()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case f, ok := <-att.Frames:
			if !ok {
				if exited, code := att.Exited(); exited {
					send(event{Type: "exit", Code: &code})
					_ = c.Close(websocket.StatusNormalClosure, "session ended")
				} else {
					_ = c.Close(websocket.StatusTryAgainLater, "too slow; reconnect")
				}
				return
			}
			if c.Write(ctx, websocket.MessageBinary, f) != nil {
				return
			}
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.Ping(pctx)
			pcancel()
			if err != nil {
				return
			}
		case <-readDone:
			return
		case <-httphost.Stopping(r):
			// The app is stopping: let go now rather than hold its shutdown up. The shells end
			// when the module stops.
			_ = c.Close(websocket.StatusGoingAway, "the app is stopping")
			return
		}
	}
}
