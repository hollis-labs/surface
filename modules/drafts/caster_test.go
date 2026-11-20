package drafts

import (
	"strings"
	"testing"

	"github.com/hollis-labs/surface/module/collab/collabtest"
	"github.com/hollis-labs/surface/module/moduletest"
)

// With new_in caster the same tools work on a collab/v1 draft in Caster: created, edited as
// revisions there, refused when stale,
// listed beside the app's own, and a revision Basalisk wrote is read.
func TestDraftsInCaster(t *testing.T) {
	fake := collabtest.NewFake()
	h := moduletest.Start(t, &mod{link: fake})
	if err := h.Settings.Set(t.Context(), "new_in", "caster"); err != nil {
		t.Fatal(err)
	}
	out, err := h.Tool("drafts_create", `{"title":"Launch Plan","body":"one\ntwo\n","lang":"markdown"}`)
	if err != nil {
		t.Fatal(err)
	}
	id := out.(map[string]any)["id"].(string)
	if id != "draft.launch-plan" {
		t.Fatalf("id %s", id)
	}
	if _, err := h.Tool("drafts_edit", `{"find":"two","replace":"2","expect_rev":1,"summary":"numerals"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Tool("drafts_edit", `{"find":"one","replace":"1","expect_rev":1}`); err == nil {
		t.Fatal("stale edit accepted")
	}
	content, rev := fake.Content("collab/draft.launch-plan")
	if rev != 2 || !strings.Contains(content, "one\n2\n") || !strings.Contains(content, "lang: markdown") {
		t.Fatalf("Caster has %d %q", rev, content)
	}

	// Basalisk writes revision 3; the draft reads it, and the diff route compares Caster's revisions.
	fake.Put("collab/draft.launch-plan", strings.Replace(content, "one\n", "ONE\n", 1), "Launch Plan", "")
	got, _ := h.Tool("drafts_get", `{"id":"`+id+`"}`)
	if s := got.(string); !strings.Contains(s, "ONE") || !strings.Contains(s, "revision 3 of 3") {
		t.Fatal(s)
	}
	if code, body := h.Do("GET", "/drafts/"+id+"/diff", ""); code != 200 || !strings.Contains(body, `"op":"+"`) {
		t.Fatal(code, body)
	}

	// The app's own drafts are still made when new_in is local, and both are listed.
	_ = h.Settings.Set(t.Context(), "new_in", "local")
	if out, err := h.Tool("drafts_create", `{"title":"Local","body":"x"}`); err != nil || !strings.HasPrefix(out.(map[string]any)["id"].(string), "d_") {
		t.Fatalf("%v %v", err, out)
	}
	if code, body := h.Do("GET", "/drafts", ""); code != 200 || !strings.Contains(body, "draft.launch-plan") || !strings.Contains(body, `"d_`) {
		t.Fatal(code, body)
	}
	if code, _ := h.Do("DELETE", "/drafts/"+id, ""); code != 200 {
		t.Fatal(code)
	}
	if code, body := h.Do("GET", "/drafts", ""); strings.Contains(body, "draft.launch-plan") {
		t.Fatal(code, "a removed draft is listed")
	}
}
