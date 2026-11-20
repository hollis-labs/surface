package registry

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
	"github.com/hollis-labs/surface/module/tetherlink"
)

// fixture is a Registry in the shapes the gateway's registry tools return (testdata/registry.json:
// the live kinds, diagnostics and project/2 schema, and records written in the Registry's
// record shape).
type fixture struct {
	Kinds       json.RawMessage            `json:"kinds"`
	Diagnostics json.RawMessage            `json:"diagnostics"`
	Schemas     map[string]json.RawMessage `json:"schemas"`
	Records     []map[string]any           `json:"records"`
	History     map[string][]map[string]any `json:"history"`
	Published   []string                   `json:"published"`
}

// fake is the gateway: it answers the Registry's read tools from the fixture and records each call.
type fake struct {
	fx    fixture
	mu    sync.Mutex
	calls []string
	args  []map[string]any
	down  bool
}

func refused(code string) error {
	return &tetherlink.RefusedError{Detail: `{"code":"` + code + `","message":"x"}`}
}

func (f *fake) Call(_ context.Context, tool string, args map[string]any) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, tool)
	f.args = append(f.args, args)
	f.mu.Unlock()
	if !allowed[tool] {
		return "", errors.New("not allowed: " + tool)
	}
	if f.down {
		return "", errors.New("tether: the MCP endpoint cannot be reached")
	}
	out := func(v any) (string, error) {
		b, err := json.Marshal(v)
		return string(b), err
	}
	published := func(id string) bool {
		for _, p := range f.fx.Published {
			if p == id {
				return true
			}
		}
		return false
	}
	find := func(id, pub string) map[string]any {
		for _, r := range f.fx.Records {
			if r["id"] == id && (pub != "published" || published(id)) {
				return r
			}
		}
		return nil
	}
	switch tool {
	case "registry_kinds_list":
		return string(f.fx.Kinds), nil
	case "registry_diagnostics":
		return string(f.fx.Diagnostics), nil
	case "registry_schema_get":
		s, ok := f.fx.Schemas[args["schema"].(string)]
		if !ok {
			return "", refused("unknown_schema")
		}
		return string(s), nil
	case "registry_record_list", "registry_record_search", "registry_review_list":
		q, _ := args["query"].(map[string]any)
		get := func(k string) string { s, _ := q[k].(string); return s }
		var recs []map[string]any
		for _, r := range f.fx.Records {
			if tool == "registry_review_list" {
				rv, _ := r["review"].(map[string]any)
				if r["kind"] != "tool" || rv == nil || rv["queue_state"] != "pending" {
					continue
				}
			} else if get("publication") == "published" && !published(r["id"].(string)) {
				continue
			}
			if k := get("kind"); k != "" && r["kind"] != k {
				continue
			}
			if l := get("lifecycle"); l != "" && r["lifecycle"] != l {
				continue
			}
			if o := get("owner"); o != "" && r["owner"] != o {
				continue
			}
			if s := strings.ToLower(get("search")); s != "" {
				if !strings.Contains(strings.ToLower(r["name"].(string)+" "+r["summary"].(string)), s) {
					continue
				}
			}
			c := map[string]any{}
			for k, v := range r {
				if k != "payload" {
					c[k] = v
				}
			}
			recs = append(recs, c)
		}
		return out(map[string]any{"records": recs, "has_more": false, "boundary": 9, "as_of": "2026-11-20T10:00:00Z"})
	case "registry_record_get":
		r := find(args["id"].(string), args["publication"].(string))
		if r == nil {
			return "", refused("not_found")
		}
		return out(r)
	case "registry_relation_list":
		id := args["id"].(string)
		var edges []any
		for _, r := range f.fx.Records {
			es, _ := r["edges"].([]any)
			for _, e := range es {
				to := e.(map[string]any)["relation"].(map[string]any)["to"].(map[string]any)
				if to["id"] == id {
					edges = append(edges, e)
				}
			}
		}
		return out(map[string]any{"id": id, "version_token": "vt", "edges": edges})
	case "registry_revision_list":
		id := args["id"].(string)
		h, ok := f.fx.History[id]
		if !ok {
			r := find(id, "draft")
			if r == nil {
				return "", refused("not_found")
			}
			h = []map[string]any{r}
		}
		return out(map[string]any{"revisions": h, "has_more": false})
	case "registry_revision_get":
		id, rev := args["id"].(string), args["revision"].(string)
		for _, r := range f.fx.History[id] {
			if r["revision_id"] == rev {
				return out(r)
			}
		}
		return "", refused("not_found")
	}
	return "", errors.New("unexpected tool " + tool)
}

