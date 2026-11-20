package workbench

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The library: every agent the Workbench keeps, under one root.
//
//	<root>/<id>/agent/            the agent directory (authored files only)
//	<root>/<id>/workbench/        the Workbench's sidecar
//	    meta.json                 name, created, source
//	    steering.json             reflexes, triggers, presets (steer-shaped)
//	    options.json              runtime options (compact_at), passed as agent.Options
//	    versions/000001/          one per save: tree/ (every file), meta.json
//	    evals/<suite>.yaml        eval suites
//	    runs/<run>.json           eval runs, keyed by digest
//	    proposals/<id>.json       improvement proposals
//	<root>/.trash/<id>-<time>/    deleted agents

// Meta is what the library keeps about an agent besides its files.
type Meta struct {
	ID       string `json:"id"`
	Created  string `json:"created"`
	Source   string `json:"source"` // template:<name>, import:<archive>, dir:<path>, duplicate:<id>
	Template string `json:"template,omitempty"`
}

// Agent is an agent as the library lists it.
type Agent struct {
	Meta
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Digest      string `json:"digest"`
	Version     int    `json:"version"`
	Updated     string `json:"updated"`
	OK          bool   `json:"ok"`
}

// Version is one saved state of an agent.
type Version struct {
	N              int    `json:"n"`
	Digest         string `json:"digest"`
	AgentdirDigest string `json:"agentdir_digest,omitempty"`
	Note           string `json:"note"`
	Author         string `json:"author"`
	Created        string `json:"created"`
	Files          int    `json:"files"`
}

// ErrInvalid is a save refused because the files do not validate; the report says why.
type ErrInvalid struct{ Report Report }

func (e *ErrInvalid) Error() string {
	var msgs []string
	for _, p := range e.Report.Problems {
		if p.Severity == "error" {
			if p.File != "" {
				msgs = append(msgs, p.File+": "+p.Message)
			} else {
				msgs = append(msgs, p.Message)
			}
		}
	}
	return "the agent does not validate: " + strings.Join(msgs, "; ")
}

var (
	idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	// ErrNotFound: no agent with that id.
	ErrNotFound = errors.New("workbench: no such agent")
	// ErrExists: an agent with that id is already in the library.
	ErrExists = errors.New("workbench: an agent with that id already exists")
)

