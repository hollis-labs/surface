// Package frag is the Frag module: quick capture into a Fragments Engine (FE).
//
// A capture is queued in the module's own store first (so it is never lost and capturing
// works offline) and delivered behind it by a worker, as an fe.capture.v1 manifest, with
// retries, receipts and idempotency. The outbox shows where each capture is and lets the
// owner retry or discard one. It is a port of Basalisk's built-in Frag in personal mode:
// the owner's own key, read from a file at the moment of use.
//
// Delivery is off until the owner turns it on (frag.delivery) and sets the FE address
// (frag.fe_url) and the key file (frag.fe_token_file).
//
// What the module knows about the moment of a capture (the window the owner was in when the
// draft began and when it was saved) is kept with the queued row and never sent to FE.
package frag

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/hollis-labs/surface/module"
)

//go:embed migrations/*.sql
var migrations embed.FS

func init() { module.Register(New()) }

// New is the module. Its HTTP client is the default one; tests set Client.
func New() *Mod { return &Mod{} }

// Mod is the Frag module.
type Mod struct {
	// Client, when set before Start, is the HTTP client delivery uses.
	Client *http.Client
	// noLoop keeps the worker from running by itself (tests drive it with w.once).
	noLoop bool

	st   *store
	w    *worker
	h    module.Host
	wake chan struct{}
	stop context.CancelFunc
	done sync.WaitGroup
}

// Manifest says what the module brings.
func (m *Mod) Manifest() module.Manifest {
	return module.Manifest{
		ID: "frag", Title: "Frag", Version: "0.1.0",
		About: "Quick capture into a Fragments Engine, queued here first and delivered behind it.",
		Views: []module.View{
			{ID: "frag", Title: "Frag", Icon: "feather", Width: 560, Height: 360, Menu: true, Bare: true,
				Render: module.Component("frag/capture"),
				Describe: func(ctx context.Context, _ module.Host, _ module.Window) (module.WindowContext, error) {
					d, err := m.st.draft(ctx)
					return module.WindowContext{Kind: "data", Ref: "frag:draft", Extra: map[string]any{"drafting": !d.empty()}}, err
				}},
			{ID: "frag.outbox", Title: "Frag outbox", Icon: "send", Width: 640, Height: 560, Menu: true, Bare: true,
				Render: module.Component("frag/outbox"),
				Describe: func(ctx context.Context, _ module.Host, _ module.Window) (module.WindowContext, error) {
					n, err := m.st.counts(ctx)
					return module.WindowContext{Kind: "data", Ref: "frag:outbox", Extra: map[string]any{"counts": n}}, err
				}},
		},
		Commands: []module.Command{
			{ID: "frag.capture", Title: "Frag: capture to Fragments Engine", Section: "Frag", Keywords: "fragment note capture fe quick", Keys: "alt+shift+f",
				Run: func(ctx context.Context, h module.Host, args map[string]any) (any, error) {
					text, _ := args["text"].(string)
					if strings.TrimSpace(text) == "" {
						return h.Desk().Open(ctx, "frag", module.OpenOptions{Args: map[string]any{"compose": true}})
					}
					tags, _ := args["tags"].(string)
					c, _, err := m.capture(ctx, Payload{Content: text, Tags: ParseTags(tags)})
					return c, err
				}},
		},
		Tools: m.tools(),
		Refs:  m.refs(),
		Store: &module.Store{Migrations: migrations, Dir: "migrations"},
		Settings: []module.Setting{
			{Key: "delivery", Title: "Deliver captures to Fragments Engine", About: "Off: captures are queued here and wait. On: the worker sends them, once the address and key file are set.", Type: module.SettingBool, Default: false},
			{Key: "fe_url", Title: "Fragments Engine address", About: "The FE origin, like https://fragments.example.test.", Type: module.SettingString, Default: ""},
			{Key: "fe_token_file", Title: "Fragments Engine key file", About: "Path of a file holding the FE API key (personal mode). Read when a capture is sent; never shown or logged.", Type: module.SettingString, Default: ""},
			{Key: "preview", Title: "Show the capture rendered", About: "The capture box shows the text as markdown. What is sent is the text as typed.", Type: module.SettingBool, Default: false},
		},
		Slots: []module.Slot{{
			Name: "frag", About: "quick capture into the owner's Fragments Engine library, and the outbox of captures on their way there",
			Views: []string{"frag", "frag.outbox"},
			Tools: []string{"frag_capture", "frag_outbox", "frag_retry"},
			Where: "the capture queue (frag_outbox)",
			Instructions: "Capture the owner's words exactly as given; never rewrite them. A capture is queued here and delivered behind it, " +
				"so 'queued' is safe. A capture that needs attention waits for the owner: say why (its code) and retry only when they ask or the cause is fixed.",
			Knowledge: func(ctx context.Context, _ module.Host, _ module.Situation) (string, error) {
				n, err := m.st.counts(ctx)
				if err != nil {
					return "", err
				}
				_, _, why := m.w.ready()
				if why == "" {
					why = "delivery is on"
				}
				return fmt.Sprintf("Frag queue: %d queued, %d need attention, %d saved (%s).", n[stateQueued], n[stateAttention], n[stateSaved], why), nil
			},
		}},
	}
}

