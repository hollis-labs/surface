package workbench

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hollis-labs/surface/module"
)

// The HTTP routes the page uses, under /api/m/workbench/. They are also the API for scripts:
// export and import are plain downloads and uploads.

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// fail answers an error: 422 with the report for a save that does not validate, 404 for an
// agent that is not there, 400 otherwise.
func fail(w http.ResponseWriter, err error) {
	var inv *ErrInvalid
	switch {
	case errors.As(err, &inv):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error(), "report": inv.Report})
	case errors.Is(err, ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
	case errors.Is(err, ErrExists):
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
	}
}

// ownerOnly refuses a request from anyone but the owner at the page: the routes that spend
// money (a live run, a proposal), change the agent without its validation-plus-review path
// (accept, rollback), delete, or read from disk (import-dir). A request with no Origin (MCP,
// the HTTP tools, the CLI) is an outside client, a delegate; such a client uses the agent
// tools, where the owner is asked.
func ownerOnly(w http.ResponseWriter, r *http.Request, what string) bool {
	if module.ActorOf(r.Context()).Delegate() {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": what + " is the owner's, from the Workbench page; an outside client uses the workbench_* tools, which ask the owner"})
		return false
	}
	return true
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 8<<20))
	return dec.Decode(v)
}

// AgentView is an agent as the editor loads it.
type AgentView struct {
	Agent    Agent             `json:"agent"`
	Files    map[string]string `json:"files"`
	Report   Report            `json:"report"`
	Versions []Version         `json:"versions"`
	Dir      string            `json:"dir"`
}

func (m *mod) view(id string) (AgentView, error) {
	a, err := m.lib.Get(id)
	if err != nil {
		return AgentView{}, err
	}
	t, err := m.lib.Tree(id)
	if err != nil {
		return AgentView{}, err
	}
	files := map[string]string{}
	for p, b := range t {
		files[p] = string(b)
	}
	vs, _ := m.lib.Versions(id)
	if vs == nil {
		vs = []Version{}
	}
	dir, _ := m.lib.AgentDir(id)
	return AgentView{Agent: a, Files: files, Report: Validate(t), Versions: vs, Dir: dir}, nil
}

// applyChanges applies file changes to a tree: a string writes the file, null removes it.
func applyChanges(t Tree, changes map[string]*string) error {
	for p, c := range changes {
		if !authoredPath(p) {
			return fmt.Errorf("%s is not a file of an agent directory", p)
		}
		if c == nil {
			if p == "agent.md" || p == "host.yaml" {
				return fmt.Errorf("%s cannot be removed", p)
			}
			delete(t, p)
			continue
		}
		t[p] = []byte(*c)
	}
	return nil
}

// yamlEdit is a form edit: keys to set in one YAML file (or a Markdown file's front matter),
// and optionally a Markdown body.
type yamlEdit struct {
	File string         `json:"file"`
	Set  map[string]any `json:"set"`
	Body *string        `json:"body,omitempty"`
}

func applyYAML(t Tree, e yamlEdit) error {
	if !authoredPath(e.File) {
		return fmt.Errorf("%s is not a file of an agent directory", e.File)
	}
	cur := t[e.File]
	var out []byte
	var err error
	switch {
	case strings.HasSuffix(e.File, ".md"):
		out = cur
		if len(e.Set) > 0 {
			if out, err = setFront(out, e.Set); err != nil {
				return fmt.Errorf("%s: %w", e.File, err)
			}
		}
		if e.Body != nil {
			if out, err = setBody(out, *e.Body); err != nil {
				return fmt.Errorf("%s: %w", e.File, err)
			}
		}
	case strings.HasSuffix(e.File, ".yaml"):
		if out, err = setYAML(cur, e.Set); err != nil {
			return fmt.Errorf("%s: %w", e.File, err)
		}
	default:
		return fmt.Errorf("%s is not YAML or Markdown", e.File)
	}
	t[e.File] = out
	return nil
}

// FileDiff is one file's change between two states.
type FileDiff struct {
	File   string     `json:"file"`
	Change string     `json:"change"` // added, removed, changed
	Lines  []DiffLine `json:"lines"`
}

