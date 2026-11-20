package workbench

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hollis-labs/libs/agent/agentdir"
	"github.com/hollis-labs/libs/llm/core/llmcontracts"
	"github.com/hollis-labs/libs/llm/core/llmtypes"
	nanite "github.com/hollis-labs/nanite/agent"
	"github.com/hollis-labs/nanite/evals"
	"gopkg.in/yaml.v3"
)

// The seam to Nanite. Everything the Workbench asks of the runtime goes through Runtime, so
// a change in Nanite's API is an edit here. The implementation runs suites with Nanite's own
// eval harness (nanite/evals, with isolated workspaces) and assembles prompts with the
// embedded agent API (nanite/agent); it imports nothing internal to Nanite.

// Runtime is what the Workbench needs from the agent runtime.
type Runtime interface {
	// RunSuite runs a suite's cases against an agent's files, through the model the spec
	// names, within its caps, and returns each case's outcome for grading.
	RunSuite(ctx context.Context, spec RunSpec) (RunOutput, error)
	// Preview assembles the agent's first request without calling a model: the system
	// prompt slot by slot (Nanite's base prompt included) and the tools offered.
	Preview(ctx context.Context, t Tree) (Preview, error)
}

// RunSpec says what to run and within what bounds.
type RunSpec struct {
	Tree  Tree
	Suite Suite
	// Model is written into the run's host.yaml for every case.
	Model string
	// Provider makes the model provider for one case. Every request of the case, its
	// subtasks' included, goes through it.
	Provider func(caseID string) llmcontracts.Provider
	// MaxUSD caps the whole run's estimated spend (nanite/evals' dollar cap, priced at its
	// conservative price). Required: a run is never uncapped.
	MaxUSD float64
	// TurnTokens caps one case's task turn, its subtasks included.
	TurnTokens int
	// Timeout bounds one case.
	Timeout time.Duration
	// Progress, when set, is told each case's id once it is graded.
	Progress func(caseID string)
}

// RunOutput is what a suite run produced.
type RunOutput struct {
	Model    string
	SpentUSD float64
	// Stopped says why the run ended early, if it did.
	Stopped string
	Cases   []CaseOutput
}

// CaseOutput is one case's run, before the rubric graders.
type CaseOutput struct {
	StubCalls map[string]int
	ID        string
	Answer    string
	Graders   []GraderResult
	// Ran: the case reached grading. Why says why it did not, or why it failed.
	Ran     bool
	Why     string
	Skipped string
	Steps   int
	Tokens  int
	CostUSD float64
	WallMS  int64
}

// Preview is an agent's assembled first request.
type Preview struct {
	System string        `json:"system,omitempty"`
	Slots  []PreviewSlot `json:"slots"`
	Tools  []string      `json:"tools"`
	Tokens int           `json:"tokens"`
}

// PreviewSlot is one block of the system prompt.
type PreviewSlot struct {
	Name     string `json:"name"`
	Content  string `json:"content"`
	Tokens   int    `json:"tokens"`
	ReadOnly bool   `json:"read_only"` // engine-owned (the base prompt): shown, not edited
}

// naniteRuntime runs Nanite evals in process. Each case owns its WorkDir.
type naniteRuntime struct{ tmpBase string }

var _ Runtime = (*naniteRuntime)(nil)

