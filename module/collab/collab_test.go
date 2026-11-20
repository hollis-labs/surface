package collab

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hollis-labs/surface/module/collab/collabtest"
)

func TestDocs(t *testing.T) {
	ctx := context.Background()
	s := &Store{Link: collabtest.NewFake().Link}
	d, err := s.Create(ctx, Doc{Kind: KindDraft, Title: "Launch Plan: v2!", Lang: "markdown", Body: "one\ntwo\n"}, "user")
	if err != nil || d.Name != "draft.launch-plan-v2" || d.Rev != 1 {
		t.Fatalf("%v %+v", err, d)
	}
	d2, _ := s.Create(ctx, Doc{Kind: KindDraft, Title: "Launch Plan: v2!"}, "user")
	if d2.Name != "draft.launch-plan-v2-2" {
		t.Fatalf("a second document of the same title gets its own name: %s", d2.Name)
	}
	got, err := s.Get(ctx, d.Name, 0)
	if err != nil || got.Body != "one\ntwo\n" || got.Lang != "markdown" || got.Title != "Launch Plan: v2!" {
		t.Fatalf("%v %+v", err, got)
	}
	saved, err := s.Save(ctx, d.Name, 1, nil, "one\nTWO\n", "agent")
	if err != nil || saved.Rev != 2 {
		t.Fatalf("%v %+v", err, saved)
	}
	var c *Conflict
	if _, err = s.Save(ctx, d.Name, 1, nil, "stale", "user"); !errors.As(err, &c) || c.Rev != 2 {
		t.Fatalf("an edit begun on an old revision is refused: %v", err)
	}
	if old, _ := s.Get(ctx, d.Name, 1); old.Body != "one\ntwo\n" {
		t.Fatalf("revision 1 is still readable: %q", old.Body)
	}
	list, _ := s.List(ctx, KindDraft)
	if len(list) != 2 {
		t.Fatalf("marks files are not listed as documents: %+v", list)
	}
	marks, err := s.MarkOp(ctx, d.Name, "add", Mark{Rev: 2, Kind: "suggestion", Anchor: Anchor{Quote: "TWO", LineStart: 2, LineEnd: 2}, Replace: "two"})
	if err != nil || len(marks) != 1 || marks[0].State != "open" || marks[0].By != "user" || marks[0].ID == "" {
		t.Fatalf("%v %+v", err, marks)
	}
	marks, err = s.MarkOp(ctx, d.Name, "update", Mark{ID: marks[0].ID, State: "done", Reply: "taken"})
	if err != nil || marks[0].State != "done" || marks[0].Reply != "taken" || marks[0].Replace != "two" {
		t.Fatalf("%v %+v", err, marks)
	}
	if list, _ := s.List(ctx, ""); len(list) != 2 {
		t.Fatalf("%+v", list)
	}
	if _, err := s.Get(ctx, "draft.../x", 0); !errors.Is(err, ErrNotFound) {
		t.Fatal("a name that is not a document's is refused")
	}
}

func TestRemovalRetainsRevisionAndRejectsStaleDelete(t *testing.T) {
	ctx := context.Background()
	s := &Store{Link: collabtest.NewFake().Link}
	d, err := s.Create(ctx, Doc{Kind: KindDraft, Title: "Keep history", Body: "original"}, "user")
	if err != nil {
		t.Fatal(err)
	}
	newer, err := s.Save(ctx, d.Name, d.Rev, nil, "revised", "user")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetDeleted(ctx, d.Name, d.Rev, true, "agent"); err == nil {
		t.Fatal("stale removal accepted")
	}
	removed, err := s.SetDeleted(ctx, d.Name, newer.Rev, true, "agent")
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.List(ctx, "")
	if err != nil || len(list) != 0 {
		t.Fatalf("removed document listed: %+v %v", list, err)
	}
	old, err := s.Get(ctx, d.Name, d.Rev)
	if err != nil || old.Body != "original" {
		t.Fatalf("history lost: %+v %v", old, err)
	}
	restored, err := s.SetDeleted(ctx, d.Name, removed.Rev, false, "owner")
	if err != nil || restored.Body != "revised" || restored.Deleted {
		t.Fatalf("restore: %+v %v", restored, err)
	}
}

func calls(f *collabtest.Fake, tool string) int {
	n := 0
	for _, c := range f.Calls {
		if c == tool {
			n++
		}
	}
	return n
}

