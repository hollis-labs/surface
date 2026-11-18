package files

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// The bounds of one search. They keep a search of a home directory quick and its answer
// small enough to hand to a model.
const (
	grepMaxDepth    = 8
	grepMaxFiles    = 2000
	grepMaxMatches  = 100
	grepMatchCap    = 500
	grepMaxFileSize = 1 << 20
	grepLineClip    = 240
)

// grepSkip are folders a search never enters: version control and dependency trees.
var grepSkip = map[string]bool{
	".git": true, ".hg": true, ".svn": true, "node_modules": true, ".venv": true, "__pycache__": true, ".cache": true,
}

// Match is one matching line.
type Match struct {
	// Path is relative to the place, slash separated.
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// grepQuery is what to search for.
type grepQuery struct {
	// Query is a substring matched without regard to case, or a regular expression (RE2)
	// when Regexp is set.
	Query  string
	Regexp bool
	// Glob, when set, keeps only files whose name matches it ("*.md").
	Glob string
	// MaxDepth is how many folders deep to go below the start; zero means grepMaxDepth.
	MaxDepth int
	// MaxMatches stops the search; zero means grepMaxMatches.
	MaxMatches int
}

// grepResult is a search's answer.
type grepResult struct {
	Matches []Match `json:"matches"`
	// Files is how many files were read.
	Files int `json:"files_searched"`
	// Truncated says a bound was reached, so there may be more.
	Truncated bool `json:"truncated"`
}

// grep searches the text files under a folder, or one file. Symbolic links are not
// followed (so the search cannot leave the place), and binary files, files over
// grepMaxFileSize, files on the denylist and the grepSkip folders are passed over.
func grep(ctx context.Context, l loc, q grepQuery) (grepResult, error) {
	res := grepResult{Matches: []Match{}}
	if q.Query == "" {
		return res, errors.New("say what to search for")
	}
	matches := func(line string) bool { return strings.Contains(strings.ToLower(line), strings.ToLower(q.Query)) }
	if q.Regexp {
		re, err := regexp.Compile(q.Query)
		if err != nil {
			return res, fmt.Errorf("the regular expression does not compile; fix it, or search for it as plain text: %w", err)
		}
		matches = re.MatchString
	}
	if q.Glob != "" {
		if _, err := path.Match(q.Glob, ""); err != nil {
			return res, fmt.Errorf("the glob %q is malformed; use a file name pattern like *.md", q.Glob)
		}
	}
	if q.MaxDepth <= 0 {
		q.MaxDepth = grepMaxDepth
	}
	if q.MaxMatches <= 0 {
		q.MaxMatches = grepMaxMatches
	}
	q.MaxMatches = min(q.MaxMatches, grepMatchCap)

	err := filepath.WalkDir(l.Abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == l.Abs {
				return describe(err, l)
			}
			return nil // an unreadable folder is passed over
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		sub, _ := filepath.Rel(l.Abs, p)
		sub = filepath.ToSlash(sub)
		if d.IsDir() {
			if p == l.Abs {
				return nil
			}
			if grepSkip[d.Name()] || sensitive(p) || strings.Count(sub, "/")+1 > q.MaxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || sensitive(p) {
			return nil
		}
		if q.Glob != "" {
			if ok, _ := path.Match(q.Glob, d.Name()); !ok {
				return nil
			}
		}
		if fi, ierr := d.Info(); ierr != nil || fi.Size() > grepMaxFileSize {
			return nil //nolint:nilerr // a file that vanished or is too big is passed over
		}
		if res.Files >= grepMaxFiles {
			res.Truncated = true
			return filepath.SkipAll
		}
		res.Files++
		rel := l.Rel
		if sub != "." {
			rel = path.Join(l.Rel, sub)
		}
		if grepFile(p, rel, matches, q.MaxMatches, &res) {
			res.Truncated = true
			return filepath.SkipAll
		}
		return nil
	})
	return res, err
}

// grepFile adds one file's matching lines to res. It reports whether the match limit was
// reached. A binary or unreadable file adds nothing.
func grepFile(abs, rel string, matches func(string) bool, limit int, res *grepResult) (full bool) {
	f, err := os.Open(abs) //nolint:gosec // abs was found by walking a folder that passed locate
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	br := bufio.NewReaderSize(f, 64<<10)
	if head, _ := br.Peek(sniffLen); looksBinary(head) {
		return false
	}
	sc := bufio.NewScanner(br)
	sc.Buffer(make([]byte, 64<<10), grepMaxFileSize+1)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if !matches(line) {
			continue
		}
		if len(res.Matches) >= limit {
			return true
		}
		res.Matches = append(res.Matches, Match{Path: rel, Line: n, Text: clip(strings.TrimSpace(line), grepLineClip)})
	}
	return false
}

// clip shortens s to about n bytes, on a character boundary.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "…"
}
