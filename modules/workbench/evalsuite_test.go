package workbench

import (
	"errors"
	"strings"
	"testing"
)

func TestGraders(t *testing.T) {
	o := Outcome{
		Answer: "Here it is.\n```json\n{\"status\":\"ok\",\"items\":[{\"n\":2}]}\n```\nANSWER: **42; done**",
		Calls:  []ToolUse{{Name: "read_file", Input: map[string]any{"path": "a.md", "limit": float64(10)}}},
		Steps:  3, Tokens: 1200,
		ReadFile: func(rel string) ([]byte, error) {
			if rel == "out.md" {
				return []byte("Reviewed: yes"), nil
			}
			return nil, errors.New("missing")
		},
	}
	cases := []struct {
		g    Grader
		pass bool
	}{
		{Grader{Contains: "here IT"}, true},
		{Grader{Contains: "absent"}, false},
		{Grader{NotContains: "absent"}, true},
		{Grader{NotContains: "here"}, false},
		{Grader{Regex: `(?m)^ANSWER: \*\*42`}, true},
		{Grader{Regex: `^nope`}, false},
		{Grader{AnswerLine: "42"}, true},
		{Grader{AnswerLine: "43"}, false},
		{Grader{JSONPath: &JSONCheck{Path: "$.status", Equals: "ok"}}, true},
		{Grader{JSONPath: &JSONCheck{Path: "items[0].n", Equals: 2}}, true},
		{Grader{JSONPath: &JSONCheck{Path: "items.0.n", Equals: 3}}, false},
		{Grader{JSONPath: &JSONCheck{Path: "missing"}}, false},
		{Grader{ToolCalled: &ToolCheck{Tool: "read_file", Args: map[string]any{"path": "a.md"}}}, true},
		{Grader{ToolCalled: &ToolCheck{Tool: "read_file", Args: map[string]any{"limit": 10}}}, true},
		{Grader{ToolCalled: &ToolCheck{Tool: "read_file", Args: map[string]any{"path": "b.md"}}}, false},
		{Grader{ToolCalled: &ToolCheck{Tool: "bash"}}, false},
		{Grader{NoToolCalled: &ToolCheck{}}, false},
		{Grader{NoToolCalled: &ToolCheck{Tool: "bash"}}, true},
		{Grader{MaxSteps: 3}, true},
		{Grader{MaxSteps: 2}, false},
		{Grader{MaxTokens: 1000}, false},
		{Grader{File: &FileCheck{Path: "out.md", Contains: "Reviewed"}}, true},
		{Grader{File: &FileCheck{Path: "gone.md"}}, false},
	}
	for i, c := range cases {
		r := c.g.grade(o)
		if r.Pass != c.pass {
			t.Errorf("%d %s: pass %v, want %v (%s)", i, r.Kind, r.Pass, c.pass, r.Detail)
		}
	}
}

func TestSuiteValidation(t *testing.T) {
	bad := map[string]string{
		"two checks in one grader": "name: s\ncases:\n  - id: a\n    turns: [hi]\n    graders: [{contains: x, regex: y}]\n",
		"no graders":               "name: s\ncases:\n  - id: a\n    turns: [hi]\n    graders: []\n",
		"multi-line prelude":       "name: s\ncases:\n  - id: a\n    turns: [\"one\\ntwo\", last]\n    graders: [{contains: x}]\n",
		"fixture over agent file":  "name: s\ncases:\n  - id: a\n    turns: [hi]\n    fixtures: {agent.md: x}\n    graders: [{contains: x}]\n",
		"fixture escapes":          "name: s\ncases:\n  - id: a\n    turns: [hi]\n    fixtures: {../x: x}\n    graders: [{contains: x}]\n",
		"unknown key":              "name: s\ncases:\n  - id: a\n    turns: [hi]\n    graders: [{containz: x}]\n",
		"bad regex":                "name: s\ncases:\n  - id: a\n    turns: [hi]\n    graders: [{regex: '('}]\n",
		"bad mode":                 "name: s\nmode: council\ncases:\n  - id: a\n    turns: [hi]\n    graders: [{contains: x}]\n",
		"duplicate ids":            "name: s\ncases:\n  - id: a\n    turns: [hi]\n    graders: [{contains: x}]\n  - id: a\n    turns: [hi]\n    graders: [{contains: x}]\n",
	}
	for name, src := range bad {
		if _, err := ParseSuite([]byte(src)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestUnifiedDiffAppliesBack(t *testing.T) {
	a := "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n"
	b := "one\n2\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\neleven\n"
	d := unifiedDiff("f.md", a, b)
	if !strings.Contains(d, "@@ -1,") || !strings.Contains(d, "+eleven") {
		t.Fatal(d)
	}
	got, err := applyUnified(a, d)
	if err != nil || got != b {
		t.Fatalf("%q %v", got, err)
	}
	// A hunk whose line numbers are off still applies where its context matches.
	shifted := strings.Replace(d, "@@ -1,", "@@ -3,", 1)
	if got, err = applyUnified(a, shifted); err != nil || got != b {
		t.Fatalf("shifted: %q %v", got, err)
	}
	// One that does not match the file is refused.
	if _, err := applyUnified("something else\n", d); err == nil {
		t.Fatal("a stale diff applied")
	}
	if d := unifiedDiff("f.md", a, a); d != "" {
		t.Fatal("no change gave a diff")
	}
}

func TestSetYAMLKeepsTheRest(t *testing.T) {
	doc := []byte("# the agent's host\nkind: embedded # kind\nruntime:\n  provider: anthropic\n  max_steps: 50\n")
	out, err := setYAML(doc, map[string]any{"runtime.max_steps": 10, "runtime.provider": "", "permissions.mode": "plan"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "# the agent's host") || !strings.Contains(s, "# kind") || !strings.Contains(s, "max_steps: 10") ||
		strings.Contains(s, "provider") || !strings.Contains(s, "mode: plan") {
		t.Fatal(s)
	}
}
