package scratch

import (
	"strings"
	"testing"
	"time"

	"github.com/hollis-labs/surface/module/collab"
	"github.com/hollis-labs/surface/module/moduletest"
)

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("never: %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// With caster_sync on, every item is written behind to caster://scratch/<id> as Basalisk writes
// it, a change is a new revision, and an item Basalisk put in the namespace is read back once.
func TestItemsAreKeptInCaster(t *testing.T) {
	fake := collab.NewFake()
	m := &mod{link: fake}
	h := moduletest.Start(t, m)

	out, err := h.Tool("scratch_add", `{"text":"todo: buy milk"}`)
	if err != nil {
		t.Fatal(err)
	}
	id := out.(Item).ID
	if _, rev := fake.Content("scratch/" + id); rev != 0 {
		t.Fatal("written to Caster with the sync off")
	}

	if err := h.Settings.Set(t.Context(), "caster_sync", true); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the item reaches Caster", func() bool {
		c, rev := fake.Content("scratch/" + id)
		return rev == 1 && c == "[ ] buy milk\n"
	})
	if _, err := h.Tool("scratch_update", `{"id":"`+id+`","done":true}`); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the change is a new revision", func() bool {
		c, rev := fake.Content("scratch/" + id)
		return rev == 2 && c == "[x] buy milk\n"
	})
	eventually(t, "the item knows its Caster ref", func() bool {
		it, _ := m.st.get(t.Context(), id)
		return it.CasterRef == Ref(id)
	})

	// Basalisk's items come back, once; a document parked in the namespace does not.
	fake.Put("scratch/scr_0001", "::idea a lighter scratch\n", "a lighter scratch")
	fake.Put("scratch/big-doc", strings.Repeat("x", maxImport+10), "a document")
	if _, err := h.Tool("scratch_add", `{"text":"kick"}`); err != nil {
		t.Fatal(err)
	}
	eventually(t, "Basalisk's item is read back", func() bool {
		it, err := m.st.get(t.Context(), "cst_scr_0001")
		return err == nil && it.Kind == "idea" && it.Text == "a lighter scratch" && it.CasterRef == "caster://scratch/scr_0001"
	})
	if _, err := m.st.get(t.Context(), "cst_big-doc"); err == nil {
		t.Fatal("a large document was taken as a scratch item")
	}
	time.Sleep(100 * time.Millisecond)
	if _, rev := fake.Content("scratch/scr_0001"); rev != 1 {
		t.Fatalf("an item read back was written again (rev %d)", rev)
	}
}

func TestMarkdownRoundTrip(t *testing.T) {
	for _, it := range []Item{
		{Kind: "note", Text: "plain words"},
		{Kind: "todo", Text: "ship it"},
		{Kind: "todo", Text: "shipped", Done: true},
		{Kind: "followup", Text: "ask about it", Done: true},
	} {
		text, kind, done := parse(markdown(it))
		if text != it.Text || kind != it.Kind || done != it.Done {
			t.Fatalf("%+v came back as %q %q %v", it, text, kind, done)
		}
	}
}