// A list is one caster_ls for documents written here: hidden, language and creation come from
// their tags, no body is read. A document Basalisk wrote without tags has only its front matter
// read; one that cannot be read is left out, and the rest are still listed.
func TestListReadsTagsNotBodies(t *testing.T) {
	ctx := context.Background()
	f := collabtest.NewFake()
	s := &Store{Link: f.Link}
	a, _ := s.Create(ctx, Doc{Kind: KindDraft, Title: "Alpha", Lang: "go", Body: strings.Repeat("x", 100000)}, "owner")
	b, _ := s.Create(ctx, Doc{Kind: KindDraft, Title: "Beta"}, "owner")
	if _, err := s.SetDeleted(ctx, b.Name, b.Rev, true, "agent"); err != nil {
		t.Fatal(err)
	}
	if tags := f.Tags("collab/" + b.Name); !strings.Contains(tags, "deleted") || !strings.Contains(tags, "kind:draft") {
		t.Fatalf("hidden is not a tag: %q", tags)
	}
	// Basalisk's: no tags; one hidden in its front matter only, one unreadable.
	f.Put("collab/draft.from-basalisk", "---\ncollab: v1\nkind: draft\ntitle: From Basalisk\nlang: python\n---\nbody", "From Basalisk", "")
	f.Put("collab/draft.hidden-there", "---\ndeleted: true\ncollab: v1\nkind: draft\ntitle: Hidden\n---\nbody", "Hidden", "")
	f.Put("collab/draft.broken", "anything", "Broken", "")
	f.Fail = func(tool string, args map[string]any) bool {
		return tool == "caster_get" && args["ref"] == "caster://collab/draft.broken"
	}
	f.Calls = nil
	list, err := s.List(ctx, KindDraft)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Doc{}
	for _, d := range list {
		got[d.Name] = d
	}
	if len(got) != 2 || got[a.Name].Lang != "go" || got[a.Name].Created == "" || got["draft.from-basalisk"].Lang != "python" {
		t.Fatalf("listed %+v", list)
	}
	if n := calls(f, "caster_get"); n != 3 {
		t.Fatalf("%d reads for a list: only the three untagged documents' front matter is read", n)
	}
}

// Caster's put takes no expected revision: a write that lands on another's is caught by the
// revision Caster answers, reported as a conflict, and both texts stay in its history. A base
// of 0 is the revision just read, so it is checked the same way.
func TestAWriteOverAnothersIsAConflict(t *testing.T) {
	ctx := context.Background()
	f := collabtest.NewFake()
	s := &Store{Link: f.Link}
	d, _ := s.Create(ctx, Doc{Kind: KindDraft, Title: "Shared", Body: "v1"}, "owner")
	f.BeforePut = func(key string) {
		f.BeforePut = nil
		f.Put(key, "---\ncollab: v1\nkind: draft\ntitle: Shared\n---\nBasalisk's v2", "", "")
	}
	var c *Conflict
	if _, err := s.Save(ctx, d.Name, 0, nil, "Desk's v2", "owner"); !errors.As(err, &c) || c.Rev != 3 {
		t.Fatalf("a write over another's was taken: %v", err)
	}
	if f.Revs("collab/"+d.Name) != 3 {
		t.Fatal("both writes should be in the history")
	}
	old, _ := s.Get(ctx, d.Name, 2)
	if !strings.Contains(old.Body, "Basalisk") {
		t.Fatalf("revision 2 is %q", old.Body)
	}

	// An unchanged text is no new revision, and no conflict.
	cur, _ := s.Get(ctx, d.Name, 0)
	same, err := s.Save(ctx, d.Name, cur.Rev, nil, cur.Body, "owner")
	if err != nil || same.Rev != cur.Rev || f.Revs("collab/"+d.Name) != 3 {
		t.Fatalf("unchanged save: %v rev %d of %d", err, same.Rev, f.Revs("collab/"+d.Name))
	}
	if cur.Updated == "" {
		t.Fatal("Get does not say when the revision was written")
	}

	// Update with base 0 (a language change) is checked the same way.
	f.BeforePut = func(key string) {
		f.BeforePut = nil
		f.Put(key, "---\ncollab: v1\nkind: draft\ntitle: Shared\n---\nanother", "", "")
	}
	if _, err := s.Update(ctx, d.Name, 0, func(d *Doc) { d.Lang = "go" }, "owner"); !errors.As(err, &c) {
		t.Fatalf("an unchecked update: %v", err)
	}
}

// Create tries slug, slug-2 … slug-200, every one checked; a name taken between the check and
// the write is given up for the next.
func TestCreateNames(t *testing.T) {
	ctx := context.Background()
	f := collabtest.NewFake()
	s := &Store{Link: f.Link}
	f.BeforePut = func(key string) {
		f.BeforePut = nil
		f.Put(key, "someone else's", "Race", "")
	}
	d, err := s.Create(ctx, Doc{Kind: KindDraft, Title: "Race"}, "owner")
	if err != nil || d.Name != "draft.race-2" || d.Rev != 1 {
		t.Fatalf("raced create: %v %+v", err, d)
	}
	f.Put("collab/draft.full", "x", "", "")
	for n := 2; n <= 199; n++ {
		f.Put("collab/draft.full-"+strconv.Itoa(n), "x", "", "")
	}
	d, err = s.Create(ctx, Doc{Kind: KindDraft, Title: "Full"}, "owner")
	if err != nil || d.Name != "draft.full-200" {
		t.Fatalf("the last name is tried: %v %+v", err, d)
	}
	if _, err = s.Create(ctx, Doc{Kind: KindDraft, Title: "Full"}, "owner"); err == nil {
		t.Fatal("a 201st name was made")
	}
}

// module/collab is a helper beside the contract: it imports only module/tetherlink, the
// standard library and YAML, never the core or a module.
func TestCollabImportsOnlyTetherlinkStdAndYAML(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			std := !strings.Contains(strings.SplitN(p, "/", 2)[0], ".")
			if !std && p != "gopkg.in/yaml.v3" && p != "github.com/hollis-labs/surface/module/tetherlink" {
				t.Fatalf("%s imports %s", name, p)
			}
		}
	}
}
