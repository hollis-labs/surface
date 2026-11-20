package workbench

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Line diffs: to compare versions, to show a proposal, and to apply a proposal's unified
// diff to the file as it is now.

// DiffLine is one line of a diff: " " kept, "-" removed, "+" added.
type DiffLine struct {
	Op   string `json:"op"`
	Text string `json:"text"`
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// maxDiffCells bounds the LCS table; larger inputs fall back to remove-all, add-all.
const maxDiffCells = 4_000_000

// diffLines is a line diff of a and b by longest common subsequence.
func diffLines(a, b string) []DiffLine {
	x, y := splitLines(a), splitLines(b)
	// Trim the common head and tail first: most edits are small.
	head := 0
	for head < len(x) && head < len(y) && x[head] == y[head] {
		head++
	}
	tail := 0
	for tail < len(x)-head && tail < len(y)-head && x[len(x)-1-tail] == y[len(y)-1-tail] {
		tail++
	}
	out := make([]DiffLine, 0, len(x)+len(y))
	for _, l := range x[:head] {
		out = append(out, DiffLine{" ", l})
	}
	mx, my := x[head:len(x)-tail], y[head:len(y)-tail]
	if len(mx)*len(my) > maxDiffCells {
		for _, l := range mx {
			out = append(out, DiffLine{"-", l})
		}
		for _, l := range my {
			out = append(out, DiffLine{"+", l})
		}
	} else {
		n, m := len(mx), len(my)
		lcs := make([][]int32, n+1)
		for i := range lcs {
			lcs[i] = make([]int32, m+1)
		}
		for i := n - 1; i >= 0; i-- {
			for j := m - 1; j >= 0; j-- {
				if mx[i] == my[j] {
					lcs[i][j] = lcs[i+1][j+1] + 1
				} else {
					lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
				}
			}
		}
		i, j := 0, 0
		for i < n && j < m {
			switch {
			case mx[i] == my[j]:
				out = append(out, DiffLine{" ", mx[i]})
				i++
				j++
			case lcs[i+1][j] >= lcs[i][j+1]:
				out = append(out, DiffLine{"-", mx[i]})
				i++
			default:
				out = append(out, DiffLine{"+", my[j]})
				j++
			}
		}
		for ; i < n; i++ {
			out = append(out, DiffLine{"-", mx[i]})
		}
		for ; j < m; j++ {
			out = append(out, DiffLine{"+", my[j]})
		}
	}
	for _, l := range x[len(x)-tail:] {
		out = append(out, DiffLine{" ", l})
	}
	return out
}

// unifiedDiff renders a diff of file from a to b with three lines of context.
func unifiedDiff(file, a, b string) string {
	lines := diffLines(a, b)
	changed := false
	for _, l := range lines {
		if l.Op != " " {
			changed = true
			break
		}
	}
	if !changed {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- a/%s\n+++ b/%s\n", file, file)
	const ctx = 3
	// Find hunks: runs of changes with up to 2*ctx kept lines between them.
	i := 0
	ai, bi := 1, 1 // line numbers at lines[i]
	for i < len(lines) {
		if lines[i].Op == " " {
			i++
			ai++
			bi++
			continue
		}
		start := max(0, i-ctx)
		end := i
		for end < len(lines) {
			if lines[end].Op != " " {
				end++
				continue
			}
			run := end
			for run < len(lines) && lines[run].Op == " " {
				run++
			}
			if run == len(lines) || run-end > 2*ctx {
				end = min(len(lines), end+ctx)
				break
			}
			end = run
		}
		as, bs := ai-(i-start), bi-(i-start)
		var an, bn int
		var body strings.Builder
		for _, l := range lines[start:end] {
			body.WriteString(l.Op + l.Text + "\n")
			if l.Op != "+" {
				an++
			}
			if l.Op != "-" {
				bn++
			}
		}
		if an == 0 {
			as--
		}
		if bn == 0 {
			bs--
		}
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", as, an, bs, bn)
		sb.WriteString(body.String())
		for _, l := range lines[i:end] {
			if l.Op != "+" {
				ai++
			}
			if l.Op != "-" {
				bi++
			}
		}
		i = end
	}
	return sb.String()
}

var hunkRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

type hunk struct {
	oldStart, oldCount, newCount int
	lines                        []DiffLine
}

// maxDrift is how far from its stated line a hunk may be found.
const maxDrift = 50

// applyUnified applies a unified diff to text, strictly: each hunk's body must have exactly
// the lines its header counts (a cut-off hunk is refused, not read as a delete), must carry
// at least one kept line unless the file is new, and must match the file exactly at its
// stated line or, failing that, at exactly one place within maxDrift lines. Any failure
// refuses the whole diff; nothing is applied in part.
func applyUnified(text, diff string) (string, error) {
	hunks, err := parseHunks(diff)
	if err != nil {
		return "", err
	}
	src := splitLines(text)
	newFile := len(src) == 0
	trailingNL := text == "" || strings.HasSuffix(text, "\n")
	var out []string
	pos := 0 // next line of src not yet copied
	for hi, h := range hunks {
		var want []string
		kept := 0
		for _, l := range h.lines {
			if l.Op != "+" {
				want = append(want, l.Text)
			}
			if l.Op == " " {
				kept++
			}
		}
		if kept == 0 && !newFile {
			return "", fmt.Errorf("hunk %d has no context line to place it by", hi+1)
		}
		at, err := placeHunk(src, want, max(h.oldStart-1, 0), pos)
		if err != nil {
			return "", fmt.Errorf("hunk %d: %w", hi+1, err)
		}
		out = append(out, src[pos:at]...)
		for _, l := range h.lines {
			if l.Op != "-" {
				out = append(out, l.Text)
			}
		}
		pos = at + len(want)
	}
	out = append(out, src[pos:]...)
	res := strings.Join(out, "\n")
	if trailingNL && len(out) > 0 {
		res += "\n"
	}
	return res, nil
}

// parseHunks reads the hunks of a unified diff and checks each body against its header.
func parseHunks(diff string) ([]hunk, error) {
	var hunks []hunk
	var cur *hunk
	var olds, news int
	done := func() error {
		if cur == nil {
			return nil
		}
		if olds != cur.oldCount || news != cur.newCount {
			return fmt.Errorf("hunk %d is cut short or too long: its header counts %d old and %d new lines, its body has %d and %d", len(hunks), cur.oldCount, cur.newCount, olds, news)
		}
		return nil
	}
	count := func(m string) int {
		if m == "" {
			return 1
		}
		n, _ := strconv.Atoi(m)
		return n
	}
	for _, raw := range strings.Split(strings.ReplaceAll(diff, "\r\n", "\n"), "\n") {
		full := cur != nil && olds == cur.oldCount && news == cur.newCount
		switch {
		case strings.HasPrefix(raw, "@@"):
			if err := done(); err != nil {
				return nil, err
			}
			m := hunkRe.FindStringSubmatch(raw)
			if m == nil {
				return nil, fmt.Errorf("bad hunk header %q", raw)
			}
			start, _ := strconv.Atoi(m[1])
			hunks = append(hunks, hunk{oldStart: start, oldCount: count(m[2]), newCount: count(m[4])})
			cur = &hunks[len(hunks)-1]
			olds, news = 0, 0
		case cur == nil || (full && (raw == "" || strings.HasPrefix(raw, "--- ") || strings.HasPrefix(raw, "+++ ") || strings.HasPrefix(raw, "diff ") || strings.HasPrefix(raw, "index "))):
			continue // headers before the first hunk, and the blank tail
		case raw == `\ No newline at end of file`:
			continue
		case full:
			return nil, fmt.Errorf("hunk %d has more lines than its header counts", len(hunks))
		default:
			op, text := " ", ""
			if raw != "" { // a blank kept line whose leading space was lost
				op, text = raw[:1], raw[1:]
			}
			switch op {
			case " ":
				olds++
				news++
			case "-":
				olds++
			case "+":
				news++
			default:
				return nil, fmt.Errorf("bad diff line %q", raw)
			}
			cur.lines = append(cur.lines, DiffLine{op, text})
		}
	}
	if err := done(); err != nil {
		return nil, err
	}
	if len(hunks) == 0 {
		return nil, errors.New("the diff has no hunks")
	}
	return hunks, nil
}

// placeHunk finds where want sits in src, at or after from: at hint when it matches there,
// else at the one place within maxDrift lines where it does. None, or more than one, is an
// error.
func placeHunk(src, want []string, hint, from int) (int, error) {
	match := func(at int) bool {
		if at < from || at+len(want) > len(src) {
			return false
		}
		for k, w := range want {
			if strings.TrimRight(src[at+k], " \t") != strings.TrimRight(w, " \t") {
				return false
			}
		}
		return true
	}
	if len(want) == 0 {
		if hint < from || hint > len(src) {
			return 0, errors.New("it is out of order")
		}
		return min(hint, len(src)), nil
	}
	if match(hint) {
		return hint, nil
	}
	found := -1
	for at := max(from, hint-maxDrift); at <= hint+maxDrift; at++ {
		if !match(at) {
			continue
		}
		if found >= 0 {
			return 0, errors.New("its context matches more than one place near its stated line")
		}
		found = at
	}
	if found < 0 {
		return 0, fmt.Errorf("it does not match the file within %d lines of line %d", maxDrift, hint+1)
	}
	return found, nil
}
