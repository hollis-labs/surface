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
	oldStart int
	lines    []DiffLine
}

// applyUnified applies a unified diff to text. Each hunk must match the text (its kept and
// removed lines, exactly) at its stated line or near it; otherwise the diff does not apply.
func applyUnified(text, diff string) (string, error) {
	var hunks []hunk
	var cur *hunk
	for _, raw := range strings.Split(strings.ReplaceAll(diff, "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(raw, "--- "), strings.HasPrefix(raw, "+++ "), strings.HasPrefix(raw, "diff "), strings.HasPrefix(raw, "index "):
			continue
		case strings.HasPrefix(raw, "@@"):
			m := hunkRe.FindStringSubmatch(raw)
			if m == nil {
				return "", fmt.Errorf("bad hunk header %q", raw)
			}
			n, _ := strconv.Atoi(m[1])
			hunks = append(hunks, hunk{oldStart: n})
			cur = &hunks[len(hunks)-1]
		case cur == nil:
			continue
		case raw == `\ No newline at end of file`:
			continue
		case raw == "":
			// A blank context line whose leading space was lost.
			cur.lines = append(cur.lines, DiffLine{" ", ""})
		default:
			op := raw[:1]
			if op != " " && op != "+" && op != "-" {
				return "", fmt.Errorf("bad diff line %q", raw)
			}
			cur.lines = append(cur.lines, DiffLine{op, raw[1:]})
		}
	}
	if len(hunks) == 0 {
		return "", errors.New("the diff has no hunks")
	}
	src := splitLines(text)
	trailingNL := text == "" || strings.HasSuffix(text, "\n")
	var out []string
	pos := 0 // next line of src not yet copied
	for hi, h := range hunks {
		// Trailing blank context lines may be artifacts of the split; drop them.
		for len(h.lines) > 0 && h.lines[len(h.lines)-1].Op == " " && h.lines[len(h.lines)-1].Text == "" {
			h.lines = h.lines[:len(h.lines)-1]
		}
		var want []string
		for _, l := range h.lines {
			if l.Op != "+" {
				want = append(want, l.Text)
			}
		}
		at := findBlock(src, want, max(h.oldStart-1, 0), pos)
		if at < 0 {
			return "", fmt.Errorf("hunk %d does not match the file as it is now", hi+1)
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

// findBlock finds want in src at or after from, nearest to hint.
func findBlock(src, want []string, hint, from int) int {
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
	for d := 0; d <= len(src); d++ {
		if match(hint + d) {
			return hint + d
		}
		if d > 0 && match(hint-d) {
			return hint - d
		}
	}
	return -1
}