// Start opens the store, serves the routes and starts the delivery worker.
func (m *Mod) Start(_ context.Context, h module.Host) error {
	m.h = h
	m.st = &store{db: h.Store()}
	m.wake = make(chan struct{}, 1)
	m.w = &worker{st: m.st, cl: newClient(m.Client),
		cfg: func() (string, string, bool) {
			c := h.Config()
			return c.String("fe_url"), c.String("fe_token_file"), c.Bool("delivery")
		},
		changed: func(seq int64) { h.Events().Publish("frag.outbox", map[string]any{"sequence": seq}) },
	}
	if !m.noLoop {
		ctx, cancel := context.WithCancel(context.Background())
		m.stop = cancel
		m.done.Add(1)
		go func() {
			defer m.done.Done()
			m.w.run(ctx, m.wake)
		}()
	}
	for _, k := range []string{"delivery", "fe_url", "fe_token_file"} {
		h.Config().Watch(k, func(any) {
			m.kick()
			h.Events().Publish("frag.status", map[string]any{"setting": k})
		})
	}
	m.routes(h)
	return nil
}

// Stop ends the worker.
func (m *Mod) Stop(context.Context) error {
	if m.stop != nil {
		m.stop()
		m.done.Wait()
	}
	return nil
}

func (m *Mod) kick() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// ownerWindow is the owner's window now: the focused one, or, when the focus is in Frag's own
// window, the window that was in front when it opened.
func ownerWindow(ctx context.Context, h module.Host) *Window {
	w, ok := h.Desk().Focused(ctx)
	if !ok {
		return nil
	}
	if w.Module == "frag" {
		if w.Opener == "" {
			return nil
		}
		for _, o := range h.Desk().Windows(ctx) {
			if o.ID == w.Opener {
				w = o
				break
			}
		}
		if w.Module == "frag" {
			return nil
		}
	}
	out := &Window{View: w.View, Module: w.Module, Title: w.Title}
	if wc, ok := h.Desk().Context(ctx, w.ID); ok {
		out.Kind, out.Ref, out.URL = wc.Kind, wc.Ref, wc.URL
		if wc.Title != "" {
			out.Title = wc.Title
		}
	}
	return out
}

