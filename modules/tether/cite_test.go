package tether

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
	"github.com/hollis-labs/surface/module/tetherlink"
)

// gateway answers each tool from a table keyed by tool and the call's id argument.
type gateway struct {
	mu      sync.Mutex
	answers map[string]string // "<tool> <id>" -> JSON
	calls   []string
	down    bool
}

func (g *gateway) Call(_ context.Context, tool string, args map[string]any) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, tool)
	if g.down {
		return "", errors.New("gateway unreachable")
	}
	if !citeAllowed[tool] {
		return "", errors.New("cite called a tool outside its read list: " + tool)
	}
	id := ""
	for _, k := range []string{"id", "item_id", "urn", "message_id", "ref", "key"} {
		if v, ok := args[k].(string); ok {
			id = v
			break
		}
	}
	if a, ok := g.answers[tool+" "+id]; ok {
		return a, nil
	}
	return "", &tetherlink.RefusedError{Detail: "not_found: no such record"}
}

func (g *gateway) List(context.Context) ([]tetherlink.ToolInfo, error) { return nil, nil }

func refKind(t *testing.T, h *moduletest.Host, kind string) module.RefKind {
	t.Helper()
	for _, r := range h.Manifest().Refs {
		if r.Kind == kind {
			return r
		}
	}
	t.Fatalf("no ref kind %s", kind)
	return module.RefKind{}
}

func TestEachKindResolvesThroughItsReadTool(t *testing.T) {
	g := &gateway{answers: map[string]string{
		"torque_task_get CW-20261119-0064":                            `{"data":{"id":"CW-20261119-0064","title":"Port Cite","status":"in_progress","priority":2,"description":"Chips everywhere.","project_id":"PRJ-20260101-0001","tags":[{"name":"surface"}]}}`,
		"torque_project_get PRJ-20260101-0001":                        `{"data":{"name":"Surface","status":"active","description":"The ADE framework."}}`,
		"tesseract_history 01M7T1BJ53BCQWTPC844XEEN4Q":                `{"results":[{"item_id":"01M7T1BJ53BCQWTPC844XEEN4Q","domain":"knowledge","namespace":"project/surface/knowledge/designs","key":"cite","status":"active","payload":{"summary":"How cite works.","body":"# Cite design\n\nDetails."}}],"manifest":{}}`,
		"tesseract_history cite":                                      `[{"domain":"knowledge","namespace":"project/surface/knowledge/designs","key":"cite","payload":{"body":"# Cite design"}}]`,
		"tether_registry_lookup msg://agent/agent-mux/agt_example001": `{"display_name":"Builder","kind":"agent","status":"active"}`,
		"tether_message_trace 0b8c7a0e-1111-4222-8333-444455556666":   `{"message":{"from":"msg://agent/a","to":"msg://agent/b","subject":"Hello","state":"delivered"}}`,
		"caster_get caster://w-portfolio/i.ab12cd34":                  `{"content":"---\ntitle: x\n---\n# Plan item\n\nBody","title":"Plan item","revision":3}`,
	}}
	h := moduletest.Start(t, &mod{link: g})
	ctx := context.Background()

	cases := []struct{ kind, id, title string }{
		{"torque.task", "CW-20261119-0064", "Port Cite"},
		{"torque.project", "PRJ-20260101-0001", "Surface"},
		{"tesseract.item", "01M7T1BJ53BCQWTPC844XEEN4Q", "Cite design"},
		{"tesseract.key", "project/surface/knowledge/designs#cite", "Cite design"},
		{"tether.agent", "msg://agent/agent-mux/agt_example001", "Builder"},
		{"tether.message", "0b8c7a0e-1111-4222-8333-444455556666", "Hello"},
		{"caster.doc", "caster://w-portfolio/i.ab12cd34", "Plan item"},
	}
	for _, c := range cases {
		r := refKind(t, h, c.kind)
		if !r.ID.MatchString(c.id) {
			t.Fatalf("%s: id %s does not match the kind", c.kind, c.id)
		}
		got, err := r.Resolve(ctx, h, []string{c.id})
		if err != nil {
			t.Fatalf("%s: %v", c.kind, err)
		}
		if got[c.id].Title != c.title {
			t.Fatalf("%s: title %q, want %q", c.kind, got[c.id].Title, c.title)
		}
		if got[c.id].Body != "" {
			t.Fatalf("%s: a preview carries a body", c.kind)
		}
	}

	// Batched: found and missing together; the missing one is left out, not an error.
	task := refKind(t, h, "torque.task")
	got, err := task.Resolve(ctx, h, []string{"CW-20261119-0064", "CW-20990101-0001"})
	if err != nil || len(got) != 1 || got["CW-20261119-0064"].Status.Tone != "info" {
		t.Fatalf("batch: %v %+v", err, got)
	}
	rec, err := task.Record(ctx, h, "CW-20261119-0064")
	if err != nil || !strings.Contains(rec.Body, "Chips everywhere.") {
		t.Fatalf("record: %v %+v", err, rec)
	}
	if _, err := task.Record(ctx, h, "CW-20990101-0001"); !errors.Is(err, module.ErrRefNotFound) {
		t.Fatalf("missing record: %v", err)
	}

	// The gateway down: an error, not "not found".
	g.mu.Lock()
	g.down = true
	g.mu.Unlock()
	if _, err := task.Resolve(ctx, h, []string{"CW-20261119-0064"}); err == nil {
		t.Fatal("a failed gateway read as not found")
	}

	// Only read tools were ever called, and never tesseract_get (it counts as a use).
	for _, c := range g.calls {
		if !citeAllowed[c] || c == "tesseract_get" {
			t.Fatalf("called %s", c)
		}
	}
}