// Library keeps agents under a root directory.
type Library struct {
	root func() string
	now  func() time.Time

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewLibrary returns a library whose root is read from root at each use (a live setting).
func NewLibrary(root func() string) *Library {
	return &Library{root: root, now: time.Now, locks: map[string]*sync.Mutex{}}
}

func (l *Library) lock(id string) func() {
	l.mu.Lock()
	m, ok := l.locks[id]
	if !ok {
		m = &sync.Mutex{}
		l.locks[id] = m
	}
	l.mu.Unlock()
	m.Lock()
	return m.Unlock
}

// Root is the library's directory, created when missing.
func (l *Library) Root() (string, error) {
	r := l.root()
	if r == "" {
		return "", errors.New("workbench: no library root")
	}
	return r, os.MkdirAll(r, 0o700)
}

func (l *Library) dirs(id string) (agentDir, side string, err error) {
	if !idRe.MatchString(id) {
		return "", "", fmt.Errorf("workbench: %q is not an agent id (lower-case letters, digits, dashes)", id)
	}
	root, err := l.Root()
	if err != nil {
		return "", "", err
	}
	base := filepath.Join(root, id)
	if _, err := os.Stat(filepath.Join(base, "agent")); err != nil { //nolint:gosec // inside the library root; the id is checked (idRe)
		if errors.Is(err, fs.ErrNotExist) {
			return "", "", ErrNotFound
		}
		return "", "", err
	}
	return filepath.Join(base, "agent"), filepath.Join(base, "workbench"), nil
}

// AgentDir is the agent directory of id, as a host would open it.
func (l *Library) AgentDir(id string) (string, error) {
	d, _, err := l.dirs(id)
	return d, err
}

// Sidecar is the Workbench's directory for id.
func (l *Library) Sidecar(id string) (string, error) {
	_, s, err := l.dirs(id)
	return s, err
}

// List returns every agent, by id.
func (l *Library) List() ([]Agent, error) {
	root, err := l.Root()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	out := []Agent{}
	for _, e := range entries {
		if !e.IsDir() || !idRe.MatchString(e.Name()) {
			continue
		}
		a, err := l.Get(e.Name())
		if err != nil {
			continue
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Get returns one agent's listing.
func (l *Library) Get(id string) (Agent, error) {
	_, side, err := l.dirs(id)
	if err != nil {
		return Agent{}, err
	}
	var m Meta
	if b, rerr := os.ReadFile(filepath.Join(side, "meta.json")); rerr == nil { //nolint:gosec // the library's own file
		_ = json.Unmarshal(b, &m)
	}
	m.ID = id
	a := Agent{Meta: m, Name: id}
	vs, _ := l.Versions(id)
	if len(vs) > 0 {
		v := vs[len(vs)-1]
		a.Version, a.Digest, a.Updated = v.N, v.Digest, v.Created
	}
	t, terr := l.Tree(id)
	if terr != nil {
		// Listed as it is, unreadable files and all: the editor shows why.
		return a, nil //nolint:nilerr // an agent whose files cannot be read is still listed
	}
	rep := Validate(t)
	a.OK, a.Digest = rep.OK, rep.Digest
	if rep.Summary != nil {
		a.Name, a.Description = rep.Summary.Name, rep.Summary.Description
	}
	return a, nil
}

// Tree reads id's authored and sidecar files.
func (l *Library) Tree(id string) (Tree, error) {
	agentDir, side, err := l.dirs(id)
	if err != nil {
		return nil, err
	}
	t, _, err := readTree(agentDir, side)
	return t, err
}

// Create adds a new agent with the given files, which must validate. It records version 1.
//
//nolint:gosec // every path is inside the library root: the id is checked against idRe
func (l *Library) Create(id string, t Tree, meta Meta, note, author string) (Agent, Report, error) {
	if !idRe.MatchString(id) {
		return Agent{}, Report{}, fmt.Errorf("workbench: %q is not an agent id (lower-case letters, digits, dashes)", id)
	}
	rep := Validate(t)
	if !rep.OK {
		return Agent{}, rep, &ErrInvalid{rep}
	}
	root, err := l.Root()
	if err != nil {
		return Agent{}, rep, err
	}
	unlock := l.lock(id)
	defer unlock()
	base := filepath.Join(root, id)
	if err = os.Mkdir(base, 0o700); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return Agent{}, rep, ErrExists
		}
		return Agent{}, rep, err
	}
	side := filepath.Join(base, "workbench")
	if err = writeAgentDir(filepath.Join(base, "agent"), t); err != nil {
		_ = os.RemoveAll(base)
		return Agent{}, rep, err
	}
	if err = writeSidecar(side, t); err != nil {
		_ = os.RemoveAll(base)
		return Agent{}, rep, err
	}
	meta.ID = id
	if meta.Created == "" {
		meta.Created = l.now().UTC().Format(time.RFC3339)
	}
	mb, _ := json.MarshalIndent(meta, "", "  ")
	if err = os.WriteFile(filepath.Join(side, "meta.json"), mb, 0o600); err != nil {
		_ = os.RemoveAll(base)
		return Agent{}, rep, err
	}
	if _, err = l.addVersion(side, t, rep, note, author); err != nil {
		_ = os.RemoveAll(base)
		return Agent{}, rep, err
	}
	a, err := l.Get(id)
	return a, rep, err
}

// Save replaces an agent's authored files with t when t validates, keeping every saved
// state in its versions. Only authored files are written or removed. base, when not empty, is the digest the
// caller edited from; a save over a newer state is refused.
func (l *Library) Save(id string, t Tree, base, note, author string) (Version, Report, error) {
	agentDir, side, err := l.dirs(id)
	if err != nil {
		return Version{}, Report{}, err
	}
	rep := Validate(t)
	if !rep.OK {
		return Version{}, rep, &ErrInvalid{rep}
	}
	unlock := l.lock(id)
	defer unlock()
	cur, _, err := readTree(agentDir, side)
	if err != nil {
		return Version{}, rep, err
	}
	if base != "" && cur.Digest() != base {
		return Version{}, rep, errors.New("workbench: the agent changed since you opened it; reload before saving")
	}
	vs, _ := l.Versions(id)
	var last *Version
	if len(vs) > 0 {
		last = &vs[len(vs)-1]
	}
	if last != nil && cur.Digest() == rep.Digest && last.Digest == rep.Digest {
		return *last, rep, nil // nothing changes
	}
	// Files changed on disk by hand since the last version (added, edited, removed) are kept
	// as a version of their own before anything overwrites them.
	if last == nil || cur.Digest() != last.Digest {
		what := strings.TrimSpace(note)
		if what == "" {
			what = "a save"
		}
		if _, err = l.addVersion(side, cur, Validate(cur), "unsaved changes on disk, kept before: "+what, "disk"); err != nil {
			return Version{}, rep, err
		}
		if cur.Digest() == rep.Digest {
			v, verr := l.Versions(id)
			if verr == nil && len(v) > 0 {
				return v[len(v)-1], rep, nil
			}
		}
	}
	// The version is written first, then the files: after a crash in between, the last
	// version is what was being saved, and the next save keeps whatever is on disk.
	v, err := l.addVersion(side, t, rep, note, author)
	if err != nil {
		return Version{}, rep, err
	}
	// In place, file by file: changed authored files are replaced atomically, removed ones
	// deleted, and nothing else in the directory is touched (Nanite's .nanite/ state, notes,
	// anything that is not an authored file). The directory itself is never moved, so a host
	// running the agent from it keeps running, and there is no moment without it.
	if err = applyTree(agentDir, cur, t); err != nil {
		return v, rep, err
	}
	if err = writeSidecar(side, t); err != nil {
		return v, rep, err
	}
	return v, rep, nil
}

//nolint:gosec // every path is inside the agent's sidecar; tree paths are authored paths
func (l *Library) addVersion(side string, t Tree, rep Report, note, author string) (Version, error) {
	dir := filepath.Join(side, "versions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Version{}, err
	}
	n := 1
	if vs, err := readVersions(dir); err == nil && len(vs) > 0 {
		n = vs[len(vs)-1].N + 1
	}
	vdir := filepath.Join(dir, fmt.Sprintf("%06d", n))
	tree := filepath.Join(vdir, "tree")
	if err := os.MkdirAll(tree, 0o700); err != nil {
		return Version{}, err
	}
	for _, p := range t.Paths() {
		target := filepath.Join(tree, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return Version{}, err
		}
		if err := os.WriteFile(target, t[p], 0o600); err != nil {
			return Version{}, err
		}
	}
	if author == "" {
		author = "owner"
	}
	v := Version{N: n, Digest: rep.Digest, AgentdirDigest: rep.AgentdirDigest, Note: strings.TrimSpace(note), Author: author,
		Created: l.now().UTC().Format(time.RFC3339), Files: len(t)}
	b, _ := json.MarshalIndent(v, "", "  ")
	return v, os.WriteFile(filepath.Join(vdir, "meta.json"), b, 0o600)
}

func readVersions(dir string) ([]Version, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Version
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil || !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name(), "meta.json")) //nolint:gosec // the library's own file
		if err != nil {
			continue
		}
		var v Version
		if json.Unmarshal(b, &v) == nil {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].N < out[j].N })
	return out, nil
}

