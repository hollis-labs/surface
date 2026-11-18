package notes

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Note is one note: a Markdown file in the notes folder.
type Note struct {
	// Path is relative to the notes folder, with forward slashes: "ideas/surface.md".
	Path  string `json:"path"`
	Title string `json:"title"`
	// Text is the file's contents. Listings leave it out.
	Text string `json:"text,omitempty"`
	// Rev identifies the contents. A write that names the rev it started from is refused
	// when the file has changed since.
	Rev      string `json:"rev,omitempty"`
	Modified string `json:"modified"`
	Size     int64  `json:"size"`
}

// Hit is one line of a note that matches a search.
type Hit struct {
	Path  string `json:"path"`
	Title string `json:"title"`
	Line  int    `json:"line"`
	Text  string `json:"text"`
}

var (
	errNotFound = errors.New("no such note")
	errConflict = errors.New("the note changed since it was read; read it again and reapply the change")
	errExists   = errors.New("a note with that name already exists")
)

const (
	trashDir = ".trash"
	maxNote  = 2 << 20 // bytes; larger files are not notes
)

// store is the notes folder. root is asked for each time, because the folder is a live
// setting: the owner can point Notes somewhere else and the next call follows.
type store struct {
	root func() (string, error)
}

// clean turns what a caller named into a path inside the folder: forward slashes, no
// leading slash, no "..", and ".md" on the end.
func clean(p string) (string, error) {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return "", errors.New("a note needs a name")
	}
	if !strings.HasSuffix(strings.ToLower(p), ".md") {
		p += ".md"
	}
	c := filepath.ToSlash(filepath.Clean(p))
	for _, part := range strings.Split(c, "/") {
		if part == ".." || part == "" || strings.HasPrefix(part, ".") {
			return "", fmt.Errorf("%q is not a note name: no hidden or parent folders", p)
		}
	}
	return c, nil
}

// abs resolves a note path to a file, refusing one that leaves the folder through a link.
func (s *store) abs(p string) (rel, full string, err error) {
	rel, err = clean(p)
	if err != nil {
		return "", "", err
	}
	root, err := s.root()
	if err != nil {
		return "", "", err
	}
	full = filepath.Join(root, filepath.FromSlash(rel))
	// The deepest existing ancestor must still be inside the folder once links are resolved.
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", err
	}
	probe := full
	for {
		if resolved, err := filepath.EvalSymlinks(probe); err == nil {
			if resolved != realRoot && !strings.HasPrefix(resolved, realRoot+string(filepath.Separator)) {
				return "", "", fmt.Errorf("%q leads outside the notes folder", rel)
			}
			break
		}
		up := filepath.Dir(probe)
		if up == probe {
			break
		}
		probe = up
	}
	return rel, full, nil
}

func rev(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:6])
}

var headingRe = regexp.MustCompile(`(?m)^#\s+(.+?)\s*$`)

// titleOf is a note's title: its first top-level heading, or its file name.
func titleOf(path string, text []byte) string {
	head := text
	if len(head) > 4096 {
		head = head[:4096]
	}
	if m := headingRe.FindSubmatch(head); m != nil {
		return string(m[1])
	}
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// list returns every note, most recently changed first, without its text.
func (s *store) list() ([]Note, error) {
	root, err := s.root()
	if err != nil {
		return nil, err
	}
	out := []Note{}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // An unreadable folder is skipped, not fatal.
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		// A link is not listed: it could lead out of the folder, and what is listed is read.
		if d.Type()&fs.ModeSymlink != 0 || strings.HasPrefix(name, ".") || !strings.EqualFold(filepath.Ext(name), ".md") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxNote {
			return nil //nolint:nilerr // A file that cannot be examined, or is too large for a note, is left out.
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		head := make([]byte, 4096)
		if f, err := os.Open(p); err == nil { //nolint:gosec // p comes from walking the notes folder
			n, _ := f.Read(head)
			head = head[:n]
			_ = f.Close()
		} else {
			head = nil
		}
		out = append(out, Note{Path: rel, Title: titleOf(rel, head), Modified: stamp(info.ModTime()), Size: info.Size()})
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].Modified != out[j].Modified {
			return out[i].Modified > out[j].Modified
		}
		return out[i].Path < out[j].Path
	})
	return out, err
}

