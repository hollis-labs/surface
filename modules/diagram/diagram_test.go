package diagram

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hollis-labs/surface/module/moduletest"
)

const flow = `{"diagram":"v1","nodes":[{"id":"api","label":"API","kind":"service"},{"id":"db","label":"Store","kind":"store"}],"edges":[{"from":"api","to":"db","label":"reads"}],"groups":[]}`

func start(t *testing.T) (*moduletest.Host, *mod) {
	t.Helper()
	m := &mod{}
	return moduletest.Start(t, m), m
}

// Every save is a revision; get returns the current body and the list, and an older revision on
// request; a save from a stale revision is refused.
func TestRevisions(t *testing.T) {
	h, m := start(t)
	ctx := context.Background()
	out, err := h.Tool("diagram_put", `{"title":"Flow","body":`+flow+`}`)
	if err != nil {
		t.Fatal(err)
	}
	id := out.(map[string]any)["id"].(string)
	if out.(map[string]any)["rev"] != 1 {
		t.Fatalf("created at %v", out)
	}
	two := strings.Replace(flow, `"label":"reads"`, `"label":"reads and writes"`, 1)
	if _, err = h.Tool("diagram_put", `{"id":"`+id+`","body":`+jsonString(two)+`,"base_rev":1}`); err != nil {
		t.Fatal(err)
	}
	if _, err = h.Tool("diagram_put", `{"id":"`+id+`","body":`+flow+`,"base_rev":1}`); err == nil || !strings.Contains(err.Error(), "changed since") {
		t.Fatalf("a stale save was taken: %v", err)
	}
	got, err := m.read(ctx, id, 0)
	if err != nil {
		t.Fatal(err)
	}
	d := got["diagram"].(Diagram)
	revs := got["revisions"].([]Revision)
	if d.Rev != 2 || !strings.Contains(d.Body, "reads and writes") || len(revs) != 2 || revs[0].N != 2 {
		t.Fatalf("current: %+v %+v", d, revs)
	}
	old, err := m.read(ctx, id, 1)
	if err != nil || strings.Contains(old["diagram"].(Diagram).Body, "reads and writes") || old["shown_rev"] != 1 {
		t.Fatalf("revision 1: %+v %v", old, err)
	}
	// The routes say the same.
	if code, body := h.Do("GET", "/diagrams/"+id+"/revisions/1", ""); code != 200 || !strings.Contains(body, `"n":1`) {
		t.Fatalf("revision route: %d %s", code, body)
	}
	if code, body := h.Do("PUT", "/diagrams/"+id, `{"body":`+jsonString(flow)+`,"rev":1}`); code != 409 {
		t.Fatalf("stale save over the route: %d %s", code, body)
	}
	if code, body := h.Do("PUT", "/diagrams/"+id, `{"title":"Renamed"}`); code != 200 || !strings.Contains(body, `"rev":2`) {
		t.Fatalf("rename made a revision: %d %s", code, body)
	}
	if code, _ := h.Do("DELETE", "/diagrams/"+id, ""); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	if code, _ := h.Do("GET", "/diagrams/"+id, ""); code != 404 {
		t.Fatalf("deleted diagram still read: %d", code)
	}
}

// What would not draw is refused, saying why.
func TestPutValidates(t *testing.T) {
	h, _ := start(t)
	for body, want := range map[string]string{
		`"not json"`:                     "not a JSON object",
		`[1,2]`:                          "not a JSON object",
		`{"diagram":"v2"}`:               `"v1"`,
		`{"nodes":{"a":1}}`:              "list of objects",
		`{"nodes":[{"kind":"service"}]}`: "no id or label",
		`{"nodes":[{"id":"a"}],"edges":[{"from":"a","to":"b"}]}`: "edges must join",
	} {
		if _, err := h.Tool("diagram_put", `{"body":`+body+`}`); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("body %s: %v, want %q", body, err, want)
		}
	}
	// A string body, and an empty one, are taken.
	if _, err := h.Tool("diagram_put", `{"body":`+jsonString(flow)+`}`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Tool("diagram_put", `{"body":""}`); err != nil {
		t.Fatal(err)
	}
	if out, _ := h.Tool("diagram_list", `{}`); len(out.([]Diagram)) != 2 {
		t.Fatalf("list: %v", out)
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
