package diagram

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/hollis-labs/surface/module/collab/collabtest"
	"github.com/hollis-labs/surface/module/moduletest"
)

// With new_in caster, a diagram is a collab/v1 document in Caster: listed beside the app's own,
// saved as revisions there, refused when it moved on, hidden (not erased) on delete. A diagram
// Basalisk wrote is read the same way.
func TestDiagramsInCaster(t *testing.T) {
	fake := collabtest.NewFake()
	m := &mod{link: fake}
	h := moduletest.Start(t, m)
	ctx := context.Background()

	local, err := h.Tool("diagram_put", `{"title":"Local one","body":`+flow+`}`)
	if err != nil {
		t.Fatal(err)
	}
	if id := local.(map[string]any)["id"].(string); !strings.HasPrefix(id, "dg_") {
		t.Fatalf("by default a new diagram is the app's own: %s", id)
	}

	if err = h.Settings.Set(ctx, "new_in", "caster"); err != nil {
		t.Fatal(err)
	}
	out, err := h.Tool("diagram_put", `{"title":"System Map","body":`+flow+`}`)
	if err != nil {
		t.Fatal(err)
	}
	id := out.(map[string]any)["id"].(string)
	if id != "diagram.system-map" || out.(map[string]any)["rev"] != 1 {
		t.Fatalf("created %v", out)
	}
	content, rev := fake.Content("collab/diagram.system-map")
	if rev != 1 || !strings.HasPrefix(content, "---\n") || !strings.Contains(content, "collab: v1") || !strings.Contains(content, `"label":"API"`) {
		t.Fatalf("not a collab/v1 document in Caster: %q", content)
	}

	// Basalisk saves a revision meanwhile: a save from revision 1 is refused, as Basalisk's is.
	d, _ := m.st.get(ctx, id)
	fake.Put("collab/diagram.system-map", strings.Replace(content, `"reads"`, `"reads (Basalisk)"`, 1), "System Map", "")
	if _, err = h.Tool("diagram_put", `{"id":"`+id+`","body":`+flow+`,"base_rev":`+strconv.Itoa(d.Rev)+`}`); err == nil || !strings.Contains(err.Error(), "changed since") {
		t.Fatalf("a stale save was taken: %v", err)
	}
	if code, _ := h.Do("PUT", "/diagrams/"+id, `{"body":`+jsonString(flow)+`,"rev":1}`); code != 409 {
		t.Fatalf("stale save over the route: %d", code)
	}
	got, err := m.read(ctx, id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if cur := got["diagram"].(Diagram); cur.Rev != 2 || !strings.Contains(cur.Body, "reads (Basalisk)") {
		t.Fatalf("Basalisk's revision is not read: %+v", cur)
	}
	if revs := got["revisions"].([]Revision); len(revs) != 2 || revs[0].N != 2 {
		t.Fatalf("history from caster_log: %+v", revs)
	}
	old, err := m.read(ctx, id, 1)
	if err != nil || strings.Contains(old["diagram"].(Diagram).Body, "Basalisk") {
		t.Fatalf("revision 1: %v %+v", err, old)
	}

	list, err := m.st.list(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("both are listed: %v %+v", err, list)
	}

	// Delete hides it; its revisions stay in Caster.
	if code, body := h.Do("DELETE", "/diagrams/"+id, ""); code != 200 {
		t.Fatalf("delete: %d %s", code, body)
	}
	if _, rev := fake.Content("collab/diagram.system-map"); rev != 3 {
		t.Fatalf("delete should write a hidden revision, at %d", rev)
	}
	if list, _ := m.st.list(ctx); len(list) != 1 {
		t.Fatalf("a deleted diagram is still listed: %+v", list)
	}

	// Caster down: the app's own diagrams are still listed.
	fake.Down = true
	if list, err := m.st.list(ctx); err != nil || len(list) != 1 {
		t.Fatalf("list with Caster down: %v %+v", err, list)
	}
}
