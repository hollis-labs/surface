package workbench

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Export and import. An archive is the agent directory itself (its authored files at the
// root, so it unpacks into a directory Nanite opens as it is), plus workbench.json, the
// manifest, and the Workbench's sidecar under workbench/ (steering, options, eval suites).
// Nanite's run-time state (.nanite/) and anything else that is not authored is never
// exported.

// WorkbenchVersion is the Workbench's version, written into each export's manifest.
const WorkbenchVersion = "0.1.0"

// Manifest is workbench.json.
type Manifest struct {
	Format           int      `json:"workbench_format"`
	Name             string   `json:"name"`
	ID               string   `json:"id"`
	Digest           string   `json:"digest"`
	AgentdirDigest   string   `json:"agentdir_digest,omitempty"`
	Created          string   `json:"created"`
	WorkbenchVersion string   `json:"workbench_version"`
	Source           string   `json:"source,omitempty"`
	Version          int      `json:"version,omitempty"`
	Files            []string `json:"files"`
	Suites           []string `json:"suites,omitempty"`
}

// Import limits.
const (
	maxArchiveBytes  = 32 << 20 // the archive as uploaded
	maxArchiveFiles  = 1000     // entries in it
	maxUnpackedBytes = 24 << 20 // everything it unpacks to
	maxSuiteBytes    = 256 << 10
)

var suiteNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}\.yaml$`)

// Bundle is what an archive holds: the tree, the eval suites and the manifest.
type Bundle struct {
	Tree     Tree
	Suites   map[string][]byte
	Manifest *Manifest
	// Skipped are entries that are not part of an agent (state, notes), left out.
	Skipped []string
}

// writeTarGz writes the bundle as a gzipped tar.
func writeTarGz(w io.Writer, b Bundle) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	mtime := time.Unix(0, 0).UTC()
	if b.Manifest != nil {
		if t, err := time.Parse(time.RFC3339, b.Manifest.Created); err == nil {
			mtime = t
		}
	}
	add := func(name string, data []byte) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), ModTime: mtime, Typeflag: tar.TypeReg, Format: tar.FormatPAX}); err != nil {
			return err
		}
		_, err := tw.Write(data)
		return err
	}
	if b.Manifest != nil {
		mb, err := json.MarshalIndent(b.Manifest, "", "  ")
		if err != nil {
			return err
		}
		if err := add("workbench.json", append(mb, '\n')); err != nil {
			return err
		}
	}
	for _, p := range b.Tree.Paths() {
		if err := add(p, b.Tree[p]); err != nil {
			return err
		}
	}
	names := make([]string, 0, len(b.Suites))
	for n := range b.Suites {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if err := add("workbench/evals/"+n, b.Suites[n]); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// readArchive reads a .tar.gz, .tgz or .zip archive, refusing anything unsafe: absolute
// paths, "..", links and devices, too many entries, too many bytes.
func readArchive(data []byte) (Bundle, error) {
	if len(data) > maxArchiveBytes {
		return Bundle{}, fmt.Errorf("the archive is larger than %d MiB", maxArchiveBytes>>20)
	}
	var b Bundle
	var err error
	switch {
	case bytes.HasPrefix(data, []byte{0x1f, 0x8b}):
		b, err = readTarGz(data)
	case bytes.HasPrefix(data, []byte("PK\x03\x04")), bytes.HasPrefix(data, []byte("PK\x05\x06")):
		b, err = readZip(data)
	default:
		return Bundle{}, errors.New("not a .tar.gz or .zip archive")
	}
	if err != nil {
		return Bundle{}, err
	}
	if _, ok := b.Tree["agent.md"]; !ok {
		return Bundle{}, errors.New("the archive has no agent.md at its root")
	}
	return b, b.Tree.check()
}

type entry struct {
	name    string
	regular bool
	dir     bool
	size    int64
	open    func() (io.Reader, error)
}

// maxStreamBytes caps everything decompressed from a .tar.gz, headers and skipped entries
// included: a small archive cannot make the reader inflate more than this.
const maxStreamBytes = maxUnpackedBytes + 4<<20

var errTooBig = fmt.Errorf("the archive unpacks to more than %d MiB", maxUnpackedBytes>>20)

// capped is a reader that fails once more than n bytes have been read from it.
type capped struct {
	r io.Reader
	n int64
}

func (c *capped) Read(p []byte) (int, error) {
	if c.n <= 0 {
		return 0, errTooBig
	}
	if int64(len(p)) > c.n {
		p = p[:c.n+1]
	}
	k, err := c.r.Read(p)
	c.n -= int64(k)
	if c.n < 0 {
		return k, errTooBig
	}
	return k, err
}

func readTarGz(data []byte) (Bundle, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return Bundle{}, err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(&capped{r: gz, n: maxStreamBytes})
	var entries []entry
	var total int64
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Bundle{}, err
		}
		if len(entries) >= maxArchiveFiles {
			return Bundle{}, fmt.Errorf("the archive has more than %d entries", maxArchiveFiles)
		}
		e := entry{name: h.Name, size: h.Size}
		switch h.Typeflag {
		case tar.TypeReg:
			e.regular = true
			if h.Size > maxFileBytes {
				return Bundle{}, fmt.Errorf("%s is larger than 1 MiB", h.Name)
			}
			if total+h.Size > maxUnpackedBytes {
				return Bundle{}, errTooBig
			}
			limited, err := io.ReadAll(io.LimitReader(tr, maxFileBytes+1))
			if err != nil {
				return Bundle{}, err
			}
			total += int64(len(limited))
			if total > maxUnpackedBytes {
				return Bundle{}, errTooBig
			}
			e.open = func() (io.Reader, error) { return bytes.NewReader(limited), nil }
			e.size = int64(len(limited))
		case tar.TypeDir:
			e.dir = true
		case tar.TypeXGlobalHeader:
			continue
		default:
			return Bundle{}, fmt.Errorf("%s: links and special files are not allowed in an agent archive", h.Name)
		}
		entries = append(entries, e)
	}
	return collect(entries)
}

func readZip(data []byte) (Bundle, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Bundle{}, err
	}
	if len(zr.File) > maxArchiveFiles {
		return Bundle{}, fmt.Errorf("the archive has more than %d entries", maxArchiveFiles)
	}
	var entries []entry
	var total int64
	for _, f := range zr.File {
		mode := f.Mode()
		e := entry{name: f.Name, size: int64(f.UncompressedSize64)} //nolint:gosec // checked against the limits below
		switch {
		case mode.IsDir():
			e.dir = true
		case mode.IsRegular():
			e.regular = true
			if f.UncompressedSize64 > maxFileBytes {
				return Bundle{}, fmt.Errorf("%s is larger than 1 MiB", f.Name)
			}
			e.open = func() (io.Reader, error) {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer func() { _ = rc.Close() }()
				// The header's size is the archive's claim; what is read is counted.
				b, err := io.ReadAll(io.LimitReader(rc, maxFileBytes+1))
				total += int64(len(b))
				if total > maxUnpackedBytes {
					return nil, errTooBig
				}
				return bytes.NewReader(b), err
			}
		default:
			return Bundle{}, fmt.Errorf("%s: links and special files are not allowed in an agent archive", f.Name)
		}
		entries = append(entries, e)
	}
	return collect(entries)
}

// collect checks every entry's path and size and sorts the files into the bundle.
func collect(entries []entry) (Bundle, error) {
	b := Bundle{Tree: Tree{}, Suites: map[string][]byte{}}
	var total int64
	seen := map[string]string{}
	for _, e := range entries {
		name := strings.TrimPrefix(e.name, "./")
		if e.dir {
			name = strings.TrimSuffix(name, "/")
			if name == "" || name == "." {
				continue
			}
		}
		if !safeRel(name) {
			return Bundle{}, fmt.Errorf("%q: archive paths must be relative and stay inside the archive", e.name)
		}
		if e.dir {
			continue
		}
		// Two names one case-insensitive file system (macOS) would write to the same file.
		if prev, dup := seen[strings.ToLower(name)]; dup {
			return Bundle{}, fmt.Errorf("%q and %q name the same file on a case-insensitive disk", prev, name)
		}
		seen[strings.ToLower(name)] = name
		if e.size > maxFileBytes {
			return Bundle{}, fmt.Errorf("%s is larger than 1 MiB", name)
		}
		total += e.size
		if total > maxUnpackedBytes {
			return Bundle{}, fmt.Errorf("the archive unpacks to more than %d MiB", maxUnpackedBytes>>20)
		}
		r, err := e.open()
		if err != nil {
			return Bundle{}, err
		}
		data, err := io.ReadAll(r)
		if err != nil {
			return Bundle{}, err
		}
		if len(data) > maxFileBytes {
			return Bundle{}, fmt.Errorf("%s is larger than 1 MiB", name)
		}
		switch {
		case name == "workbench.json":
			var m Manifest
			if err := json.Unmarshal(data, &m); err != nil {
				return Bundle{}, fmt.Errorf("workbench.json: %w", err)
			}
			b.Manifest = &m
		case strings.HasPrefix(name, "workbench/evals/"):
			n := strings.TrimPrefix(name, "workbench/evals/")
			if !suiteNameRe.MatchString(n) || len(data) > maxSuiteBytes {
				b.Skipped = append(b.Skipped, name)
				continue
			}
			b.Suites[n] = data
		case authoredPath(name):
			b.Tree[name] = data
		default:
			b.Skipped = append(b.Skipped, name)
		}
	}
	return b, nil
}

// readDirBundle reads an agent directory from anywhere on disk, without changing it: only
// its authored files, never .nanite/ or anything else; a symlink refuses the whole import.
func readDirBundle(dir string) (Bundle, error) {
	if !filepath.IsAbs(dir) {
		return Bundle{}, errors.New("give the directory as an absolute path")
	}
	st, err := os.Lstat(dir)
	if err != nil {
		return Bundle{}, err
	}
	if !st.IsDir() {
		return Bundle{}, fmt.Errorf("%s is not a directory", dir)
	}
	t, skipped, err := readTree(dir, "")
	if err != nil {
		return Bundle{}, err
	}
	if _, ok := t["agent.md"]; !ok {
		return Bundle{}, fmt.Errorf("%s has no agent.md", dir)
	}
	return Bundle{Tree: t, Suites: map[string][]byte{}, Skipped: skipped}, nil
}
