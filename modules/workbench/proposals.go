package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/hollis-labs/libs/llm/core/llmtypes"
)

// The improvement loop. From a run with failures, a model is shown the failing cases and
// the agent's prompt, skills, variants, context and steering, with a fixed meta-prompt, and
// returns edit proposals: a file, a unified diff and a rationale. Nothing is applied until
// the owner accepts a proposal; accepting saves a new version. There is no automatic
// acceptance, and no agent tool accepts one.

const metaPrompt = `You improve an AI agent's configuration so that it passes its evals.

You are given the agent's files (its prompt agent.md, its skills, subtask variants, context files and steering reflexes) and the eval cases it failed: each case's input, what the graders checked, the agent's answer and an excerpt of the transcript.

Propose the smallest edits that would make the failing cases pass without breaking what works. Prefer changing instructions over adding new files. Do not change front matter fields you do not need to. Never add credentials.

Reply with JSON only, no prose around it, in this shape:
{"proposals": [{"file": "<path as given>", "diff": "<unified diff of that file, with --- a/<file> and +++ b/<file> headers and @@ hunks with three lines of context>", "rationale": "<one or two sentences: which failure this fixes and why>"}]}

Give at most 5 proposals. Each diff must apply to the file exactly as it was shown. To add a new skill, context or variant file, give "content" (the whole file) instead of "diff".`

// Proposal is one improvement request's outcome: a set of edits for the owner to review.
type Proposal struct {
	ID      string `json:"id"`
	Agent   string `json:"agent"`
	Run     string `json:"run"`
	Digest  string `json:"digest"`
	Created string `json:"created"`
	Model   string `json:"model"`
	Tokens  int    `json:"tokens"`
	// Error is set when the model's reply could not be read.
	Error string `json:"error,omitempty"`
	Items []Edit `json:"items"`
}

// Edit is one proposed change to one file.
type Edit struct {
	N         int    `json:"n"`
	File      string `json:"file"`
	Diff      string `json:"diff"`
	Rationale string `json:"rationale"`
	// Status is pending, accepted, rejected, invalid (does not apply or validate) or stale
	// (the file changed since).
	Status  string `json:"status"`
	Problem string `json:"problem,omitempty"`
	// Version is the version an accepted edit was saved as.
	Version int `json:"version,omitempty"`
}

// editable are the files a proposal may change: the prompt, skills, variants, context and
// steering. host.yaml, the tools and providers are the owner's to change by hand.
func editable(p string) bool {
	switch {
	case p == "agent.md", p == steeringFile:
		return true
	case strings.HasPrefix(p, "skills/"), strings.HasPrefix(p, "variants/"), strings.HasPrefix(p, "context/"):
		return authoredPath(p)
	}
	return false
}

func (x *Runner) proposalsDir(agent string) (string, error) {
	side, err := x.lib.Sidecar(agent)
	if err != nil {
		return "", err
	}
	return filepath.Join(side, "proposals"), nil
}

func (x *Runner) saveProposal(p Proposal) error {
	dir, err := x.proposalsDir(p.Agent)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, p.ID+".json"), b)
}

// GetProposal reads a proposal.
func (x *Runner) GetProposal(agent, id string) (Proposal, error) {
	if !caseIDRe.MatchString(id) {
		return Proposal{}, errors.New("not a proposal id")
	}
	dir, err := x.proposalsDir(agent)
	if err != nil {
		return Proposal{}, err
	}
	b, err := os.ReadFile(filepath.Join(dir, id+".json")) //nolint:gosec // a checked id in the library
	if err != nil {
		return Proposal{}, fmt.Errorf("workbench: no proposal %q", id)
	}
	var p Proposal
	return p, json.Unmarshal(b, &p)
}

