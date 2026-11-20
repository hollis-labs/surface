// Package drafts is the Drafts module: documents (prose, code, plans) the owner and the agent
// work on together. Every save is a revision, any two revisions can be compared, and either
// side can pin a mark (a comment, a question, a suggested change) to a passage; the marks are
// the core's, so they look and work the same as everywhere else in the app.
//
// The agent writes a draft only against the revision it read (expect_rev), so it never
// silently overwrites the owner's edits.
package drafts

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/collab"
	"github.com/hollis-labs/surface/module/tetherlink"
)

//go:embed migrations/*.sql
var migrations embed.FS

func init() { module.Register(&mod{}) }

type mod struct {
	module.Base
	st   backend
	h    module.Host
	link tetherlink.Caller // tests: Caster's stand-in; else from the settings
}

// ref is what a draft's marks are pinned to.
func ref(id string) string { return "drafts:" + id }

func (m *mod) Manifest() module.Manifest {
	return module.Manifest{
		ID: "drafts", Title: "Drafts", Version: "0.1.0",
		About: "Documents the owner and the agent work on together: revisions, comparisons and marks.",
		Views: []module.View{{
			ID: "drafts", Title: "Drafts", Icon: "file-pen", Width: 1100, Height: 760, Menu: true, Bare: true,
			Multi: true, DefaultKey: "drafts",
			Render:   module.Component("drafts/main"),
			Describe: m.describe,
		}},
		Commands: []module.Command{
			{ID: "drafts.open", Title: "Drafts: open a draft", Section: "Drafts", Hidden: true,
				Run: func(ctx context.Context, _ module.Host, args map[string]any) (any, error) {
					id, _ := args["id"].(string)
					d, err := m.st.get(ctx, id)
					if err != nil {
						return nil, err
					}
					return m.open(ctx, d)
				}},
			{ID: "drafts.new", Title: "Drafts: new draft", Section: "Drafts", Keywords: "write document new",
				Run: func(ctx context.Context, h module.Host, args map[string]any) (any, error) {
					title, _ := args["title"].(string)
					body, _ := args["body"].(string)
					lang, _ := args["lang"].(string)
					d, err := m.create(ctx, title, lang, body, "owner")
					if err != nil {
						return nil, err
					}
					return m.open(ctx, d)
				}},
			{ID: "drafts.from-selection", Title: "Drafts: start one from the selection in front", Section: "Drafts",
				Keywords: "review annotate collaborate selection",
				Run: func(ctx context.Context, h module.Host, _ map[string]any) (any, error) {
					c, ok := h.Desk().Context(ctx, "")
					if !ok || strings.TrimSpace(c.Selection) == "" {
						return h.Desk().Open(ctx, "drafts", module.OpenOptions{})
					}
					d, err := m.create(ctx, c.Title, langOf(c.Ref), c.Selection, "owner")
					if err != nil {
						return nil, err
					}
					return m.open(ctx, d)
				}},
		},
		Tools: m.tools(),
		Store: &module.Store{Migrations: migrations, Dir: "migrations"},
		Settings: []module.Setting{
			{Key: "default_lang", Title: "New drafts are written in", Type: module.SettingEnum, Default: "markdown",
				Options: []string{"markdown", "text", "go", "typescript", "python", "shell", "sql", "yaml", "json"}},
			casterSettings[0], casterSettings[1], casterSettings[2], casterSettings[3],
		},
		Slots: []module.Slot{{
			Name: "drafts", About: "writing and revising a document with the owner: prose, code or a plan, by revisions and marks",
			Views: []string{"drafts"},
			Tools: []string{"drafts_get", "drafts_edit", "drafts_write", "drafts_diff", "drafts_revisions", "marks_list", "marks_add", "marks_reply", "marks_resolve", "drafts_list", "drafts_create"},
			Where: "the draft in front (drafts_get, with its revisions and marks); other drafts (drafts_list)",
			Instructions: "Read the draft before changing it, and change it against the revision you read (expect_rev). " +
				"Prefer drafts_edit for a passage over rewriting the whole text. Answer the owner's open marks on the draft " +
				"(marks_reply, and marks_resolve once done); when you suggest rather than change, pin a mark to the passage. " +
				"Keep the owner's voice; say in a line what each revision changed.",
			Knowledge: m.knowledge,
		}},
	}
}

func langOf(ref string) string {
	ext := ref[strings.LastIndex(ref, ".")+1:]
	switch strings.ToLower(ext) {
	case "go":
		return "go"
	case "ts", "tsx":
		return "typescript"
	case "py":
		return "python"
	case "sh":
		return "shell"
	case "yaml", "yml":
		return "yaml"
	case "json":
		return "json"
	case "sql":
		return "sql"
	}
	return "markdown"
}

func (m *mod) Start(_ context.Context, h module.Host) error {
	m.h = h
	m.st = &router{m: m, local: &store{db: h.Store()}, cast: &casterStore{docs: &collab.Store{Link: m.tether}}}
	m.routes(h)
	return nil
}

