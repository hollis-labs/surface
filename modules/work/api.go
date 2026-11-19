package work

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"

	"github.com/hollis-labs/surface/module"
)

// The routes keep Basalisk's paths and JSON shapes (internal/api/work.go, used by
// frontend/src/api/work.ts), under the module's prefix: Basalisk's /api/work/... is
// /api/m/work/work/... here, so a view's client calls the same relative paths
// (api.get("work/<ws>")). README.md lists them.

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func errorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusServiceUnavailable:
		return "unavailable"
	case http.StatusRequestEntityTooLarge:
		return "too_large"
	}
	return "upstream_error"
}

// fail writes {code, message, error}: Basalisk's {code, message}, and error, which the page's
// api client reads.
func fail(w http.ResponseWriter, status int, message string) {
	failWith(w, status, message, nil)
}

func failWith(w http.ResponseWriter, status int, message string, extra map[string]any) {
	body := map[string]any{"code": errorCode(status), "message": message, "error": message}
	for k, v := range extra {
		body[k] = v
	}
	reply(w, status, body)
}

// decodeLimit reads a JSON body of at most limit bytes, with no unknown keys.
func decodeLimit(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		fail(w, http.StatusBadRequest, "Invalid request.")
		return false
	}
	return true
}

// workFail answers a store error: no link to Tether, no such thing, a conflict, or Caster's.
func workFail(w http.ResponseWriter, err error) {
	var c *Conflict
	switch {
	case errors.As(err, &c):
		failWith(w, http.StatusConflict, c.Error(), map[string]any{"conflict": true, "rev": c.Rev, "suggestions": c.Suggestions})
	case errors.Is(err, ErrNotFound):
		fail(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrNoCaster):
		fail(w, http.StatusServiceUnavailable, err.Error())
	default:
		fail(w, http.StatusBadGateway, err.Error())
	}
}

// failOr400 is workFail for a missing link or thing, 400 for anything else (a refused input).
func failOr400(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNoCaster) || errors.Is(err, ErrNotFound) {
		workFail(w, err)
		return
	}
	fail(w, http.StatusBadRequest, err.Error())
}

func (m *mod) routes(h module.Host) {
	handle := func(pattern string, fn http.HandlerFunc) { h.Handle(pattern, fn) }
	handle("GET /work", m.workList)
	handle("POST /work", m.workNew)
	handle("GET /work/{ws}", m.workOpen)
	handle("PUT /work/{ws}", m.workWorkspaceEdit)
	handle("DELETE /work/{ws}", m.workWorkspaceDelete)
	handle("GET /work/{ws}/log", m.workLog)
	handle("GET /work/{ws}/at", m.workAt)
	handle("POST /work/{ws}/items", m.workMake)
	handle("GET /work/{ws}/items/{id}", m.workDetail)
	handle("PUT /work/{ws}/items/{id}", m.workEdit)
	handle("PUT /work/{ws}/items/{id}/move", m.workMove)
	handle("PUT /work/{ws}/items/{id}/place", m.workPlace)
	handle("POST /work/{ws}/items/{id}/torque", m.workSendTorque)
	handle("GET /work/{ws}/items/{id}/torque", m.workTorqueStatus)
	handle("POST /work/{ws}/items/{id}/cut", m.workCut)
	handle("POST /work/{ws}/items/{id}/restore", m.workRestore)
	handle("DELETE /work/{ws}/notes/{id}", m.workDropNote)
	handle("POST /work/{ws}/items/{id}/notes", m.workAddNote)
	handle("PUT /work/{ws}/notes/{id}", m.workEditNote)
	handle("POST /work/{ws}/items/{id}/round", m.workSendRound)
	handle("POST /work/{ws}/items/{id}/signoff", m.workSignOff)
	handle("DELETE /work/{ws}/items/{id}/signoff", m.workUnsign)
	handle("PUT /work/{ws}/owners", m.workOwners)
	handle("POST /work/{ws}/views", m.workView)
	handle("PUT /work/{ws}/views/{id}", m.workView)
	handle("PUT /work/{ws}/views/{id}/layout", m.workLayout)
}

// listResult is GET /work: the workspaces, the presets, and what went wrong reading Caster.
func (m *mod) listResult(r *http.Request, app string, force bool) (map[string]any, error) {
	_, rerr := m.st.Refresh(r.Context(), IndexNS, force)
	list, err := m.st.Workspaces(r.Context(), app)
	if err != nil {
		return nil, err
	}
	presets := []Preset{}
	for _, p := range Presets() {
		presets = append(presets, p)
	}
	sort.Slice(presets, func(i, j int) bool { return presets[i].ID < presets[j].ID })
	out := map[string]any{"workspaces": list, "presets": presets}
	if rerr != nil {
		out["problem"] = rerr.Error()
	}
	return out, nil
}

