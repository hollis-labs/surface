package workbench

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hollis-labs/libs/llm/core/llmcontracts"
	"github.com/hollis-labs/libs/llm/core/llmtypes"
	"github.com/hollis-labs/surface/module"
)

// Eval runs: the Workbench's side. A run is started only by the owner (the page's Run
// button, or the confirm-level tool), against the gateway in the app's agent config, with a
// dollar cap, a per-case token cap and a run token budget. Results are kept per run, keyed by
// the agent's digest, so two versions can be compared.

// Run is one eval run, as stored in runs/<id>.json.
type Run struct {
	ID       string `json:"id"`
	Agent    string `json:"agent"`
	Suite    string `json:"suite"`
	Digest   string `json:"digest"`
	Version  int    `json:"version"`
	Model    string `json:"model"`
	Started  string `json:"started"`
	Finished string `json:"finished,omitempty"`
	// Status is running, done, stopped (a cap or the budget ended it) or failed.
	Status   string       `json:"status"`
	Stopped  string       `json:"stopped,omitempty"`
	SpentUSD float64      `json:"spent_usd"`
	Tokens   int          `json:"tokens"`
	Caps     Caps         `json:"caps"`
	Passed   int          `json:"passed"`
	Failed   int          `json:"failed"`
	Cases    []CaseResult `json:"cases"`
	// Previous is the run this one re-ran, for the comparison.
	Previous string `json:"previous,omitempty"`
	// Mode is offline (each case's script stands in for the model: a harness check, no
	// model is called, nothing is spent) or live (the app's gateway; costs money).
	Mode string `json:"mode"`
}

// Caps bound a run.
type Caps struct {
	MaxUSD     float64 `json:"max_usd"`
	TurnTokens int     `json:"turn_tokens"`
	RunTokens  int     `json:"run_tokens"`
}

// CaseResult is one case's outcome.
type CaseResult struct {
	ID         string         `json:"id"`
	Pass       bool           `json:"pass"`
	Why        string         `json:"why,omitempty"`
	Skipped    string         `json:"skipped,omitempty"`
	Graders    []GraderResult `json:"graders"`
	Answer     string         `json:"answer"`
	Transcript []Exchange     `json:"transcript"`
	Steps      int            `json:"steps"`
	Tokens     int            `json:"tokens"`
	CostUSD    float64        `json:"cost_usd"`
	WallMS     int64          `json:"wall_ms"`
}

// Exchange is one model request's last message and the reply.
type Exchange struct {
	// Sent is what the model was last given: the owner's turn, or the tool results.
	Sent string `json:"sent"`
	// Reply is the model's text; Calls its tool calls.
	Reply string    `json:"reply,omitempty"`
	Calls []ToolUse `json:"calls,omitempty"`
	// Subtask marks a request of a subtask's conversation.
	Subtask bool `json:"subtask,omitempty"`
	Tokens  int  `json:"tokens"`
}

// recorder is the provider one case's requests go through: it records each exchange.
type recorder struct {
	llmcontracts.Provider
	mu        sync.Mutex
	exchanges []Exchange
	calls     []ToolUse
}

func (r *recorder) Calls() []ToolUse {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ToolUse(nil), r.calls...)
}

func (r *recorder) transcript() []Exchange {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Exchange(nil), r.exchanges...)
}

const maxShown = 4000

func lastSent(req llmtypes.ChatRequest) (string, bool) {
	sub := false
	if len(req.Messages) > 0 {
		first := req.Messages[0]
		text := first.Content
		for _, b := range first.ContentBlocks {
			text += b.Text
		}
		sub = strings.Contains(text, "Goal: ")
	}
	if len(req.Messages) == 0 {
		return "", sub
	}
	m := req.Messages[len(req.Messages)-1]
	var parts []string
	if m.Content != "" {
		parts = append(parts, m.Content)
	}
	for _, b := range m.ContentBlocks {
		switch b.Type {
		case "text":
			parts = append(parts, b.Text)
		case "tool_result":
			label := "tool result"
			if b.IsError {
				label = "tool error"
			}
			parts = append(parts, fmt.Sprintf("[%s] %s", label, b.Content))
		}
	}
	s := strings.Join(parts, "\n")
	if len(s) > maxShown {
		s = s[:maxShown] + "…"
	}
	return s, sub
}