func (m *mod) create(ctx context.Context, title, lang, body, author string) (Draft, error) {
	if lang == "" && m.h != nil {
		lang = m.h.Config().String("default_lang")
	}
	d, err := m.st.create(ctx, title, lang, body, author)
	if err == nil {
		m.h.Events().Publish("drafts.changed", map[string]any{"id": d.ID, "rev": d.Rev, "author": author, "created": true})
	}
	return d, err
}

func (m *mod) write(ctx context.Context, id, body, author, summary string, expect int, title *string) (Draft, error) {
	d, err := m.st.write(ctx, id, body, author, summary, expect, title)
	if err == nil {
		m.h.Events().Publish("drafts.changed", map[string]any{"id": d.ID, "rev": d.Rev, "author": author})
	}
	return d, err
}

func (m *mod) open(ctx context.Context, d Draft) (module.Window, error) {
	return m.h.Desk().Open(ctx, "drafts", module.OpenOptions{Key: d.ID, Title: d.Title, Args: map[string]any{"id": d.ID}})
}

// windowDraft is the draft a window shows, or "".
func windowDraft(w module.Window) string {
	id, _ := w.Args["id"].(string)
	return id
}

// errOwnerWorking refuses a delegate's edit to a draft the owner has unsaved changes or
// unsent input in: their text would be overwritten (docs/desk-rules.md, rule 7).
var errOwnerWorking = errors.New("the owner is editing this draft right now (unsaved changes in its window): wait until they save, or ask them; do not retry at once")

// ownerWorking reports that a delegate is acting while the owner works in a window showing
// the draft.
func (m *mod) ownerWorking(ctx context.Context, id string) bool {
	if !module.ActorOf(ctx).Delegate() {
		return false
	}
	for _, w := range m.h.Desk().Windows(ctx) {
		if w.View == "drafts" && windowDraft(w) == id && (w.Dirty || w.Composing) {
			return true
		}
	}
	return false
}

// focused is the draft in the window in front, else the one in the frontmost Drafts window.
func (m *mod) focused(ctx context.Context) string {
	if w, ok := m.h.Desk().Focused(ctx); ok && w.View == "drafts" {
		if id := windowDraft(w); id != "" {
			return id
		}
	}
	for _, w := range m.h.Desk().Windows(ctx) {
		if w.View == "drafts" {
			if id := windowDraft(w); id != "" {
				return id
			}
		}
	}
	return ""
}

func (m *mod) describe(ctx context.Context, _ module.Host, w module.Window) (module.WindowContext, error) {
	id := windowDraft(w)
	if id == "" {
		return module.WindowContext{Kind: "data", Ref: "drafts:list"}, nil
	}
	d, err := m.st.get(ctx, id)
	if errors.Is(err, errNotFound) {
		// The window outlived its draft: say what it was, without failing the context.
		return module.WindowContext{Kind: "document", Ref: ref(id)}, nil
	}
	if err != nil {
		return module.WindowContext{}, err
	}
	c := module.WindowContext{Kind: "document", Ref: ref(id), Title: d.Title,
		Extra: map[string]any{"draft": id, "rev": d.Rev, "lang": d.Lang, "lines": d.Lines}}
	if s, _ := w.State["selection"].(string); s != "" {
		c.Selection = s
		if a, ok := w.State["line_start"].(float64); ok {
			c.Extra["selection_lines"] = fmt.Sprintf("%d-%v", int(a), w.State["line_end"])
		}
	}
	if mode, _ := w.State["mode"].(string); mode != "" {
		c.Extra["mode"] = mode
	}
	return c, nil
}

// knowledge is what the agent is told about the draft in front.
func (m *mod) knowledge(ctx context.Context, _ module.Host, s module.Situation) (string, error) {
	id := ""
	if s.Focused != nil && s.Focused.Module == "drafts" {
		id, _ = s.Focused.Extra["draft"].(string)
	}
	if id == "" {
		list, err := m.st.list(ctx, "")
		if err != nil || len(list) == 0 {
			return "No drafts yet.", err
		}
		var b strings.Builder
		b.WriteString("Recent drafts:\n")
		for i, d := range list {
			if i == 8 {
				break
			}
			fmt.Fprintf(&b, "- %s %q (%s, rev %d, %d lines)\n", d.ID, d.Title, d.Lang, d.Rev, d.Lines)
		}
		return b.String(), nil
	}
	d, err := m.st.get(ctx, id)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Draft %s %q: %s, revision %d, %d lines.\n", d.ID, d.Title, d.Lang, d.Rev, d.Lines)
	marks, _ := m.h.Marks().List(ctx, ref(id))
	open := 0
	for _, mk := range marks {
		if mk.Status != "open" {
			continue
		}
		if open == 0 {
			b.WriteString("Open marks:\n")
		}
		open++
		if open <= 8 {
			fmt.Fprintf(&b, "- [%s] %s on %q: %s", mk.ID, mk.Author, clip(mk.Quote, 80), clip(mk.Body, 200))
			if n := len(mk.Replies); n > 0 {
				fmt.Fprintf(&b, " (%d replies, last by %s)", n, mk.Replies[n-1].Author)
			}
			b.WriteString("\n")
		}
	}
	if open == 0 {
		b.WriteString("No open marks.\n")
	}
	if len(d.Body) <= 5000 {
		b.WriteString("The text:\n" + numbered(d.Body, 0, 0))
	} else {
		b.WriteString("The text is long; read it with drafts_get. It begins:\n" + numbered(d.Body, 1, 40))
	}
	return b.String(), nil
}

