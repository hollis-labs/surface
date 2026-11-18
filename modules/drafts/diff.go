package drafts

import (
	"fmt"
	"strings"
)

// Line is one line of a comparison: " " kept, "-" removed, "+" added.
type Line struct {
	Op   string `json:"op"`
	Text string `json:"text"`
}

// maxDiffCells bounds the comparison table; past it the whole text is shown as replaced.
const maxDiffCells = 4_000_000

// diffLines compares two texts by line (longest common subsequence).
func diffLines(a, b string) []Line {
	x, y := split(a), split(b)
	if len(x)*len(y) > maxDiffCells {
		out := make([]Line, 0, len(x)+len(y))
		for _, l := range x {
			out = append(out, Line{"-", l})
		}
		for _, l := range y {
			out = append(out, Line{"+", l})
		}
		return out
	}
	n, m := len(x), len(y)
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []Line
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case x[i] == y[j]:
			out = append(out, Line{" ", x[i]})
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, Line{"-", x[i]})
			i++
		default:
			out = append(out, Line{"+", y[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, Line{"-", x[i]})
	}
	for ; j < m; j++ {
		out = append(out, Line{"+", y[j]})
	}
	return out
}

func split(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// unified writes a comparison as a unified diff with three lines of context, for the agent.
func unified(lines []Line) string {
	const ctx = 3
	var b strings.Builder
	keep := make([]bool, len(lines))
	for i, l := range lines {
		if l.Op != " " {
			for k := max(0, i-ctx); k <= min(len(lines)-1, i+ctx); k++ {
				keep[k] = true
			}
		}
	}
	gap := false
	added, removed := 0, 0
	for i, l := range lines {
		switch l.Op {
		case "+":
			added++
		case "-":
			removed++
		}
		if !keep[i] {
			gap = true
			continue
		}
		if gap && b.Len() > 0 {
			b.WriteString("…\n")
		}
		gap = false
		b.WriteString(l.Op + l.Text + "\n")
	}
	if added == 0 && removed == 0 {
		return "no changes"
	}
	return fmt.Sprintf("+%d -%d\n%s", added, removed, b.String())
}
