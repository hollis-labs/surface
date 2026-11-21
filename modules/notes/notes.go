// Package notes is the Notes module: Markdown files in a folder, written by the owner and
// the agent, linked with [[wiki links]] and marked up with the core's marks.
//
// The notes are plain files, so anything else that edits Markdown can share the folder.
// Which folder is a live setting: point it somewhere else and the list follows at once.
package notes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hollis-labs/surface/module"
)

func init() { module.Register(&mod{}) }

type mod struct {
	module.Base
	st *store
	h  module.Host
}

func (m *mod) Manifest() module.Manifest {
	return module.Manifest{
		ID: "notes", Title: "Notes", Version: "0.1.0",
		About: "Markdown notes in a folder, with wiki links, search and marks.",
		Views: []module.View{{
			ID: "notes", Title: "Notes", Icon: "notebook-pen", Width: 980, Height: 680,
			Menu: true, Multi: true, DefaultKey: "notes", Bare: true,
			Render: module.Component("notes/main"), Opens: []string{"*.md"},
			Describe: m.describe,
		}},
		Commands: []module.Command{
			{ID: "notes.new", Title: "Notes: new note", Section: "Notes", Keywords: "create write markdown", Keys: "alt+shift+n",
				Run: func(ctx context.Context, h module.Host, args map[string]any) (any, error) {
					title, _ := args["title"].(string)
					if strings.TrimSpace(title) == "" {
						title = "Untitled " + time.Now().Format("2006-01-02 1504")
					}
					text, _ := args["text"].(string)
					if text == "" {
						text = "# " + title + "\n\n"
					}
					n, err := m.st.write(title, text, "", true)
					if err != nil {
						return nil, err
					}
					m.changed(n.Path, "created")
					return m.open(ctx, n.Path)
				}},
			{ID: "notes.today", Title: "Notes: today's note", Section: "Notes", Keywords: "daily journal",
				Run: func(ctx context.Context, h module.Host, _ map[string]any) (any, error) {
					day := time.Now().Format("2006-01-02")
					p := day
					if dir := strings.Trim(h.Config().String("daily_folder"), "/ "); dir != "" {
						p = dir + "/" + day
					}
					if _, err := m.st.read(p); errors.Is(err, errNotFound) {
						if _, err := m.st.write(p, "# "+day+"\n\n", "", true); err != nil {
							return nil, err
						}
						m.changed(p, "created")
					}
					return m.open(ctx, p)
				}},
			{ID: "notes.open", Title: "Notes: open a note", Section: "Notes", Hidden: true,
				Run: func(ctx context.Context, _ module.Host, args map[string]any) (any, error) {
					p, _ := args["path"].(string)
					if _, err := m.st.read(p); err != nil {
						return nil, err
					}
					return m.open(ctx, p)
				}},
		},
		Palette: []module.PaletteProvider{{ID: "search", Title: "Notes", Mode: "commands", Search: m.palette}},
		Tools:   m.tools(),
		Settings: []module.Setting{
			{Key: "folder", Title: "Notes folder", About: "Absolute path of the folder the notes live in. Empty keeps them in the app's own data.", Type: module.SettingString, Default: ""},
			{Key: "daily_folder", Title: "Daily notes go in", About: "Folder, inside the notes folder, for the note of the day.", Type: module.SettingString, Default: "daily"},
			{Key: "layout", Title: "A note opens in", About: "preview: the note as read, with Edit and Split to write it (as Basalisk's Notes); edit: the Markdown alone; split: both. A new note opens for writing.", Type: module.SettingEnum, Default: "preview", Options: []string{"preview", "edit", "split"}},
		},
		Slots: []module.Slot{{
			Name: "notes", About: "the owner's notes: what they wrote down and want kept, found or built on",
			Views:        []string{"notes"},
			Tools:        []string{"notes_read", "notes_list", "notes_search", "notes_edit", "notes_write", "marks_list", "marks_add"},
			Where:        "the notes folder (notes_list, notes_search); a note's own marks (marks_list with its ref)",
			Instructions: "The notes are the owner's own words. Change a note only when asked, and prefer notes_edit (one passage) over rewriting the whole note. To comment on a passage instead of changing it, pin a mark to it with marks_add (ref \"notes:<path>\", quote the passage). Link related notes with [[name]].",
			Knowledge:    m.knowledge,
		}},
	}
}

// root is the notes folder as the setting has it now.
func (m *mod) root() (string, error) {
	dir := strings.TrimSpace(m.h.Config().String("folder"))
	if dir == "" {
		dir = filepath.Join(m.h.DataDir(), "notes")
	} else if strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, dir[2:])
	}
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("the notes folder %q must be an absolute path (Settings, Notes folder)", dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("the notes folder %q cannot be used: %w", dir, err)
	}
	return dir, nil
}