func treeDiff(a, b Tree) []FileDiff {
	var paths []string
	seen := map[string]bool{}
	for _, p := range append(a.Paths(), b.Paths()...) {
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	slices.Sort(paths)
	out := []FileDiff{}
	for _, p := range paths {
		x, inA := a[p]
		y, inB := b[p]
		switch {
		case inA && !inB:
			out = append(out, FileDiff{File: p, Change: "removed", Lines: diffLines(string(x), "")})
		case !inA && inB:
			out = append(out, FileDiff{File: p, Change: "added", Lines: diffLines("", string(y))})
		case !bytes.Equal(x, y):
			out = append(out, FileDiff{File: p, Change: "changed", Lines: diffLines(string(x), string(y))})
		}
	}
	return out
}

func (m *mod) export(id string) ([]byte, string, []string, error) {
	t, err := m.lib.Tree(id)
	if err != nil {
		return nil, "", nil, err
	}
	a, err := m.lib.Get(id)
	if err != nil {
		return nil, "", nil, err
	}
	rep := Validate(t)
	if !rep.OK {
		return nil, "", nil, &ErrInvalid{rep}
	}
	suites := map[string][]byte{}
	infos, _ := m.run.Suites(id)
	var names []string
	all := t.Clone()
	for _, s := range infos {
		suites[s.Name+".yaml"] = []byte(s.Source)
		names = append(names, s.Name)
		all["workbench/evals/"+s.Name+".yaml"] = []byte(s.Source)
	}
	// Every file is scanned, the suites too: a well-known credential shape anywhere refuses
	// the export; a credential-like assignment is reported with it.
	var warnings []string
	for _, f := range scanTree(all) {
		if f.Refuse {
			return nil, "", nil, fmt.Errorf("workbench: not exported: %s line %d holds %s; remove it and pass it by reference", f.File, f.Line, f.What)
		}
		warnings = append(warnings, fmt.Sprintf("%s:%d: %s", f.File, f.Line, f.What))
	}
	man := &Manifest{Format: 1, Name: a.Name, ID: id, Digest: rep.Digest, AgentdirDigest: rep.AgentdirDigest, Created: time.Now().UTC().Format(time.RFC3339),
		WorkbenchVersion: WorkbenchVersion, Source: a.Source, Version: a.Version, Files: t.Paths(), Suites: names}
	var buf bytes.Buffer
	if err := writeTarGz(&buf, Bundle{Tree: t, Suites: suites, Manifest: man}); err != nil {
		return nil, "", nil, err
	}
	name := fmt.Sprintf("%s-v%d.tar.gz", id, a.Version)
	return buf.Bytes(), name, warnings, nil
}

func (m *mod) routes(h module.Host) {
	h.Handle("GET /templates", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, Templates())
	}))
	h.Handle("GET /agents", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		list, err := m.lib.List()
		if err != nil {
			fail(w, err)
			return
		}
		root, _ := m.lib.Root()
		writeJSON(w, http.StatusOK, map[string]any{"agents": list, "root": root})
	}))
	h.Handle("POST /agents", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ ID, Name, Template string }
		if err := decode(r, &in); err != nil {
			fail(w, err)
			return
		}
		a, err := m.create(in.ID, in.Name, in.Template, "owner")
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, a)
	}))
	h.Handle("GET /agents/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, err := m.view(r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
	}))
	h.Handle("DELETE /agents/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ownerOnly(w, r, "deleting an agent") {
			return
		}
		id := r.PathValue("id")
		dest, err := m.lib.Delete(id)
		if err != nil {
			fail(w, err)
			return
		}
		m.changed(id, map[string]any{"deleted": true})
		writeJSON(w, http.StatusOK, map[string]any{"trash": dest})
	}))
	h.Handle("POST /agents/{id}/duplicate", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		src := r.PathValue("id")
		var in struct{ ID string }
		_ = decode(r, &in)
		t, err := m.lib.Tree(src)
		if err != nil {
			fail(w, err)
			return
		}
		id := in.ID
		if id == "" {
			if id, err = m.lib.FreeID(src + "-copy"); err != nil {
				fail(w, err)
				return
			}
		}
		a, _, err := m.lib.Create(id, t, Meta{Source: "duplicate:" + src}, "duplicated from "+src, "owner")
		if err != nil {
			fail(w, err)
			return
		}
		if infos, err := m.run.Suites(src); err == nil {
			for _, s := range infos {
				_, _ = m.run.SaveSuite(id, []byte(s.Source))
			}
		}
		m.changed(id, map[string]any{"created": true})
		writeJSON(w, http.StatusCreated, a)
	}))
	// Save file changes. Body: {"changes": {"<path>": "<content>" | null}, "base": "<digest>", "note": "..."}
	h.Handle("PUT /agents/{id}/files", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var in struct {
			Changes map[string]*string `json:"changes"`
			Edits   []yamlEdit         `json:"edits"`
			Base    string             `json:"base"`
			Note    string             `json:"note"`
			DryRun  bool               `json:"dry_run"`
		}
		if err := decode(r, &in); err != nil {
			fail(w, err)
			return
		}
		t, err := m.lib.Tree(id)
		if err != nil {
			fail(w, err)
			return
		}
		if err = applyChanges(t, in.Changes); err != nil {
			fail(w, err)
			return
		}
		for _, e := range in.Edits {
			if err = applyYAML(t, e); err != nil {
				fail(w, err)
				return
			}
		}
		if in.DryRun {
			writeJSON(w, http.StatusOK, map[string]any{"report": Validate(t), "files": stringsOf(t)})
			return
		}
		v, rep, err := m.lib.Save(id, t, in.Base, in.Note, "owner")
		if err != nil {
			fail(w, err)
			return
		}
		m.changed(id, map[string]any{"version": v.N})
		writeJSON(w, http.StatusOK, map[string]any{"version": v, "report": rep})
	}))
	h.Handle("GET /agents/{id}/versions/{n}/diff", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		n, err := strconv.Atoi(r.PathValue("n"))
		if err != nil {
			fail(w, err)
			return
		}
		a, err := m.lib.VersionTree(id, n)
		if err != nil {
			fail(w, err)
			return
		}
		var b Tree
		if to := r.URL.Query().Get("to"); to != "" && to != "current" {
			k, kerr := strconv.Atoi(to)
			if kerr != nil {
				fail(w, kerr)
				return
			}
			b, err = m.lib.VersionTree(id, k)
			if err != nil {
				fail(w, err)
				return
			}
		} else if b, err = m.lib.Tree(id); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, treeDiff(a, b))
	}))
	h.Handle("POST /agents/{id}/versions/{n}/rollback", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ownerOnly(w, r, "a rollback") {
			return
		}
		id := r.PathValue("id")
		n, err := strconv.Atoi(r.PathValue("n"))
		if err != nil {
			fail(w, err)
			return
		}
		v, _, err := m.lib.Rollback(id, n, "owner")
		if err != nil {
			fail(w, err)
			return
		}
		m.changed(id, map[string]any{"version": v.N})
		writeJSON(w, http.StatusOK, v)
	}))
	h.Handle("GET /agents/{id}/preview", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t, err := m.lib.Tree(r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		p, err := m.rt.Preview(r.Context(), t)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, p)
	}))
	h.Handle("GET /agents/{id}/export", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, name, warnings, err := m.export(r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		if len(warnings) > 0 {
			w.Header().Set("X-Workbench-Warnings", strings.Join(warnings, "; "))
		}
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		_, _ = w.Write(data) //nolint:gosec // a gzip download, sent as application/gzip
	}))
	// Import an archive: the raw bytes, or JSON {"name": "...", "data": "<base64>", "id": "..."}.
	h.Handle("POST /import", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, maxArchiveBytes*2+1))
		if err != nil {
			fail(w, err)
			return
		}
		want, source := r.URL.Query().Get("id"), "archive"
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			var in struct{ Name, Data, ID string }
			if err = json.Unmarshal(raw, &in); err != nil {
				fail(w, err)
				return
			}
			if raw, err = base64.StdEncoding.DecodeString(in.Data); err != nil {
				fail(w, fmt.Errorf("data: %w", err))
				return
			}
			if in.ID != "" {
				want = in.ID
			}
			if in.Name != "" {
				source = path.Base(in.Name)
			}
		}
		b, err := readArchive(raw)
		if err != nil {
			fail(w, fmt.Errorf("import: %w", err))
			return
		}
		a, rep, notes, err := m.importBundle(b, want, "import:"+source, "owner")
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"agent": a, "report": rep, "notes": notes})
	}))
	// Import a directory by copying it, read-only. Body: {"path": "/abs/dir", "id": "..."}
	h.Handle("POST /import-dir", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ownerOnly(w, r, "importing a directory from disk") {
			return
		}
		var in struct{ Path, ID string }
		if err := decode(r, &in); err != nil {
			fail(w, err)
			return
		}
		b, err := readDirBundle(in.Path)
		if err != nil {
			fail(w, fmt.Errorf("import: %w", err))
			return
		}
		a, rep, notes, err := m.importBundle(b, in.ID, "dir:"+in.Path, "owner")
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"agent": a, "report": rep, "notes": notes})
	}))

	// Evals.
	h.Handle("GET /agents/{id}/suites", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, err := m.run.Suites(r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, s)
	}))
	h.Handle("PUT /agents/{id}/suites", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Source string }
		if err := decode(r, &in); err != nil {
			fail(w, err)
			return
		}
		s, err := m.run.SaveSuite(r.PathValue("id"), []byte(in.Source))
		if err != nil {
			fail(w, err)
			return
		}
		m.changed(r.PathValue("id"), map[string]any{"suite": s.Name})
		writeJSON(w, http.StatusOK, s)
	}))
	h.Handle("DELETE /agents/{id}/suites/{name}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := m.run.DeleteSuite(r.PathValue("id"), r.PathValue("name")); err != nil {
			fail(w, err)
			return
		}
		m.changed(r.PathValue("id"), nil)
		writeJSON(w, http.StatusOK, map[string]any{"deleted": r.PathValue("name")})
	}))
	h.Handle("GET /agents/{id}/suites/{name}/estimate", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e, err := m.run.Estimate(r.Context(), r.PathValue("id"), r.PathValue("name"), Caps{})
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, e)
	}))
	// Start a run: offline by default; a live run only with confirm: true, which the page
	// sends once the owner has agreed.
	h.Handle("POST /agents/{id}/suites/{name}/run", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Mode    string `json:"mode"`
			Confirm bool   `json:"confirm"`
			Caps    Caps   `json:"caps"`
		}
		if err := decode(r, &in); err != nil {
			fail(w, err)
			return
		}
		if in.Mode == "live" && (!ownerOnly(w, r, "a live eval run") || !confirmed(w, in.Confirm)) {
			return
		}
		run, err := m.run.Start(r.Context(), r.PathValue("id"), r.PathValue("name"), in.Mode, in.Caps, "")
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, run)
	}))
	h.Handle("GET /agents/{id}/runs", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runs, err := m.run.Runs(r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, runs)
	}))
	h.Handle("GET /agents/{id}/runs/{run}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		run, err := m.run.GetRun(r.PathValue("id"), r.PathValue("run"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, run)
	}))
	h.Handle("POST /agents/{id}/runs/{run}/cancel", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"canceled": m.run.Cancel(r.PathValue("run"))})
	}))
	h.Handle("POST /agents/{id}/runs/{run}/rerun", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Mode    string `json:"mode"`
			Confirm bool   `json:"confirm"`
			Caps    Caps   `json:"caps"`
		}
		if err := decode(r, &in); err != nil {
			fail(w, err)
			return
		}
		if in.Mode == "live" && !ownerOnly(w, r, "a live eval run") {
			return
		}
		prev, err := m.run.GetRun(r.PathValue("id"), r.PathValue("run"))
		if err != nil {
			fail(w, err)
			return
		}
		if in.Mode == "" {
			in.Mode = prev.Mode
		}
		if in.Mode == "live" && (!ownerOnly(w, r, "a live eval run") || !confirmed(w, in.Confirm)) {
			return
		}
		run, err := m.run.Start(r.Context(), prev.Agent, prev.Suite, in.Mode, in.Caps, prev.ID)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, run)
	}))
	h.Handle("GET /agents/{id}/compare", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := m.run.Compare(r.PathValue("id"), r.URL.Query().Get("a"), r.URL.Query().Get("b"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, c)
	}))
	h.Handle("POST /agents/{id}/runs/{run}/propose", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Confirm bool `json:"confirm"`
		}
		if !ownerOnly(w, r, "a proposal") {
			return
		}
		if err := decode(r, &in); err != nil || !in.Confirm {
			fail(w, errors.New("a proposal calls a model and costs money: send confirm: true once the owner has agreed"))
			return
		}
		p, err := m.run.Propose(r.Context(), r.PathValue("id"), r.PathValue("run"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, p)
	}))
	h.Handle("GET /agents/{id}/runs/{run}/propose-estimate", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e, err := m.run.EstimatePropose(r.Context(), r.PathValue("id"), r.PathValue("run"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, e)
	}))
	h.Handle("GET /agents/{id}/proposals", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ps, err := m.run.Proposals(r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, ps)
	}))
	h.Handle("POST /agents/{id}/proposals/{p}/{n}/{decision}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ownerOnly(w, r, "accepting or rejecting a proposal") {
			return
		}
		n, err := strconv.Atoi(r.PathValue("n"))
		if err != nil {
			fail(w, err)
			return
		}
		d := r.PathValue("decision")
		if d != "accept" && d != "reject" {
			fail(w, errors.New("decide accept or reject"))
			return
		}
		p, err := m.run.Decide(r.PathValue("id"), r.PathValue("p"), n, d == "accept")
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, p)
	}))
}

func stringsOf(t Tree) map[string]string {
	out := map[string]string{}
	for p, b := range t {
		out[p] = string(b)
	}
	return out
}

func confirmed(w http.ResponseWriter, ok bool) bool {
	if !ok {
		fail(w, errors.New("a live eval run calls a model and costs money: send confirm: true once the owner has agreed"))
	}
	return ok
}
