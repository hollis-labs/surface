package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hollis-labs/surface/module"
)

// The agent tools: the app's own agent (or an outside one over MCP) can read and edit the
// library, write eval suites, and, at confirm level, start runs and ask for proposals. No
// tool accepts or rejects a proposal, rolls back or deletes: those are the owner's.

func schema(s string) json.RawMessage { return json.RawMessage(s) }

func args[T any](in json.RawMessage) (T, error) {
	var v T
	if len(in) == 0 {
		in = json.RawMessage(`{}`)
	}
	err := json.Unmarshal(in, &v)
	return v, err
}

// ownerEditing reports whether the owner has unsaved work in a Workbench window on agent.
func ownerEditing(ctx context.Context, h module.Host, agent string) bool {
	for _, w := range h.Desk().Windows(ctx) {
		if w.View == "workbench" && w.Dirty {
			if a, _ := w.Args["agent"].(string); a == agent {
				return true
			}
		}
	}
	return false
}

func (m *mod) tools() []module.Tool {
	return []module.Tool{
		{Name: "workbench_list", Level: module.LevelRead,
			Description: "List the agents in the Agent Workbench library: id, name, whether it validates, its digest and version.",
			Input:       schema(`{"type":"object","properties":{}}`),
			Run: func(context.Context, module.Host, json.RawMessage) (any, error) {
				return m.lib.List()
			}},
		{Name: "workbench_get", Level: module.LevelRead,
			Description: "Read a Workbench agent: without file, its file list and validation report; with file (agent.md, host.yaml, .mcp.json, permissions.yaml, providers.yaml, skills/<n>/SKILL.md, context/<n>.md, variants/<n>.md, workbench/steering.json, workbench/options.json), that file's text.",
			Input:       schema(`{"type":"object","required":["agent"],"properties":{"agent":{"type":"string"},"file":{"type":"string"}}}`),
			Run: func(_ context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ Agent, File string }](in)
				if err != nil {
					return nil, err
				}
				t, err := m.lib.Tree(a.Agent)
				if err != nil {
					return nil, err
				}
				if a.File != "" {
					b, ok := t[a.File]
					if !ok {
						return nil, fmt.Errorf("%s has no %s", a.Agent, a.File)
					}
					return string(b), nil
				}
				return map[string]any{"files": t.Paths(), "report": Validate(t)}, nil
			}},
		{Name: "workbench_validate", Level: module.LevelRead,
			Description: "Validate a Workbench agent as Nanite will read it and list the problems by file.",
			Input:       schema(`{"type":"object","required":["agent"],"properties":{"agent":{"type":"string"}}}`),
			Run: func(_ context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ Agent string }](in)
				if err != nil {
					return nil, err
				}
				t, err := m.lib.Tree(a.Agent)
				if err != nil {
					return nil, err
				}
				return Validate(t), nil
			}},
		{Name: "workbench_create", Level: module.LevelWrite,
			Description: "Create a Workbench agent from a template: blank, embedded (with a skill, context, a variant, steering and an eval suite), task or stable.",
			Input:       schema(`{"type":"object","required":["template"],"properties":{"template":{"type":"string","enum":["blank","embedded","task","stable"]},"name":{"type":"string"},"id":{"type":"string"}}}`),
			Describe:    func(json.RawMessage) string { return "Create an agent in the Workbench" },
			Run: func(_ context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ Template, Name, ID string }](in)
				if err != nil {
					return nil, err
				}
				ag, err := m.create(a.ID, a.Name, a.Template, "agent")
				return ag, err
			}},
		{Name: "workbench_write_file", Level: module.LevelWrite,
			Description: "Write one file of a Workbench agent (the whole file) and save a new version: agent.md, skills/<n>/..., variants/<n>.md, context/<n>.md, workbench/steering.json or workbench/options.json. The agent must still validate; if not, nothing is saved and the problems are returned. Say in note what the change is for. host.yaml, .mcp.json, permissions.yaml and providers.yaml go through workbench_write_config.",
			Input:       schema(`{"type":"object","required":["agent","file","content","note"],"properties":{"agent":{"type":"string"},"file":{"type":"string"},"content":{"type":"string"},"note":{"type":"string"}}}`),
			Describe: func(in json.RawMessage) string {
				a, _ := args[struct{ Agent, File string }](in)
				return fmt.Sprintf("Write %s of agent %s", a.File, a.Agent)
			},
			Run: func(ctx context.Context, h module.Host, in json.RawMessage) (any, error) {
				return m.writeTool(ctx, h, in, false)
			}},
		{Name: "workbench_write_config", Level: module.LevelConfirm,
			Description: "Write one of a Workbench agent's configuration files (the whole file) and save a new version: host.yaml, .mcp.json (the commands and servers it starts), permissions.yaml or providers.yaml. These change what the agent may run and reach, so the owner is asked every time. Say in note what the change is for.",
			Input:       schema(`{"type":"object","required":["agent","file","content","note"],"properties":{"agent":{"type":"string"},"file":{"type":"string","enum":["host.yaml",".mcp.json","permissions.yaml","providers.yaml"]},"content":{"type":"string"},"note":{"type":"string"}}}`),
			Describe: func(in json.RawMessage) string {
				a, _ := args[struct{ Agent, File, Note string }](in)
				return fmt.Sprintf("Change %s of agent %s (what it may run and reach): %s", a.File, a.Agent, a.Note)
			},
			Run: func(ctx context.Context, h module.Host, in json.RawMessage) (any, error) {
				return m.writeTool(ctx, h, in, true)
			}},
		{Name: "workbench_import_dir", Level: module.LevelWrite,
			Description: "Copy an agent directory from disk into the Workbench library. The source is only read, never changed; only its authored files are copied.",
			Input:       schema(`{"type":"object","required":["path"],"properties":{"path":{"type":"string","description":"absolute path"},"id":{"type":"string"}}}`),
			Describe: func(in json.RawMessage) string {
				a, _ := args[struct{ Path string }](in)
				return "Copy the agent at " + a.Path + " into the Workbench"
			},
			Run: func(_ context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ Path, ID string }](in)
				if err != nil {
					return nil, err
				}
				b, err := readDirBundle(a.Path)
				if err != nil {
					return nil, err
				}
				ag, rep, notes, err := m.importBundle(b, a.ID, "dir:"+a.Path, "agent")
				if err != nil {
					return nil, err
				}
				return map[string]any{"agent": ag, "report": rep, "notes": notes}, nil
			}},
		{Name: "workbench_versions", Level: module.LevelRead,
			Description: "List a Workbench agent's saved versions: number, digest, note, who saved it, when.",
			Input:       schema(`{"type":"object","required":["agent"],"properties":{"agent":{"type":"string"}}}`),
			Run: func(_ context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ Agent string }](in)
				if err != nil {
					return nil, err
				}
				return m.lib.Versions(a.Agent)
			}},
		{Name: "workbench_suites", Level: module.LevelRead,
			Description: "List a Workbench agent's eval suites with their YAML source.",
			Input:       schema(`{"type":"object","required":["agent"],"properties":{"agent":{"type":"string"}}}`),
			Run: func(_ context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ Agent string }](in)
				if err != nil {
					return nil, err
				}
				return m.run.Suites(a.Agent)
			}},
		{Name: "workbench_write_suite", Level: module.LevelWrite,
			Description: "Write an eval suite (YAML; see the Workbench README for the format: cases with turns, fixtures and graders such as contains, regex, answer_line, json_path, tool_called, no_tool_called, max_steps, max_tokens, file, rubric).",
			Input:       schema(`{"type":"object","required":["agent","source"],"properties":{"agent":{"type":"string"},"source":{"type":"string"}}}`),
			Describe:    func(json.RawMessage) string { return "Write an eval suite in the Workbench" },
			Run: func(_ context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ Agent, Source string }](in)
				if err != nil {
					return nil, err
				}
				s, err := m.run.SaveSuite(a.Agent, []byte(a.Source))
				if err != nil {
					return nil, err
				}
				m.changed(a.Agent, map[string]any{"suite": s.Name})
				return map[string]any{"saved": s.Name, "cases": len(s.Cases)}, nil
			}},
		{Name: "workbench_estimate", Level: module.LevelRead,
			Description: "Estimate an eval run before starting it: cases, model, estimated tokens and the caps it would run under. Calls no model.",
			Input:       schema(`{"type":"object","required":["agent","suite"],"properties":{"agent":{"type":"string"},"suite":{"type":"string"}}}`),
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ Agent, Suite string }](in)
				if err != nil {
					return nil, err
				}
				return m.run.Estimate(ctx, a.Agent, a.Suite, Caps{})
			}},
		{Name: "workbench_run_eval", Level: module.LevelConfirm,
			Description: "Start an eval run of a suite against a Workbench agent. offline (the default) plays each case's script with no model called; live calls a real model and costs money: use live only when the owner asks for it. Runs within the dollar cap and token budgets; returns the run id at once.",
			Input:       schema(`{"type":"object","required":["agent","suite"],"properties":{"agent":{"type":"string"},"suite":{"type":"string"},"mode":{"type":"string","enum":["offline","live"]},"max_usd":{"type":"number","description":"lower the dollar cap for this run"}}}`),
			Describe: func(in json.RawMessage) string {
				a, _ := args[struct{ Agent, Suite, Mode string }](in)
				if a.Mode == "live" {
					return fmt.Sprintf("Run eval suite %s against agent %s live (calls a model, costs money)", a.Suite, a.Agent)
				}
				return fmt.Sprintf("Run eval suite %s against agent %s offline (no model)", a.Suite, a.Agent)
			},
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct {
					Agent, Suite, Mode string
					MaxUSD             float64 `json:"max_usd"`
				}](in)
				if err != nil {
					return nil, err
				}
				r, err := m.run.Start(ctx, a.Agent, a.Suite, a.Mode, Caps{MaxUSD: a.MaxUSD}, "")
				if err != nil {
					return nil, err
				}
				return map[string]any{"run": r.ID, "status": r.Status, "caps": r.Caps}, nil
			}},
		{Name: "workbench_runs", Level: module.LevelRead,
			Description: "List a Workbench agent's eval runs, newest first: suite, digest, version, passed and failed, tokens, spend.",
			Input:       schema(`{"type":"object","required":["agent"],"properties":{"agent":{"type":"string"}}}`),
			Run: func(_ context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ Agent string }](in)
				if err != nil {
					return nil, err
				}
				return m.run.Runs(a.Agent)
			}},
		{Name: "workbench_run_get", Level: module.LevelRead,
			Description: "Read one eval run: each case's pass or fail, the graders' verdicts and the answer; with transcripts, the model exchanges too.",
			Input:       schema(`{"type":"object","required":["agent","run"],"properties":{"agent":{"type":"string"},"run":{"type":"string"},"transcripts":{"type":"boolean"}}}`),
			Run: func(_ context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct {
					Agent, Run  string
					Transcripts bool
				}](in)
				if err != nil {
					return nil, err
				}
				r, err := m.run.GetRun(a.Agent, a.Run)
				if err != nil {
					return nil, err
				}
				if !a.Transcripts {
					for i := range r.Cases {
						r.Cases[i].Transcript = nil
					}
				}
				return r, nil
			}},
		{Name: "workbench_compare", Level: module.LevelRead,
			Description: "Compare two eval runs of an agent case by case: fixed, broken, tokens and steps.",
			Input:       schema(`{"type":"object","required":["agent","a","b"],"properties":{"agent":{"type":"string"},"a":{"type":"string"},"b":{"type":"string"}}}`),
			Run: func(_ context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ Agent, A, B string }](in)
				if err != nil {
					return nil, err
				}
				return m.run.Compare(a.Agent, a.A, a.B)
			}},
		{Name: "workbench_propose", Level: module.LevelConfirm,
			Description: "Ask a model for edits to an agent's prompt, skills, variants, context or steering that would fix a run's failed cases. It calls a real model and costs money: only when the owner asks. The owner reviews and accepts or rejects each edit in the Workbench; this tool never applies one.",
			Input:       schema(`{"type":"object","required":["agent","run"],"properties":{"agent":{"type":"string"},"run":{"type":"string"}}}`),
			Describe: func(in json.RawMessage) string {
				a, _ := args[struct{ Agent, Run string }](in)
				return fmt.Sprintf("Ask a model for improvements to %s from run %s (calls a model, costs money)", a.Agent, a.Run)
			},
			Run: func(ctx context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ Agent, Run string }](in)
				if err != nil {
					return nil, err
				}
				return m.run.Propose(ctx, a.Agent, a.Run)
			}},
		{Name: "workbench_proposals", Level: module.LevelRead,
			Description: "List a Workbench agent's improvement proposals and the status of each edit (pending, accepted, rejected, invalid, stale).",
			Input:       schema(`{"type":"object","required":["agent"],"properties":{"agent":{"type":"string"}}}`),
			Run: func(_ context.Context, _ module.Host, in json.RawMessage) (any, error) {
				a, err := args[struct{ Agent string }](in)
				if err != nil {
					return nil, err
				}
				return m.run.Proposals(a.Agent)
			}},
	}
}