func TestCiteIsNotReadyWithoutAGateway(t *testing.T) {
	h := moduletest.Start(t, &mod{})
	r := refKind(t, h, "torque.task")
	if r.Ready(context.Background(), h) {
		t.Fatal("ready with no gateway configured")
	}
}

// caster answers caster_get with one document, cut at max_bytes as Caster does.
type caster struct {
	content string
	reads   []int
}

func (c *caster) Call(_ context.Context, tool string, args map[string]any) (string, error) {
	if tool != "caster_get" {
		return "", errors.New("unexpected tool " + tool)
	}
	limit, _ := args["max_bytes"].(int)
	c.reads = append(c.reads, limit)
	content, cut := c.content, false
	if limit > 0 && len(content) > limit {
		content, cut = content[:limit], true
	}
	b, _ := json.Marshal(map[string]any{"content": content, "truncated": cut, "revision": 1, "title": "Evidence", "state": "resolved"})
	return string(b), nil
}

func (c *caster) List(context.Context) ([]tetherlink.ToolInfo, error) { return nil, nil }

func TestCasterJSONDocumentIsSummarisedNotExcerpted(t *testing.T) {
	long := strings.Repeat("A line of evidence.\n", 400) // past the preview's first read
	doc, _ := json.MarshalIndent(map[string]any{
		"status": "candidate evidence only",
		"messages": []any{
			map[string]any{"id": "m1", "subject": "First", "body": long},
			map[string]any{"id": "m2", "subject": "Second", "body": "short"},
			map[string]any{"id": "m3", "subject": "Third", "body": "short"},
		},
	}, "", "  ")
	c := &caster{content: string(doc)}
	h := moduletest.Start(t, &mod{link: c})
	r := refKind(t, h, "caster.doc")
	ref := "caster://chief/0078-evidence"
	got, err := r.Resolve(context.Background(), h, []string{ref})
	if err != nil {
		t.Fatal(err)
	}
	e := got[ref]
	if e.Description != "candidate evidence only\n3 messages" {
		t.Fatalf("description %q", e.Description)
	}
	if strings.Contains(e.Description, "{") {
		t.Fatal("the preview shows JSON text")
	}
	if len(c.reads) != 2 || c.reads[0] != casterPreviewBytes || c.reads[1] != casterMaxBytes {
		t.Fatalf("reads %v: a JSON preview cut short is read again whole", c.reads)
	}
	for _, f := range e.Fields {
		if f.Label == "Ref" {
			t.Fatal("the ref is repeated as a field; the card shows it under the title")
		}
	}
	rec, err := r.Record(context.Background(), h, ref)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal([]byte(rec.Body), &back); err != nil {
		t.Fatalf("the record's body is not the JSON document: %v", err)
	}

	// Markdown keeps its excerpt, front matter left out.
	c.content, c.reads = "---\ntitle: x\n---\n# Plan\n\nThe body.", nil
	got, _ = r.Resolve(context.Background(), h, []string{ref})
	if d := got[ref].Description; d != "# Plan\n\nThe body." {
		t.Fatalf("markdown description %q", d)
	}

	// JSON cut short even at the record's size: shown as code, never as broken JSON.
	c.content = `{"messages":[` + strings.Repeat(`"x",`, casterMaxBytes/4) + `"y"]}`
	rec, _ = r.Record(context.Background(), h, ref)
	if !strings.HasPrefix(rec.Body, "```json\n") {
		t.Fatalf("cut JSON body starts %q", rec.Body[:20])
	}
}

func TestSummaryOfJSON(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`{"summary":"What it is.\n\nMore.","items":[1],"tags":["a","b"]}`, "What it is.\n1 item · 2 tags"},
		{`{"a":1,"b":2}`, "2 fields: a, b"},
		{`[{"x":1},{"x":2}]`, "2 items"},
		{`{"open_items":[]}`, "0 open items"},
	}
	for _, c := range cases {
		v, ok := parseJSONDoc(c.in)
		if !ok {
			t.Fatalf("%s did not parse", c.in)
		}
		if got := summaryOfJSON(v); got != c.want {
			t.Fatalf("%s: %q, want %q", c.in, got, c.want)
		}
	}
	if _, ok := parseJSONDoc("# Not JSON {"); ok {
		t.Fatal("markdown parsed as JSON")
	}
}