// Versions lists id's versions, oldest first.
func (l *Library) Versions(id string) ([]Version, error) {
	_, side, err := l.dirs(id)
	if err != nil {
		return nil, err
	}
	return readVersions(filepath.Join(side, "versions"))
}

// VersionTree reads the files of version n.
func (l *Library) VersionTree(id string, n int) (Tree, error) {
	_, side, err := l.dirs(id)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(side, "versions", fmt.Sprintf("%06d", n), "tree")
	if _, err = os.Stat(dir); err != nil {
		return nil, fmt.Errorf("workbench: %s has no version %d", id, n)
	}
	t := Tree{}
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p) //nolint:gosec // the library's own version copy
		if err != nil {
			return err
		}
		t[filepath.ToSlash(rel)] = b
		return nil
	})
	return t, err
}

// Rollback saves version n's files as a new version.
func (l *Library) Rollback(id string, n int, author string) (Version, Report, error) {
	t, err := l.VersionTree(id, n)
	if err != nil {
		return Version{}, Report{}, err
	}
	// No base: a rollback is a decision about the whole agent. What is on disk and not yet
	// in a version is kept first, by Save.
	return l.Save(id, t, "", fmt.Sprintf("rolled back to version %d", n), author)
}

// Delete moves an agent to the library's trash; nothing is removed.
func (l *Library) Delete(id string) (string, error) {
	if _, _, err := l.dirs(id); err != nil {
		return "", err
	}
	unlock := l.lock(id)
	defer unlock()
	root, err := l.Root()
	if err != nil {
		return "", err
	}
	trash := filepath.Join(root, ".trash")
	if err := os.MkdirAll(trash, 0o700); err != nil {
		return "", err
	}
	dest := filepath.Join(trash, id+"-"+l.now().UTC().Format("20060102T150405.000000000"))
	return dest, os.Rename(filepath.Join(root, id), dest) //nolint:gosec // a checked id or name inside the library; the key file the owner's config names
}