// capture queues one capture from the agent, a command or the API. Without an id it gets one
// made from its text and tags, so the same capture twice is queued once.
func (m *Mod) capture(ctx context.Context, p Payload) (Capture, bool, error) {
	if p.Tags == nil {
		p.Tags = []string{}
	}
	if p.CaptureID == "" {
		p.CaptureID = "frag-" + digest(p.Content + "\x00" + strings.Join(p.Tags, ","))[:24]
	}
	at := stamp()
	if p.SubmittedAt == "" {
		p.SubmittedAt = at
	}
	if p.DraftAt == "" {
		p.DraftAt = p.SubmittedAt
	}
	c, fresh, err := m.st.enqueue(ctx, p, &OwnerContext{Save: &Moment{Window: ownerWindow(ctx, m.h)}})
	if err == nil && fresh {
		m.h.Events().Publish("frag.outbox", map[string]any{"sequence": c.Sequence, "queued": true})
		m.kick()
	}
	return c, fresh, err
}

// editDraft keeps the box's text. A draft that begins notes the moment.
func (m *Mod) editDraft(ctx context.Context, content, tags string) (Draft, error) {
	if len(content) > maxContent*2 || len(tags) > maxTags*(maxTag+2) {
		return Draft{}, invalidf("the draft is too large")
	}
	d, err := m.st.draft(ctx)
	if err != nil {
		return d, err
	}
	if d.empty() {
		d.DraftAt = stamp()
		d.Context, _ = json.Marshal(Moment{Window: ownerWindow(ctx, m.h)})
	}
	d.Content, d.Tags = content, tags
	if d.empty() {
		err = m.st.clearDraft(ctx)
		d = Draft{}
	} else {
		err = m.st.saveDraft(ctx, d)
	}
	if err == nil {
		m.h.Events().Publish("frag.draft", map[string]any{})
	}
	return d, err
}

// commit queues the draft. The draft is done once the capture is queued; it is kept if not.
func (m *Mod) commit(ctx context.Context) (Capture, bool, error) {
	d, err := m.st.draft(ctx)
	if err != nil {
		return Capture{}, false, err
	}
	if strings.TrimSpace(d.Content) == "" {
		return Capture{}, false, invalidf("there is nothing to capture: the text is empty")
	}
	if d.DraftAt == "" {
		d.DraftAt = stamp()
	}
	oc := &OwnerContext{Save: &Moment{Window: ownerWindow(ctx, m.h)}}
	if len(d.Context) > 0 {
		var dm Moment
		if json.Unmarshal(d.Context, &dm) == nil && dm.Window != nil {
			oc.Draft = &dm
		}
	}
	try := func() (Capture, bool, error) {
		d = d.withID()
		// Kept first: if the answer is lost, the same text goes again under the same id.
		if e := m.st.saveDraft(ctx, d); e != nil {
			return Capture{}, false, e
		}
		return m.st.enqueue(ctx, Payload{CaptureID: d.CaptureID, Content: d.Content, Tags: ParseTags(d.Tags), DraftAt: d.DraftAt, SubmittedAt: stamp()}, oc)
	}
	c, fresh, err := try()
	if errors.Is(err, errIDTaken) {
		// The id was used for other text (a save whose answer was lost, then an edit): a new id.
		d.CaptureID = ""
		c, fresh, err = try()
	}
	if err != nil {
		return c, false, err
	}
	if err := m.st.clearDraft(ctx); err != nil {
		return c, fresh, err
	}
	m.h.Events().Publish("frag.draft", map[string]any{"committed": c.Sequence})
	m.h.Events().Publish("frag.outbox", map[string]any{"sequence": c.Sequence, "queued": fresh})
	m.kick()
	return c, fresh, nil
}

// Status is how captures are delivered now.
type Status struct {
	Delivery bool           `json:"delivery"`
	Ready    bool           `json:"ready"`
	Reason   string         `json:"reason,omitempty"`
	FEURL    string         `json:"fe_url,omitempty"`
	Counts   map[string]int `json:"counts"`
}

func (m *Mod) status(ctx context.Context) (Status, error) {
	fe, _, why := m.w.ready()
	n, err := m.st.counts(ctx)
	return Status{Delivery: m.h.Config().Bool("delivery"), Ready: why == "", Reason: why, FEURL: fe, Counts: n}, err
}