func (f *fake) called(tool string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == tool {
			n++
		}
	}
	return n
}

func start(t *testing.T) (*moduletest.Host, *mod, *fake) {
	t.Helper()
	b, err := os.ReadFile("testdata/registry.json")
	if err != nil {
		t.Fatal(err)
	}
	f := &fake{}
	if err := json.Unmarshal(b, &f.fx); err != nil {
		t.Fatal(err)
	}
	m := &mod{caller: f}
	return moduletest.Start(t, m), m, f
}

func get[T any](t *testing.T, h *moduletest.Host, p string) T {
	t.Helper()
	code, body := h.Do("GET", p, "")
	if code != 200 {
		t.Fatalf("GET %s: %d %s", p, code, body)
	}
	var v T
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("GET %s: %v in %s", p, err, body)
	}
	return v
}

func TestOverviewCountsKindsAndQueue(t *testing.T) {
	h, _, _ := start(t)
	o := get[Overview](t, h, "/overview")
	if o.Publication != "draft" {
		t.Fatalf("default publication %q, want the working copy", o.Publication)
	}
	counts := map[string]int{}
	for _, k := range o.Kinds {
		counts[k.Kind] = k.Count
		if k.Kind == "relation" || k.Kind == "classification" {
			t.Fatalf("%s is a value schema, not a record kind", k.Kind)
		}
	}
	if counts["project"] != 3 || counts["tool"] != 2 || counts["agent"] != 1 || counts["workstream"] != 0 {
		t.Fatalf("counts %v", counts)
	}
	if _, ok := counts["mcp_server"]; !ok {
		t.Fatalf("a kind with no records is still listed: %v", counts)
	}
	if o.Total != 9 || len(o.Reviews) != 2 || o.Reviews[0].Review == nil || o.Reviews[0].Review.QueueState != "pending" {
		t.Fatalf("total %d reviews %+v", o.Total, o.Reviews)
	}
	if !strings.Contains(string(o.Diagnostics), `"ready"`) || !strings.Contains(string(o.Edges), "owned_by") {
		t.Fatalf("diagnostics %s edges %s", o.Diagnostics, o.Edges)
	}
	pub := get[Overview](t, h, "/overview?publication=published")
	if pub.Total != 3 {
		t.Fatalf("published total %d, want 3", pub.Total)
	}
}

func TestOverviewDownIsAnError(t *testing.T) {
	h, _, f := start(t)
	f.down = true
	if code, body := h.Do("GET", "/overview", ""); code != 502 || !strings.Contains(body, "cannot be reached") {
		t.Fatalf("%d %s", code, body)
	}
}

func TestNotConfigured(t *testing.T) {
	m := &mod{}
	h := moduletest.Start(t, m)
	code, body := h.Do("GET", "/overview", "")
	if code != 503 || !strings.Contains(body, "registry.tether_command") {
		t.Fatalf("%d %s", code, body)
	}
	if m.Manifest().Refs[0].Ready(context.Background(), h) {
		t.Fatal("ready with no way to reach the gateway")
	}
}

func TestRecordsFilterAndSearch(t *testing.T) {
	h, _, f := start(t)
	p := get[Page](t, h, "/records?kind=project&lifecycle=active")
	if len(p.Records) != 2 {
		t.Fatalf("active projects %+v", p.Records)
	}
	p = get[Page](t, h, "/records?kind=project&owner="+url.QueryEscape("agent:archie"))
	if len(p.Records) != 1 || p.Records[0].ID != "project:frag" {
		t.Fatalf("by owner %+v", p.Records)
	}
	p = get[Page](t, h, "/records?kind=project")
	for _, r := range p.Records {
		if r.ID == "project:surface" && (r.Edges != 2 || r.Proposed != 1 || r.Owner != "user:owner") {
			t.Fatalf("brief %+v", r)
		}
	}
	p = get[Page](t, h, "/records?q=restore")
	if len(p.Records) != 1 || p.Records[0].ID != "document:guide/operations" || f.called("registry_record_search") != 1 {
		t.Fatalf("search %+v", p.Records)
	}
	if code, _ := h.Do("GET", "/records?kind=Bad%20Kind", ""); code != 400 {
		t.Fatalf("a malformed kind: %d", code)
	}
}