// FreeID returns want, or want-2, want-3, ... whichever is not taken.
func (l *Library) FreeID(want string) (string, error) {
	want = slug(want)
	root, err := l.Root()
	if err != nil {
		return "", err
	}
	for i := 1; i < 1000; i++ {
		id := want
		if i > 1 {
			id = fmt.Sprintf("%s-%d", want, i)
		}
		if _, err := os.Stat(filepath.Join(root, id)); errors.Is(err, fs.ErrNotExist) { //nolint:gosec // a checked id or name inside the library; the key file the owner's config names
			return id, nil
		}
	}
	return "", errors.New("workbench: no free id")
}

var nonSlug = regexp.MustCompile(`[^a-z0-9-]+`)

// slug makes a name into an agent id.
func slug(s string) string {
	s = nonSlug.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-")
	s = strings.Trim(s, "-")
	if len(s) > 50 {
		s = strings.Trim(s[:50], "-")
	}
	if s == "" {
		s = "agent"
	}
	return s
}

// applyTree changes dir's authored files from cur to next: writes what is new or changed
// (each through a temporary file and a rename), removes the authored files next lacks, and
// leaves everything else alone.
//
//nolint:gosec // every path is an authored path (agentPath): relative, clean, inside dir
func applyTree(dir string, cur, next Tree) error {
	for _, p := range next.Paths() {
		if !agentPath(p) {
			continue
		}
		if old, ok := cur[p]; ok && bytes.Equal(old, next[p]) {
			continue
		}
		target := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		if err := writeFileAtomic(target, next[p]); err != nil {
			return err
		}
	}
	for _, p := range cur.Paths() {
		if _, keep := next[p]; keep || !agentPath(p) {
			continue
		}
		target := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		// An emptied skill, context or variants directory goes too; one that still holds
		// anything (a file that is not authored) stays.
		for d := filepath.Dir(target); d != dir && strings.HasPrefix(d, dir+string(filepath.Separator)); d = filepath.Dir(d) {
			if os.Remove(d) != nil {
				break
			}
		}
	}
	return nil
}

// Recover puts the library right after a crash: an agent directory left aside by an older
// Workbench's directory swap is put back, and temporary files of an interrupted write are
// removed. Only the library's own leftovers are touched.
//
//nolint:gosec // paths under the library root
func (l *Library) Recover() error {
	root, err := l.Root()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() || !idRe.MatchString(e.Name()) {
			continue
		}
		base := filepath.Join(root, e.Name())
		agentDir := filepath.Join(base, "agent")
		if _, err := os.Stat(agentDir); errors.Is(err, fs.ErrNotExist) {
			if _, err := os.Stat(agentDir + ".old"); err == nil {
				if err := os.Rename(agentDir+".old", agentDir); err != nil {
					return err
				}
			}
		}
		_ = os.RemoveAll(agentDir + ".next")
		for _, d := range []string{agentDir, filepath.Join(base, "workbench")} {
			_ = filepath.WalkDir(d, func(p string, de fs.DirEntry, err error) error {
				if err != nil {
					return nil //nolint:nilerr // best effort: a part that cannot be read is left as it is
				}
				// Hidden directories (.nanite/, .git/, the owner's own) are never entered.
				if de.IsDir() && p != d && strings.HasPrefix(de.Name(), ".") {
					return filepath.SkipDir
				}
				if de.Type().IsRegular() && strings.HasPrefix(de.Name(), tmpPrefix) {
					_ = os.Remove(p)
				}
				return nil
			})
		}
	}
	return nil
}
