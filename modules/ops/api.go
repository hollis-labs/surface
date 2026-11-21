package ops

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/hollis-labs/surface/module"
)

// The routes are the page's, under /api/m/ops/. They are the owner's own actions (a drag, a
// move, a reorder, a seed's apply): no approval is asked. README.md lists them.

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, message string, extra map[string]any) {
	body := map[string]any{"error": message}
	for k, v := range extra {
		body[k] = v
	}
	reply(w, status, body)
}

// failErr answers a store error.
func failErr(w http.ResponseWriter, err error) {
	var c *Conflict
	var t *Taken
	switch {
	case errors.As(err, &c):
		fail(w, http.StatusConflict, c.Error(), map[string]any{"conflict": true, "rev": c.Rev})
	case errors.As(err, &t):
		fail(w, http.StatusConflict, t.Error(), map[string]any{"taken": true})
	case isInput(err):
		fail(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, ErrNotFound):
		fail(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, ErrNoCaster):
		fail(w, http.StatusServiceUnavailable, err.Error(), nil)
	default:
		fail(w, http.StatusBadGateway, err.Error(), nil)
	}
}

// decode reads a JSON body of at most limit bytes, with no unknown keys.
func decode(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		fail(w, http.StatusBadRequest, "Invalid request.", nil)
		return false
	}
	return true
}

// ownerOnly refuses a change from anyone but the owner at the page. The core says who is
// asking: the app's own page is the owner; a request without an Origin (MCP, the HTTP tools,
// the CLI) is a client from outside, a delegate. A delegate uses the ops_* tools, where the
// owner is asked; these routes are the owner's own actions and ask nothing.
func ownerOnly(w http.ResponseWriter, r *http.Request, what string) bool {
	if module.ActorOf(r.Context()).Delegate() {
		fail(w, http.StatusForbidden, what+" is the owner's, from the Operations page; an outside client uses the ops_* tools, which ask the owner", nil)
		return false
	}
	return true
}

// gated wraps a route that changes something in ownerOnly.
func gated(what string, fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ownerOnly(w, r, what) {
			fn(w, r)
		}
	}
}

func (m *mod) routes(h module.Host) {
	handle := func(pattern string, fn http.HandlerFunc) { h.Handle(pattern, fn) }
	handle("GET /fleet", func(w http.ResponseWriter, r *http.Request) {
		result, err := m.fleet(r.Context(), r.URL.Query().Get("host"))
		if err != nil {
			failErr(w, err)
			return
		}
		reply(w, http.StatusOK, result)
	})
	handle("GET /usage", func(w http.ResponseWriter, r *http.Request) {
		result, err := m.usage(r.Context(), r.URL.Query().Get("source"))
		if err != nil {
			failErr(w, err)
			return
		}
		reply(w, http.StatusOK, result)
	})
	handle("GET /board", m.apiBoard)
	handle("GET /activity", m.apiGlobalActivity)
	handle("GET /agents", m.apiAgents)
	handle("POST /workstreams", gated("making a workstream", m.apiCreate))
	handle("GET /workstreams/{id}", m.apiGet)
	handle("PUT /workstreams/{id}", gated("changing a workstream", m.apiUpdate))
	handle("POST /workstreams/{id}/unsettle", gated("unsettling a workstream", m.apiUnsettle))
	handle("POST /workstreams/{id}/orchestration", gated("sending to orchestration", m.apiSendOrchestration))
	handle("POST /workstreams/{id}/settle", gated("settling a workstream", m.apiSettle))
	handle("POST /workstreams/{id}/move", gated("moving a workstream", m.apiMove))
	handle("POST /workstreams/{id}/links", gated("linking", m.apiLink))
	handle("DELETE /workstreams/{id}/links", gated("unlinking", m.apiUnlink))
	handle("GET /workstreams/{id}/activity", m.apiActivity)
	handle("GET /workstreams/{id}/history", m.apiHistory)
	handle("GET /workstreams/{id}/at/{rev}", m.apiAt)
	handle("POST /queue/reorder", gated("reordering the queue", m.apiReorder))
	handle("POST /seed/preview", m.apiSeedPreview) // a path is the owner's only: checked inside
	handle("GET /seed/{plan}", m.apiSeedGet)
	handle("PUT /seed/{plan}", gated("editing a seed preview", m.apiSeedSave))
	handle("POST /seed/{plan}/apply", gated("applying a seed", m.apiSeedApply))
}