func (s *store) read(p string) (Note, error) {
	rel, full, err := s.abs(p)
	if err != nil {
		return Note{}, err
	}
	info, err := os.Stat(full) //nolint:gosec // full passed the containment check in abs
	if errors.Is(err, fs.ErrNotExist) {
		return Note{}, fmt.Errorf("%w: %s", errNotFound, rel)
	}
	if err != nil {
		return Note{}, err
	}
	if info.IsDir() || info.Size() > maxNote {
		return Note{}, fmt.Errorf("%s is not a note", rel)
	}
	b, err := os.ReadFile(full) //nolint:gosec // full passed the containment check in abs
	if err != nil {
		return Note{}, err
	}
	return Note{Path: rel, Title: titleOf(rel, b), Text: string(b), Rev: rev(b), Modified: stamp(info.ModTime()), Size: info.Size()}, nil
}

// write stores a note's text. expect, when not empty, is the rev the caller read: the write
// is refused if the file has changed since. createOnly refuses to replace an existing note.
func (s *store) write(p, text, expect string, createOnly bool) (Note, error) {
	rel, full, err := s.abs(p)
	if err != nil {
		return Note{}, err
	}
	if len(text) > maxNote {
		return Note{}, errors.New("the text is too long for a note")
	}
	old, err := os.ReadFile(full) //nolint:gosec // full passed the containment check in abs
	switch {
	case err == nil && createOnly:
		return Note{}, fmt.Errorf("%w: %s", errExists, rel)
	case err == nil && expect != "" && rev(old) != expect:
		return Note{}, errConflict
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return Note{}, err
	case err != nil && expect != "":
		return Note{}, fmt.Errorf("%w: %s", errNotFound, rel)
	}
	if err = os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return Note{}, err
	}
	// Write beside the file and rename over it, so a note is never left half written.
	tmp, err := os.CreateTemp(filepath.Dir(full), ".note-*")
	if err != nil {
		return Note{}, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(text); err != nil {
		_ = tmp.Close()
		return Note{}, err
	}
	if err := tmp.Close(); err != nil {
		return Note{}, err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return Note{}, err
	}
	if err := os.Rename(tmp.Name(), full); err != nil {
		return Note{}, err
	}
	return s.read(rel)
}

// edit replaces one passage of a note. find must occur exactly once, so the change lands
// where the caller meant it to.
func (s *store) edit(p, find, replace, expect string) (Note, error) {
	n, err := s.read(p)
	if err != nil {
		return n, err
	}
	if expect != "" && n.Rev != expect {
		return n, errConflict
	}
	switch strings.Count(n.Text, find) {
	case 0:
		return n, errors.New("the passage to replace is not in the note; read it again and quote it exactly")
	case 1:
	default:
		return n, errors.New("the passage to replace occurs more than once; quote more of it so it is unique")
	}
	return s.write(n.Path, strings.Replace(n.Text, find, replace, 1), n.Rev, false)
}

func (s *store) rename(from, to string) (Note, error) {
	relFrom, fullFrom, err := s.abs(from)
	if err != nil {
		return Note{}, err
	}
	relTo, fullTo, err := s.abs(to)
	if err != nil {
		return Note{}, err
	}
	if _, err := os.Stat(fullFrom); err != nil {
		return Note{}, fmt.Errorf("%w: %s", errNotFound, relFrom)
	}
	if _, err := os.Stat(fullTo); err == nil {
		return Note{}, fmt.Errorf("%w: %s", errExists, relTo)
	}
	if err := os.MkdirAll(filepath.Dir(fullTo), 0o700); err != nil {
		return Note{}, err
	}
	if err := os.Rename(fullFrom, fullTo); err != nil {
		return Note{}, err
	}
	return s.read(relTo)
}

