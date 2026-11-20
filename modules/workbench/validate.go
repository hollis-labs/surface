package workbench

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/hollis-labs/libs/agent/agentdir"
	nanite "github.com/hollis-labs/nanite/agent"
	"gopkg.in/yaml.v3"
)

// Problem is one thing wrong (or worth knowing) about an agent's files.
type Problem struct {
	File     string `json:"file,omitempty"`
	Message  string `json:"message"`
	Severity string `json:"severity"` // error or warning
}

// Report is the outcome of validating an agent.
type Report struct {
	OK bool `json:"ok"`
	// Digest is the Workbench's digest of every authored and sidecar file (Tree.Digest);
	// AgentdirDigest is agentdir.Load's, over the files agentdir reads.
	Digest         string    `json:"digest"`
	AgentdirDigest string    `json:"agentdir_digest,omitempty"`
	Problems       []Problem `json:"problems"`
	// Summary is what the files define, for the page.
	Summary *Summary `json:"summary,omitempty"`
}

// Summary is what a valid agent defines.
type Summary struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Kind        string   `json:"kind"`
	Model       string   `json:"model,omitempty"`
	Skills      []string `json:"skills"`
	Context     []string `json:"context"`
	Variants    []string `json:"variants"`
	MCPServers  []string `json:"mcp_servers"`
	Providers   []string `json:"providers"`
	Reflexes    int      `json:"reflexes"`
	Presets     int      `json:"presets"`
}

func (r *Report) add(file, sev, format string, args ...any) {
	r.Problems = append(r.Problems, Problem{File: file, Severity: sev, Message: fmt.Sprintf(format, args...)})
}

