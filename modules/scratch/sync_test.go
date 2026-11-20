package scratch

import (
	"strings"
	"testing"
	"time"

	"github.com/hollis-labs/surface/module/collab/collabtest"
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

func syncing(t *testing.T) (*moduletest.Host, *mod, *collabtest.Fake) {
	t.Helper()
	askEvery = 0
	t.Cleanup(func() { askEvery = 30 * time.Second })
	fake := collabtest.NewFake()
	m := &mod{link: fake}
	h := moduletest.Start(t, m)
	t.Cleanup(func() { _ = m.Stop(t.Context()) })
	return h, m, fake
}

func row(t *testing.T, m *mod, id string) (it Item, deleted bool, rev, conflict int, keep, syncErr string) {
	t.Helper()
	var d *string
	err := m.st.db.QueryRowContext(t.Context(), `SELECT text, kind, done, tags, caster_ref, deleted_at, caster_rev, conflict_rev, caster_tags, sync_error FROM items WHERE id=?`, id).
		Scan(&it.Text, &it.Kind, &it.Done, &it.Tags, &it.CasterRef, &d, &rev, &conflict, &keep, &syncErr)
	if err != nil {
		return Item{}, false, 0, 0, "", ""
	}
	return it, d != nil, rev, conflict, keep, syncErr
}

// With caster_sync on, every item is written behind to caster://scratch/<id> as Basalisk writes
// it, a change is a new revision, and the revision Caster answers is kept.
func TestItemsAreWrittenToCaster(t *testing.T) {
	h, m, fake := syncing(t)
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
	eventually(t, "the change is revision 2, and kept", func() bool {
		c, rev := fake.Content("scratch/" + id)
		_, _, have, _, _, _ := row(t, m, id)
		return rev == 2 && c == "[x] buy milk\n" && have == 2
	})
	if tags := fake.Tags("scratch/" + id); !strings.Contains(tags, "done") || !strings.Contains(tags, "kind:todo") {
		t.Fatalf("tags %q", tags)
	}
}

// Opening the list reads Caster back: Basalisk's items arrive with what their tags say (put
// away, own tags, a kind and links this module does not have, kept on every rewrite); a change
// Basalisk made to an item held here is taken when this item has none waiting, and flagged as a
// conflict when it has, writing over neither.
func TestCasterIsReadBack(t *testing.T) {
	h, m, fake := syncing(t)
	fake.Put("scratch/scr_0001", "::idea a lighter scratch\n", "a lighter scratch", "scratch kind:idea day:2026-11-19 tag:ux")
	fake.Put("scratch/scr_0002", "$ make desk\n", "make desk", "scratch kind:command day:2026-11-19 task:CW-20261119-0064")
	fake.Put("scratch/scr_0003", "old thought\n", "old thought", "scratch kind:note day:2026-11-18 archived")
	fake.Put("scratch/big-doc", strings.Repeat("x", maxImport+10), "a document", "")
	_ = h.Settings.Set(t.Context(), "caster_sync", true)
	h.Do("GET", "/items", "")
	eventually(t, "Basalisk's items are read back", func() bool {
		it, _, _, _, _, _ := row(t, m, "cst_scr_0001")
		return it.Kind == "idea" && it.Tags == "ux"
	})
	if it, _, rev, _, keep, _ := row(t, m, "cst_scr_0002"); it.Text != "$ make desk" || keep != "kind:command task:CW-20261119-0064" || rev != 1 {
		t.Fatalf("command item: %+v %d %q", it, rev, keep)
	}
	if _, deleted, _, _, _, _ := row(t, m, "cst_scr_0003"); !deleted {
		t.Fatal("an archived item came back as open")
	}
	if it, _, _, _, _, _ := row(t, m, "cst_big-doc"); it.Text != "" {
		t.Fatal("a large document was taken as a scratch item")
	}
	time.Sleep(100 * time.Millisecond)
	if _, rev := fake.Content("scratch/scr_0001"); rev != 1 {
		t.Fatalf("an item read back was written again (rev %d)", rev)
	}

	// Desk edits the command item: Basalisk's kind and link stay on it.
	if _, err := h.Tool("scratch_update", `{"id":"cst_scr_0002","text":"$ make desk studio"}`); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the edit is written with Basalisk's tags kept", func() bool {
		tags := fake.Tags("scratch/scr_0002")
		return fake.Revs("scratch/scr_0002") == 2 && strings.Contains(tags, "kind:command") && strings.Contains(tags, "task:CW-20261119-0064") && !strings.Contains(tags, "kind:note")
	})

	// Basalisk changes scr_0001; nothing is waiting here: taken.
	fake.Put("scratch/scr_0001", "::idea a lighter, faster scratch  (done)\n", "", "scratch kind:idea day:2026-11-19 tag:ux done")
	h.Do("GET", "/items", "")
	eventually(t, "Basalisk's change is taken", func() bool {
		it, _, rev, _, _, _ := row(t, m, "cst_scr_0001")
		return it.Text == "a lighter, faster scratch" && it.Done && rev == 2
	})

	// A change here not written yet (the sync off), and one in Basalisk: when the sync is back
	// on, Caster is read first and the item is flagged; neither change is written over.
	_ = h.Settings.Set(t.Context(), "caster_sync", false)
	if _, err := h.Tool("scratch_update", `{"id":"cst_scr_0001","text":"Desk's words"}`); err != nil {
		t.Fatal(err)
	}
	fake.Put("scratch/scr_0001", "::idea Basalisk's words\n", "", "")
	_ = h.Settings.Set(t.Context(), "caster_sync", true)
	eventually(t, "the conflict is flagged", func() bool {
		_, _, _, conflict, _, msg := row(t, m, "cst_scr_0001")
		return conflict == 3 && strings.HasPrefix(msg, "conflict")
	})
	time.Sleep(150 * time.Millisecond)
	if c, rev := fake.Content("scratch/scr_0001"); rev != 3 || !strings.Contains(c, "Basalisk's words") {
		t.Fatalf("Basalisk's change was written over: %d %q", rev, c)
	}
	if it, _, _, _, _, _ := row(t, m, "cst_scr_0001"); it.Text != "Desk's words" {
		t.Fatalf("Desk's change was written over: %q", it.Text)
	}
}

// One item Caster refuses does not hold the others back.
func TestOneFailingItemDoesNotBlockTheRest(t *testing.T) {
	h, _, fake := syncing(t)
	bad, _ := h.Tool("scratch_add", `{"text":"refused"}`)
	good, _ := h.Tool("scratch_add", `{"text":"accepted"}`)
	badName := "scratch/" + bad.(Item).ID
	fake.Fail = func(tool string, args map[string]any) bool { return tool == "caster_put" && args["name"] == badName }
	_ = h.Settings.Set(t.Context(), "caster_sync", true)
	eventually(t, "the good item is written", func() bool {
		_, rev := fake.Content("scratch/" + good.(Item).ID)
		return rev == 1
	})
}

func TestBackoff(t *testing.T) {
	b := time.Duration(0)
	var got []time.Duration
	for range 9 {
		b = nextBackoff(b)
		got = append(got, b)
	}
	if got[0] != 5*time.Second || got[1] != 10*time.Second || got[len(got)-1] != 5*time.Minute {
		t.Fatalf("backoff %v", got)
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
