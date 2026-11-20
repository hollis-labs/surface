package workbench

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The authored files of an agent directory, as the Workbench reads, writes, versions and
// exports them. Anything else in a directory (Nanite's .nanite state, backups, notes) is not
// part of the agent: an import skips it and a write refuses it.

// Sidecar files: the Workbench's own state about an agent, kept beside the agent directory
// (never inside it) and carried in versions and exports under "workbench/".
const (
	steeringFile = "workbench/steering.json"
	optionsFile  = "workbench/options.json"
)

// Limits on one agent's files.
const (
	maxFileBytes  = 1 << 20  // agentdir's own limit per authored file
	maxTreeBytes  = 16 << 20 // the whole authored tree
	maxTreeFiles  = 512
	maxSkillDepth = 4 // skills/<name>/a/b/c
)

var (
	segmentRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	topFiles  = map[string]bool{"agent.md": true, "host.yaml": true, ".mcp.json": true, "permissions.yaml": true, "providers.yaml": true}
)

// authoredPath reports whether rel (slash-separated, relative) names an authored file of an
// agent directory, or one of the Workbench's sidecar files.
func authoredPath(rel string) bool {
	if rel == steeringFile || rel == optionsFile {
		return true
	}
	if !safeRel(rel) {
		return false
	}
	if topFiles[rel] {
		return true
	}
	parts := strings.Split(rel, "/")
	for _, p := range parts {
		if !segmentRe.MatchString(p) {
			return false
		}
	}
	switch parts[0] {
	case "context", "variants":
		return len(parts) == 2 && strings.HasSuffix(parts[1], ".md")
	case "skills":
		return len(parts) >= 3 && len(parts) <= maxSkillDepth+1
	}
	return false
}

// agentPath reports whether rel is an authored file of the agent directory itself (not a
// sidecar file).
func agentPath(rel string) bool { return authoredPath(rel) && !strings.HasPrefix(rel, "workbench/") }

// safeRel reports whether rel is a clean, relative, slash-separated path that stays inside
// its root.
func safeRel(rel string) bool {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "\\") || strings.ContainsRune(rel, 0) {
		return false
	}
	if path.Clean(rel) != rel {
		return false
	}
	for _, p := range strings.Split(rel, "/") {
		if p == ".." || p == "." || p == "" {
			return false
		}
	}
	return !filepath.IsAbs(rel)
}

// Tree is an agent's authored files by relative path, sidecar files included.
type Tree map[string][]byte

// Paths are the tree's paths, sorted.
func (t Tree) Paths() []string {
	out := make([]string, 0, len(t))
	for p := range t {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Clone copies the tree.
func (t Tree) Clone() Tree {
	out := make(Tree, len(t))
	for k, v := range t {
		out[k] = append([]byte(nil), v...)
	}
	return out
}

// Digest is a SHA-256 over every file in path order, length-prefixed like agentdir's own
// digest so that no two trees share one by concatenation. It covers what agentdir's digest
// leaves out (variants/, providers.yaml, skill resources) and the sidecar files.
func (t Tree) Digest() string {
	h := sha256.New()
	for _, p := range t.Paths() {
		fmt.Fprintf(h, "%d:%s%d:", len(p), p, len(t[p]))
		h.Write(t[p])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// check enforces the size limits.
func (t Tree) check() error {
	if len(t) > maxTreeFiles {
		return fmt.Errorf("an agent has at most %d files", maxTreeFiles)
	}
	total := 0
	for p, b := range t {
		if !authoredPath(p) {
			return fmt.Errorf("%s is not an authored path", p)
		}
		if len(b) > maxFileBytes {
			return fmt.Errorf("%s is larger than 1 MiB", p)
		}
		total += len(b)
	}
	if total > maxTreeBytes {
		return fmt.Errorf("the agent's files come to more than %d MiB", maxTreeBytes>>20)
	}
	return nil
}

// readTree reads the authored files of an agent directory and, when side is not empty, the
// sidecar files from it. A symlink anywhere is an error; other files are listed in skipped.
func readTree(agentDir, side string) (Tree, []string, error) {
	t := Tree{}
	var skipped []string
	err := filepath.WalkDir(agentDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(agentDir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink; an agent directory holds real files only", rel)
		}
		if d.IsDir() {
			if rel == ".nanite" || strings.HasPrefix(d.Name(), ".") {
				skipped = append(skipped, rel+"/")
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !agentPath(rel) {
			skipped = append(skipped, rel)
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxFileBytes {
			return fmt.Errorf("%s is larger than 1 MiB", rel)
		}
		b, err := os.ReadFile(p) //nolint:gosec // a file of the agent directory being read, checked above
		if err != nil {
			return err
		}
		t[rel] = b
		if len(t) > maxTreeFiles {
			return fmt.Errorf("more than %d authored files", maxTreeFiles)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if side != "" {
		for _, name := range []string{steeringFile, optionsFile} {
			b, err := os.ReadFile(filepath.Join(side, strings.TrimPrefix(name, "workbench/"))) //nolint:gosec // a fixed sidecar file name
			switch {
			case errors.Is(err, fs.ErrNotExist):
			case err != nil:
				return nil, nil, err
			default:
				t[name] = b
			}
		}
	}
	return t, skipped, t.check()
}

// writeAgentDir writes the tree's agent files into dir, which must not exist yet.
func writeAgentDir(dir string, t Tree) error {
	if err := os.MkdirAll(dir, 0o700); err != nil { //nolint:gosec // the library's own directory or a temporary one
		return err
	}
	for _, p := range t.Paths() {
		if !agentPath(p) {
			continue
		}
		target := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil { //nolint:gosec // p is an authored path (agentPath): relative, clean, inside dir
			return err
		}
		if err := os.WriteFile(target, t[p], 0o600); err != nil { //nolint:gosec // as above
			return err
		}
	}
	return nil
}

// writeSidecar writes the tree's sidecar files into side, removing those the tree lacks.
func writeSidecar(side string, t Tree) error {
	if err := os.MkdirAll(side, 0o700); err != nil { //nolint:gosec // the library's own sidecar directory
		return err
	}
	for _, name := range []string{steeringFile, optionsFile} {
		target := filepath.Join(side, strings.TrimPrefix(name, "workbench/"))
		b, ok := t[name]
		if !ok {
			if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) { //nolint:gosec // a fixed sidecar file name
				return err
			}
			continue
		}
		if err := writeFileAtomic(target, b); err != nil {
			return err
		}
	}
	return nil
}

// writeFileAtomic writes a file through a temporary file and a rename.
func writeFileAtomic(name string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(name), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name()) //nolint:gosec // inside the library root; the id is checked (idRe)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name()) //nolint:gosec // inside the library root; the id is checked (idRe)
		return err
	}
	return os.Rename(tmp.Name(), name) //nolint:gosec // inside the library root; the id is checked (idRe)
}