// canonicalHost re-renders host.yaml so it has a runtime section in the shape
// nanite/evals' model override expects ("\nruntime:\n" with two-space keys).
func canonicalHost(raw []byte) ([]byte, error) {
	var p agentdir.Profile
	if err := yaml.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.Runtime.Provider == "" {
		p.Runtime.Provider = "anthropic"
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(p); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	if !bytes.Contains(out, []byte("\nruntime:\n")) {
		return nil, errors.New("host.yaml has no runtime section")
	}
	return out, nil
}

// scenarioFiles writes one case as nanite/evals takes it into dir and returns it as an
// fs.FS: the agent's files (without .mcp.json: no MCP server is started in an eval), the
// case's fixtures, task.md and prelude.txt.
//
//nolint:gosec // dir is the run's own temporary directory; paths are authored or checked fixture paths
func scenarioFiles(dir string, t Tree, c Case) (fs.FS, error) {
	write := func(rel string, data []byte) error {
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	}
	for _, p := range t.Paths() {
		if !agentPath(p) || p == ".mcp.json" || p == "host.yaml" {
			continue
		}
		if err := write(p, t[p]); err != nil {
			return nil, err
		}
	}
	host, err := canonicalHost(t["host.yaml"])
	if err != nil {
		return nil, fmt.Errorf("host.yaml: %w", err)
	}
	if err := write("host.yaml", host); err != nil {
		return nil, err
	}
	for p, body := range c.Fixtures {
		if !safeRel(p) {
			return nil, fmt.Errorf("fixture %q is not a workspace path", p)
		}
		if err := write(p, []byte(body)); err != nil {
			return nil, err
		}
	}
	turns := c.Turns
	if err := write("task.md", []byte(turns[len(turns)-1]+"\n")); err != nil {
		return nil, err
	}
	if len(turns) > 1 {
		if err := write("prelude.txt", []byte(strings.Join(turns[:len(turns)-1], "\n")+"\n")); err != nil {
			return nil, err
		}
	}
	return os.DirFS(dir), nil
}

type caseState struct {
	mu      sync.Mutex
	records []nanite.TraceRecord
	out     CaseOutput
}

func (r *naniteRuntime) RunSuite(ctx context.Context, spec RunSpec) (RunOutput, error) {
	if spec.MaxUSD <= 0 {
		return RunOutput{}, errors.New("workbench: a run needs a dollar cap")
	}
	if spec.Provider == nil || spec.Model == "" {
		return RunOutput{}, errors.New("workbench: a run needs a model and a provider")
	}
	var opts Options
	if raw, ok := spec.Tree[optionsFile]; ok {
		o, err := parseOptions(raw)
		if err != nil {
			return RunOutput{}, err
		}
		opts = o
	}
	steering, err := evalSteering(spec.Tree[steeringFile])
	if err != nil {
		return RunOutput{}, err
	}
	states := map[string]*caseState{}
	var scenarios []evals.Scenario
	staging, err := os.MkdirTemp(r.tmpBase, "workbench-scenarios-")
	if err != nil {
		return RunOutput{}, err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	for _, c := range spec.Suite.Cases {
		files, err := scenarioFiles(filepath.Join(staging, c.ID), spec.Tree, c)
		if err != nil {
			return RunOutput{}, err
		}
		st := &caseState{out: CaseOutput{ID: c.ID}}
		states[c.ID] = st
		graders := c.Graders
		hook := func(o *nanite.Options) {
			o.Steering = steering
			prev := o.TraceSink
			o.TraceSink = func(ctx context.Context, rec nanite.TraceRecord) {
				if prev != nil {
					prev(ctx, rec)
				}
				st.mu.Lock()
				st.records = append(st.records, rec)
				st.mu.Unlock()
			}
			if opts.CompactAt > 0 {
				o.CompactAt = opts.CompactAt
			}
		}
		scenarios = append(scenarios, evals.Scenario{
			Name: c.ID, Files: files, Writes: c.Writes, Stubs: c.naniteStubs(),
			Arms: []evals.Arm{
				{Name: string(evals.Direct), Mode: evals.Direct, Options: hook},
				{Name: string(evals.Subtask), Mode: evals.Subtask, Options: hook},
			},
			Grade: func(answer, workspace string) (bool, string) {
				st.mu.Lock()
				defer st.mu.Unlock()
				steps, tokens := taskTurn(st.records)
				calls := callsOf(spec.Provider, c.ID)
				o := Outcome{Answer: answer, Workspace: workspace, Calls: calls, Steps: steps, Tokens: tokens,
					ReadFile: func(rel string) ([]byte, error) {
						if !safeRel(rel) {
							return nil, errors.New("not a workspace path")
						}
						root, err := os.OpenRoot(workspace)
						if err != nil {
							return nil, err
						}
						defer func() { _ = root.Close() }()
						return root.ReadFile(rel)
					}}
				st.out.Ran, st.out.Answer, st.out.Steps, st.out.Tokens = true, answer, steps, tokens
				pass := true
				var why []string
				for _, g := range graders {
					if g.Kind() == "rubric" {
						continue // graded after the run, by the caller
					}
					gr := g.grade(o)
					st.out.Graders = append(st.out.Graders, gr)
					if !gr.Pass {
						pass = false
						why = append(why, gr.Detail)
					}
				}
				if spec.Progress != nil {
					spec.Progress(c.ID)
				}
				return pass, strings.Join(why, "; ")
			},
		})
	}
	mode := evals.Direct
	if spec.Suite.Mode == "subtask" {
		mode = evals.Subtask
	}
	runDir := filepath.Join(staging, "runs")
	if err := os.Mkdir(runDir, 0o700); err != nil {
		return RunOutput{}, err
	}
	cfg := evals.Config{
		Dir:        runDir,
		Provider:   func(s evals.Scenario, _ evals.Arm) (llmcontracts.Provider, error) { return spec.Provider(s.Name), nil },
		Model:      spec.Model,
		Modes:      []evals.Mode{mode},
		TurnTokens: spec.TurnTokens,
		MaxUSD:     spec.MaxUSD,
		Timeout:    spec.Timeout,
	}
	rep, runErr := evals.Run(ctx, cfg, scenarios)
	out := RunOutput{Model: rep.Model, SpentUSD: rep.SpentUSD, Stopped: rep.Stopped}
	byID := map[string]evals.Result{}
	for _, res := range rep.Results {
		byID[res.Scenario] = res
	}
	for _, c := range spec.Suite.Cases {
		st := states[c.ID]
		st.mu.Lock()
		co := st.out
		st.mu.Unlock()
		if res, ok := byID[c.ID]; ok {
			co.Skipped, co.WallMS, co.CostUSD = res.Skipped, res.WallMS, res.CostUSD
			co.StubCalls = res.StubCalls
			if res.TotalTokens > co.Tokens {
				co.Tokens = res.TotalTokens
			}
			if !co.Ran || !res.Success {
				co.Why = scrub(res.Why)
			}
		} else {
			co.Skipped = "not run"
		}
		out.Cases = append(out.Cases, co)
	}
	if runErr != nil && out.Stopped == "" {
		out.Stopped = "error: " + evals.Shorten(runErr.Error())
	}
	return out, nil
}

// callsOf returns the tool calls a case's provider recorded, when it records them.
func callsOf(mk func(string) llmcontracts.Provider, caseID string) []ToolUse {
	if rec, ok := mk(caseID).(interface{ Calls() []ToolUse }); ok {
		return rec.Calls()
	}
	return nil
}

// taskTurn reads the steps and the tree's tokens of the last root turn: the task (the
// prelude's turns come first).
func taskTurn(recs []nanite.TraceRecord) (steps, tokens int) {
	for i := len(recs) - 1; i >= 0; i-- {
		r := recs[i]
		if r.ParentConversationID != "" {
			continue
		}
		u := r.Usage
		tokens = u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheWriteTokens
		for _, s := range r.Subtasks {
			su := s.Usage
			tokens += su.InputTokens + su.OutputTokens + su.CacheReadTokens + su.CacheWriteTokens
		}
		return r.Outcome.Steps, tokens
	}
	return 0, 0
}

// captureProvider answers one request with a fixed reply and keeps the request.
type captureProvider struct {
	mu  sync.Mutex
	req *llmtypes.ChatRequest
}

func (p *captureProvider) StreamChat(_ context.Context, req llmtypes.ChatRequest) (<-chan llmtypes.StreamEvent, error) {
	p.mu.Lock()
	if p.req == nil {
		r := req
		p.req = &r
	}
	p.mu.Unlock()
	ch := make(chan llmtypes.StreamEvent, 3)
	ch <- llmtypes.StreamEvent{Type: llmtypes.EventDelta, Content: "(preview)"}
	ch <- llmtypes.StreamEvent{Type: llmtypes.EventUsage, Usage: &llmtypes.Usage{InputTokens: 1, OutputTokens: 1, StopReason: llmtypes.StopReasonEndTurn}}
	ch <- llmtypes.StreamEvent{Type: llmtypes.EventDone}
	close(ch)
	return ch, nil
}

func (p *captureProvider) Complete(context.Context, llmtypes.ChatRequest) (string, error) {
	return "", errors.New("preview: no completions")
}

func (p *captureProvider) Capabilities() llmtypes.ProviderCapabilities {
	return llmtypes.ProviderCapabilities{SupportsToolCalling: true, MaxTokens: 8192, ContextWindowSize: 200000}
}

// engineSlots are the slots Nanite writes itself: shown read-only.
var engineSlots = map[string]bool{string(nanite.SlotUniversal): true, string(nanite.SlotPermissions): true,
	string(nanite.SlotHandoff): true, string(nanite.SlotConversation): true, string(nanite.SlotSystem): true}

func (r *naniteRuntime) Preview(ctx context.Context, t Tree) (Preview, error) {
	tmp, err := os.MkdirTemp("", "workbench-preview-")
	if err != nil {
		return Preview{}, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	dir := filepath.Join(tmp, "agent")
	pt := t.Clone()
	delete(pt, ".mcp.json") // no MCP server is started for a preview
	if err = writeAgentDir(dir, pt); err != nil {
		return Preview{}, err
	}
	capture := &captureProvider{}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	a, err := nanite.Open(ctx, nanite.Options{Dir: dir, DataDir: filepath.Join(tmp, "state"), GlobalSkillsDir: "-", Provider: capture,
		Builtins: []nanite.BuiltinGroup{nanite.BuiltinSkills}})
	if err != nil {
		return Preview{}, err
	}
	defer func() { _ = a.Close() }()
	conv, err := a.NewConversation(ctx)
	if err != nil {
		return Preview{}, err
	}
	run, err := conv.Send(ctx, nanite.Input{Text: "(preview)"})
	if err != nil {
		return Preview{}, err
	}
	for range run.Events() {
	}
	if _, err := run.Wait(); err != nil {
		return Preview{}, err
	}
	capture.mu.Lock()
	req := capture.req
	capture.mu.Unlock()
	if req == nil {
		return Preview{}, errors.New("preview: the agent made no request")
	}
	p := Preview{System: req.SystemPrompt, Slots: []PreviewSlot{}, Tools: []string{}}
	total := len(req.SystemPrompt)
	for _, s := range req.SlotBlocks {
		if s.Content == "" {
			continue
		}
		p.Slots = append(p.Slots, PreviewSlot{Name: s.Name, Content: strings.ReplaceAll(s.Content, tmp, "<preview>"), Tokens: len(s.Content) / 4, ReadOnly: engineSlots[s.Name]})
		total += len(s.Content)
	}
	for _, tool := range req.Tools {
		p.Tools = append(p.Tools, tool.Name)
	}
	p.Tokens = total / 4
	return p, nil
}