func (m *mod) changed(path, what string) {
	m.h.Events().Publish("notes.changed", map[string]any{"path": path, "what": what})
}

func (m *mod) open(ctx context.Context, path string) (module.Window, error) {
	rel, err := clean(path)
	if err != nil {
		return module.Window{}, err
	}
	return m.h.Desk().Open(ctx, "notes", module.OpenOptions{Args: map[string]any{"path": rel}})
}

// current is the note a window shows: what the page reported, or what it was opened with.
func current(w module.Window) string {
	if p, _ := w.State["path"].(string); p != "" {
		return p
	}
	p, _ := w.Args["path"].(string)
	return p
}

// errOwnerWorking refuses a delegate's write to a note the owner has unsaved changes or
// unsent input in: their text would be overwritten (docs/desk-rules.md, rule 7).
var errOwnerWorking = errors.New("the owner is editing this note right now (unsaved changes in its window): wait until they save, or ask them; do not retry at once")

// ownerWorking reports that a delegate is acting while the owner works in a window showing
// the note at path (with or without its .md).
func (m *mod) ownerWorking(ctx context.Context, path string) bool {
	if !module.ActorOf(ctx).Delegate() {
		return false
	}
	want := strings.TrimSuffix(strings.TrimPrefix(path, "/"), ".md")
	for _, w := range m.h.Desk().Windows(ctx) {
		if w.View == "notes" && strings.TrimSuffix(strings.TrimPrefix(current(w), "/"), ".md") == want && (w.Dirty || w.Composing) {
			return true
		}
	}
	return false
}

// front is the note in the focused Notes window, or "".
func (m *mod) front(ctx context.Context) string {
	if w, ok := m.h.Desk().Focused(ctx); ok && w.View == "notes" {
		return current(w)
	}
	for _, w := range m.h.Desk().Windows(ctx) {
		if w.View == "notes" {
			return current(w)
		}
	}
	return ""
}

func (m *mod) describe(_ context.Context, _ module.Host, w module.Window) (module.WindowContext, error) {
	p := current(w)
	if p == "" {
		return module.WindowContext{Kind: "workspace", Ref: "notes:"}, nil
	}
	wc := module.WindowContext{Kind: "document", Ref: "notes:" + p, Extra: map[string]any{"path": p}}
	if n, err := m.st.read(p); err == nil {
		wc.Extra["title"] = n.Title
		wc.Extra["rev"] = n.Rev
		wc.Extra["words"] = len(strings.Fields(n.Text))
	}
	return wc, nil
}

// knowledge is what the agent should know with Notes in front: the note's outline, what
// links to it, and how many marks are open on it. No text: it reads that with notes_read.
func (m *mod) knowledge(ctx context.Context, h module.Host, s module.Situation) (string, error) {
	p := ""
	if s.Focused != nil && s.Focused.View == "notes" {
		p = strings.TrimPrefix(s.Focused.Ref, "notes:")
	}
	if p == "" {
		notes, err := m.st.list()
		if err != nil || len(notes) == 0 {
			return "", err
		}
		var b strings.Builder
		b.WriteString("No note is open. The most recently changed notes:\n")
		for i, n := range notes {
			if i == 12 {
				break
			}
			fmt.Fprintf(&b, "- %s (%s)\n", n.Path, n.Title)
		}
		return b.String(), nil
	}
	n, err := m.st.read(p)
	if err != nil {
		// The note in front is new or gone; there is nothing to add.
		return "", nil //nolint:nilerr // a note that cannot be read adds no context; that is not a failure
	}
	var b strings.Builder
	fmt.Fprintf(&b, "The note in front is %q (path %s, rev %s, %d words).\n", n.Title, n.Path, n.Rev, len(strings.Fields(n.Text)))
	if heads := outline(n.Text); len(heads) > 0 {
		if len(heads) > 30 {
			heads = heads[:30]
		}
		b.WriteString("Its outline:\n  " + strings.Join(heads, "\n  ") + "\n")
	}
	if back, err := m.st.backlinks(p); err == nil && len(back) > 0 {
		var names []string
		for i, bn := range back {
			if i == 10 {
				break
			}
			names = append(names, bn.Path)
		}
		b.WriteString("Linked from: " + strings.Join(names, ", ") + "\n")
	}
	if marks, err := h.Marks().List(ctx, "notes:"+n.Path); err == nil {
		open := 0
		for _, mk := range marks {
			if mk.Status == "open" {
				open++
			}
		}
		if open > 0 {
			fmt.Fprintf(&b, "It has %d open mark(s): read them with marks_list (ref %q).\n", open, "notes:"+n.Path)
		}
	}
	return b.String(), nil
}

