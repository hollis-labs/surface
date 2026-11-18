package drafts

import (
	"strings"
	"testing"

	"github.com/hollis-labs/surface/module/moduletest"
)

func TestToolsThroughTheContract(t *testing.T) {
	h := moduletest.Start(t, &mod{})
	out, err := h.Tool("drafts_create", `{"title":"Pitch","body":"one\ntwo\n"}`)
	if err != nil {
		t.Fatal(err)
	}
	id := out.(map[string]any)["id"].(string)
	// It opened in a window, which is now in front: the tools default to it.
	if w, ok := h.DeskState.Focused(t.Context()); !ok || w.Args["id"] != id {
		t.Fatalf("not opened: %+v", w)
	}
	if _, err := h.Tool("drafts_edit", `{"find":"two","replace":"2","expect_rev":1,"summary":"numerals"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Tool("drafts_edit", `{"find":"one","replace":"1","expect_rev":1}`); err == nil {
		t.Fatal("stale edit accepted")
	}
	got, _ := h.Tool("drafts_get", `{}`)
	if s := got.(string); !strings.Contains(s, "revision 2 of 2") || !strings.Contains(s, "   2  2") {
		t.Fatal(s)
	}
	if code, body := h.Do("GET", "/drafts/"+id+"/diff", ""); code != 200 || !strings.Contains(body, `"op":"+"`) {
		t.Fatal(code, body)
	}
	if n := strings.Count(strings.Join(h.Bus.Topics(), ","), "drafts.changed"); n != 2 {
		t.Fatalf("events: %v", h.Bus.Topics())
	}
}
