// Package files is the Files module: a file browser over a small set of named places
// (root directories on this machine), for the owner and for the agent.
//
// Every path is a place and a path relative to it. locate resolves and checks each one:
// nothing outside a place is reached, by ".." or through a symbolic link, and files that
// look like credentials are refused (see sensitive). Nothing is ever overwritten except a
// text file by a write that names it, and there is no recursive delete.
package files

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path"
	"reflect"
	"strings"

	"github.com/hollis-labs/surface/module"
)

//go:embed migrations/*.sql
var migrations embed.FS

func init() { module.Register(&mod{}) }

const (
	viewID = "files"
	// mainKey is the key of the window a plain "open Files" brings forward.
	mainKey = "main"
	// topic is published after every change, with the place and path that changed.
	topic = "files.changed"
	// knowledgeNames is how many names of the folder in front the agent is told.
	knowledgeNames = 40
	// maxBody bounds a request body, and so the text one write can carry.
	maxBody = 8 << 20
)

type mod struct {
	module.Base
	st *store
}

func (m *mod) Manifest() module.Manifest {
	return module.Manifest{
		ID: "files", Title: "Files", Version: "0.1.0",
		About: "A file browser over a few named folders on this machine.",
		Views: []module.View{{
			ID: viewID, Title: "Files", Icon: "folder", Width: 900, Height: 600,
			Menu: true, Multi: true, DefaultKey: mainKey, Bare: true,
			Render:   module.Component("files/main"),
			Describe: m.windowContext,
		}},
		Commands: []module.Command{
			{ID: "files.open", Title: "Files: open a folder", Section: "Files", Keywords: "browse directory place finder explorer",
				Run: func(ctx context.Context, h module.Host, args map[string]any) (any, error) {
					place, _ := args["place"].(string)
					rel, _ := args["path"].(string)
					if place == "" {
						return h.Desk().Open(ctx, viewID, module.OpenOptions{Key: mainKey})
					}
					return m.open(ctx, h, place, rel, false)
				}},
			// For other modules (Host.RunCommand): the real path of a Files location, after
			// the same checks as every other access.
			{ID: "files.path", Title: "Files: the path on disk of a location", Section: "Files", Hidden: true,
				Run: func(ctx context.Context, _ module.Host, args map[string]any) (any, error) {
					place, _ := args["place"].(string)
					rel, _ := args["path"].(string)
					if place == "" {
						return nil, errors.New("files.path needs a place and a path")
					}
					l, err := m.at(ctx, place, rel, true)
					if err != nil {
						return nil, err
					}
					return map[string]string{"path": l.Abs}, nil
				}},
		},
		Tools: m.tools(),
		Store: &module.Store{Migrations: migrations, Dir: "migrations"},
		Settings: []module.Setting{
			{Key: "show_hidden", Title: "Show hidden files", About: "List files and folders whose name starts with a dot.", Type: module.SettingBool, Default: false},
			{Key: "preview_limit_kb", Title: "Text preview limit (KB)", About: "How much of a text file is read for a preview or by the agent in one call.", Type: module.SettingInt, Default: 256},
		},
		Slots: []module.Slot{{
			Name: "files", About: "files and folders: browsing the owner's places on this machine, reading, searching and editing text files",
			Views:        []string{viewID},
			Tools:        []string{"files_places", "files_list", "files_read", "files_grep", "files_write"},
			Where:        "the owner's places (files_places), their folders (files_list) and text files (files_read, files_grep)",
			Instructions: "Paths are a place and a path relative to it; \"this folder\" is the one in the Files window in front. Read a file before replacing it, and write it back whole. Never guess at a file's content from its name. Files that look like credentials are refused: do not look for another way to them.",
			Knowledge:    m.knowledge,
		}},
	}
}