// Proposals lists an agent's proposals, newest first.
func (x *Runner) Proposals(agent string) ([]Proposal, error) {
	dir, err := x.proposalsDir(agent)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return []Proposal{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Proposal{}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		if p, err := x.GetProposal(agent, id); err == nil {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// proposeInput is the request to the model: the files and the failures.
func proposeInput(t Tree, run Run, suite Suite) string {
	var b strings.Builder
	b.WriteString("# The agent's files\n\n")
	for _, p := range t.Paths() {
		if !editable(p) {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n````\n%s````\n\n", p, ensureNL(string(t[p])))
	}
	cases := map[string]Case{}
	for _, c := range suite.Cases {
		cases[c.ID] = c
	}
	b.WriteString("# Failed cases\n\n")
	for _, c := range run.Cases {
		if c.Pass {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n", c.ID)
		if sc, ok := cases[c.ID]; ok {
			for i, turn := range sc.Turns {
				fmt.Fprintf(&b, "Owner turn %d: %s\n", i+1, turn)
			}
			for _, g := range sc.Graders {
				gb, _ := json.Marshal(g)
				fmt.Fprintf(&b, "Grader: %s\n", gb)
			}
		}
		fmt.Fprintf(&b, "Why it failed: %s\n", c.Why)
		fmt.Fprintf(&b, "Answer:\n%s\n", clipKeep(c.Answer, 2000))
		tr := c.Transcript
		if len(tr) > 6 {
			tr = tr[len(tr)-6:]
		}
		for _, ex := range tr {
			fmt.Fprintf(&b, "- sent: %s\n  reply: %s\n", clipKeep(ex.Sent, 600), clipKeep(ex.Reply, 600))
			for _, call := range ex.Calls {
				in, _ := json.Marshal(call.Input)
				fmt.Fprintf(&b, "  called %s %s\n", call.Name, clipKeep(string(in), 300))
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func ensureNL(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

func clipKeep(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

var jsonObjRe = regexp.MustCompile(`(?s)\{.*\}`)

// Propose asks the model for edits that would fix a run's failures. It is a model call: the
// owner's action only (the page, or a confirm-level tool).
func (x *Runner) Propose(ctx context.Context, agent, runID string) (Proposal, error) {
	run, err := x.GetRun(agent, runID)
	if err != nil {
		return Proposal{}, err
	}
	if run.Failed == 0 {
		return Proposal{}, errors.New("workbench: the run has no failures to improve on")
	}
	suite, err := x.suite(agent, run.Suite)
	if err != nil {
		return Proposal{}, err
	}
	t, err := x.lib.Tree(agent)
	if err != nil {
		return Proposal{}, err
	}
	gw, err := x.gw(ctx)
	if err != nil {
		return Proposal{}, fmt.Errorf("workbench: no model gateway: %w", err)
	}
	input := proposeInput(t, run, suite)
	caps := x.caps(Caps{})
	worst := proposeWorst(input)
	if worst > caps.MaxUSD {
		return Proposal{}, fmt.Errorf("workbench: the request could cost up to $%.2f, over the $%.2f cap", worst, caps.MaxUSD)
	}
	model := runModel(t, gw)
	meter := &usageMeter{budget: caps.RunTokens}
	base, err := newGatewayProvider(t, gw)
	if err != nil {
		return Proposal{}, err
	}
	p := &budgeted{Provider: base, meter: meter}
	text, err := p.Complete(ctx, llmtypes.ChatRequest{Model: model, MaxTokens: proposeMaxOut, SystemPrompt: metaPrompt,
		Messages: []llmtypes.ChatMessage{{Role: "user", Content: input}}})
	if err != nil {
		return Proposal{}, errors.New(scrub(err.Error()))
	}
	used, _ := meter.state()
	prop := Proposal{ID: newID(), Agent: agent, Run: runID, Digest: t.Digest(), Created: x.now().UTC().Format(time.RFC3339), Model: model, Tokens: used, Items: []Edit{}}
	var reply struct {
		Proposals []struct {
			File      string `json:"file"`
			Diff      string `json:"diff"`
			Content   string `json:"content"`
			Rationale string `json:"rationale"`
		} `json:"proposals"`
	}
	raw := jsonObjRe.FindString(text)
	if raw == "" || json.Unmarshal([]byte(raw), &reply) != nil {
		prop.Error = "the model's reply was not the JSON asked for: " + clip(text, 300)
	}
	for i, r := range reply.Proposals {
		if i >= 5 {
			break
		}
		e := Edit{N: i + 1, File: strings.TrimPrefix(r.File, "./"), Diff: r.Diff, Rationale: strings.TrimSpace(r.Rationale), Status: "pending"}
		if e.Diff == "" && r.Content != "" {
			e.Diff = unifiedDiff(e.File, string(t[e.File]), r.Content)
		}
		if _, problem := applyEdit(t, e); problem != "" {
			e.Status, e.Problem = "invalid", problem
		}
		prop.Items = append(prop.Items, e)
	}
	if err := x.saveProposal(prop); err != nil {
		return prop, err
	}
	x.h.Events().Publish("workbench.proposal", map[string]any{"agent": agent, "proposal": prop.ID})
	return prop, nil
}

// applyEdit applies one edit to a copy of t and validates it; problem says why it cannot be
// accepted.
func applyEdit(t Tree, e Edit) (Tree, string) {
	if !editable(e.File) {
		return nil, fmt.Sprintf("%s is not a file a proposal may change", e.File)
	}
	if strings.TrimSpace(e.Diff) == "" {
		return nil, "the proposal has no diff"
	}
	cur := string(t[e.File])
	after, err := applyUnified(cur, e.Diff)
	if err != nil {
		return nil, "the diff does not apply: " + err.Error()
	}
	next := t.Clone()
	next[e.File] = []byte(after)
	if rep := Validate(next); !rep.OK {
		return nil, (&ErrInvalid{rep}).Error()
	}
	return next, ""
}

// Decide accepts or rejects one edit of a proposal. Accepting applies it to the agent as it
// is now and saves a new version. Only the owner decides: this is not an agent tool.
func (x *Runner) Decide(agent, propID string, n int, accept bool) (Proposal, error) {
	// One decision at a time per proposal: two accepts of the same edit cannot both apply.
	unlock := x.lib.lock("proposal:" + agent + "/" + propID)
	defer unlock()
	prop, err := x.GetProposal(agent, propID)
	if err != nil {
		return prop, err
	}
	i := -1
	for k := range prop.Items {
		if prop.Items[k].N == n {
			i = k
		}
	}
	if i < 0 {
		return prop, fmt.Errorf("workbench: proposal %s has no edit %d", propID, n)
	}
	e := &prop.Items[i]
	if e.Status != "pending" {
		return prop, fmt.Errorf("workbench: edit %d is %s", n, e.Status)
	}
	if !accept {
		e.Status = "rejected"
		return prop, x.saveProposal(prop)
	}
	t, err := x.lib.Tree(agent)
	if err != nil {
		return prop, err
	}
	base := t.Digest()
	next, problem := applyEdit(t, *e)
	if problem != "" {
		e.Status, e.Problem = "stale", problem
		_ = x.saveProposal(prop)
		return prop, errors.New("workbench: " + problem)
	}
	// Saved over the state the edit was applied to: a save in between refuses it.
	v, _, err := x.lib.Save(agent, next, base, fmt.Sprintf("accepted proposal %s #%d (%s): %s", propID, n, e.File, clip(e.Rationale, 200)), "owner")
	if err != nil {
		return prop, err
	}
	e.Status, e.Version = "accepted", v.N
	if err := x.saveProposal(prop); err != nil {
		return prop, err
	}
	x.h.Events().Publish("workbench.changed", map[string]any{"agent": agent, "version": v.N})
	return prop, nil
}

// proposeMaxOut is the output allowance of a proposal request.
const proposeMaxOut = 6000

// proposeWorst is the most a proposal request can cost, priced conservatively as
// nanite/evals prices: its input at three characters a token and its whole output allowance.
func proposeWorst(input string) float64 {
	return (float64(len(input)+len(metaPrompt))/3*15 + proposeMaxOut*75) / 1e6
}

// ProposeEstimate is what a proposal would cost, worked out before the owner agrees.
type ProposeEstimate struct {
	Model       string  `json:"model"`
	InputTokens int     `json:"input_tokens"`
	MaxOutput   int     `json:"max_output"`
	WorstUSD    float64 `json:"worst_usd"`
	CapUSD      float64 `json:"cap_usd"`
	Failed      int     `json:"failed"`
}

// EstimatePropose works out a proposal's size and worst cost without calling a model.
func (x *Runner) EstimatePropose(ctx context.Context, agent, runID string) (ProposeEstimate, error) {
	run, err := x.GetRun(agent, runID)
	if err != nil {
		return ProposeEstimate{}, err
	}
	suite, err := x.suite(agent, run.Suite)
	if err != nil {
		return ProposeEstimate{}, err
	}
	t, err := x.lib.Tree(agent)
	if err != nil {
		return ProposeEstimate{}, err
	}
	gw, _ := x.gw(ctx)
	input := proposeInput(t, run, suite)
	return ProposeEstimate{Model: runModel(t, gw), InputTokens: (len(input) + len(metaPrompt)) / 3, MaxOutput: proposeMaxOut,
		WorstUSD: proposeWorst(input), CapUSD: x.caps(Caps{}).MaxUSD, Failed: run.Failed}, nil
}
