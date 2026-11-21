// Package api is the core's HTTP surface: what the page draws from and what an outside
// client (a CLI, a script, another agent) drives the app through.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hollis-labs/substrate/modules/httphost"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/hollis-labs/surface/core/internal/cards"
	"github.com/hollis-labs/surface/core/internal/desk"
	"github.com/hollis-labs/surface/core/internal/kernel"
	"github.com/hollis-labs/surface/module"
)

// Mounter is a core service with routes of its own (the agent).
type Mounter interface {
	Mount(mux *http.ServeMux)
}

// Handler serves the API, the modules' routes and, for everything else, the built page.
func Handler(a *kernel.App, assets fs.FS, services ...Mounter) http.Handler {
	mux := http.NewServeMux()
	s := &server{a: a}

	mux.HandleFunc("GET /api/app", s.app)
	mux.HandleFunc("GET /api/desk", s.deskState)
	mux.HandleFunc("POST /api/desk/actions", s.deskAction)
	mux.HandleFunc("GET /api/context", s.context)
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("POST /api/events", s.publish)

	mux.HandleFunc("GET /api/palette/providers", s.paletteProviders)
	mux.HandleFunc("GET /api/palette/search", s.paletteSearch)
	mux.HandleFunc("GET /api/commands", s.commands)
	mux.HandleFunc("POST /api/commands/{id}", s.runCommand)

	mux.HandleFunc("GET /api/settings", s.settings)
	mux.HandleFunc("PUT /api/settings/{key}", s.setSetting)
	mux.HandleFunc("DELETE /api/settings/{key}", s.resetSetting)

	mux.HandleFunc("GET /api/marks", s.marks)
	mux.HandleFunc("POST /api/marks", s.addMark)
	mux.HandleFunc("POST /api/marks/{id}/replies", s.replyMark)
	mux.HandleFunc("POST /api/marks/{id}/resolve", s.resolveMark)

	mux.HandleFunc("GET /api/cards", s.cards)
	mux.HandleFunc("POST /api/cards", s.showCard)
	mux.HandleFunc("GET /api/cards/{id}", s.card)
	mux.HandleFunc("POST /api/cards/{id}/response", s.respondCard)
	mux.HandleFunc("POST /api/cards/{id}/hold", s.holdCard)

	mux.HandleFunc("GET /api/layouts", s.layouts)
	mux.HandleFunc("POST /api/layouts", s.saveLayout)
	mux.HandleFunc("PUT /api/layouts/{name}", s.putLayout)
	mux.HandleFunc("POST /api/layouts/{name}/load", s.loadLayout)
	mux.HandleFunc("DELETE /api/layouts/{name}", s.deleteLayout)

	mux.HandleFunc("GET /api/cite", s.citeKinds)
	mux.HandleFunc("GET /api/cite/resolve", s.citeResolve)
	mux.HandleFunc("GET /api/cite/record", s.citeRecord)

	mux.HandleFunc("GET /api/tools", s.tools)
	mux.HandleFunc("POST /api/tools/{name}", s.callTool)
	mux.HandleFunc("POST /mcp", s.mcp)

	mux.Handle("/api/m/", a.ModuleRoutes())
	for _, svc := range services {
		svc.Mount(mux)
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		fail(w, http.StatusNotFound, fmt.Errorf("no route %s %s", r.Method, r.URL.Path))
	})
	mux.Handle("/", spa(assets))
	return guard(mux)
}

type server struct{ a *kernel.App }

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func ok(w http.ResponseWriter, v any) { write(w, http.StatusOK, v) }

func fail(w http.ResponseWriter, status int, err error) {
	write(w, status, map[string]string{"error": err.Error()})
}

func read(r *http.Request, v any) error {
	if r.Body == nil {
		return nil
	}
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 8<<20))
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("bad request body: %w", err)
	}
	return nil
}

