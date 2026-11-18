package drafts

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"database/sql"
	"io/fs"

	_ "modernc.org/sqlite"
)

func TestRevisionsAndStaleWrites(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "d.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	ddl, _ := fs.ReadFile(migrations, "migrations/0001_init.sql")
	if _, xerr := db.ExecContext(ctx, string(ddl)); xerr != nil {
		t.Fatal(xerr)
	}
	s := &store{db: db}
	d, err := s.create(ctx, "Plan", "markdown", "one\ntwo\n", "owner")
	if err != nil || d.Rev != 1 {
		t.Fatal(d, err)
	}
	d2, err := s.write(ctx, d.ID, "one\n2\n", "agent", "numerals", 1, nil)
	if err != nil || d2.Rev != 2 {
		t.Fatal(d2, err)
	}
	if _, werr := s.write(ctx, d.ID, "x", "owner", "", 1, nil); !errors.Is(werr, errStale) {
		t.Fatalf("stale write: %v", werr)
	}
	// Same body, new title: no new revision.
	title := "Plan B"
	d3, err := s.write(ctx, d.ID, "one\n2\n", "owner", "", 2, &title)
	if err != nil || d3.Rev != 2 || d3.Title != "Plan B" {
		t.Fatal(d3, err)
	}
	revs, _ := s.revisions(ctx, d.ID)
	if len(revs) != 2 || revs[0].Author != "agent" {
		t.Fatalf("%+v", revs)
	}
	if u := unified(diffLines("one\ntwo\n", "one\n2\n")); !strings.Contains(u, "-two") || !strings.Contains(u, "+2") {
		t.Fatal(u)
	}
	if got := numbered("a\nb", 2, 0); got != "   2  b\n" {
		t.Fatalf("%q", got)
	}
}
