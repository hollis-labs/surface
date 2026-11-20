package workbench

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Suite is one eval suite: workbench/evals/<name>.yaml in the agent's sidecar, outside the
// agent directory. Each case becomes a Nanite eval scenario (nanite/evals): the agent's
// directory, the case's fixtures as the workspace, its last turn as task.md and its earlier
// turns as prelude.txt. Its graders become the scenario's Grade.
type Suite struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// Mode is the Nanite evals arm the cases run in: direct (the default; the agent does the
	// work itself) or subtask (the subtask tool is offered too).
	Mode  string `yaml:"mode,omitempty" json:"mode,omitempty"`
	Cases []Case `yaml:"cases" json:"cases"`
}

// Case is one eval case.
type Case struct {
	ID string `yaml:"id" json:"id"`
	// Turns are the owner's messages in order; the last is the task, the rest are sent first.
	// Every turn but the last must be one line (Nanite's prelude takes one message a line).
	Turns []string `yaml:"turns" json:"turns"`
	// Fixtures are workspace files the agent can read (path: content).
	Fixtures map[string]string `yaml:"fixtures,omitempty" json:"fixtures,omitempty"`
	// Writes offers the file-writing tools (the case changes the workspace).
	Writes bool   `yaml:"writes,omitempty" json:"writes,omitempty"`
	Stubs  []Stub `yaml:"stubs,omitempty" json:"stubs,omitempty"`
	// Script is what the stand-in model does in an offline run, step by step, for the last
	// turn (earlier turns are answered "Noted."). Without one, an offline run answers once
	// and stops. It is a harness check, not evidence about a model.
	Script  []ScriptStep `yaml:"script,omitempty" json:"script,omitempty"`
	Graders []Grader     `yaml:"graders" json:"graders"`
}

// ScriptStep is one stand-in model response: tool calls, or a final answer.
type ScriptStep struct {
	Say   string      `yaml:"say,omitempty" json:"say,omitempty"`
	Calls []ToolCheck `yaml:"calls,omitempty" json:"calls,omitempty"`
}

// Grader is one check of a case's outcome. Exactly one field is set.
type Grader struct {
	// Contains and NotContains look for text in the final answer (case-insensitive).
	Contains    string `yaml:"contains,omitempty" json:"contains,omitempty"`
	NotContains string `yaml:"not_contains,omitempty" json:"not_contains,omitempty"`
	// Regex matches the final answer (Go syntax; (?i) for case-insensitive).
	Regex string `yaml:"regex,omitempty" json:"regex,omitempty"`
	// AnswerLine checks the answer's last "ANSWER:" line contains this (Nanite evals'
	// convention), case-insensitive.
	AnswerLine string `yaml:"answer_line,omitempty" json:"answer_line,omitempty"`
	// JSONPath reads the answer as JSON (or its first ```json block) and checks a value.
	JSONPath *JSONCheck `yaml:"json_path,omitempty" json:"json_path,omitempty"`
	// ToolCalled passes when the agent called the tool, with at least these arguments.
	ToolCalled *ToolCheck `yaml:"tool_called,omitempty" json:"tool_called,omitempty"`
	// NoToolCalled passes when no tool was called ({}), or not this one ({tool: bash}).
	NoToolCalled *ToolCheck `yaml:"no_tool_called,omitempty" json:"no_tool_called,omitempty"`
	// MaxSteps and MaxTokens bound the task turn: model steps, and tokens across the turn
	// and its subtasks.
	MaxSteps  int `yaml:"max_steps,omitempty" json:"max_steps,omitempty"`
	MaxTokens int `yaml:"max_tokens,omitempty" json:"max_tokens,omitempty"`
	// File checks a workspace file after the run.
	File *FileCheck `yaml:"file,omitempty" json:"file,omitempty"`
	// Rubric is graded by a model: it is told the rubric, the task and the answer and
	// answers PASS or FAIL. It costs a model call and counts against the run's budget.
	Rubric string `yaml:"rubric,omitempty" json:"rubric,omitempty"`
}

// JSONCheck is a value at a path in the answer read as JSON: "$.a.b[0]" or "a.b.0".
// equals compares the value (equals: null wants a JSON null); exists: false wants the path
// absent; with neither, the path must exist.
type JSONCheck struct {
	Path   string `yaml:"path" json:"path"`
	Equals any    `yaml:"equals,omitempty" json:"equals,omitempty"`
	Exists *bool  `yaml:"exists,omitempty" json:"exists,omitempty"`
	// HasEquals records that equals was given, so that equals: null is a check for null.
	HasEquals bool `yaml:"-" json:"has_equals,omitempty"`
}