// guard keeps the app to its own page: a browser on another site cannot drive it. Requests
// with no Origin (a CLI, an agent) pass; a page must be the app's own. And the app answers only
// to a name that cannot be rebound (allowedHost): a page whose own name is made to point at this
// machine (DNS rebinding) passes an origin check, since its origin and host agree.
func guard(next http.Handler) http.Handler {
	extra := allowedHostsFromEnv()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowedHost(r.Host, extra) {
			fail(w, http.StatusMisdirectedRequest, fmt.Errorf("host %q is not this app's: reach it at 127.0.0.1 or localhost, or add the name to SURFACE_ALLOWED_HOSTS", r.Host))
			return
		}
		o := r.Header.Get("Origin")
		if o != "" && r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !sameHost(o, r.Host) && !devOrigin(o) {
				fail(w, http.StatusForbidden, errors.New("cross-origin request refused"))
				return
			}
		}
		// Who is asking (docs/desk-rules.md). The app's own page is the owner. Anything without
		// an Origin (MCP, the HTTP tools, the CLI) is a client from outside, held to the agent's
		// etiquette on the desk.
		actor := module.ActorOutside
		if o != "" {
			actor = module.ActorOwner
		}
		next.ServeHTTP(w, r.WithContext(module.WithActor(r.Context(), actor)))
	})
}

// allowedHost says whether a request's Host names this machine in a way a web page cannot
// redirect: an IP address (rebinding needs a name), localhost or a name under .localhost, or a
// name the operator listed (SURFACE_ALLOWED_HOSTS, comma separated). An empty Host (HTTP/1.0, a
// test) passes.
func allowedHost(hostport string, extra map[string]bool) bool {
	if hostport == "" {
		return true
	}
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
	if net.ParseIP(host) != nil || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	return extra[host]
}

func allowedHostsFromEnv() map[string]bool {
	out := map[string]bool{}
	for _, h := range strings.Split(os.Getenv("SURFACE_ALLOWED_HOSTS"), ",") {
		h = strings.TrimSpace(h)
		if name, _, err := net.SplitHostPort(h); err == nil {
			h = name // a port given with the name is not part of what is compared
		}
		if h = strings.ToLower(strings.TrimSuffix(strings.Trim(h, "[]"), ".")); h != "" {
			out[h] = true
		}
	}
	return out
}

func sameHost(origin, host string) bool {
	_, rest, found := strings.Cut(origin, "://")
	return found && rest == host
}

// devOrigin allows the Vite dev server on loopback.
func devOrigin(origin string) bool {
	return strings.HasPrefix(origin, "http://127.0.0.1:") || strings.HasPrefix(origin, "http://localhost:")
}

func (s *server) app(w http.ResponseWriter, _ *http.Request) {
	type view struct {
		module.View
		Module string `json:"module"`
	}
	views := []view{}
	for _, v := range s.a.WM().Views() {
		views = append(views, view{View: v.View, Module: v.Module})
	}
	ok(w, map[string]any{
		"name": s.a.AppName(), "title": s.a.Title(),
		"modules": s.a.Modules(), "views": views, "commands": s.commandList(),
	})
}

type commandOut struct {
	module.Command
	Module string `json:"module"`
	// Server is true when the server runs it; false means the page does.
	Server bool `json:"server"`
}

func (s *server) commandList() []commandOut {
	out := []commandOut{}
	for _, c := range s.a.CommandInfos() {
		out = append(out, commandOut{Command: c.Command, Module: c.Module, Server: c.Run != nil})
	}
	return out
}

func (s *server) commands(w http.ResponseWriter, _ *http.Request) { ok(w, s.commandList()) }

func (s *server) runCommand(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Args map[string]any `json:"args"`
	}
	if err := read(r, &in); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	out, err := s.a.RunCommand(r.Context(), r.PathValue("id"), in.Args)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, err)
		return
	}
	ok(w, map[string]any{"result": out})
}

func (s *server) deskState(w http.ResponseWriter, _ *http.Request) { ok(w, s.a.WM().Snapshot()) }

func (s *server) deskAction(w http.ResponseWriter, r *http.Request) {
	var a desk.Action
	if err := read(r, &a); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	win, err := s.a.WM().Apply(r.Context(), a)
	if errors.Is(err, desk.ErrDirty) {
		write(w, http.StatusConflict, map[string]any{"error": err.Error(), "dirty": true, "window": win})
		return
	}
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, err)
		return
	}
	ok(w, map[string]any{"window": win})
}

func (s *server) context(w http.ResponseWriter, r *http.Request) {
	wc, found := s.a.Desk().Context(r.Context(), r.URL.Query().Get("window"))
	if !found {
		ok(w, map[string]any{"context": nil, "situation": s.a.Situation(r.Context())})
		return
	}
	ok(w, map[string]any{"context": wc, "situation": s.a.Situation(r.Context())})
}