// configFile: a file that decides what the agent may run and reach.
func configFile(p string) bool {
	return p == "host.yaml" || p == ".mcp.json" || p == "permissions.yaml" || p == "providers.yaml"
}

// writeTool writes one file for workbench_write_file (config false: the prompt, skills,
// variants, context, steering, options) or workbench_write_config (config true).
func (m *mod) writeTool(ctx context.Context, h module.Host, in json.RawMessage, config bool) (any, error) {
	a, err := args[struct{ Agent, File, Content, Note string }](in)
	if err != nil {
		return nil, err
	}
	switch {
	case config && !configFile(a.File):
		return nil, fmt.Errorf("workbench_write_config writes host.yaml, .mcp.json, permissions.yaml or providers.yaml, not %s", a.File)
	case !config && configFile(a.File):
		return nil, fmt.Errorf("%s decides what the agent may run and reach: use workbench_write_config, which asks the owner", a.File)
	case !config && !editable(a.File) && a.File != optionsFile:
		return nil, fmt.Errorf("%s is not a file this tool writes", a.File)
	}
	if module.ActorOf(ctx).Delegate() && ownerEditing(ctx, h, a.Agent) {
		return nil, errors.New("the owner has unsaved changes to this agent in the Workbench; wait, or ask them to save first")
	}
	t, err := m.lib.Tree(a.Agent)
	if err != nil {
		return nil, err
	}
	base := t.Digest()
	c := a.Content
	if err = applyChanges(t, map[string]*string{a.File: &c}); err != nil {
		return nil, err
	}
	v, rep, err := m.lib.Save(a.Agent, t, base, a.Note, "agent")
	if err != nil {
		var inv *ErrInvalid
		if errors.As(err, &inv) {
			return map[string]any{"saved": false, "report": inv.Report}, nil
		}
		return nil, err
	}
	m.changed(a.Agent, map[string]any{"version": v.N})
	return map[string]any{"saved": true, "version": v, "report": rep}, nil
}