// UnmarshalYAML reads a JSONCheck strictly and notes whether equals was given.
func (c *JSONCheck) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return errors.New("json_path: a mapping with path, and equals or exists")
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i].Value, n.Content[i+1]
		switch k {
		case "path":
			if err := v.Decode(&c.Path); err != nil {
				return err
			}
		case "equals":
			c.HasEquals = true
			if err := v.Decode(&c.Equals); err != nil {
				return err
			}
		case "exists":
			var b bool
			if err := v.Decode(&b); err != nil {
				return err
			}
			c.Exists = &b
		default:
			return fmt.Errorf("json_path: unknown field %q", k)
		}
	}
	if c.HasEquals && c.Exists != nil && !*c.Exists {
		return errors.New("json_path: equals and exists: false contradict each other")
	}
	return nil
}

// ToolCheck names a tool and, optionally, arguments its call must include.
type ToolCheck struct {
	Tool string         `yaml:"tool,omitempty" json:"tool,omitempty"`
	Args map[string]any `yaml:"args,omitempty" json:"args,omitempty"`
}

// FileCheck is a workspace file that must exist and contain a text.
type FileCheck struct {
	Path     string `yaml:"path" json:"path"`
	Contains string `yaml:"contains,omitempty" json:"contains,omitempty"`
}

// Kind names the grader's check.
func (g Grader) Kind() string {
	var kinds []string
	add := func(on bool, k string) {
		if on {
			kinds = append(kinds, k)
		}
	}
	add(g.Contains != "", "contains")
	add(g.NotContains != "", "not_contains")
	add(g.Regex != "", "regex")
	add(g.AnswerLine != "", "answer_line")
	add(g.JSONPath != nil, "json_path")
	add(g.ToolCalled != nil, "tool_called")
	add(g.NoToolCalled != nil, "no_tool_called")
	add(g.MaxSteps > 0, "max_steps")
	add(g.MaxTokens > 0, "max_tokens")
	add(g.File != nil, "file")
	add(g.Rubric != "", "rubric")
	if len(kinds) != 1 {
		return ""
	}
	return kinds[0]
}

var caseIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

// ParseSuite reads a suite strictly and checks it.
func ParseSuite(raw []byte) (Suite, error) {
	var s Suite
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&s); err != nil && !errors.Is(err, io.EOF) {
		return s, err
	}
	return s, s.Validate()
}

// Validate checks a suite.
func (s Suite) Validate() error {
	var errs []error
	if !caseIDRe.MatchString(s.Name) {
		errs = append(errs, fmt.Errorf("name %q: lower-case letters, digits, dashes, underscores", s.Name))
	}
	switch s.Mode {
	case "", "direct", "subtask":
	default:
		errs = append(errs, fmt.Errorf("mode %q: direct or subtask", s.Mode))
	}
	if len(s.Cases) == 0 {
		errs = append(errs, errors.New("a suite needs at least one case"))
	}
	seen := map[string]bool{}
	for i, c := range s.Cases {
		where := fmt.Sprintf("case %d (%s)", i+1, c.ID)
		if !caseIDRe.MatchString(c.ID) {
			errs = append(errs, fmt.Errorf("%s: id: lower-case letters, digits, dashes, underscores", where))
		}
		if seen[c.ID] {
			errs = append(errs, fmt.Errorf("%s: id is used twice", where))
		}
		seen[c.ID] = true
		if len(c.Turns) == 0 || strings.TrimSpace(c.Turns[len(c.Turns)-1]) == "" {
			errs = append(errs, fmt.Errorf("%s: needs at least one turn", where))
		}
		for j, t := range c.Turns {
			if j < len(c.Turns)-1 && (strings.Contains(strings.TrimSpace(t), "\n") || strings.TrimSpace(t) == "") {
				errs = append(errs, fmt.Errorf("%s: turn %d: every turn but the last must be one line", where, j+1))
			}
		}
		for p := range c.Fixtures {
			if !safeRel(p) || p == "task.md" || p == "prelude.txt" || topFiles[p] || strings.HasPrefix(p, "skills/") || strings.HasPrefix(p, "context/") || strings.HasPrefix(p, "variants/") || strings.HasPrefix(p, ".") {
				errs = append(errs, fmt.Errorf("%s: fixture %q: a relative workspace path that is not one of the agent's own files", where, p))
			}
		}
		if err := validateStubs(c.Stubs); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", where, err))
		}
		for j, st := range c.Script {
			if (st.Say == "") == (len(st.Calls) == 0) {
				errs = append(errs, fmt.Errorf("%s: script step %d: say or calls, one of them", where, j+1))
			}
			for _, call := range st.Calls {
				if call.Tool == "" {
					errs = append(errs, fmt.Errorf("%s: script step %d: a call needs a tool", where, j+1))
				}
			}
		}
		if len(c.Graders) == 0 {
			errs = append(errs, fmt.Errorf("%s: needs at least one grader", where))
		}
		for j, g := range c.Graders {
			k := g.Kind()
			if k == "" {
				errs = append(errs, fmt.Errorf("%s: grader %d: set exactly one check", where, j+1))
				continue
			}
			if err := g.check(); err != nil {
				errs = append(errs, fmt.Errorf("%s: grader %d (%s): %w", where, j+1, k, err))
			}
		}
	}
	return errors.Join(errs...)
}