// Card is a workstream as the board draws it: the document's fields, and what the cached
// roll-up says (no network on the board).
type Card struct {
	Workstream
	Tasks      string `json:"tasks,omitempty"`
	Last       *Entry `json:"last,omitempty"`
	Suggest    string `json:"suggest,omitempty"`
	SuggestWhy string `json:"suggest_why,omitempty"`
	// NoLiveAgent is the open tasks assigned or claimed that no live agent holds.
	NoLiveAgent []string `json:"no_live_agent,omitempty"`
}

func (m *mod) boardResult(ctx context.Context, force bool, showSettled ...bool) (map[string]any, error) {
	syncErr := m.sync(ctx, force)
	b, err := m.st.All(ctx)
	if err != nil {
		return nil, err
	}
	if len(showSettled) == 0 || !showSettled[0] {
		b = b.unsettled()
	}
	cached := m.st.Cached(ctx)
	cards := make([]Card, 0, len(b.Workstreams))
	for _, w := range b.Workstreams {
		c := Card{Workstream: w}
		if r, ok := cached[w.ID]; ok {
			c.Tasks, c.Last, c.Suggest, c.SuggestWhy = CountsText(r.Counts), r.Last, r.Suggest, r.SuggestWhy
			c.NoLiveAgent = r.NoLiveAgent
		}
		cards = append(cards, c)
	}
	out := map[string]any{"workstreams": cards, "queue": b.Queue, "followups": m.st.Followups(ctx), "stages": Stages}
	if syncErr != nil {
		out["error"] = syncErr.Error()
	}
	return out, nil
}

func (m *mod) apiBoard(w http.ResponseWriter, r *http.Request) {
	out, err := m.boardResult(r.Context(), r.URL.Query().Get("force") == "1", r.URL.Query().Get("show_settled") == "1")
	if err != nil {
		failErr(w, err)
		return
	}
	m.kickRoll()
	reply(w, http.StatusOK, out)
}

func (m *mod) apiCreate(w http.ResponseWriter, r *http.Request) {
	var in NewWorkstream
	if !decode(w, r, &in, 1<<20) {
		return
	}
	got, err := m.st.Create(r.Context(), in, actor(r.Context()))
	if err != nil {
		failErr(w, err)
		return
	}
	m.changed(got.ID, "created")
	reply(w, http.StatusCreated, got)
}

func (m *mod) apiGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_ = m.sync(r.Context(), false)
	got, ok, err := m.st.Get(r.Context(), id)
	if err != nil {
		failErr(w, err)
		return
	}
	if !ok {
		failErr(w, ErrNotFound)
		return
	}
	b, _ := m.st.All(r.Context())
	kids := []map[string]any{}
	for _, c := range b.Descendants(id) {
		if c.Parent == id {
			kids = append(kids, map[string]any{"id": c.ID, "title": c.Title, "stage": c.Stage, "held": c.Held, "children": len(c.Children)})
		}
	}
	var path []map[string]any
	for p, n := got.Parent, 0; p != "" && n < maxDepth; n++ {
		x, ok := b.Find(p)
		if !ok {
			break
		}
		path = append([]map[string]any{{"id": x.ID, "title": x.Title}}, path...)
		p = x.Parent
	}
	reply(w, http.StatusOK, map[string]any{"workstream": got, "children": kids, "path": path, "stages": Stages})
}

func (m *mod) apiUpdate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		BaseRev int `json:"base_rev"`
		Patch
	}
	if !decode(w, r, &in, 1<<20) {
		return
	}
	id := r.PathValue("id")
	got, err := m.st.Update(r.Context(), id, in.BaseRev, in.Patch)
	if err != nil {
		failErr(w, err)
		return
	}
	m.changed(id, "updated")
	reply(w, http.StatusOK, got)
}

