package tether

import (
	"context"
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