func (r *recorder) see(req llmtypes.ChatRequest, reply anthropicReply) {
	sent, sub := lastSent(req)
	ex := Exchange{Sent: sent, Subtask: sub,
		Tokens: reply.Usage.InputTokens + reply.Usage.OutputTokens + reply.Usage.CacheReadTokens + reply.Usage.CacheCreationTokens}
	var text []string
	for _, b := range reply.Content {
		switch b.Type {
		case "text":
			text = append(text, b.Text)
		case "tool_use":
			in := map[string]any{}
			_ = json.Unmarshal(b.Input, &in)
			ex.Calls = append(ex.Calls, ToolUse{Name: b.Name, Input: in})
		}
	}
	ex.Reply = strings.Join(text, "\n")
	r.mu.Lock()
	r.exchanges = append(r.exchanges, ex)
	r.calls = append(r.calls, ex.Calls...)
	r.mu.Unlock()
}

// Runner keeps suites and runs for the library's agents.
type Runner struct {
	lib *Library
	rt  Runtime
	h   module.Host
	// gateway is the gateway runs go through; nil reads the app's agent config.
	gateway func(ctx context.Context) (module.Gateway, error)
	now     func() time.Time

	mu      sync.Mutex
	running map[string]context.CancelFunc
	// wg tracks runs in progress, so a test (or Stop) can wait for them.
	wg sync.WaitGroup
}

// Defaults and ceilings of the cost guard.
const (
	defaultMaxUSD     = 10
	ceilingMaxUSD     = 50
	defaultTurnTokens = 150000
	defaultRunTokens  = 500000
)

func (x *Runner) suitesDir(id string) (string, error) {
	side, err := x.lib.Sidecar(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(side, "evals"), nil
}

// Suites lists an agent's suites, parsed; a suite that does not parse is listed with its
// error.
func (x *Runner) Suites(id string) ([]SuiteInfo, error) {
	dir, err := x.suitesDir(id)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return []SuiteInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []SuiteInfo{}
	for _, e := range entries {
		if !suiteNameRe.MatchString(e.Name()) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name())) //nolint:gosec // the library's own file
		if err != nil {
			continue
		}
		info := SuiteInfo{Name: strings.TrimSuffix(e.Name(), ".yaml"), Source: string(raw)}
		if s, err := ParseSuite(raw); err != nil {
			info.Error = err.Error()
		} else {
			info.Cases = len(s.Cases)
			info.Description = s.Description
		}
		out = append(out, info)
	}
	return out, nil
}

// SuiteInfo is a suite as listed.
type SuiteInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Cases       int    `json:"cases"`
	Error       string `json:"error,omitempty"`
	Source      string `json:"source"`
}

// SaveSuite writes a suite after checking it; its file name is its name.
func (x *Runner) SaveSuite(id string, raw []byte) (Suite, error) {
	if len(raw) > maxSuiteBytes {
		return Suite{}, errors.New("a suite is at most 256 KiB")
	}
	s, err := ParseSuite(raw)
	if err != nil {
		return s, err
	}
	dir, err := x.suitesDir(id)
	if err != nil {
		return s, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil { //nolint:gosec // a checked id or name inside the library; the key file the owner's config names
		return s, err
	}
	return s, writeFileAtomic(filepath.Join(dir, suiteFileName(s.Name)), raw)
}

// DeleteSuite removes a suite.
func (x *Runner) DeleteSuite(id, name string) error {
	if !caseIDRe.MatchString(name) {
		return errors.New("not a suite name")
	}
	dir, err := x.suitesDir(id)
	if err != nil {
		return err
	}
	return os.Remove(filepath.Join(dir, suiteFileName(name))) //nolint:gosec // a checked id or name inside the library; the key file the owner's config names
}

func (x *Runner) suite(id, name string) (Suite, error) {
	if !caseIDRe.MatchString(name) {
		return Suite{}, errors.New("not a suite name")
	}
	dir, err := x.suitesDir(id)
	if err != nil {
		return Suite{}, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, suiteFileName(name))) //nolint:gosec // a checked name in the library
	if err != nil {
		return Suite{}, fmt.Errorf("workbench: no suite %q", name)
	}
	return ParseSuite(raw)
}