// events streams the bus to the page as server-sent events. The desk's state is sent first,
// so a page that connects (or reconnects) starts from the truth.
func (s *server) events(w http.ResponseWriter, r *http.Request) {
	fl, can := w.(http.Flusher)
	if !can {
		fail(w, http.StatusInternalServerError, errors.New("streaming is not supported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	ch := make(chan module.Event, 256)
	stop := s.a.Bus().Subscribe("", func(ev module.Event) {
		select {
		case ch <- ev:
		default: // A page that cannot keep up misses events; it re-reads state on reconnect.
		}
	})
	defer stop()
	send := func(topic string, data []byte) {
		fmt.Fprintf(w, "event: surface\ndata: {\"topic\":%q,\"data\":%s}\n\n", topic, data)
		fl.Flush()
	}
	first, _ := json.Marshal(s.a.WM().Snapshot())
	send("desk.changed", first)
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-httphost.Stopping(r):
			return
		case <-r.Context().Done():
			return
		case ev := <-ch:
			data := []byte(ev.Data)
			if len(data) == 0 {
				data = []byte("null")
			}
			send(ev.Topic, data)
		case <-tick.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			fl.Flush()
		}
	}
}

// publish lets the page put an event on the bus (page.* topics only): the desk came up, a
// view wants the others to know something.
func (s *server) publish(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Topic string          `json:"topic"`
		Data  json.RawMessage `json:"data"`
	}
	if err := read(r, &in); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if !strings.HasPrefix(in.Topic, "page.") {
		fail(w, http.StatusBadRequest, errors.New("the page publishes page.* topics only"))
		return
	}
	s.a.Bus().Publish(in.Topic, in.Data)
	ok(w, map[string]bool{"ok": true})
}

func (s *server) settings(w http.ResponseWriter, _ *http.Request) { ok(w, s.a.Settings().Entries()) }

func (s *server) setSetting(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Value any `json:"value"`
	}
	if err := read(r, &in); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := s.a.Settings().Set(r.Context(), r.PathValue("key"), in.Value); err != nil {
		fail(w, http.StatusUnprocessableEntity, err)
		return
	}
	ok(w, s.a.Settings().Entries())
}

func (s *server) resetSetting(w http.ResponseWriter, r *http.Request) {
	if err := s.a.Settings().Reset(r.Context(), r.PathValue("key")); err != nil {
		fail(w, http.StatusUnprocessableEntity, err)
		return
	}
	ok(w, s.a.Settings().Entries())
}

func (s *server) marks(w http.ResponseWriter, r *http.Request) {
	out, err := s.a.Marks().List(r.Context(), r.URL.Query().Get("ref"))
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	ok(w, out)
}

func (s *server) addMark(w http.ResponseWriter, r *http.Request) {
	var m module.Mark
	if err := read(r, &m); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	m.Author = "owner"
	out, err := s.a.Marks().Add(r.Context(), m)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, err)
		return
	}
	ok(w, out)
}

func (s *server) replyMark(w http.ResponseWriter, r *http.Request) {
	var in module.MarkReply
	if err := read(r, &in); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	in.Author = "owner"
	out, err := s.a.Marks().Reply(r.Context(), r.PathValue("id"), in)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, err)
		return
	}
	ok(w, out)
}

func (s *server) resolveMark(w http.ResponseWriter, r *http.Request) {
	if err := s.a.Marks().Resolve(r.Context(), r.PathValue("id")); err != nil {
		fail(w, http.StatusUnprocessableEntity, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}

// cards lists the open cards; with ?all=1 the closed ones as well.
// cards lists the open cards; ?all=1 the recent closed ones too; ?late=1 the open ones and
// the agent's questions that still take a late answer.
func (s *server) cards(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, err := s.a.Cards().List(r.Context(), q.Get("all") != "")
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	if late, can := s.a.Cards().(interface {
		LateOpen(ctx context.Context) ([]module.Card, error)
	}); q.Get("late") != "" && q.Get("all") == "" && can {
		more, err := late.LateOpen(r.Context())
		if err != nil {
			fail(w, http.StatusInternalServerError, err)
			return
		}
		out = append(out, more...)
	}
	ok(w, out)
}

// showCard lets an outside client (a script, another agent) put a card in front of the user.
func (s *server) showCard(w http.ResponseWriter, r *http.Request) {
	var c module.Card
	if err := read(r, &c); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if c.Source == "" {
		c.Source = "api"
	}
	out, err := s.a.Cards().Show(r.Context(), c)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, err)
		return
	}
	ok(w, out)
}