func (m *mod) Start(ctx context.Context, h module.Host) error {
	m.st = &store{db: h.Store()}
	places, err := m.st.list(ctx)
	if err != nil {
		return err
	}
	if len(places) == 0 {
		// First start: one place, the owner's home directory.
		if home, err := os.UserHomeDir(); err != nil {
			h.Log().Warn("files: no home directory to make the first place from", "error", err)
		} else if _, err := m.st.add(ctx, "Home", home); err != nil {
			h.Log().Warn("files: could not make the home place", "error", err)
		}
	}
	m.routes(h)
	// The listing depends on a live setting: tell the views when it changes.
	h.Config().Watch("show_hidden", func(any) { h.Events().Publish(topic, map[string]any{"setting": "show_hidden"}) })
	return nil
}

// at locates a path in a place by the place's id.
func (m *mod) at(ctx context.Context, place, rel string, follow bool) (loc, error) {
	p, err := m.st.get(ctx, place)
	if err != nil {
		return loc{}, err
	}
	return locate(p, rel, follow)
}

func changed(h module.Host, place, rel string) {
	h.Events().Publish(topic, map[string]string{"place": place, "path": rel})
}

// previewLimit is the live text limit, in bytes.
func previewLimit(h module.Host) int64 {
	kb := h.Config().Int("preview_limit_kb")
	if kb <= 0 {
		kb = 256
	}
	return int64(min(kb, 4096)) << 10
}

func ref(place, rel string) string { return "files:" + place + ":" + rel }

// front is the folder in the focused window, when that is a Files window that has one.
func front(ctx context.Context, h module.Host) (place, rel string, ok bool) {
	w, ok := h.Desk().Focused(ctx)
	if !ok || w.View != viewID {
		return "", "", false
	}
	place, _ = w.Args["place"].(string)
	rel, _ = w.Args["path"].(string)
	return place, rel, place != ""
}

// windowContext is the view's Describe: the folder a window shows, or the file selected
// in it.
func (m *mod) windowContext(ctx context.Context, h module.Host, w module.Window) (module.WindowContext, error) {
	place, _ := w.Args["place"].(string)
	rel, _ := w.Args["path"].(string)
	wc := module.WindowContext{Kind: "folder"}
	if place == "" {
		return wc, nil
	}
	wc.Ref = ref(place, rel)
	wc.Extra = map[string]any{"place": place, "path": rel}
	if sel, _ := w.State["selection"].(string); sel != "" {
		wc.Kind, wc.Ref = "file", ref(place, sel)
		wc.Extra["selected"] = sel
	}
	if l, err := m.at(ctx, place, rel, true); err == nil {
		if _, n, err := names(l, h.Config().Bool("show_hidden"), 0); err == nil {
			wc.Extra["entries"] = n
		}
	}
	return wc, nil
}

// knowledge is the slot's: the first names of the folder in front. One directory read.
func (m *mod) knowledge(ctx context.Context, h module.Host, s module.Situation) (string, error) {
	if s.Focused == nil || s.Focused.View != viewID {
		return "", nil
	}
	place, _ := s.Focused.Extra["place"].(string)
	rel, _ := s.Focused.Extra["path"].(string)
	if place == "" {
		return "", nil
	}
	// A folder that is gone or refused is nothing to tell the agent, not a failed turn.
	l, err := m.at(ctx, place, rel, true)
	if err != nil {
		return "", nil //nolint:nilerr // see above: no knowledge, the turn goes on
	}
	list, total, err := names(l, h.Config().Bool("show_hidden"), knowledgeNames)
	if err != nil {
		return "", nil //nolint:nilerr // as above
	}
	var b strings.Builder
	fmt.Fprintf(&b, "The Files window in front shows place %q, folder %q (%d items).", place, "/"+l.Rel, total)
	if sel, _ := s.Focused.Extra["selected"].(string); sel != "" {
		fmt.Fprintf(&b, " The selected file is %q.", sel)
	}
	if total > 0 {
		fmt.Fprintf(&b, " The first %d:\n", len(list))
		for _, n := range list {
			b.WriteString("- " + n + "\n")
		}
	}
	return b.String(), nil
}