func TestRecordDetail(t *testing.T) {
	h, m, _ := start(t)
	d := get[Detail](t, h, "/record?id="+url.QueryEscape("project:tether"))
	if d.Brief.Name != "Tether" || d.Publication != "draft" {
		t.Fatalf("detail %+v", d.Brief)
	}
	// Two records point at Tether: Surface and Frag consume it.
	if len(d.Incoming) != 2 || len(d.Outgoing) != 1 || d.Outgoing[0].Relation.Type != "owned_by" {
		t.Fatalf("incoming %d outgoing %+v", len(d.Incoming), d.Outgoing)
	}
	if len(d.Revisions) != 1 {
		t.Fatalf("revisions %+v", d.Revisions)
	}
	// A record that is not published, asked for published, is shown as its working copy.
	d = get[Detail](t, h, "/record?publication=published&id="+url.QueryEscape("registry:agent:archie"))
	if d.Publication != "draft" || d.Brief.ID != "agent:archie" {
		t.Fatalf("fallback %+v", d)
	}
	if _, ok := m.seen["agent:archie"]; !ok {
		t.Fatal("the record read is not remembered for the agent")
	}
	if code, _ := h.Do("GET", "/record?id="+url.QueryEscape("project:nope"), ""); code != 404 {
		t.Fatalf("missing: %d", code)
	}
	if code, _ := h.Do("GET", "/record?id=no-colon", ""); code != 400 {
		t.Fatalf("malformed: %d", code)
	}
}

func TestRevisionDiff(t *testing.T) {
	h, _, _ := start(t)
	d := get[Detail](t, h, "/record?id="+url.QueryEscape("project:surface"))
	if len(d.Revisions) != 2 {
		t.Fatalf("history %+v", d.Revisions)
	}
	changes := get[[]Change](t, h, "/diff?id="+url.QueryEscape("project:surface")+"&from="+d.Revisions[1].RevisionID+"&to="+d.Revisions[0].RevisionID)
	by := map[string]Change{}
	for _, c := range changes {
		by[c.Path] = c
		if c.Path == "revision_id" || c.Path == "version_token" || c.Path == "sequence" || c.Path == "updated_at" {
			t.Fatalf("noise in the diff: %+v", c)
		}
	}
	if c := by["lifecycle"]; c.Op != "changed" || c.Before != "experimental" || c.After != "active" {
		t.Fatalf("lifecycle %+v", c)
	}
	if c := by["payload.interfaces[2].type"]; c.Op != "added" || c.After != "cli" {
		t.Fatalf("added interface %+v in %+v", c, changes)
	}
	if c := by["edges[1].relation.type"]; c.Op != "added" {
		t.Fatalf("added edge %+v", c)
	}
}

func TestSchema(t *testing.T) {
	h, _, _ := start(t)
	s := get[map[string]any](t, h, "/schema?schema=project/2")
	if _, ok := s["properties"].(map[string]any)["owner"]; !ok {
		t.Fatalf("schema %v", s)
	}
	if code, _ := h.Do("GET", "/schema?schema=nope", ""); code != 400 {
		t.Fatalf("malformed schema: %d", code)
	}
	if code, body := h.Do("GET", "/schema?schema=agent/9", ""); code != 502 || !strings.Contains(body, "unknown_schema") {
		t.Fatalf("unknown schema: %d %s", code, body)
	}
}

func TestOnlyReadToolsAreAllowed(t *testing.T) {
	for name := range allowed {
		for _, w := range []string{"create", "update", "delete", "archive", "verify", "decide", "propose", "remove"} {
			if strings.Contains(name, w) {
				t.Fatalf("%s is a write", name)
			}
		}
	}
}