func (m *mod) workList(w http.ResponseWriter, r *http.Request) {
	out, err := m.listResult(r, r.URL.Query().Get("app"), r.URL.Query().Get("refresh") == "1")
	if err != nil {
		fail(w, http.StatusInternalServerError, "The workspaces could not be read.")
		return
	}
	reply(w, http.StatusOK, out)
}

func (m *mod) workNew(w http.ResponseWriter, r *http.Request) {
	var in struct{ Workspace, Title, Preset, About, Template string }
	if !decodeLimit(w, r, &in, 32<<10) {
		return
	}
	slug, err := m.st.NewWorkspace(r.Context(), in.Workspace, in.Title, in.Preset, in.About)
	if err != nil {
		if errors.Is(err, ErrNoCaster) {
			workFail(w, err)
			return
		}
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	defer m.changed(slug, "", "workspace")
	// A starting tree (Planner templates): the workspace exists either way.
	if in.Template != "" {
		if err := m.st.ApplyTemplate(r.Context(), slug, in.Template); err != nil {
			reply(w, http.StatusCreated, map[string]any{"workspace": slug, "problem": "the template was not all made: " + err.Error()})
			return
		}
	}
	reply(w, http.StatusCreated, map[string]any{"workspace": slug})
}

func (m *mod) workOpen(w http.ResponseWriter, r *http.Request) {
	ws := r.PathValue("ws")
	rerr := m.sync(r.Context(), ws, r.URL.Query().Get("refresh") == "1")
	sp, ok, err := m.st.Open(r.Context(), ws)
	if err != nil {
		fail(w, http.StatusInternalServerError, "The workspace could not be read.")
		return
	}
	if !ok {
		fail(w, http.StatusNotFound, "There is no such workspace.")
		return
	}
	out := map[string]any{"space": sp, "relations": Relations}
	if rerr != nil {
		out["problem"] = rerr.Error()
	}
	reply(w, http.StatusOK, out)
}

func (m *mod) workDetail(w http.ResponseWriter, r *http.Request) {
	d, ok, err := m.st.Detail(r.Context(), r.PathValue("ws"), r.PathValue("id"))
	if err != nil {
		fail(w, http.StatusInternalServerError, "The item could not be read.")
		return
	}
	if !ok {
		fail(w, http.StatusNotFound, "There is no such item.")
		return
	}
	reply(w, http.StatusOK, d)
}

func (m *mod) workMake(w http.ResponseWriter, r *http.Request) {
	var in NewItem
	if !decodeLimit(w, r, &in, 64<<10) {
		return
	}
	ws := r.PathValue("ws")
	id, err := m.st.MakeItem(r.Context(), ws, in)
	if err != nil {
		failOr400(w, err)
		return
	}
	m.changed(ws, id, "item")
	reply(w, http.StatusCreated, map[string]any{"id": id})
}

func (m *mod) workEdit(w http.ResponseWriter, r *http.Request) {
	var in Edit
	if !decodeLimit(w, r, &in, 4<<20) {
		return
	}
	ws, id := r.PathValue("ws"), r.PathValue("id")
	rev, err := m.st.EditItem(r.Context(), ws, id, in)
	if err != nil {
		var c *Conflict
		if errors.As(err, &c) {
			m.changed(ws, id, "note")
		}
		workFail(w, err)
		return
	}
	m.changed(ws, id, "item")
	reply(w, http.StatusOK, map[string]any{"rev": rev})
}

func (m *mod) workMove(w http.ResponseWriter, r *http.Request) {
	var in struct{ Key, Value string }
	if !decodeLimit(w, r, &in, 4<<10) {
		return
	}
	ws, id := r.PathValue("ws"), r.PathValue("id")
	rev, err := m.st.Move(r.Context(), ws, id, in.Key, in.Value)
	if err != nil {
		failOr400(w, err)
		return
	}
	m.changed(ws, id, "item")
	reply(w, http.StatusOK, map[string]any{"rev": rev})
}

// workSendTorque makes the item's Torque task once ({task, created}).
func (m *mod) workSendTorque(w http.ResponseWriter, r *http.Request) {
	ws, id := r.PathValue("ws"), r.PathValue("id")
	task, created, err := m.st.SendToTorque(r.Context(), ws, id)
	if err != nil {
		switch {
		case errors.Is(err, ErrNoCaster), errors.Is(err, ErrNotFound):
			workFail(w, err)
		case errors.Is(err, ErrNoTorque):
			fail(w, http.StatusServiceUnavailable, err.Error())
		case task != "":
			failWith(w, http.StatusBadGateway, err.Error(), map[string]any{"task": task})
		default:
			fail(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	if created {
		m.changed(ws, id, "item")
	}
	reply(w, http.StatusOK, map[string]any{"task": task, "created": created})
}

// workTorqueStatus is the item's Torque task as Torque has it now.
func (m *mod) workTorqueStatus(w http.ResponseWriter, r *http.Request) {
	d, ok, err := m.st.Detail(r.Context(), r.PathValue("ws"), r.PathValue("id"))
	if err != nil || !ok {
		workFail(w, ErrNotFound)
		return
	}
	if d.Task == "" {
		reply(w, http.StatusOK, map[string]any{"task": nil})
		return
	}
	st, err := m.st.TorqueStatus(r.Context(), d.Task)
	if err != nil {
		reply(w, http.StatusOK, map[string]any{"task": map[string]any{"id": d.Task}, "problem": err.Error()})
		return
	}
	reply(w, http.StatusOK, map[string]any{"task": st})
}

// workPlace puts an item in the outline: {parent, before, view}.
func (m *mod) workPlace(w http.ResponseWriter, r *http.Request) {
	var in struct{ Parent, Before, View string }
	if !decodeLimit(w, r, &in, 4<<10) {
		return
	}
	ws, id := r.PathValue("ws"), r.PathValue("id")
	if err := m.st.Place(r.Context(), ws, id, in.Parent, in.Before, in.View); err != nil {
		failOr400(w, err)
		return
	}
	m.changed(ws, id, "item")
	reply(w, http.StatusOK, map[string]any{"ok": true})
}

func (m *mod) workCut(w http.ResponseWriter, r *http.Request) {
	ws, id := r.PathValue("ws"), r.PathValue("id")
	if err := m.st.Cut(r.Context(), ws, id); err != nil {
		workFail(w, err)
		return
	}
	m.changed(ws, id, "item")
	reply(w, http.StatusOK, map[string]any{"ok": true})
}

func (m *mod) workRestore(w http.ResponseWriter, r *http.Request) {
	ws, id := r.PathValue("ws"), r.PathValue("id")
	if err := m.st.Restore(r.Context(), ws, id); err != nil {
		workFail(w, err)
		return
	}
	m.changed(ws, id, "item")
	reply(w, http.StatusOK, map[string]any{"ok": true})
}

func (m *mod) workDropNote(w http.ResponseWriter, r *http.Request) {
	ws := r.PathValue("ws")
	if err := m.st.DropNote(r.Context(), ws, r.PathValue("id")); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	m.changed(ws, "", "note")
	reply(w, http.StatusOK, map[string]any{"ok": true})
}

// workView makes (POST) or changes (PUT …/{id}) a saved view: the keys sent are set, null
// takes one out, the rest stay.
func (m *mod) workView(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	if !decodeLimit(w, r, &in, 1<<20) {
		return
	}
	ws, id := r.PathValue("ws"), r.PathValue("id")
	if r.Method == http.MethodPost {
		id = NewID()
	}
	rev, err := m.st.SaveView(r.Context(), ws, id, in)
	if err != nil {
		failOr400(w, err)
		return
	}
	m.changed(ws, id, "view")
	reply(w, http.StatusOK, map[string]any{"id": id, "rev": rev})
}

// workLayout changes a canvas's arrangement by id (cards, stacks, notes), applied to the
// view as it is now.
func (m *mod) workLayout(w http.ResponseWriter, r *http.Request) {
	var in LayoutPatch
	if !decodeLimit(w, r, &in, 1<<20) {
		return
	}
	ws, id := r.PathValue("ws"), r.PathValue("id")
	rev, err := m.st.PatchLayout(r.Context(), ws, id, in)
	if err != nil {
		failOr400(w, err)
		return
	}
	m.changed(ws, id, "view")
	reply(w, http.StatusOK, map[string]any{"rev": rev})
}

// workLog is an item's revisions (name: i.<id>, v.<id> or kind.<kind>).
func (m *mod) workLog(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if k, _ := ParseName(name); k == NameOther {
		fail(w, http.StatusBadRequest, "That is not a work item.")
		return
	}
	revs, err := m.st.Log(r.Context(), r.PathValue("ws"), name)
	if err != nil {
		workFail(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"revisions": revs})
}

// atResult is an item at a revision, as its text and as the detail reads it.
func atResult(it Item) map[string]any {
	out := map[string]any{"name": it.Name, "rev": it.Rev, "content": it.Content}
	if d, err := ParseDoc(it.Content); err == nil {
		secs := map[string]string{}
		for _, s := range ParseBody(d.Body).Sections {
			secs[s.Heading] = s.Text
		}
		out["sections"] = secs
		out["title"] = d.Str("title")
		out["stage"] = d.Str("stage")
	}
	return out
}

func (m *mod) workAt(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if k, _ := ParseName(name); k == NameOther {
		fail(w, http.StatusBadRequest, "That is not a work item.")
		return
	}
	rev, _ := strconv.Atoi(r.URL.Query().Get("rev"))
	it, ok, err := m.st.At(r.Context(), r.PathValue("ws"), name, rev)
	if err != nil {
		workFail(w, err)
		return
	}
	if !ok {
		fail(w, http.StatusNotFound, "There is no such revision.")
		return
	}
	reply(w, http.StatusOK, atResult(it))
}

// ---- Rounds and sign-off ----

func (m *mod) workAddNote(w http.ResponseWriter, r *http.Request) {
	var in Note
	if !decodeLimit(w, r, &in, 128<<10) {
		return
	}
	ws, id := r.PathValue("ws"), r.PathValue("id")
	n, err := m.st.AddNote(r.Context(), ws, id, in)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			workFail(w, err)
			return
		}
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	m.changed(ws, id, "note")
	reply(w, http.StatusOK, n)
}

func (m *mod) workEditNote(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text    *string `json:"text"`
		Replace *string `json:"replace"`
	}
	if !decodeLimit(w, r, &in, 128<<10) {
		return
	}
	ws := r.PathValue("ws")
	if err := m.st.EditNote(r.Context(), ws, r.PathValue("id"), in.Text, in.Replace); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	m.changed(ws, "", "note")
	reply(w, http.StatusOK, map[string]any{"ok": true})
}

// workSendRound sends the item's draft round. The agent has no tool for this: it is the
// owner's click.
func (m *mod) workSendRound(w http.ResponseWriter, r *http.Request) {
	ws, id := r.PathValue("ws"), r.PathValue("id")
	sent, err := m.st.SendRound(r.Context(), ws, id, m.notifier())
	if err != nil {
		failOr400(w, err)
		return
	}
	m.changed(ws, id, "round")
	reply(w, http.StatusOK, sent)
}

// workSignOff signs the item off ({"anyway": true} when the last round has open notes).
// The owner's click only, as the round is.
func (m *mod) workSignOff(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Anyway bool `json:"anyway"`
	}
	if r.ContentLength != 0 && !decodeLimit(w, r, &in, 1<<10) {
		return
	}
	ws, id := r.PathValue("ws"), r.PathValue("id")
	tell := m.notifier()
	signed, err := m.st.SignOff(r.Context(), ws, id, in.Anyway, tell, tell)
	if err != nil {
		var open *ErrOpenNotes
		if errors.As(err, &open) {
			failWith(w, http.StatusConflict, err.Error(), map[string]any{"open": open.Open, "round": open.Round})
			return
		}
		workFail(w, err)
		return
	}
	m.changed(ws, id, "item")
	reply(w, http.StatusOK, signed)
}

func (m *mod) workUnsign(w http.ResponseWriter, r *http.Request) {
	ws, id := r.PathValue("ws"), r.PathValue("id")
	if err := m.st.Unsign(r.Context(), ws, id); err != nil {
		workFail(w, err)
		return
	}
	m.changed(ws, id, "item")
	reply(w, http.StatusOK, map[string]any{"ok": true})
}

// workOwners sets who a workspace's rounds and sign-offs are mailed to.
func (m *mod) workOwners(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Owners []string `json:"owners"`
	}
	if !decodeLimit(w, r, &in, 16<<10) {
		return
	}
	ws := r.PathValue("ws")
	if err := m.st.SetOwners(r.Context(), ws, in.Owners); err != nil {
		failOr400(w, err)
		return
	}
	m.changed(ws, "", "workspace")
	reply(w, http.StatusOK, map[string]any{"ok": true})
}

func (m *mod) workWorkspaceEdit(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title    *string `json:"title"`
		Archived *bool   `json:"archived"`
	}
	if !decodeLimit(w, r, &in, 32<<10) {
		return
	}
	ws := r.PathValue("ws")
	if err := m.st.UpdateWorkspace(r.Context(), ws, in.Title, in.Archived, nil); err != nil {
		workFail(w, err)
		return
	}
	m.changed(ws, "", "workspace")
	reply(w, http.StatusOK, map[string]bool{"updated": true})
}

func (m *mod) workWorkspaceDelete(w http.ResponseWriter, r *http.Request) {
	ws := r.PathValue("ws")
	deleted := true
	if err := m.st.UpdateWorkspace(r.Context(), ws, nil, nil, &deleted); err != nil {
		workFail(w, err)
		return
	}
	m.changed(ws, "", "workspace")
	reply(w, http.StatusOK, map[string]bool{"deleted": true})
}