// open shows a location in a Files window: the main one, taken there, or one of its own
// with newWindow. A file opens its folder with the file selected.
func (m *mod) open(ctx context.Context, h module.Host, place, rel string, newWindow bool) (module.Window, error) {
	l, err := m.at(ctx, place, rel, true)
	if err != nil {
		return module.Window{}, err
	}
	fi, err := os.Stat(l.Abs)
	if err != nil {
		return module.Window{}, describe(err, l)
	}
	args := map[string]any{"place": l.Place.ID, "path": l.Rel}
	if !fi.IsDir() {
		args["path"], args["select"] = parent(l.Rel), l.Rel
	}
	opts := module.OpenOptions{Args: args, Key: mainKey}
	if newWindow {
		opts.Key = l.Place.ID + ":" + args["path"].(string)
	}
	w, err := h.Desk().Open(ctx, viewID, opts)
	if err != nil {
		return w, err
	}
	// A window that was already open is brought forward as it was: take it there.
	if !reflect.DeepEqual(w.Args, args) {
		if err := h.Desk().SetArgs(ctx, w.ID, args); err != nil {
			return w, err
		}
		w.Args = args
	}
	return w, nil
}

// parent is the folder of a relative path, "" for an entry of the root.
func parent(rel string) string {
	if d := path.Dir(rel); d != "." {
		return d
	}
	return ""
}

// The operations, as the routes and the tools share them. Each publishes files.changed.

func (m *mod) write(ctx context.Context, h module.Host, place, rel, text string, createOnly bool) (map[string]any, error) {
	l, err := m.at(ctx, place, rel, true)
	if err != nil {
		return nil, err
	}
	created, err := writeText(l, text, createOnly)
	if err != nil {
		return nil, err
	}
	changed(h, place, l.Rel)
	return map[string]any{"place": place, "path": l.Rel, "created": created, "size": len(text)}, nil
}

func (m *mod) mkdir(ctx context.Context, h module.Host, place, rel string) (map[string]any, error) {
	l, err := m.at(ctx, place, rel, false)
	if err != nil {
		return nil, err
	}
	if l.Rel == "" {
		return nil, errors.New("say what to call the new folder")
	}
	if err := makeDir(l); err != nil {
		return nil, err
	}
	changed(h, place, l.Rel)
	return map[string]any{"place": place, "path": l.Rel}, nil
}

func (m *mod) move(ctx context.Context, h module.Host, place, from, to string) (map[string]any, error) {
	src, err := m.at(ctx, place, from, false)
	if err != nil {
		return nil, err
	}
	dst, err := m.at(ctx, place, to, false)
	if err != nil {
		return nil, err
	}
	if err := move(src, dst); err != nil {
		return nil, err
	}
	changed(h, place, src.Rel)
	changed(h, place, dst.Rel)
	return map[string]any{"place": place, "from": src.Rel, "path": dst.Rel}, nil
}

func (m *mod) delete(ctx context.Context, h module.Host, place, rel string) (map[string]any, error) {
	l, err := m.at(ctx, place, rel, false)
	if err != nil {
		return nil, err
	}
	if err := remove(l); err != nil {
		return nil, err
	}
	changed(h, place, l.Rel)
	return map[string]any{"place": place, "deleted": l.Rel}, nil
}

