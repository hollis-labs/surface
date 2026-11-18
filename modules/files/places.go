package files

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Place is a named root directory on this machine. Files reaches nothing outside its
// places.
type Place struct {
	// ID is a short slug made from the name ("home", "work-notes").
	ID   string `json:"id"`
	Name string `json:"name"`
	// Path is the absolute path of the root directory.
	Path string `json:"path"`
}

var (
	errNotFound  = errors.New("not found")
	errExists    = errors.New("already exists")
	errOutside   = errors.New("outside the place")
	errSensitive = errors.New("refused: it looks like a credential")
)

type store struct{ db *sql.DB }

// list returns the places, oldest first.
func (s *store) list(ctx context.Context) ([]Place, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, path FROM places ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Place{}
	for rows.Next() {
		var p Place
		if err := rows.Scan(&p.ID, &p.Name, &p.Path); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *store) get(ctx context.Context, id string) (Place, error) {
	var p Place
	err := s.db.QueryRowContext(ctx, `SELECT id, name, path FROM places WHERE id=?`, id).Scan(&p.ID, &p.Name, &p.Path)
	if errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("%w: there is no place %q", errNotFound, id)
	}
	return p, err
}

var notSlug = regexp.MustCompile(`[^a-z0-9]+`)

// add records a place for an existing directory. dir may start with "~/". An empty name
// becomes the directory's own.
func (s *store) add(ctx context.Context, name, dir string) (Place, error) {
	dir = strings.TrimSpace(dir)
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return Place{}, err
		}
		dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
	}
	if !filepath.IsAbs(dir) {
		return Place{}, errors.New("a place needs the full path of a folder, like /srv/projects or ~/Documents")
	}
	dir = filepath.Clean(dir)
	if sensitive(dir) {
		return Place{}, fmt.Errorf("%w: %s cannot be a place", errSensitive, dir)
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return Place{}, fmt.Errorf("%s is not a folder on this machine; create it first or check the path", dir)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = filepath.Base(dir)
	}
	base := strings.Trim(notSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if base == "" {
		base = "place"
	}
	p := Place{ID: base, Name: name, Path: dir}
	for n := 2; ; n++ {
		_, err = s.get(ctx, p.ID)
		if errors.Is(err, errNotFound) {
			break
		}
		if err != nil {
			return Place{}, err
		}
		p.ID = fmt.Sprintf("%s-%d", base, n)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO places (id, name, path, created_at) VALUES (?, ?, ?, ?)`,
		p.ID, p.Name, p.Path, time.Now().UTC().Format(time.RFC3339Nano))
	return p, err
}

// remove forgets a place. The directory and everything in it stay as they are.
func (s *store) remove(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM places WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: there is no place %q", errNotFound, id)
	}
	return nil
}

// loc is a path inside a place, resolved and checked.
type loc struct {
	Place Place
	// Rel is the cleaned path relative to the place, slash separated; "" is the root.
	Rel string
	// Abs is the real path on disk, symbolic links resolved.
	Abs string
}

// name is what to call the location: the last element, or the place's name at its root.
func (l loc) name() string {
	if l.Rel == "" {
		return l.Place.Name
	}
	return path.Base(l.Rel)
}

// cleanRel returns rel as a clean slash path relative to a place root, "" for the root.
// A path that is absolute or climbs out with ".." is refused, not silently clamped.
func cleanRel(rel string) (string, error) {
	if rel == "" || rel == "." || rel == "/" {
		return "", nil
	}
	if strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("%w: %q is absolute; give a path relative to the place, like notes/todo.md", errOutside, rel)
	}
	c := path.Clean(rel)
	if c == "." {
		return "", nil
	}
	if !filepath.IsLocal(filepath.FromSlash(c)) {
		return "", fmt.Errorf("%w: %q leaves the place", errOutside, rel)
	}
	return c, nil
}

// locate resolves a relative path inside a place and checks it. Every path Files touches
// comes through here.
//
// The path is cleaned (traversal is refused), symbolic links are resolved, and the result
// must still lie inside the place's real root and off the denylist. With follow, a final
// symbolic link is resolved too, which is right for reading and writing content. Without
// it only the parent is resolved and Abs names the entry itself, which is right for
// renaming or deleting, where the link is what changes and not its target. The path need
// not exist: a new file resolves through its nearest existing folder.
func locate(p Place, rel string, follow bool) (loc, error) {
	rel, err := cleanRel(rel)
	if err != nil {
		return loc{}, err
	}
	root, err := filepath.EvalSymlinks(p.Path)
	if err != nil {
		return loc{}, fmt.Errorf("%w: the folder of place %q (%s) is not there", errNotFound, p.ID, p.Path)
	}
	l := loc{Place: p, Rel: rel, Abs: root}
	if rel != "" {
		target := filepath.Join(root, filepath.FromSlash(rel))
		if follow {
			l.Abs, err = evalExisting(target)
		} else {
			l.Abs, err = evalExisting(filepath.Dir(target))
			l.Abs = filepath.Join(l.Abs, filepath.Base(target))
		}
		if err != nil {
			return loc{}, err
		}
	}
	if l.Abs != root && !strings.HasPrefix(l.Abs, root+string(filepath.Separator)) {
		return loc{}, fmt.Errorf("%w: %q leads out of %s through a link", errOutside, rel, p.Name)
	}
	if sensitive(rel) || sensitive(l.Abs) {
		return loc{}, fmt.Errorf("%w (%s)", errSensitive, rel)
	}
	return l, nil
}

// evalExisting resolves the symbolic links in p. Elements that do not exist yet are kept
// as written after the nearest existing ancestor. A link whose target is missing is an
// error: creating "through" it would write wherever it points.
func evalExisting(p string) (string, error) {
	resolved, err := filepath.EvalSymlinks(p)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if _, lerr := os.Lstat(p); lerr == nil { //nolint:gosec // only looked at; locate checks the result stays in the place
		return "", fmt.Errorf("%w: %s is a broken link", errNotFound, filepath.Base(p))
	}
	parent := filepath.Dir(p)
	if parent == p {
		return "", err
	}
	resolved, err = evalExisting(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, filepath.Base(p)), nil
}