func (m *mod) apiMove(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Stage string `json:"stage"`
		Why   string `json:"why"`
	}
	if !decode(w, r, &in, 64<<10) {
		return
	}
	id := r.PathValue("id")
	got, err := m.st.Move(r.Context(), id, in.Stage, in.Why, actor(r.Context()))
	if err != nil && got.ID == "" {
		failErr(w, err)
		return
	}
	m.changed(id, "moved")
	if err != nil {
		fail(w, http.StatusBadGateway, err.Error(), map[string]any{"workstream": got})
		return
	}
	reply(w, http.StatusOK, got)
}

func (m *mod) apiLink(w http.ResponseWriter, r *http.Request) {
	var in Link
	if !decode(w, r, &in, 64<<10) {
		return
	}
	id := r.PathValue("id")
	got, err := m.st.Link(r.Context(), id, in)
	if err != nil {
		failErr(w, err)
		return
	}
	m.changed(id, "linked")
	reply(w, http.StatusOK, got)
}

func (m *mod) apiUnlink(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	q := r.URL.Query()
	got, err := m.st.Unlink(r.Context(), id, q.Get("kind"), q.Get("ref"))
	if err != nil {
		failErr(w, err)
		return
	}
	m.changed(id, "unlinked")
	reply(w, http.StatusOK, got)
}

func (m *mod) apiActivity(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_ = m.sync(r.Context(), false)
	got, err := m.rollup(r.Context(), id, r.URL.Query().Get("refresh") == "1")
	if err != nil {
		failErr(w, err)
		return
	}
	reply(w, http.StatusOK, got)
}

func (m *mod) apiHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	got, ok, err := m.st.Get(r.Context(), id)
	if err != nil || !ok {
		failErr(w, errOr(err, ErrNotFound))
		return
	}
	out := map[string]any{"transitions": got.Transitions, "revisions": []Revision{}, "ref": DocRef(WSName(id)), "settled_at": got.SettledAt, "settled_by": got.SettledBy, "settled_summary": got.SettledSummary}
	if revs, err := m.st.Log(r.Context(), id); err != nil {
		out["error"] = err.Error()
	} else {
		out["revisions"] = revs
	}
	reply(w, http.StatusOK, out)
}

func (m *mod) apiAt(w http.ResponseWriter, r *http.Request) {
	rev, err := strconv.Atoi(r.PathValue("rev"))
	if err != nil || rev < 1 {
		fail(w, http.StatusBadRequest, "Invalid revision.", nil)
		return
	}
	it, ok, err := m.st.At(r.Context(), r.PathValue("id"), rev)
	if err != nil || !ok {
		failErr(w, errOr(err, ErrNotFound))
		return
	}
	reply(w, http.StatusOK, map[string]any{"rev": it.Rev, "content": it.Content})
}

func (m *mod) apiReorder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID     string `json:"id"`
		Before string `json:"before"`
		After  string `json:"after"`
		Dir    string `json:"dir"`
	}
	if !decode(w, r, &in, 64<<10) {
		return
	}
	order, err := m.st.Reorder(r.Context(), in.ID, in.Before, in.After, in.Dir)
	if err != nil {
		failErr(w, err)
		return
	}
	m.changed(in.ID, "reordered")
	reply(w, http.StatusOK, map[string]any{"queue": order})
}

// ---- Seed ----