// remove moves a note into the folder's trash, where it stays until the owner empties it.
func (s *store) remove(p string) (string, error) {
	rel, full, err := s.abs(p)
	if err != nil {
		return "", err
	}
	if _, err = os.Stat(full); err != nil { //nolint:gosec // full passed the containment check in abs
		return "", fmt.Errorf("%w: %s", errNotFound, rel)
	}
	root, err := s.root()
	if err != nil {
		return "", err
	}
	trash := filepath.Join(root, trashDir)
	if err := os.MkdirAll(trash, 0o700); err != nil {
		return "", err
	}
	kept := time.Now().UTC().Format("20060102-150405") + "-" + strings.ReplaceAll(rel, "/", "__")
	if err := os.Rename(full, filepath.Join(trash, kept)); err != nil { //nolint:gosec // full passed the containment check in abs; the trash is inside the notes folder
		return "", err
	}
	return trashDir + "/" + kept, nil
}

// search returns the lines that contain q (case does not matter), at most limit of them.
func (s *store) search(q string, limit int) ([]Hit, error) {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil, errors.New("search needs something to look for")
	}
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	notes, err := s.list()
	if err != nil {
		return nil, err
	}
	root, err := s.root()
	if err != nil {
		return nil, err
	}
	hits := []Hit{}
	for _, n := range notes {
		f, err := os.Open(filepath.Join(root, filepath.FromSlash(n.Path))) //nolint:gosec // n.Path comes from list, which walks the notes folder
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for line := 1; sc.Scan(); line++ {
			if strings.Contains(strings.ToLower(sc.Text()), q) {
				hits = append(hits, Hit{Path: n.Path, Title: n.Title, Line: line, Text: clip(strings.TrimSpace(sc.Text()), 240)})
				if len(hits) >= limit {
					break
				}
			}
		}
		_ = f.Close()
		if len(hits) >= limit {
			break
		}
	}
	return hits, nil
}

var wikiRe = regexp.MustCompile(`\[\[([^\]|#]+)(?:[|#][^\]]*)?\]\]`)

// links are the notes a text links to with [[name]], as written.
func links(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range wikiRe.FindAllStringSubmatch(text, -1) {
		name := strings.TrimSpace(m[1])
		if name != "" && !seen[strings.ToLower(name)] {
			seen[strings.ToLower(name)] = true
			out = append(out, name)
		}
	}
	return out
}

// resolve finds the note a [[name]] means: by path, by file name, then by title.
func resolve(notes []Note, name string) (Note, bool) {
	want := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), ".md"))
	for _, pass := range []func(Note) string{
		func(n Note) string { return strings.TrimSuffix(n.Path, ".md") },
		func(n Note) string { return strings.TrimSuffix(filepath.Base(n.Path), ".md") },
		func(n Note) string { return n.Title },
	} {
		for _, n := range notes {
			if strings.ToLower(pass(n)) == want {
				return n, true
			}
		}
	}
	return Note{}, false
}

// backlinks are the notes that link to the note at p.
func (s *store) backlinks(p string) ([]Note, error) {
	rel, _, err := s.abs(p)
	if err != nil {
		return nil, err
	}
	notes, err := s.list()
	if err != nil {
		return nil, err
	}
	root, err := s.root()
	if err != nil {
		return nil, err
	}
	out := []Note{}
	for _, n := range notes {
		if n.Path == rel {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(n.Path))) //nolint:gosec // n.Path comes from list, which walks the notes folder
		if err != nil {
			continue
		}
		for _, name := range links(string(b)) {
			if target, ok := resolve(notes, name); ok && target.Path == rel {
				out = append(out, n)
				break
			}
		}
	}
	return out, nil
}

// outline is a note's headings, indented by level.
func outline(text string) []string {
	var out []string
	fenced := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if fenced || !strings.HasPrefix(line, "#") {
			continue
		}
		level := len(line) - len(strings.TrimLeft(line, "#"))
		if level > 6 || len(line) <= level || line[level] != ' ' {
			continue
		}
		out = append(out, strings.Repeat("  ", level-1)+strings.TrimSpace(line[level:]))
	}
	return out
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