// Estimate is what a run would cost, worked out before it is started.
type Estimate struct {
	Agent  string `json:"agent"`
	Suite  string `json:"suite"`
	Cases  int    `json:"cases"`
	Model  string `json:"model"`
	Digest string `json:"digest"`
	// PromptTokens is the assembled first request (system prompt and tools), estimated.
	PromptTokens int `json:"prompt_tokens"`
	// Tokens is the estimate for the whole run: per case, three model requests carrying the
	// prompt, the case's turns and fixtures, and an output allowance each. Rubric graders
	// add one request each.
	Tokens int `json:"tokens"`
	// Caps are the bounds a run starts with (the settings, unless the request names lower).
	Caps     Caps   `json:"caps"`
	Gateway  string `json:"gateway"`
	Ready    bool   `json:"ready"`
	NotReady string `json:"not_ready,omitempty"`
}

func (x *Runner) caps(want Caps) Caps {
	c := Caps{MaxUSD: float64(x.h.Config().Int("eval_max_usd")), TurnTokens: x.h.Config().Int("eval_turn_tokens"), RunTokens: x.h.Config().Int("eval_run_tokens")}
	if c.MaxUSD <= 0 {
		c.MaxUSD = defaultMaxUSD
	}
	if c.TurnTokens <= 0 {
		c.TurnTokens = defaultTurnTokens
	}
	if c.RunTokens <= 0 {
		c.RunTokens = defaultRunTokens
	}
	if want.MaxUSD > 0 && want.MaxUSD < c.MaxUSD {
		c.MaxUSD = want.MaxUSD
	}
	if want.TurnTokens > 0 && want.TurnTokens < c.TurnTokens {
		c.TurnTokens = want.TurnTokens
	}
	if want.RunTokens > 0 && want.RunTokens < c.RunTokens {
		c.RunTokens = want.RunTokens
	}
	c.MaxUSD = min(c.MaxUSD, ceilingMaxUSD)
	return c
}

func (x *Runner) gw(ctx context.Context) (module.Gateway, error) {
	if x.gateway != nil {
		return x.gateway(ctx)
	}
	return x.h.Agent().Gateway(ctx)
}

// model is the run's model: the agent's own (host.yaml runtime.model), else the gateway's.
func runModel(t Tree, gw module.Gateway) string {
	if m := hostModel(t); m != "" {
		return m
	}
	return gw.Model
}

func hostModel(t Tree) string {
	rep := Validate(t)
	if rep.Summary != nil {
		return rep.Summary.Model
	}
	return ""
}

// Estimate works out a run's size and bounds without calling a model.
func (x *Runner) Estimate(ctx context.Context, id, suiteName string, want Caps) (Estimate, error) {
	s, err := x.suite(id, suiteName)
	if err != nil {
		return Estimate{}, err
	}
	t, err := x.lib.Tree(id)
	if err != nil {
		return Estimate{}, err
	}
	rep := Validate(t)
	e := Estimate{Agent: id, Suite: s.Name, Cases: len(s.Cases), Digest: rep.Digest, Caps: x.caps(want)}
	gw, gerr := x.gw(ctx)
	switch {
	case gerr != nil:
		e.NotReady = "no model gateway: the app's agent config needs model, gateway_url and key_file"
	case !rep.OK:
		e.NotReady = "the agent does not validate"
	case strings.Contains(string(t["host.yaml"]), "provider: openai"):
		e.NotReady = "eval runs speak the Anthropic Messages API; this agent's provider is openai"
	default:
		e.Ready = true
		e.Gateway = gw.URL
	}
	e.Model = runModel(t, gw)
	prompt := 0
	if p, err := x.rt.Preview(ctx, t); err == nil {
		prompt = p.Tokens + 600*len(p.Tools)/10
	} else {
		for _, b := range t {
			prompt += len(b) / 4
		}
		prompt += 2500 // the base prompt
	}
	e.PromptTokens = prompt
	const requests, outAllowance = 3, 1024
	for _, c := range s.Cases {
		size := 0
		for _, turn := range c.Turns {
			size += len(turn) / 4
		}
		for _, f := range c.Fixtures {
			size += len(f) / 4
		}
		e.Tokens += requests*(prompt+size) + requests*outAllowance
		for _, g := range c.Graders {
			if g.Kind() == "rubric" {
				e.Tokens += 800 + len(g.Rubric)/4
			}
		}
	}
	return e, nil
}

// newID is a time-ordered id: runs and proposals list newest first by it.
func newID() string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	now := time.Now().UTC()
	return fmt.Sprintf("%s-%06d-%s", now.Format("20060102-150405"), now.Nanosecond()/1000, hex.EncodeToString(b[:]))
}