func (m *mod) Start(_ context.Context, h module.Host) error {
	m.h = h
	m.st = &store{root: m.root}

	h.Handle("GET /notes", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		notes, err := m.st.list()
		root, _ := m.root()
		reply(w, map[string]any{"notes": notes, "folder": root}, err)
	}))
	h.Handle("GET /note", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, err := m.st.read(r.URL.Query().Get("path"))
		if err != nil {
			reply(w, nil, err)
			return
		}
		back, _ := m.st.backlinks(n.Path)
		all, _ := m.st.list()
		// Which of the note's [[links]] lead somewhere, so the page can draw the rest as new.
		resolved := map[string]string{}
		for _, name := range links(n.Text) {
			if t, ok := resolve(all, name); ok {
				resolved[name] = t.Path
			}
		}
		reply(w, map[string]any{"note": n, "backlinks": back, "links": resolved}, nil)
	}))
	h.Handle("PUT /note", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Path, Text, Rev string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			reply(w, nil, err)
			return
		}
		n, err := m.st.write(in.Path, in.Text, in.Rev, false)
		if err == nil {
			m.changed(n.Path, "saved")
		}
		reply(w, n, err)
	}))
	h.Handle("POST /notes", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Path, Text string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			reply(w, nil, err)
			return
		}
		if in.Text == "" {
			in.Text = "# " + strings.TrimSuffix(filepath.Base(in.Path), ".md") + "\n\n"
		}
		n, err := m.st.write(in.Path, in.Text, "", true)
		if err == nil {
			m.changed(n.Path, "created")
		}
		reply(w, n, err)
	}))
	h.Handle("POST /rename", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ From, To string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			reply(w, nil, err)
			return
		}
		n, err := m.st.rename(in.From, in.To)
		if err == nil {
			m.changed(n.Path, "renamed")
		}
		reply(w, n, err)
	}))
	h.Handle("DELETE /note", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("path")
		kept, err := m.st.remove(p)
		if err == nil {
			m.changed(p, "deleted")
		}
		reply(w, map[string]string{"trash": kept}, err)
	}))
	// A file of the folder, for a note that shows an image (or links a file) by a relative path.
	// Served as data: the browser is told not to guess its type, and the page it would make is
	// sandboxed (an SVG or HTML file opened on its own runs no script).
	h.Handle("GET /asset", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, full, err := m.st.file(r.URL.Query().Get("path"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		info, err := os.Stat(full) //nolint:gosec // full passed the containment check in file
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; sandbox")
		http.ServeFile(w, r, full)
	}))
	h.Handle("GET /search", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits, err := m.st.search(r.URL.Query().Get("q"), 0)
		reply(w, hits, err)
	}))
	// A Files location (place, path) as a note path, when the file is inside the notes folder.
	h.Handle("POST /locate", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Place, Path string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			reply(w, nil, err)
			return
		}
		rel, err := m.locate(r.Context(), in.Place, in.Path)
		reply(w, map[string]string{"path": rel}, err)
	}))
	// The folder is a live setting: when it changes, the windows list the new folder at once.
	h.Config().Watch("folder", func(any) { m.changed("", "folder") })
	return nil
}

// locate turns a Files location into a note path. Notes does not know Files; it asks for
// the file's real path through Files' command, and answers only for a file in its folder.
func (m *mod) locate(ctx context.Context, place, path string) (string, error) {
	out, err := m.h.RunCommand(ctx, "files.path", map[string]any{"place": place, "path": path})
	if err != nil {
		return "", fmt.Errorf("that file cannot be opened as a note: %w", err)
	}
	raw, _ := json.Marshal(out)
	var got struct{ Path string }
	if json.Unmarshal(raw, &got) != nil || got.Path == "" {
		return "", errors.New("that file cannot be opened as a note")
	}
	root, err := m.root()
	if err != nil {
		return "", err
	}
	if resolved, evalErr := filepath.EvalSymlinks(root); evalErr == nil {
		root = resolved
	}
	rel, err := filepath.Rel(root, got.Path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("%s is outside the notes folder (%s). Point the Notes folder setting at its folder to work on it here", filepath.Base(got.Path), root)
	}
	return clean(filepath.ToSlash(rel))
}

func reply(w http.ResponseWriter, v any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		status := http.StatusUnprocessableEntity
		switch {
		case errors.Is(err, errNotFound):
			status = http.StatusNotFound
		case errors.Is(err, errConflict), errors.Is(err, errExists):
			status = http.StatusConflict
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}