func (s *server) card(w http.ResponseWriter, r *http.Request) {
	c, found, err := s.a.Cards().Get(r.Context(), r.PathValue("id"))
	switch {
	case err != nil:
		fail(w, http.StatusInternalServerError, err)
	case !found:
		fail(w, http.StatusNotFound, cards.ErrNotFound)
	default:
		ok(w, c)
	}
}

// respondCard records the user's answer. A card that is no longer open answers 409 with
// the card as it stands, so the page can show what happened instead.
func (s *server) respondCard(w http.ResponseWriter, r *http.Request) {
	var o module.CardOutcome
	if err := read(r, &o); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	c, err := s.a.Cards().Respond(r.Context(), r.PathValue("id"), o)
	switch {
	case errors.Is(err, cards.ErrClosed):
		write(w, http.StatusConflict, map[string]any{"error": err.Error(), "card": c})
	case errors.Is(err, cards.ErrNotFound):
		fail(w, http.StatusNotFound, err)
	case err != nil:
		fail(w, http.StatusUnprocessableEntity, err)
	default:
		ok(w, c)
	}
}

// holdCard releases a blocking agent question from waiting in-turn so the owner can chat
// with the agent or inspect the desk and answer it in the Cards window.
func (s *server) holdCard(w http.ResponseWriter, r *http.Request) {
	c, err := s.a.Cards().End(r.Context(), r.PathValue("id"), cards.Abandoned)
	switch {
	case errors.Is(err, cards.ErrNotFound):
		fail(w, http.StatusNotFound, err)
	case err != nil:
		fail(w, http.StatusUnprocessableEntity, err)
	default:
		ok(w, c)
	}
}

func (s *server) layouts(w http.ResponseWriter, r *http.Request) {
	out, err := s.a.Desk().Layouts(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	ok(w, out)
}

func (s *server) saveLayout(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	if err := read(r, &in); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	out, err := s.a.RunCommand(r.Context(), "layout.save", in)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, err)
		return
	}
	name, _ := in["name"].(string)
	_, _ = s.a.WM().Apply(r.Context(), desk.Action{Type: "active", Layout: strings.TrimSpace(name)})
	ok(w, out)
}

func (s *server) putLayout(w http.ResponseWriter, r *http.Request) {
	var l module.Layout
	if err := read(r, &l); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	l.Name = r.PathValue("name")
	if err := s.a.Desk().SaveLayout(r.Context(), l); err != nil {
		fail(w, http.StatusUnprocessableEntity, err)
		return
	}
	ok(w, l)
}

func (s *server) loadLayout(w http.ResponseWriter, r *http.Request) {
	out, err := s.a.RunCommand(r.Context(), "layout.load", map[string]any{"name": r.PathValue("name")})
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, err)
		return
	}
	ok(w, out)
}

func (s *server) deleteLayout(w http.ResponseWriter, r *http.Request) {
	if err := s.a.Desk().DeleteLayout(r.Context(), r.PathValue("name")); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}

type toolOut struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	Level       module.Level    `json:"level"`
	Module      string          `json:"module"`
}

func (s *server) tools(w http.ResponseWriter, _ *http.Request) {
	out := []toolOut{}
	for _, t := range s.a.Tools() {
		out = append(out, toolOut{Name: t.Name, Description: t.Description, InputSchema: t.Input, Level: t.Level, Module: t.Module})
	}
	ok(w, out)
}

// call runs a tool by name and renders its result as the text a model is told.
func (s *server) call(ctx context.Context, name string, input json.RawMessage) (string, error) {
	for _, t := range s.a.Tools() {
		if t.Name != name {
			continue
		}
		out, err := t.Call(ctx, input)
		if err != nil {
			return "", err
		}
		return Render(out), nil
	}
	return "", fmt.Errorf("no tool %q", name)
}

// Render is a tool result as text: a string as is, anything else as JSON.
func Render(out any) string {
	switch v := out.(type) {
	case nil:
		return "done"
	case string:
		return v
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return fmt.Sprint(out)
	}
	return string(raw)
}

func (s *server) callTool(w http.ResponseWriter, r *http.Request) {
	var in json.RawMessage
	if err := read(r, &in); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	out, err := s.call(r.Context(), r.PathValue("name"), in)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, err)
		return
	}
	ok(w, map[string]string{"result": out})
}