func (x *Runner) runsDir(id string) (string, error) {
	side, err := x.lib.Sidecar(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(side, "runs"), nil
}

func (x *Runner) saveRun(r Run) error {
	dir, err := x.runsDir(r.Agent)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, r.ID+".json"), b)
}

// GetRun reads a run.
func (x *Runner) GetRun(agent, runID string) (Run, error) {
	if !caseIDRe.MatchString(runID) {
		return Run{}, errors.New("not a run id")
	}
	dir, err := x.runsDir(agent)
	if err != nil {
		return Run{}, err
	}
	b, err := os.ReadFile(filepath.Join(dir, runID+".json")) //nolint:gosec // a checked id in the library
	if err != nil {
		return Run{}, fmt.Errorf("workbench: no run %q", runID)
	}
	var r Run
	return r, json.Unmarshal(b, &r)
}

// RunInfo is a run as listed (no transcripts).
type RunInfo struct {
	ID       string  `json:"id"`
	Mode     string  `json:"mode"`
	Suite    string  `json:"suite"`
	Digest   string  `json:"digest"`
	Version  int     `json:"version"`
	Status   string  `json:"status"`
	Started  string  `json:"started"`
	Passed   int     `json:"passed"`
	Failed   int     `json:"failed"`
	Tokens   int     `json:"tokens"`
	SpentUSD float64 `json:"spent_usd"`
	Stopped  string  `json:"stopped,omitempty"`
	Previous string  `json:"previous,omitempty"`
}