// Check is the answer of a read against FE.
type Check struct {
	OK            bool   `json:"ok"`
	FEURL         string `json:"fe_url,omitempty"`
	SchemaVersion string `json:"schema_version,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

// check is a read against FE (GET /v1/capabilities) with the configured address and key: it
// says whether both are accepted, and sends nothing. It works with delivery off.
func (m *Mod) check(ctx context.Context) Check {
	fe, bad := origin(m.h.Config().String("fe_url"))
	if bad != nil {
		return Check{Reason: bad.Error()}
	}
	tok, bad := readToken(m.h.Config().String("fe_token_file"))
	if bad != nil {
		return Check{FEURL: fe, Reason: "the key file could not be read (frag.fe_token_file)"}
	}
	v, bad := m.w.cl.capabilities(ctx, fe, tok)
	if bad != nil {
		return Check{FEURL: fe, Reason: bad.Error()}
	}
	return Check{OK: true, FEURL: fe, SchemaVersion: v}
}

func (m *Mod) routes(h module.Host) {
	handle := func(pattern string, fn func(r *http.Request) (int, any, error)) {
		h.Handle(pattern, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, 256*1024)
			code, v, err := fn(r)
			reply(w, code, v, err)
		}))
	}
	seq := func(r *http.Request) (int64, error) {
		n, err := strconv.ParseInt(r.PathValue("seq"), 10, 64)
		if err != nil || n < 1 {
			return 0, invalidf("not a capture number")
		}
		return n, nil
	}
	ok := func(v any, err error) (int, any, error) { return http.StatusOK, v, err }

	handle("GET /status", func(r *http.Request) (int, any, error) { return ok(m.status(r.Context())) })
	handle("POST /check", func(r *http.Request) (int, any, error) { return ok(m.check(r.Context()), nil) })
	handle("GET /draft", func(r *http.Request) (int, any, error) { return ok(m.st.draft(r.Context())) })
	handle("PUT /draft", func(r *http.Request) (int, any, error) {
		var in struct{ Content, Tags string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			return 0, nil, invalidf("the draft is not JSON")
		}
		return ok(m.editDraft(r.Context(), in.Content, in.Tags))
	})
	handle("DELETE /draft", func(r *http.Request) (int, any, error) {
		err := m.st.clearDraft(r.Context())
		if err == nil {
			h.Events().Publish("frag.draft", map[string]any{})
		}
		return ok(map[string]bool{"ok": err == nil}, err)
	})
	handle("POST /draft/commit", func(r *http.Request) (int, any, error) {
		c, fresh, err := m.commit(r.Context())
		if fresh {
			return http.StatusAccepted, c, err
		}
		return ok(c, err)
	})
	handle("POST /captures", func(r *http.Request) (int, any, error) {
		var in struct {
			CaptureID string   `json:"capture_id"`
			Content   string   `json:"content"`
			Tags      []string `json:"tags"`
			DraftAt   string   `json:"draft_at"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			return 0, nil, invalidf("the capture is not JSON")
		}
		c, fresh, err := m.capture(r.Context(), Payload{CaptureID: in.CaptureID, Content: in.Content, Tags: in.Tags, DraftAt: in.DraftAt})
		if fresh {
			return http.StatusAccepted, c, err
		}
		return ok(c, err)
	})
	handle("GET /outbox", func(r *http.Request) (int, any, error) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		return ok(m.st.list(r.Context(), q.Get("state"), limit))
	})
	handle("GET /outbox/{seq}", func(r *http.Request) (int, any, error) {
		n, err := seq(r)
		if err != nil {
			return 0, nil, err
		}
		return ok(m.st.get(r.Context(), n))
	})
	handle("POST /outbox/{seq}/retry", func(r *http.Request) (int, any, error) {
		n, err := seq(r)
		if err == nil {
			err = m.retry(r.Context(), n)
		}
		return ok(map[string]bool{"ok": err == nil}, err)
	})
	handle("DELETE /outbox/{seq}", func(r *http.Request) (int, any, error) {
		n, err := seq(r)
		if err == nil {
			err = m.st.discard(r.Context(), n)
		}
		if err == nil {
			h.Events().Publish("frag.outbox", map[string]any{"sequence": n, "discarded": true})
		}
		return ok(map[string]bool{"ok": err == nil}, err)
	})
}