func TestCite(t *testing.T) {
	h, m, f := start(t)
	k := m.Manifest().Refs[0]
	if k.Kind != "registry.record" || !module.ValidRefKind(k.Kind) {
		t.Fatalf("kind %q", k.Kind)
	}
	for _, id := range []string{"project:surface", "tool:mcp/tether/torque_task_get", "document:guide/operations"} {
		if !k.ID.MatchString(id) {
			t.Fatalf("%s should be a Registry id", id)
		}
	}
	for _, id := range []string{"surface", "project: surface", "CW-20261118-0026"} {
		if k.ID.MatchString(id) {
			t.Fatalf("%s should not be a Registry id", id)
		}
	}
	got, err := k.Resolve(context.Background(), h, []string{"project:surface", "agent:archie", "project:nope"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("resolved %v", got)
	}
	s := got["project:surface"]
	if s.ChipLabel != "project: Surface" || s.Status == nil || s.Status.Tone != "success" || s.Open == nil || s.Open.Command != "registry.open" {
		t.Fatalf("entity %+v", s)
	}
	if got["agent:archie"].Body != "" {
		t.Fatal("a resolve carries no body")
	}
	rec, err := k.Record(context.Background(), h, "agent:archie")
	if err != nil || !strings.Contains(rec.Body, `"harness"`) {
		t.Fatalf("record %v %+v", err, rec)
	}
	if _, err := k.Record(context.Background(), h, "project:nope"); !errors.Is(err, module.ErrRefNotFound) {
		t.Fatalf("missing record: %v", err)
	}
	f.down = true
	if _, err := k.Resolve(context.Background(), h, []string{"project:surface"}); err == nil {
		t.Fatal("a gateway that is down is an error, not not-found")
	}
}

func TestOpenCommandAndWindowContext(t *testing.T) {
	h, m, _ := start(t)
	ctx := context.Background()
	man := m.Manifest()
	var open module.Command
	for _, c := range man.Commands {
		if c.ID == "registry.open" {
			open = c
		}
	}
	if _, err := open.Run(ctx, h, map[string]any{"id": "registry:project:surface"}); err != nil {
		t.Fatal(err)
	}
	w, ok := h.DeskState.Focused(ctx)
	if !ok || w.View != "registry" || w.Args["id"] != "project:surface" || w.Args["kind"] != "project" {
		t.Fatalf("window %+v", w)
	}
	if _, err := open.Run(ctx, h, map[string]any{"id": "not an id"}); err == nil {
		t.Fatal("a malformed id opens nothing")
	}

	// Before the window read the record, the agent is told to read it.
	wc, err := man.Views[0].Describe(ctx, h, w)
	if err != nil || wc.Kind != "record" || wc.Ref != "registry:project:surface" {
		t.Fatalf("context %+v %v", wc, err)
	}
	wc.Module = "registry"
	k, _ := m.knowledge(ctx, h, module.Situation{Focused: &wc})
	if !strings.Contains(k, "registry_record_get") {
		t.Fatalf("knowledge before reading: %s", k)
	}

	// The window reads a tool waiting for review: the agent learns what a decision needs.
	get[Detail](t, h, "/record?id="+url.QueryEscape("tool:mcp/tether/torque_task_create"))
	w.Args = map[string]any{"kind": "tool", "id": "tool:mcp/tether/torque_task_create"}
	wc, _ = man.Views[0].Describe(ctx, h, w)
	wc.Module = "registry"
	k, _ = m.knowledge(ctx, h, module.Situation{Focused: &wc})
	for _, want := range []string{"tool:mcp/tether/torque_task_create", "vt-7", "sha256:bb22", "registry_review_decide", "owner's word"} {
		if !strings.Contains(k, want) {
			t.Fatalf("knowledge lacks %q:\n%s", want, k)
		}
	}
	if b, ok := wc.Extra["record"].(brief); !ok || b.VersionToken == "" {
		t.Fatalf("extra %+v", wc.Extra)
	}

	// Another module's window in front: nothing.
	if k, _ := m.knowledge(ctx, h, module.Situation{Focused: &module.WindowContext{Module: "notes"}}); k != "" {
		t.Fatalf("knowledge for another window: %q", k)
	}
}

func TestSlot(t *testing.T) {
	_, m, _ := start(t)
	s := m.Manifest().Slots[0]
	if s.Name != "registry" || len(s.Views) != 1 || s.Views[0] != "registry" {
		t.Fatalf("slot %+v", s)
	}
	for _, want := range []string{"registry_record_get", "registry_review_decide", "registry_record_update"} {
		found := false
		for _, n := range s.Tools {
			found = found || n == want
		}
		if !found {
			t.Fatalf("slot tools lack %s", want)
		}
	}
	if !strings.Contains(s.Instructions, "owner's word") || !strings.Contains(s.Where, "registry_*") {
		t.Fatalf("slot text %+v", s)
	}
}
