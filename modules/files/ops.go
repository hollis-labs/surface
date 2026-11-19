package files

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Entry is one item of a folder listing.
type Entry struct {
	Name string `json:"name"`
	// Path is relative to the place, slash separated.
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
	// Size is in bytes; zero for a folder.
	Size int64 `json:"size"`
	// Modified is RFC 3339, UTC.
	Modified string `json:"modified"`
	Symlink  bool   `json:"symlink,omitempty"`
	// Locked marks an entry on the denylist: it is listed, and nothing else can be done
	// with it.
	Locked bool `json:"locked,omitempty"`
}

func hidden(name string) bool { return strings.HasPrefix(name, ".") }

// list returns a folder's entries, folders first and then by name. Dotfiles are left out
// unless showHidden.
func list(l loc, showHidden bool) ([]Entry, error) {
	des, err := os.ReadDir(l.Abs)
	if err != nil {
		return nil, describe(err, l)
	}
	out := make([]Entry, 0, len(des))
	for _, de := range des {
		if !showHidden && hidden(de.Name()) {
			continue
		}
		fi, err := de.Info()
		if err != nil {
			continue // gone since the directory was read
		}
		e := Entry{Name: de.Name(), Path: path.Join(l.Rel, de.Name()), Symlink: fi.Mode()&os.ModeSymlink != 0}
		if e.Symlink {
			// Show what the link leads to; a broken one keeps the link's own details.
			if target, err := os.Stat(filepath.Join(l.Abs, de.Name())); err == nil {
				fi = target
			}
		}
		e.Dir = fi.IsDir()
		if !e.Dir {
			e.Size = fi.Size()
		}
		e.Modified = fi.ModTime().UTC().Format(time.RFC3339)
		e.Locked = sensitive(e.Path) || sensitive(filepath.Join(l.Abs, de.Name()))
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// names returns up to limit entry names of a folder (folders with a trailing slash) and how
// many entries it has. It reads the directory and nothing else, so it is cheap enough to
// call while the agent's context is assembled.
func names(l loc, showHidden bool, limit int) ([]string, int, error) {
	des, err := os.ReadDir(l.Abs)
	if err != nil {
		return nil, 0, describe(err, l)
	}
	var out []string
	total := 0
	for _, de := range des {
		if !showHidden && hidden(de.Name()) {
			continue
		}
		total++
		if len(out) < limit {
			n := de.Name()
			if de.IsDir() {
				n += "/"
			}
			out = append(out, n)
		}
	}
	return out, total, nil
}

// Text is part of a text file.
type Text struct {
	Text string `json:"text"`
	// Truncated says the file goes on after Text.
	Truncated bool `json:"truncated"`
	// Size is the whole file's size in bytes.
	Size int64 `json:"size"`
	// StartLine and EndLine are the first and last line in Text, counted from 1. EndLine
	// is zero when Text is empty.
	StartLine int `json:"start_line"`
	EndLine   int `json:"end_line"`
}

var errBinary = errors.New("it is not a text file")

// sniffLen is how much of a file is looked at to tell text from binary.
const sniffLen = 8000

// looksBinary reports whether the start of a file holds a NUL byte, which text does not.
func looksBinary(head []byte) bool { return bytes.IndexByte(head, 0) >= 0 }

// readText reads lines of a text file: at most limit lines starting at line offset
// (counted from 1), and at most maxBytes bytes. A limit of zero means no line limit. A
// binary file is refused.
func readText(l loc, offset, limit int, maxBytes int64) (Text, error) {
	f, err := os.Open(l.Abs)
	if err != nil {
		return Text{}, describe(err, l)
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return Text{}, err
	}
	if fi.IsDir() {
		return Text{}, fmt.Errorf("%s is a folder; list it instead", l.name())
	}
	if !fi.Mode().IsRegular() {
		return Text{}, fmt.Errorf("%s is not a regular file", l.name())
	}
	br := bufio.NewReaderSize(f, 64<<10)
	if head, _ := br.Peek(sniffLen); looksBinary(head) {
		return Text{}, fmt.Errorf("%s cannot be shown as text: %w", l.name(), errBinary)
	}
	t, err := scanText(br, 1, offset, limit, maxBytes)
	t.Size = fi.Size()
	return t, err
}

// scanText reads lines from br, whose first line is line firstLine of the file: at most
// limit lines starting at line offset, and at most maxBytes bytes. Size is left to the
// caller.
func scanText(br *bufio.Reader, firstLine, offset, limit int, maxBytes int64) (Text, error) {
	if offset < 1 {
		offset = 1
	}
	t := Text{StartLine: offset}
	var b bytes.Buffer
	line := firstLine
	for {
		// A line longer than the buffer comes in pieces (ErrBufferFull); they are taken
		// one by one, so a huge line costs no more memory than maxBytes.
		chunk, err := br.ReadSlice('\n')
		if len(chunk) > 0 && line >= offset {
			if limit > 0 && line >= offset+limit {
				t.Truncated = true
				break
			}
			if room := maxBytes - int64(b.Len()); int64(len(chunk)) > room {
				if room > 0 {
					b.Write(chunk[:room])
					t.EndLine = line
				}
				t.Truncated = true
				break
			}
			b.Write(chunk)
			t.EndLine = line
		}
		if len(chunk) > 0 && chunk[len(chunk)-1] == '\n' {
			line++
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil && !errors.Is(err, bufio.ErrBufferFull) {
			return Text{}, err
		}
	}
	text := b.Bytes()
	if t.Truncated {
		// The byte limit may have cut a character in half.
		for i := 0; i < utf8.UTFMax-1 && len(text) > 0; i++ {
			if r, size := utf8.DecodeLastRune(text); r != utf8.RuneError || size != 1 {
				break
			}
			text = text[:len(text)-1]
		}
	}
	t.Text = strings.ToValidUTF8(string(text), "�")
	return t, nil
}

// writeText creates a text file or replaces one. It never replaces a folder or a binary
// file, and with createOnly it never replaces anything. Missing parent folders are
// created. It reports whether the file is new.
func writeText(l loc, text string, createOnly bool) (created bool, err error) {
	if l.Rel == "" {
		return false, errors.New("that is the place itself; give the path of a file in it")
	}
	fi, err := os.Stat(l.Abs)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err = os.MkdirAll(filepath.Dir(l.Abs), 0o755); err != nil { //nolint:gosec // the owner's own folders, in a place they chose
			return false, err
		}
		var f *os.File
		f, err = os.OpenFile(l.Abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) //nolint:gosec // the owner's own file, readable like the others beside it
		if err != nil {
			return false, describe(err, l)
		}
		_, err = f.WriteString(text)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return true, err
	case err != nil:
		return false, err
	case fi.IsDir():
		return false, fmt.Errorf("%s is a folder; give the path of a file", l.name())
	case createOnly:
		return false, fmt.Errorf("%w: %s; pick another name", errExists, l.Rel)
	case !fi.Mode().IsRegular():
		return false, fmt.Errorf("%s is not a regular file", l.name())
	}
	if err := checkText(l); err != nil {
		return false, err
	}
	return false, replace(l.Abs, text, fi.Mode().Perm())
}

// checkText refuses a file that is not text, so a write cannot replace an image or an
// archive with a string.
func checkText(l loc) error {
	f, err := os.Open(l.Abs)
	if err != nil {
		return describe(err, l)
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, sniffLen)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return err
	}
	if looksBinary(head[:n]) {
		return fmt.Errorf("%s was left as it is: %w, and Files only replaces text", l.name(), errBinary)
	}
	return nil
}

// replace swaps a file's content in one step: the text goes to a temporary file beside
// it, which is then renamed over it. A reader sees the old content or the new, never half.
func replace(abs, text string, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(abs), "."+filepath.Base(abs)+".*.tmp")
	if err != nil {
		return err
	}
	_, err = tmp.WriteString(text)
	if err == nil {
		err = tmp.Chmod(perm)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), abs)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}