func (m *Mod) retry(ctx context.Context, seq int64) error {
	err := m.st.retry(ctx, seq)
	if err == nil {
		m.h.Events().Publish("frag.outbox", map[string]any{"sequence": seq, "retry": true})
		m.kick()
	}
	return err
}

func reply(w http.ResponseWriter, code int, v any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		switch {
		case errors.Is(err, errInvalid):
			code = http.StatusBadRequest
		case errors.Is(err, errNotFound):
			code = http.StatusNotFound
		case errors.Is(err, errConflict):
			code = http.StatusConflict
		default:
			code = http.StatusInternalServerError
		}
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if code != 0 && code != http.StatusOK {
		w.WriteHeader(code)
	}
	_ = json.NewEncoder(w).Encode(v)
}

func clip(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

func (m *Mod) tools() []module.Tool {
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	return []module.Tool{
		{Name: "frag_capture", Level: module.LevelAct,
			Description: "Queue a note for the owner's Fragments Engine library, word for word, with optional tags. It is kept here and delivered behind it (when delivery is on), so this only queues; " +
				"the same text and tags twice are queued once. Use it when the owner asks to capture, frag or save something to Fragments.",
			Input: raw(`{"type":"object","required":["text"],"properties":{"text":{"type":"string","description":"The note, exactly as it should be kept (markdown or plain)."},"tags":{"type":"array","items":{"type":"string"},"description":"Tags, at most 32."}}}`),
			Describe: func(in json.RawMessage) string {
				var a struct{ Text string }
				_ = json.Unmarshal(in, &a)
				return "Queue for Fragments Engine: “" + clip(a.Text, 80) + "”"
			},
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					Text string   `json:"text"`
					Tags []string `json:"tags"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				c, _, err := m.capture(ctx, Payload{Content: a.Text, Tags: a.Tags})
				return c, err
			}},
		{Name: "frag_outbox", Level: module.LevelRead,
			Description: "List the Frag outbox, newest first: each capture's state (queued, needs_attention, saved, discarded), its code (why it waits), attempts and, once saved, the fragment id. " +
				"With id, one capture. Also says whether delivery is on and, if not, why.",
			Input: raw(`{"type":"object","properties":{"id":{"type":"integer","description":"One capture's sequence number."},"state":{"type":"string","enum":["queued","needs_attention","saved","discarded"]},"limit":{"type":"integer"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					ID    int64  `json:"id"`
					State string `json:"state"`
					Limit int    `json:"limit"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				if a.ID > 0 {
					return m.st.get(ctx, a.ID)
				}
				st, err := m.status(ctx)
				if err != nil {
					return nil, err
				}
				items, err := m.st.list(ctx, a.State, a.Limit)
				return map[string]any{"status": st, "items": items}, err
			}},
		{Name: "frag_retry", Level: module.LevelAct,
			Description: "Send a waiting capture again now: one that needs attention (after its cause is fixed) or one backing off. It is refused for a capture being delivered, saved or discarded.",
			Input:       raw(`{"type":"object","required":["id"],"properties":{"id":{"type":"integer","description":"The capture's sequence number."}}}`),
			Describe: func(in json.RawMessage) string {
				var a struct{ ID int64 }
				_ = json.Unmarshal(in, &a)
				return fmt.Sprintf("Retry Frag capture %d", a.ID)
			},
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				var a struct {
					ID int64 `json:"id"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, err
				}
				if err := m.retry(ctx, a.ID); err != nil {
					return nil, err
				}
				return m.st.get(ctx, a.ID)
			}},
	}
}