// Runs lists an agent's runs, newest first.
func (x *Runner) Runs(agent string) ([]RunInfo, error) {
	dir, err := x.runsDir(agent)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return []RunInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []RunInfo{}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		r, err := x.GetRun(agent, id)
		if err != nil {
			continue
		}
		out = append(out, RunInfo{ID: r.ID, Mode: r.Mode, Suite: r.Suite, Digest: r.Digest, Version: r.Version, Status: r.Status, Started: r.Started,
			Passed: r.Passed, Failed: r.Failed, Tokens: r.Tokens, SpentUSD: r.SpentUSD, Stopped: r.Stopped, Previous: r.Previous})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// Start begins a run and returns it while it runs; it is saved as it goes and when done.
// The caller has already asked the owner: the page's Run button, or a confirm-level tool.
//
// mode is "offline" (the default: no model is called) or "live".
func (x *Runner) Start(ctx context.Context, agent, suiteName, mode string, want Caps, previous string) (Run, error) {
	switch mode {
	case "", "offline":
		mode = "offline"
	case "live":
	default:
		return Run{}, fmt.Errorf("workbench: mode %q: offline or live", mode)
	}
	s, err := x.suite(agent, suiteName)
	if err != nil {
		return Run{}, err
	}
	t, err := x.lib.Tree(agent)
	if err != nil {
		return Run{}, err
	}
	rep := Validate(t)
	if !rep.OK {
		return Run{}, &ErrInvalid{rep}
	}
	if strings.Contains(string(t["host.yaml"]), "provider: openai") {
		return Run{}, errors.New("workbench: eval runs speak the Anthropic Messages API; this agent's provider is openai")
	}
	var gw module.Gateway
	model := offlineModel
	if mode == "live" {
		if gw, err = x.gw(ctx); err != nil {
			return Run{}, fmt.Errorf("workbench: no model gateway: %w", err)
		}
		model = runModel(t, gw)
	}
	caps := x.caps(want)
	version := 0
	if vs, _ := x.lib.Versions(agent); len(vs) > 0 {
		version = vs[len(vs)-1].N
	}
	run := Run{ID: newID(), Agent: agent, Suite: s.Name, Digest: rep.Digest, Version: version, Model: model,
		Started: x.now().UTC().Format(time.RFC3339), Status: "running", Caps: caps, Cases: []CaseResult{}, Previous: previous, Mode: mode}
	if err := x.saveRun(run); err != nil {
		return Run{}, err
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	x.mu.Lock()
	if x.running == nil {
		x.running = map[string]context.CancelFunc{}
	}
	x.running[run.ID] = cancel
	x.mu.Unlock()
	x.wg.Add(1)
	go func() {
		defer x.wg.Done()
		defer cancel()
		final := x.execute(runCtx, run, s, t, gw)
		x.mu.Lock()
		delete(x.running, run.ID)
		x.mu.Unlock()
		if err := x.saveRun(final); err != nil {
			x.h.Log().Warn("workbench: run not saved", "run", run.ID, "err", err)
		}
		x.h.Events().Publish("workbench.run", map[string]any{"agent": agent, "run": run.ID, "status": final.Status})
	}()
	x.h.Events().Publish("workbench.run", map[string]any{"agent": agent, "run": run.ID, "status": run.Status})
	return run, nil
}

// Cancel stops a run in progress.
func (x *Runner) Cancel(runID string) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	c, ok := x.running[runID]
	if ok {
		c()
	}
	return ok
}

// Wait waits for every run in progress.
func (x *Runner) Wait() { x.wg.Wait() }

func (x *Runner) execute(ctx context.Context, run Run, s Suite, t Tree, gw module.Gateway) Run {
	meter := &usageMeter{budget: run.Caps.RunTokens}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	meter.cancel = cancel
	recorders := map[string]*recorder{}
	for _, c := range s.Cases {
		rec := &recorder{}
		if run.Mode == "live" {
			base := newGatewayProvider(gw.URL, gw.KeyFile)
			base.record = rec.see
			rec.Provider = &budgeted{Provider: base, meter: meter}
		} else {
			rec.Provider = &budgeted{Provider: &offlineProvider{c: c, record: rec.see}, meter: meter}
		}
		recorders[c.ID] = rec
	}
	out, err := x.rt.RunSuite(ctx, RunSpec{Tree: t, Suite: s, Model: run.Model, MaxUSD: run.Caps.MaxUSD, TurnTokens: run.Caps.TurnTokens,
		Timeout:  10 * time.Minute,
		Provider: func(caseID string) llmcontracts.Provider { return recorders[caseID] }})
	run.Finished = x.now().UTC().Format(time.RFC3339)
	if err != nil {
		run.Status, run.Stopped = "failed", scrub(err.Error())
		return run
	}
	// Rubric graders ask the model through a provider of their own: counted against the
	// run's token budget, kept out of the case's transcript, and skipped once the dollar cap
	// would be passed (priced as nanite/evals prices, conservatively).
	judge := &budgeted{Provider: newGatewayProvider(gw.URL, gw.KeyFile), meter: meter}
	spent := out.SpentUSD
	if run.Mode != "live" {
		spent = 0 // the stand-in model costs nothing; nanite/evals priced its estimated usage
	}
	byID := map[string]CaseOutput{}
	for _, c := range out.Cases {
		byID[c.ID] = c
	}
	for _, c := range s.Cases {
		co := byID[c.ID]
		rec := recorders[c.ID]
		cr := CaseResult{ID: c.ID, Answer: co.Answer, Graders: co.Graders, Steps: co.Steps, Tokens: co.Tokens, CostUSD: co.CostUSD, WallMS: co.WallMS,
			Skipped: co.Skipped, Transcript: rec.transcript()}
		if cr.Graders == nil {
			cr.Graders = []GraderResult{}
		}
		cr.Pass = co.Ran && co.Skipped == ""
		for _, g := range cr.Graders {
			if !g.Pass {
				cr.Pass = false
			}
		}
		if co.Ran {
			for _, g := range c.Graders {
				if g.Kind() != "rubric" {
					continue
				}
				if run.Mode != "live" {
					cr.Graders = append(cr.Graders, GraderResult{Kind: "rubric", Skipped: true, Detail: "not graded: model-graded, and this run is offline"})
					continue
				}
				// The most a rubric request can cost: its input and 200 tokens out.
				worst := (float64(len(co.Answer)+len(g.Rubric)+len(c.Turns[len(c.Turns)-1])+400)/3*15 + 200*75) / 1e6
				if spent+worst > run.Caps.MaxUSD {
					cr.Graders = append(cr.Graders, GraderResult{Kind: "rubric", Detail: "not graded: the dollar cap would be passed"})
					cr.Pass = false
					continue
				}
				spent += worst
				gr := x.rubric(ctx, judge, run.Model, c, g.Rubric, co.Answer)
				cr.Graders = append(cr.Graders, gr)
				if !gr.Pass {
					cr.Pass = false
				}
			}
		}
		switch {
		case co.Skipped != "":
			cr.Why = "skipped: " + co.Skipped
		case !co.Ran:
			cr.Why = co.Why
			if cr.Why == "" {
				cr.Why = "the case did not finish"
			}
		case !cr.Pass:
			var why []string
			for _, g := range cr.Graders {
				if !g.Pass && !g.Skipped {
					why = append(why, g.Kind+": "+g.Detail)
				}
			}
			cr.Why = strings.Join(why, "; ")
		}
		if cr.Pass {
			run.Passed++
		} else {
			run.Failed++
		}
		run.Cases = append(run.Cases, cr)
	}
	used, over := meter.state()
	run.Tokens = used
	run.SpentUSD = spent
	run.Status = "done"
	switch {
	case over:
		run.Status, run.Stopped = "stopped", fmt.Sprintf("the run's token budget (%d) was spent", run.Caps.RunTokens)
	case out.Stopped != "":
		run.Status, run.Stopped = "stopped", out.Stopped
	}
	return run
}

const rubricPrompt = `You grade one answer of an AI agent against a rubric. Read the task, the answer and the rubric. Reply with one line: PASS or FAIL, a colon, and one sentence saying why.`

// rubric asks the model whether an answer meets a rubric.
func (x *Runner) rubric(ctx context.Context, p llmcontracts.Provider, model string, c Case, rubric, answer string) GraderResult {
	r := GraderResult{Kind: "rubric"}
	req := llmtypes.ChatRequest{Model: model, MaxTokens: 200, SystemPrompt: rubricPrompt, Messages: []llmtypes.ChatMessage{{Role: "user",
		Content: fmt.Sprintf("Task:\n%s\n\nAnswer:\n%s\n\nRubric:\n%s", c.Turns[len(c.Turns)-1], answer, rubric)}}}
	text, err := p.Complete(ctx, req)
	if err != nil {
		r.Detail = "not graded: " + scrub(err.Error())
		return r
	}
	text = strings.TrimSpace(text)
	r.Pass = strings.HasPrefix(strings.ToUpper(text), "PASS")
	r.Detail = clip(text, 300)
	return r
}

// Comparison is two runs side by side, case by case.
type Comparison struct {
	A     RunInfo       `json:"a"`
	B     RunInfo       `json:"b"`
	Cases []CaseCompare `json:"cases"`
	// Fixed and Broken count cases that went from fail to pass, and from pass to fail.
	Fixed  int `json:"fixed"`
	Broken int `json:"broken"`
}

// CaseCompare is one case in two runs.
type CaseCompare struct {
	ID      string `json:"id"`
	A       *bool  `json:"a,omitempty"`
	B       *bool  `json:"b,omitempty"`
	ATokens int    `json:"a_tokens"`
	BTokens int    `json:"b_tokens"`
	ASteps  int    `json:"a_steps"`
	BSteps  int    `json:"b_steps"`
	Change  string `json:"change"` // fixed, broken, same, new, gone
}

func info(r Run) RunInfo {
	return RunInfo{ID: r.ID, Mode: r.Mode, Suite: r.Suite, Digest: r.Digest, Version: r.Version, Status: r.Status, Started: r.Started, Passed: r.Passed,
		Failed: r.Failed, Tokens: r.Tokens, SpentUSD: r.SpentUSD, Stopped: r.Stopped, Previous: r.Previous}
}

// Compare compares two runs of an agent.
func (x *Runner) Compare(agent, a, b string) (Comparison, error) {
	ra, err := x.GetRun(agent, a)
	if err != nil {
		return Comparison{}, err
	}
	rb, err := x.GetRun(agent, b)
	if err != nil {
		return Comparison{}, err
	}
	cmp := Comparison{A: info(ra), B: info(rb), Cases: []CaseCompare{}}
	seen := map[string]int{}
	for _, c := range ra.Cases {
		pass := c.Pass
		seen[c.ID] = len(cmp.Cases)
		cmp.Cases = append(cmp.Cases, CaseCompare{ID: c.ID, A: &pass, ATokens: c.Tokens, ASteps: c.Steps, Change: "gone"})
	}
	for _, c := range rb.Cases {
		pass := c.Pass
		i, ok := seen[c.ID]
		if !ok {
			cmp.Cases = append(cmp.Cases, CaseCompare{ID: c.ID, B: &pass, BTokens: c.Tokens, BSteps: c.Steps, Change: "new"})
			continue
		}
		cc := &cmp.Cases[i]
		cc.B, cc.BTokens, cc.BSteps = &pass, c.Tokens, c.Steps
		switch {
		case !*cc.A && pass:
			cc.Change = "fixed"
			cmp.Fixed++
		case *cc.A && !pass:
			cc.Change = "broken"
			cmp.Broken++
		default:
			cc.Change = "same"
		}
	}
	return cmp, nil
}