// numbered returns lines from..to (1-based, 0 for all) with their numbers.
func numbered(body string, from, to int) string {
	ls := split(body)
	if from < 1 {
		from = 1
	}
	if to <= 0 || to > len(ls) {
		to = len(ls)
	}
	var b strings.Builder
	for i := from; i <= to; i++ {
		fmt.Fprintf(&b, "%4d  %s\n", i, ls[i-1])
	}
	return b.String()
}

func clip(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func (m *mod) routes(h module.Host) {
	handle := func(pattern string, fn func(r *http.Request) (any, error)) {
		h.Handle(pattern, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			v, err := fn(r)
			reply(w, v, err)
		}))
	}
	body := func(r *http.Request, v any) error {
		return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 8<<20)).Decode(v)
	}
	handle("GET /drafts", func(r *http.Request) (any, error) { return m.st.list(r.Context(), r.URL.Query().Get("q")) })
	handle("POST /drafts", func(r *http.Request) (any, error) {
		var in struct{ Title, Lang, Body string }
		if err := body(r, &in); err != nil {
			return nil, err
		}
		return m.create(r.Context(), in.Title, in.Lang, in.Body, "owner")
	})
	handle("GET /drafts/{id}", func(r *http.Request) (any, error) { return m.st.get(r.Context(), r.PathValue("id")) })
	handle("PUT /drafts/{id}", func(r *http.Request) (any, error) {
		var in struct {
			Body      string  `json:"body"`
			Title     *string `json:"title"`
			Lang      string  `json:"lang"`
			Summary   string  `json:"summary"`
			ExpectRev int     `json:"expect_rev"`
		}
		if err := body(r, &in); err != nil {
			return nil, err
		}
		if in.Lang != "" {
			if err := m.st.setLang(r.Context(), r.PathValue("id"), in.Lang); err != nil {
				return nil, err
			}
		}
		return m.write(r.Context(), r.PathValue("id"), in.Body, "owner", in.Summary, in.ExpectRev, in.Title)
	})
	handle("DELETE /drafts/{id}", func(r *http.Request) (any, error) {
		err := m.st.remove(r.Context(), r.PathValue("id"))
		if err == nil {
			h.Events().Publish("drafts.changed", map[string]any{"id": r.PathValue("id"), "removed": true})
		}
		return map[string]bool{"ok": err == nil}, err
	})
	handle("GET /drafts/{id}/revisions", func(r *http.Request) (any, error) { return m.st.revisions(r.Context(), r.PathValue("id")) })
	handle("GET /drafts/{id}/revisions/{rev}", func(r *http.Request) (any, error) {
		n, _ := strconv.Atoi(r.PathValue("rev"))
		return m.st.revision(r.Context(), r.PathValue("id"), n)
	})
	handle("GET /drafts/{id}/diff", func(r *http.Request) (any, error) {
		from, _ := strconv.Atoi(r.URL.Query().Get("from"))
		to, _ := strconv.Atoi(r.URL.Query().Get("to"))
		a, b, err := m.pair(r.Context(), r.PathValue("id"), from, to)
		if err != nil {
			return nil, err
		}
		return map[string]any{"from": a.Rev, "to": b.Rev, "lines": diffLines(a.Body, b.Body)}, nil
	})
}

// pair returns two revisions to compare: from (default the one before to) and to (default latest).
func (m *mod) pair(ctx context.Context, id string, from, to int) (Revision, Revision, error) {
	d, err := m.st.get(ctx, id)
	if err != nil {
		return Revision{}, Revision{}, err
	}
	if to <= 0 || to > d.Rev {
		to = d.Rev
	}
	if from <= 0 || from >= to {
		from = max(1, to-1)
	}
	a, err := m.st.revision(ctx, id, from)
	if err != nil {
		return a, Revision{}, err
	}
	b, err := m.st.revision(ctx, id, to)
	return a, b, err
}

func reply(w http.ResponseWriter, v any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		status := http.StatusUnprocessableEntity
		switch {
		case errors.Is(err, errNotFound):
			status = http.StatusNotFound
		case errors.Is(err, errStale):
			status = http.StatusConflict
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}