// Validate checks a tree as Nanite will read it: agentdir.Load over the directory, then the
// files agentdir does not read (variants/*.md and providers.yaml, as Nanite v0.6.0 parses
// them), the limits Nanite enforces when the agent opens, the sidecar files, and literal
// secrets where only references belong.
func Validate(t Tree) Report {
	rep := Report{Digest: t.Digest(), Problems: []Problem{}}
	if err := t.check(); err != nil {
		rep.add("", "error", "%v", err)
		return rep
	}
	dir, err := os.MkdirTemp("", "workbench-validate-")
	if err != nil {
		rep.add("", "error", "%v", err)
		return rep
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err = writeAgentDir(dir+"/a", t); err != nil {
		rep.add("", "error", "%v", err)
		return rep
	}
	snap, err := agentdir.Load(dir + "/a")
	if err != nil {
		msg := strings.ReplaceAll(err.Error(), dir+"/a/", "")
		rep.add(fileOf(msg), "error", "%s", msg)
	} else {
		rep.AgentdirDigest = snap.Digest
		sum := &Summary{Name: snap.Definition.Name, Description: snap.Definition.Description, Kind: string(snap.Profile.Kind),
			Model: snap.Profile.Runtime.Model, Skills: []string{}, Context: []string{}, Variants: []string{}, MCPServers: []string{}, Providers: []string{}}
		for _, s := range snap.Skills {
			sum.Skills = append(sum.Skills, s.Name)
			if strings.TrimSpace(s.Description) == "" {
				rep.add("skills/"+path.Base(s.Dir)+"/SKILL.md", "warning", "skill %s has no description; the model chooses skills by it", s.Name)
			}
		}
		for _, c := range snap.Context {
			sum.Context = append(sum.Context, c.Name)
		}
		for n := range snap.MCP.Servers {
			sum.MCPServers = append(sum.MCPServers, n)
		}
		slices.Sort(sum.MCPServers)
		checkRuntime(&rep, snap.Profile)
		checkMCPSecrets(&rep, snap.MCP)
		rep.Summary = sum
	}
	for _, p := range t.Paths() {
		if strings.HasPrefix(p, "variants/") {
			name := strings.TrimSuffix(path.Base(p), ".md")
			if err := checkVariant(name, t[p]); err != nil {
				rep.add(p, "error", "%v", err)
			} else if rep.Summary != nil {
				rep.Summary.Variants = append(rep.Summary.Variants, name)
			}
		}
	}
	if raw, ok := t["providers.yaml"]; ok {
		names, err := checkProviders(raw)
		if err != nil {
			rep.add("providers.yaml", "error", "%v", err)
		} else if rep.Summary != nil {
			rep.Summary.Providers = names
		}
	}
	if s, err := parseSteering(t[steeringFile]); err != nil {
		for _, e := range splitJoined(err) {
			rep.add(steeringFile, "error", "%s", e)
		}
	} else if rep.Summary != nil {
		rep.Summary.Reflexes, rep.Summary.Presets = len(s.Reflexes), len(s.Presets)
	}
	if _, err := parseOptions(t[optionsFile]); err != nil {
		rep.add(optionsFile, "error", "%v", err)
	}
	for _, f := range scanTree(t) {
		msg := "line %d holds %s"
		if f.Refuse {
			msg += "; an export is refused while it is there"
		} else {
			msg = "line %d has %s; make sure it is not a real credential"
		}
		rep.add(f.File, "warning", msg, f.Line, f.What)
	}
	rep.OK = !slices.ContainsFunc(rep.Problems, func(p Problem) bool { return p.Severity == "error" })
	return rep
}

func splitJoined(err error) []string {
	var out []string
	for _, line := range strings.Split(err.Error(), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// fileOf finds the authored file an agentdir error is about.
func fileOf(msg string) string {
	for _, f := range []string{"agent.md", "host.yaml", ".mcp.json", "permissions.yaml"} {
		if strings.Contains(msg, f) {
			return f
		}
	}
	if m := regexp.MustCompile(`skills/[A-Za-z0-9._-]+/SKILL\.md`).FindString(msg); m != "" {
		return m
	}
	if strings.Contains(msg, "host profile") || strings.Contains(msg, "MCP server") {
		if strings.Contains(msg, "MCP") {
			return ".mcp.json"
		}
		return "host.yaml"
	}
	return ""
}

// Nanite's limits that agentdir does not check: subtask_limit is refused above 8 when the
// agent opens; a subtask result cap above about 5,900 tokens has no further effect.
const (
	maxSubtaskLimit  = 8
	subtaskResultMax = 5900
)

func checkRuntime(rep *Report, p agentdir.Profile) {
	r := p.Runtime
	if r.SubtaskLimit > maxSubtaskLimit {
		rep.add("host.yaml", "error", "runtime.subtask_limit is at most %d (Nanite refuses the agent above it)", maxSubtaskLimit)
	}
	if r.SubtaskResultCap > subtaskResultMax {
		rep.add("host.yaml", "warning", "runtime.subtask_result_cap above about %d tokens has no further effect: one tool result shows at most that much", subtaskResultMax)
	}
	if r.Provider == "openai" && r.Model == "" {
		rep.add("host.yaml", "warning", "runtime.provider is openai but no model is set; Nanite's default model is an Anthropic one")
	}
	for k, v := range r.Env {
		if why := secretValue(k, v); why != "" {
			rep.add("host.yaml", "error", "runtime.env.%s %s; keep credentials out of agent files", k, why)
		}
	}
}

func checkMCPSecrets(rep *Report, c agentdir.MCPConfig) {
	for name, s := range c.Servers {
		for k, v := range s.Env {
			if why := secretValue(k, v); why != "" {
				rep.add(".mcp.json", "error", "server %s: env %s %s; use ${VARIABLE} or a reference (keychain://, file://, env://)", name, k, why)
			}
		}
		for k, v := range s.Headers {
			if why := secretValue(k, v); why != "" {
				rep.add(".mcp.json", "error", "server %s: header %s %s; use ${VARIABLE}, Bearer ${VARIABLE} or a reference", name, k, why)
			}
		}
		for i, a := range s.Args {
			// "--api-key sk-..." or "--token=abc": the value after a credential flag counts too.
			flag := ""
			if i > 0 && strings.HasPrefix(s.Args[i-1], "-") {
				flag = s.Args[i-1]
			}
			if f, v, ok := strings.Cut(a, "="); ok && strings.HasPrefix(f, "-") {
				flag, a = f, v
			}
			if strings.HasPrefix(a, "-") && !strings.Contains(a, "=") {
				continue // a flag, not a value
			}
			field := strings.TrimLeft(flag, "-")
			if why := secretValue(field, a); why != "" {
				rep.add(".mcp.json", "error", "server %s: argument %d %s; pass credentials through env with ${VARIABLE}", name, i+1, why)
			}
		}
		if why := urlSecret(s.URL); why != "" {
			rep.add(".mcp.json", "error", "server %s: url %s", name, why)
		}
		if p := pattern(s.Command); p != "" {
			rep.add(".mcp.json", "error", "server %s: command holds %s; pass credentials through env with ${VARIABLE}", name, p)
		}
	}
}

// variantFront is Nanite v0.6.0's variant front matter (internal/subagent), decoded strictly
// as Nanite decodes it.
//
// STOPGAP, pending a Nanite export (asked of Nanite): the variant and providers.yaml checks
// below mirror Nanite's internal rules, which a host cannot call. When Nanite exports a
// validator, they go.
type variantFront struct {
	Description string `yaml:"description"`
	Tools       struct {
		Allow []string `yaml:"allow"`
		Deny  []string `yaml:"deny"`
	} `yaml:"tools"`
	ReadOnly bool   `yaml:"read_only"`
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	Budgets  struct {
		Steps  int     `yaml:"steps"`
		Tokens int     `yaml:"tokens"`
		Wall   string  `yaml:"wall"`
		Cost   float64 `yaml:"cost"`
	} `yaml:"budgets"`
	LastN     int  `yaml:"last_n"`
	ResultCap int  `yaml:"result_cap"`
	CanSpawn  bool `yaml:"can_spawn"`
}

var (
	envNameRe       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	variantNameRe   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
	builtinVariants = []string{"code", "research", "worker", "planner", "chat"}
)

// maxLastN is the most parent messages a variant may hand its child.
const maxLastN = 50

// checkVariant validates variants/<name>.md as Nanite v0.6.0 does when it loads it.
func checkVariant(name string, raw []byte) error {
	if !variantNameRe.MatchString(name) || slices.Contains(builtinVariants, name) {
		return fmt.Errorf("variant name %q: lower-case letters, digits and dashes, at most 40, and not %s", name, strings.Join(builtinVariants, ", "))
	}
	head, body, err := splitFront(raw)
	if err != nil {
		return err
	}
	var m variantFront
	dec := yaml.NewDecoder(bytes.NewReader(head))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("front matter: %w", err)
	}
	if strings.TrimSpace(body) == "" {
		return errors.New("the body (the role prompt) is empty")
	}
	if strings.TrimSpace(m.Provider) != "" && strings.TrimSpace(m.Model) == "" {
		return fmt.Errorf("provider %q needs a model", m.Provider)
	}
	if m.ReadOnly && m.CanSpawn {
		return errors.New("read_only and can_spawn cannot be combined")
	}
	if m.Budgets.Wall != "" {
		if d, err := time.ParseDuration(m.Budgets.Wall); err != nil || d <= 0 {
			return fmt.Errorf("budgets.wall %q: use a positive duration such as 10m", m.Budgets.Wall)
		}
	}
	for field, n := range map[string]float64{"budgets.steps": float64(m.Budgets.Steps), "budgets.tokens": float64(m.Budgets.Tokens),
		"budgets.cost": m.Budgets.Cost, "last_n": float64(m.LastN), "result_cap": float64(m.ResultCap)} {
		if n < 0 {
			return fmt.Errorf("%s must not be negative", field)
		}
	}
	if m.LastN > maxLastN {
		return fmt.Errorf("last_n must be at most %d", maxLastN)
	}
	for _, p := range slices.Concat(m.Tools.Allow, m.Tools.Deny) {
		if _, err := path.Match(p, ""); err != nil || strings.TrimSpace(p) == "" {
			return fmt.Errorf("invalid tool pattern %q", p)
		}
	}
	return nil
}

// splitFront splits "---\n<yaml>\n---\n<body>"; no leading fence means no front matter.
func splitFront(raw []byte) (head []byte, body string, err error) {
	text := string(raw)
	first, rest, ok := strings.Cut(text, "\n")
	if !ok || strings.TrimSuffix(first, "\r") != "---" {
		return nil, text, nil
	}
	remaining := rest
	for remaining != "" {
		line, next, _ := strings.Cut(remaining, "\n")
		if strings.TrimSuffix(line, "\r") == "---" {
			return []byte(rest[:len(rest)-len(remaining)]), next, nil
		}
		remaining = next
	}
	return nil, "", errors.New("unterminated front matter")
}

// providersDoc is providers.yaml as Nanite v0.6.0 reads it (internal/definition).
type providersDoc struct {
	Providers map[string]nanite.ProviderSpec `yaml:"providers"`
	Use       map[string]struct {
		Provider string `yaml:"provider"`
		Model    string `yaml:"model"`
	} `yaml:"use"`
}

// checkProviders validates providers.yaml with Nanite's own ProviderSpec.Validate.
func checkProviders(raw []byte) ([]string, error) {
	var doc providersDoc
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	var names []string
	for name, spec := range doc.Providers {
		spec.Name = name
		if err := spec.Validate(); err != nil {
			return nil, err
		}
		if spec.KeyEnv != "" && !envNameRe.MatchString(spec.KeyEnv) {
			return nil, fmt.Errorf("provider %s: key_env names an environment variable (such as MY_KEY), not a value", name)
		}
		if spec.KeyRef != "" && (!isRef(spec.KeyRef) || pattern(spec.KeyRef) != "") {
			return nil, fmt.Errorf("provider %s: key_ref must be a secret reference (keychain://, file://, env://), not a key", name)
		}
		if why := urlSecret(spec.BaseURL); why != "" {
			return nil, fmt.Errorf("provider %s: base_url %s", name, why)
		}
		for k, v := range spec.Headers {
			if why := secretValue(k, v); why != "" {
				return nil, fmt.Errorf("provider %s: header %s %s", name, k, why)
			}
		}
		names = append(names, name)
	}
	for use, sel := range doc.Use {
		switch use {
		case "chat", "summarizer", "subtask":
		default:
			return nil, fmt.Errorf("use %q must be chat, summarizer or subtask", use)
		}
		if sel.Provider == "" || sel.Model == "" {
			return nil, fmt.Errorf("use %s needs provider and model", use)
		}
	}
	slices.Sort(names)
	return names, nil
}