func (m *mod) routes(h module.Host) {
	handle := func(pattern string, fn func(r *http.Request) (any, error)) {
		h.Handle(pattern, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			v, err := fn(r)
			reply(w, v, err)
		}))
	}

	handle("GET /places", func(r *http.Request) (any, error) { return m.st.list(r.Context()) })
	handle("POST /places", func(r *http.Request) (any, error) {
		var in struct{ Name, Path string }
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		p, err := m.st.add(r.Context(), in.Name, in.Path)
		if err == nil {
			changed(h, p.ID, "")
		}
		return p, err
	})
	handle("DELETE /places/{id}", func(r *http.Request) (any, error) {
		id := r.PathValue("id")
		if err := m.st.remove(r.Context(), id); err != nil {
			return nil, err
		}
		changed(h, id, "")
		return map[string]bool{"ok": true}, nil
	})
	handle("GET /list", func(r *http.Request) (any, error) {
		l, err := m.at(r.Context(), r.URL.Query().Get("place"), r.URL.Query().Get("path"), true)
		if err != nil {
			return nil, err
		}
		entries, err := list(l, h.Config().Bool("show_hidden"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"place": l.Place.ID, "path": l.Rel, "entries": entries}, nil
	})
	handle("GET /read", func(r *http.Request) (any, error) {
		l, err := m.at(r.Context(), r.URL.Query().Get("place"), r.URL.Query().Get("path"), true)
		if err != nil {
			return nil, err
		}
		return readText(l, 1, 0, previewLimit(h))
	})
	h.Handle("GET /raw", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := m.serveRaw(w, r); err != nil {
			reply(w, nil, err)
		}
	}))
	handle("POST /mkdir", func(r *http.Request) (any, error) {
		var in struct{ Place, Path string }
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		return m.mkdir(r.Context(), h, in.Place, in.Path)
	})
	handle("POST /write", func(r *http.Request) (any, error) {
		var in struct {
			Place, Path, Text string
			CreateOnly        bool `json:"create_only"`
		}
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		return m.write(r.Context(), h, in.Place, in.Path, in.Text, in.CreateOnly)
	})
	handle("POST /move", func(r *http.Request) (any, error) {
		var in struct{ Place, From, To string }
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		return m.move(r.Context(), h, in.Place, in.From, in.To)
	})
	handle("POST /delete", func(r *http.Request) (any, error) {
		var in struct{ Place, Path string }
		if err := decode(r, &in); err != nil {
			return nil, err
		}
		return m.delete(r.Context(), h, in.Place, in.Path)
	})
}

// serveRaw sends a file's bytes, for an image in the preview or a download (?download=1).
// It writes nothing when it returns an error.
func (m *mod) serveRaw(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	l, err := m.at(r.Context(), q.Get("place"), q.Get("path"), true)
	if err != nil {
		return err
	}
	f, err := os.Open(l.Abs)
	if err != nil {
		return describe(err, l)
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a file that can be sent", l.name())
	}
	hd := w.Header()
	if ct := mime.TypeByExtension(path.Ext(l.Rel)); ct != "" {
		hd.Set("Content-Type", ct)
	}
	// A file is content, never a page of this app: an HTML or SVG file opened from here
	// runs no script in the app's origin.
	hd.Set("Content-Security-Policy", "sandbox")
	hd.Set("X-Content-Type-Options", "nosniff")
	if q.Get("download") != "" {
		hd.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(l.Rel)}))
	}
	http.ServeContent(w, r, path.Base(l.Rel), fi.ModTime(), f)
	return nil
}

// errBadRequest marks a request body that could not be read.
var errBadRequest = errors.New("bad request")

func decode(r *http.Request, into any) error {
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBody)).Decode(into); err != nil {
		return fmt.Errorf("%w: the body must be a JSON object of at most %d MB: %w", errBadRequest, maxBody>>20, err)
	}
	return nil
}

func reply(w http.ResponseWriter, v any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		status := http.StatusUnprocessableEntity
		switch {
		case errors.Is(err, errBadRequest):
			status = http.StatusBadRequest
		case errors.Is(err, errNotFound):
			status = http.StatusNotFound
		case errors.Is(err, errOutside), errors.Is(err, errSensitive):
			status = http.StatusForbidden
		case errors.Is(err, errExists):
			status = http.StatusConflict
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}
