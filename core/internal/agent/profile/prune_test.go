package profile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func pruneFixture(t *testing.T, root string, n int) []string {
	t.Helper()
	var dirs []string
	for i := range n {
		dir := filepath.Join(root, fmt.Sprintf("%064x", i))
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "agent.md"), []byte("snapshot"), 0600); err != nil {
			t.Fatal(err)
		}
		stamp := time.Unix(int64(i), 0)
		if err := os.Chtimes(dir, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, dir)
	}
	return dirs
}

func TestPruneBindingsKeepsNewestAndReferences(t *testing.T) {
	root := t.TempDir()
	dirs := pruneFixture(t, root, 8)
	// Older snapshots can belong to a current binding, an active descendant,
	// or a recoverable turn. Retention is in addition to these references.
	err := PruneBindings(root, 2, func() ([]string, error) { return dirs[:3], nil })
	if err != nil {
		t.Fatal(err)
	}
	for i, dir := range dirs {
		_, statErr := os.Stat(dir)
		want := i < 3 || i >= 6
		if want && statErr != nil || !want && !os.IsNotExist(statErr) {
			t.Fatalf("snapshot %d: want retained=%t, stat=%v", i, want, statErr)
		}
	}
}

func TestPruneBindingsFailsClosed(t *testing.T) {
	root := t.TempDir()
	dirs := pruneFixture(t, root, 3)
	want := errors.New("reference database unavailable")
	if err := PruneBindings(root, 0, func() ([]string, error) { return nil, want }); !errors.Is(err, want) {
		t.Fatalf("%v", err)
	}
	for _, dir := range dirs {
		if _, err := os.Stat(dir); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPruneBindingsOwnsOnlyDigestDirectories(t *testing.T) {
	root := t.TempDir()
	dirs := pruneFixture(t, root, 1)
	outside := t.TempDir()
	marker := filepath.Join(outside, "keep")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, fmt.Sprintf("%064x", 1))
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dirs[0], "nested")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dirs[0], time.Unix(0, 0), time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(root, "notes")
	if err := os.Mkdir(unrelated, 0700); err != nil {
		t.Fatal(err)
	}
	if err := PruneBindings(root, 0, func() ([]string, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, unrelated, marker} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(dirs[0]); !os.IsNotExist(err) {
		t.Fatalf("snapshot not pruned: %v", err)
	}
}

func TestPruneBindingsRetainsRecentAndFutureUse(t *testing.T) {
	root := t.TempDir()
	dirs := pruneFixture(t, root, 4)
	for i, age := range []time.Duration{time.Hour, -time.Hour} {
		stamp := time.Now().Add(-age)
		if err := os.Chtimes(dirs[i], stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	if err := TouchBinding(root, dirs[2]); err != nil {
		t.Fatal(err)
	}
	if err := PruneBindings(root, 0, func() ([]string, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	for _, dir := range dirs[:3] {
		if _, err := os.Stat(dir); err != nil {
			t.Fatal(err)
		}
	}
	if err := TouchBinding(root, dirs[3]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing recovery snapshot must fail: %v", err)
	}
}

func TestStagingRefreshesSnapshotUse(t *testing.T) {
	p, err := LoadBinding("sample", "Sample", bindingFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dir, err := p.StageBinding(root)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * BindingMaxAge)
	if err = os.Chtimes(dir, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err = p.StageBinding(root); err != nil {
		t.Fatal(err)
	}
	if err = PruneBindings(root, 0, func() ([]string, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(dir); err != nil {
		t.Fatal(err)
	}
}