// makeDir creates a folder, and its missing parents. A taken name is an error.
func makeDir(l loc) error {
	if _, err := os.Lstat(l.Abs); err == nil {
		return fmt.Errorf("%w: %s; pick another name", errExists, l.Rel)
	}
	return os.MkdirAll(l.Abs, 0o755) //nolint:gosec // the owner's own folder, in a place they chose
}

// move renames or moves a file or folder inside one place. Both ends are located without
// following a final link, so a link is moved itself. The destination is the full new
// path; a taken one is an error, never replaced.
func move(from, to loc) error {
	if from.Rel == "" || to.Rel == "" {
		return errors.New("a place itself cannot be moved or replaced")
	}
	src, err := os.Lstat(from.Abs)
	if err != nil {
		return describe(err, from)
	}
	if strings.HasPrefix(to.Abs, from.Abs+string(filepath.Separator)) {
		return fmt.Errorf("%s cannot be moved into itself", from.name())
	}
	// On a file system that ignores case, "notes.md" to "Notes.md" finds the file itself
	// at the destination; that is a rename, not a collision.
	if dst, err := os.Lstat(to.Abs); err == nil && (from.Abs == to.Abs || !os.SameFile(src, dst)) {
		return fmt.Errorf("%w: %s; pick another name", errExists, to.Rel)
	}
	if fi, err := os.Stat(filepath.Dir(to.Abs)); err != nil || !fi.IsDir() {
		return fmt.Errorf("%w: the folder %s is not there; create it first", errNotFound, path.Dir(to.Rel))
	}
	return os.Rename(from.Abs, to.Abs)
}

// remove deletes a regular file, a symbolic link (the link, not its target) or an empty
// folder. There is no recursive delete: a folder with anything in it is refused.
func remove(l loc) error {
	if l.Rel == "" {
		return errors.New("a place's own folder is never deleted; remove the place instead, which keeps its files")
	}
	fi, err := os.Lstat(l.Abs)
	if err != nil {
		return describe(err, l)
	}
	switch {
	case fi.IsDir():
		des, err := os.ReadDir(l.Abs)
		if err != nil {
			return err
		}
		if len(des) > 0 {
			return fmt.Errorf("%s is not empty (%d items); delete what is in it first", l.name(), len(des))
		}
	case !fi.Mode().IsRegular() && fi.Mode()&os.ModeSymlink == 0:
		return fmt.Errorf("%s is not a regular file", l.name())
	}
	return os.Remove(l.Abs)
}

// describe puts a file system error in the owner's terms: the path as they gave it, not
// the real path on disk.
func describe(err error, l loc) error {
	what := l.Rel
	if what == "" {
		what = l.Place.Name
	}
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("%w: %s is not there", errNotFound, what)
	case errors.Is(err, os.ErrExist):
		return fmt.Errorf("%w: %s", errExists, what)
	case errors.Is(err, os.ErrPermission):
		return fmt.Errorf("this machine does not allow that on %s (permission denied)", what)
	}
	var pe *os.PathError
	if errors.As(err, &pe) {
		return fmt.Errorf("%s: %w", what, pe.Err)
	}
	return err
}