func (m *mod) preview(ctx context.Context, path, raw string) (SeedPlan, error) {
	var data []byte
	source := "pasted"
	switch {
	case strings.TrimSpace(raw) != "":
		data = []byte(raw)
	case strings.TrimSpace(path) != "":
		b, err := ReadTracker(path)
		if err != nil {
			return SeedPlan{}, err
		}
		data, source = b, "file "+filepathBase(path)
	default:
		return SeedPlan{}, bad("give the tracker: upload or paste its JSON, or give its path")
	}
	t, err := ParseTracker(data)
	if err != nil {
		return SeedPlan{}, err
	}
	_ = m.sync(ctx, true)
	b, err := m.st.All(ctx)
	if err != nil {
		return SeedPlan{}, err
	}
	have := map[string]bool{}
	for _, w := range b.Workstreams {
		have[w.ID] = true
	}
	return m.st.NewPlan(ctx, Preview(t, source, have))
}

func filepathBase(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func (m *mod) apiSeedPreview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path string `json:"path"`
		JSON string `json:"json"`
	}
	if !decode(w, r, &in, maxTracker+1<<10) {
		return
	}
	// A pasted path is a path; anything that starts like JSON is JSON.
	if t := strings.TrimSpace(in.JSON); t != "" && !strings.HasPrefix(t, "{") && in.Path == "" {
		in.Path, in.JSON = t, ""
	}
	// Reading a file on this machine is the owner's, from the page.
	if strings.TrimSpace(in.Path) != "" && !ownerOnly(w, r, "reading the tracker from a path") {
		return
	}
	p, err := m.preview(r.Context(), in.Path, in.JSON)
	if err != nil {
		failErr(w, err)
		return
	}
	reply(w, http.StatusOK, p)
}

func (m *mod) apiSeedGet(w http.ResponseWriter, r *http.Request) {
	p, ok, err := m.st.Plan(r.Context(), r.PathValue("plan"))
	if err != nil || !ok {
		failErr(w, errOr(err, bad("there is no such seed preview")))
		return
	}
	reply(w, http.StatusOK, p)
}

func (m *mod) apiSeedSave(w http.ResponseWriter, r *http.Request) {
	var p SeedPlan
	if !decode(w, r, &p, maxTracker+1<<10) {
		return
	}
	have, ok, err := m.st.Plan(r.Context(), r.PathValue("plan"))
	if err != nil || !ok {
		failErr(w, errOr(err, bad("there is no such seed preview")))
		return
	}
	p.ID, p.CreatedAt, p.Source = have.ID, have.CreatedAt, have.Source
	if err := m.st.SavePlan(r.Context(), p); err != nil {
		if errors.Is(err, ErrApplied) {
			fail(w, http.StatusConflict, err.Error(), nil)
			return
		}
		failErr(w, err)
		return
	}
	reply(w, http.StatusOK, p)
}

// apiSeedApply applies the preview as it is kept (the page saves the owner's edits first):
// never a plan from the request. The preview is claimed first, so it applies once; an apply
// that wrote nothing gives the claim back.
func (m *mod) apiSeedApply(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("plan")
	if _, ok, err := m.st.Plan(r.Context(), id); err != nil || !ok {
		failErr(w, errOr(err, bad("there is no such seed preview")))
		return
	}
	if err := m.st.ClaimPlan(r.Context(), id); err != nil {
		if errors.Is(err, ErrApplied) {
			fail(w, http.StatusConflict, err.Error(), nil)
			return
		}
		failErr(w, err)
		return
	}
	p, _, err := m.st.Plan(r.Context(), id)
	if err != nil {
		_ = m.st.ReleasePlan(r.Context(), id)
		failErr(w, err)
		return
	}
	res, err := m.st.Apply(r.Context(), p, actor(r.Context())+" (seed)")
	if len(res.Created) == 0 && len(res.Noted) == 0 && res.Followups == 0 {
		// Nothing was written, whatever went wrong: the preview can be applied again.
		_ = m.st.ReleasePlan(r.Context(), id)
		if err == nil && len(res.Failed) > 0 {
			fail(w, http.StatusBadGateway, "nothing was written: "+strings.Join(res.Failed, "; "), map[string]any{"result": res})
			return
		}
	}
	if err != nil {
		failErr(w, err)
		return
	}
	m.changed("", "seeded")
	reply(w, http.StatusOK, res)
}

func errOr(err, other error) error {
	if err != nil {
		return err
	}
	return other
}