func (g Grader) check() error {
	switch g.Kind() {
	case "regex":
		_, err := regexp.Compile(g.Regex)
		return err
	case "json_path":
		if strings.TrimSpace(g.JSONPath.Path) == "" {
			return errors.New("path is required")
		}
	case "tool_called":
		if g.ToolCalled.Tool == "" {
			return errors.New("tool is required")
		}
	case "file":
		if !safeRel(g.File.Path) {
			return errors.New("path must be a relative workspace path")
		}
	}
	return nil
}

// Outcome is what a case's graders look at.
type Outcome struct {
	Answer    string
	Workspace string // the run's copy of the workspace
	Calls     []ToolUse
	Steps     int
	Tokens    int
	// ReadFile reads a workspace file; nil reads from Workspace.
	ReadFile func(rel string) ([]byte, error)
}

// ToolUse is one tool call the model made.
type ToolUse struct {
	Name  string         `json:"name"`
	Input map[string]any `json:"input,omitempty"`
}

// GraderResult is one grader's verdict.
type GraderResult struct {
	Kind string `json:"kind"`
	Pass bool   `json:"pass"`
	// Skipped: not graded (a rubric in an offline run); it does not count either way.
	Skipped bool   `json:"skipped,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// grade runs a deterministic grader. Rubric graders are graded by the runner.
func (g Grader) grade(o Outcome) GraderResult {
	r := GraderResult{Kind: g.Kind()}
	low := strings.ToLower(o.Answer)
	switch r.Kind {
	case "contains":
		r.Pass = strings.Contains(low, strings.ToLower(g.Contains))
		r.Detail = fmt.Sprintf("answer contains %q", g.Contains)
	case "not_contains":
		r.Pass = !strings.Contains(low, strings.ToLower(g.NotContains))
		r.Detail = fmt.Sprintf("answer does not contain %q", g.NotContains)
	case "regex":
		re, err := regexp.Compile(g.Regex)
		r.Pass = err == nil && re.MatchString(o.Answer)
		r.Detail = fmt.Sprintf("answer matches /%s/", g.Regex)
	case "answer_line":
		line, ok := answerLine(o.Answer)
		r.Pass = ok && strings.Contains(strings.ToLower(line), strings.ToLower(g.AnswerLine))
		if !ok {
			r.Detail = "no ANSWER: line"
		} else {
			r.Detail = fmt.Sprintf("ANSWER: %s (want %q)", clip(line, 120), g.AnswerLine)
		}
	case "json_path":
		r.Pass, r.Detail = jsonCheck(o.Answer, *g.JSONPath)
	case "tool_called":
		for _, c := range o.Calls {
			if c.Name == g.ToolCalled.Tool && argsMatch(c.Input, g.ToolCalled.Args) {
				r.Pass = true
				break
			}
		}
		r.Detail = fmt.Sprintf("%s called%s; calls: %s", g.ToolCalled.Tool, argsNote(g.ToolCalled.Args), callNames(o.Calls))
	case "no_tool_called":
		r.Pass = true
		for _, c := range o.Calls {
			if g.NoToolCalled.Tool == "" || c.Name == g.NoToolCalled.Tool {
				r.Pass = false
			}
		}
		if g.NoToolCalled.Tool != "" {
			r.Detail = fmt.Sprintf("%s not called; calls: %s", g.NoToolCalled.Tool, callNames(o.Calls))
		} else {
			r.Detail = "no tool called; calls: " + callNames(o.Calls)
		}
	case "max_steps":
		r.Pass = o.Steps <= g.MaxSteps
		r.Detail = fmt.Sprintf("%d steps (at most %d)", o.Steps, g.MaxSteps)
	case "max_tokens":
		r.Pass = o.Tokens <= g.MaxTokens
		r.Detail = fmt.Sprintf("%d tokens (at most %d)", o.Tokens, g.MaxTokens)
	case "file":
		read := o.ReadFile
		if read == nil {
			read = func(string) ([]byte, error) { return nil, errors.New("no workspace") }
		}
		b, err := read(g.File.Path)
		switch {
		case err != nil:
			r.Detail = fmt.Sprintf("%s was not written", g.File.Path)
		default:
			r.Pass = strings.Contains(string(b), g.File.Contains)
			r.Detail = fmt.Sprintf("%s contains %q", g.File.Path, g.File.Contains)
		}
	}
	return r
}

func callNames(calls []ToolUse) string {
	if len(calls) == 0 {
		return "none"
	}
	var n []string
	for _, c := range calls {
		n = append(n, c.Name)
	}
	return strings.Join(n, ", ")
}

func argsNote(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	b, _ := json.Marshal(args)
	return " with " + string(b)
}

// argsMatch: every wanted argument is present with an equal value (compared as JSON).
func argsMatch(got, want map[string]any) bool {
	for k, w := range want {
		g, ok := got[k]
		if !ok {
			return false
		}
		gb, _ := json.Marshal(g)
		wb, _ := json.Marshal(normalizeYAML(w))
		var gv, wv any
		_ = json.Unmarshal(gb, &gv)
		_ = json.Unmarshal(wb, &wv)
		if !reflect.DeepEqual(gv, wv) {
			return false
		}
	}
	return true
}

// normalizeYAML turns yaml.v3's map[string]any / []any values into JSON-able ones.
func normalizeYAML(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			out[k] = normalizeYAML(e)
		}
		return out
	case map[any]any:
		out := map[string]any{}
		for k, e := range x {
			out[fmt.Sprint(k)] = normalizeYAML(e)
		}
		return out
	case []any:
		for i := range x {
			x[i] = normalizeYAML(x[i])
		}
		return x
	}
	return v
}

// answerLine is the answer's last line starting "ANSWER:", without the prefix, as Nanite's
// evals read it.
func answerLine(answer string) (string, bool) {
	lines := strings.Split(answer, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(strings.Trim(strings.TrimSpace(lines[i]), "*`"))
		if len(line) >= 7 && strings.EqualFold(line[:7], "ANSWER:") {
			return strings.TrimSpace(line[7:]), true
		}
	}
	return "", false
}

var fenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*\\n(.*?)```")

func jsonCheck(answer string, c JSONCheck) (bool, string) {
	text := strings.TrimSpace(answer)
	var v any
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		m := fenceRe.FindStringSubmatch(text)
		if m == nil || json.Unmarshal([]byte(m[1]), &v) != nil {
			return false, "the answer is not JSON"
		}
	}
	got, ok := jsonAt(v, c.Path)
	if c.Exists != nil && !*c.Exists {
		if ok {
			return false, fmt.Sprintf("%s is in the answer (want it absent)", c.Path)
		}
		return true, fmt.Sprintf("%s is absent", c.Path)
	}
	if !ok {
		return false, fmt.Sprintf("%s is not in the answer", c.Path)
	}
	if !c.HasEquals && c.Equals == nil {
		return true, fmt.Sprintf("%s exists", c.Path)
	}
	gb, _ := json.Marshal(got)
	wb, _ := json.Marshal(normalizeYAML(c.Equals))
	var gv, wv any
	_ = json.Unmarshal(gb, &gv)
	_ = json.Unmarshal(wb, &wv)
	return reflect.DeepEqual(gv, wv), fmt.Sprintf("%s = %s (want %s)", c.Path, clip(string(gb), 80), string(wb))
}

var pathPart = regexp.MustCompile(`[^.\[\]]+`)

func jsonAt(v any, p string) (any, bool) {
	p = strings.TrimPrefix(strings.TrimPrefix(p, "$"), ".")
	for _, part := range pathPart.FindAllString(p, -1) {
		switch x := v.(type) {
		case map[string]any:
			n, ok := x[part]
			if !ok {
				return nil, false
			}
			v = n
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(x) {
				return nil, false
			}
			v = x[i]
		default:
			return nil, false
		}
	}
	return v, true
}

func clip(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// suiteFileName is the file a suite is kept in.
func suiteFileName(name string) string { return path.Clean(name) + ".yaml" }
