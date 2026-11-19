package notes

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testStore(t *testing.T) (*store, string) {
	t.Helper()
	dir := t.TempDir()
	return &store{root: func() (string, error) { return dir, nil }}, dir
}

func TestCleanRefusesWhatIsNotANoteName(t *testing.T) {
	for _, bad := range []string{"", "../x", "a/../../x", ".hidden", "a/.git/x", "/"} {
		if got, err := clean(bad); err == nil {
			t.Errorf("clean(%q) = %q, want an error", bad, got)
		}
	}
	for in, want := range map[string]string{"x": "x.md", "/a/b": "a/b.md", "a\\b.md": "a/b.md", "A.MD": "A.MD"} {
		if got, err := clean(in); err != nil || got != want {
			t.Errorf("clean(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestWriteReadAndTheRevCheck(t *testing.T) {
	s, _ := testStore(t)
	n, err := s.write("ideas/first", "# First idea\n\nbody\n", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if n.Path != "ideas/first.md" || n.Title != "First idea" || n.Rev == "" {
		t.Fatalf("unexpected note: %+v", n)
	}
	if _, err = s.write("ideas/first", "again", "", true); !errors.Is(err, errExists) {
		t.Errorf("creating over a note: %v, want errExists", err)
	}
	if _, err = s.write("ideas/first", "stale", "000000000000", false); !errors.Is(err, errConflict) {
		t.Errorf("a write from a stale rev: %v, want errConflict", err)
	}
	n2, err := s.write("ideas/first", "# First idea\n\nnew body\n", n.Rev, false)
	if err != nil || n2.Rev == n.Rev {
		t.Fatalf("a write from the current rev: %+v, %v", n2, err)
	}
	if _, err := s.read("ideas/missing"); !errors.Is(err, errNotFound) {
		t.Errorf("reading a missing note: %v, want errNotFound", err)
	}
}

func TestEditReplacesExactlyOnePassage(t *testing.T) {
	s, _ := testStore(t)
	if _, err := s.write("n", "one two one\n", "", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.edit("n", "one", "1", ""); err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Errorf("an ambiguous passage: %v", err)
	}
	if _, err := s.edit("n", "three", "3", ""); err == nil {
		t.Error("a passage that is not there was accepted")
	}
	n, err := s.edit("n", "two", "2", "")
	if err != nil || n.Text != "one 2 one\n" {
		t.Errorf("edit: %q, %v", n.Text, err)
	}
}

func TestALinkOutOfTheFolderIsRefused(t *testing.T) {
	s, dir := testStore(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "out")); err != nil {
		t.Skip("no symlinks here")
	}
	if _, err := s.read("out/secret"); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Errorf("reading through a link out of the folder: %v", err)
	}
	if _, err := s.write("out/new", "x", "", true); err == nil {
		t.Error("writing through a link out of the folder was accepted")
	}
	// A link to a file outside is not listed, so search and backlinks never read it.
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(dir, "linked.md")); err != nil {
		t.Fatal(err)
	}
	if notes, _ := s.list(); len(notes) != 0 {
		t.Errorf("a link out of the folder was listed: %+v", notes)
	}
}

func TestListSearchBacklinksAndTrash(t *testing.T) {
	s, dir := testStore(t)
	must := func(p, text string) {
		t.Helper()
		if _, err := s.write(p, text, "", true); err != nil {
			t.Fatal(err)
		}
	}
	must("surface", "# Surface\n\nThe ADE framework.\n")
	must("log/today", "# Today\n\nWorked on [[Surface]] and [[missing note]].\n")
	must("other", "nothing here\n")
	notes, err := s.list()
	if err != nil || len(notes) != 3 {
		t.Fatalf("list: %d notes, %v", len(notes), err)
	}
	hits, err := s.search("ade FRAMEWORK", 0)
	if err != nil || len(hits) != 1 || hits[0].Path != "surface.md" || hits[0].Line != 3 {
		t.Errorf("search: %+v, %v", hits, err)
	}
	back, err := s.backlinks("surface")
	if err != nil || len(back) != 1 || back[0].Path != "log/today.md" {
		t.Errorf("backlinks: %+v, %v", back, err)
	}
	kept, err := s.remove("other")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(kept))); err != nil {
		t.Errorf("the deleted note is not in the trash: %v", err)
	}
	if notes, _ := s.list(); len(notes) != 2 {
		t.Errorf("after a delete the list has %d notes, want 2 (the trash is not listed)", len(notes))
	}
}

func TestOutlineSkipsCodeFences(t *testing.T) {
	got := outline("# A\n\n```\n# not a heading\n```\n\n## B\n#notaheading\n")
	if len(got) != 2 || got[0] != "A" || got[1] != "  B" {
		t.Errorf("outline: %q", got)
	}
}

func TestFileResolvesImagesInsideTheFolderOnly(t *testing.T) {
	s, dir := testStore(t)
	if err := os.MkdirAll(filepath.Join(dir, "img"), 0o700); err != nil {
		t.Fatal(err)
	}
	rel, full, err := s.file("img/a.png")
	if err != nil || rel != "img/a.png" || full != filepath.Join(dir, "img", "a.png") {
		t.Errorf("file(img/a.png) = %q, %q, %v", rel, full, err)
	}
	for _, bad := range []string{"../x.png", "img/../../x.png", ".trash/a.png", ""} {
		if _, _, err := s.file(bad); err == nil {
			t.Errorf("file(%q) was accepted", bad)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "out")); err != nil {
		t.Skip("no symlinks here")
	}
	if _, _, err := s.file("out/a.png"); err == nil {
		t.Error("a file through a link out of the folder was accepted")
	}
}
