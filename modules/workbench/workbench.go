// Package workbench is the Agent Workbench: a Surface module for making and improving
// Nanite agents. An agent is an agent directory (libs agent/agentdir: agent.md, host.yaml,
// .mcp.json, permissions.yaml, providers.yaml, skills/, context/, variants/) kept in a
// library, with the Workbench's own sidecar beside it: steering (reflexes, triggers,
// presets), host options, versions, eval suites, runs and improvement proposals.
//
// The owner creates agents from templates or imports them (an archive, or a directory copied
// read-only), edits each part with validation on every save (agentdir.Load and Nanite's own
// rules), keeps every saved state as a version, exports an agent as an archive, runs eval
// suites through Nanite's eval harness against the app's model gateway (only when the owner
// starts a run, within a dollar cap and token budgets), asks a model for edits that would
// fix the failures, accepts or rejects each, and runs again to compare.
//
// Everything Nanite-specific is behind Runtime (runtime.go). See README.md for the formats.
package workbench

import (
	"context"
	"embed"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/hollis-labs/surface/module"
)

func init() {
	module.Register(&mod{})
}

// profileFiles is the Workbench's agent profile, the Agent Architect (profile/): the app's
// agent works as it while the Workbench is in front (module.Manifest.Profile).
//
//go:embed all:profile
var profileFiles embed.FS

type mod struct {
	module.Base
	h   module.Host
	lib *Library
	run *Runner
	// rt is the runtime; tests may set one before Start.
	rt Runtime
}

func (m *mod) Manifest() module.Manifest {
	return module.Manifest{
		ID: "workbench", Title: "Agent Workbench", Version: WorkbenchVersion, Gateway: true,
		Profile: module.ProfileFS(profileFiles, "profile"),
		About:   "Make, edit, evaluate and improve Nanite agents: prompt, skills, tools, variants, providers, reflexes and triggers, context; versions; import and export; eval runs and improvement proposals.",
		Views: []module.View{{
			ID: "workbench", Title: "Agent Workbench", Icon: "flask-conical", Width: 1180, Height: 760, Menu: true, Bare: true,
			Render: module.Component("workbench/main"),
			Describe: func(ctx context.Context, h module.Host, w module.Window) (module.WindowContext, error) {
				id, _ := w.Args["agent"].(string)
				if id == "" {
					return module.WindowContext{Kind: "data", Ref: "workbench:agents"}, nil
				}
				wc := module.WindowContext{Kind: "data", Ref: "workbench:" + id, Extra: map[string]any{"agent": id}}
				if tab, _ := w.State["tab"].(string); tab != "" {
					wc.Extra["tab"] = tab
				}
				if m.lib == nil {
					return wc, nil
				}
				if a, err := m.lib.Get(id); err == nil {
					wc.Extra["name"], wc.Extra["digest"], wc.Extra["version"], wc.Extra["valid"] = a.Name, a.Digest, a.Version, a.OK
				}
				return wc, nil
			},
		}},
		Commands: []module.Command{
			{ID: "workbench.open", Title: "Workbench: open an agent", Section: "Workbench", Keywords: "agent nanite eval prompt skills",
				Run: func(ctx context.Context, h module.Host, args map[string]any) (any, error) {
					id, _ := args["agent"].(string)
					opts := module.OpenOptions{}
					if id != "" {
						opts.Args = map[string]any{"agent": id}
					}
					return h.Desk().Open(ctx, "workbench", opts)
				}},
		},
		Tools: m.tools(),
		Settings: []module.Setting{
			{Key: "root", Title: "Agent library", About: "The directory the Workbench keeps its agents in. Empty: agents/ in the module's data directory.", Type: module.SettingString, Default: ""},
			{Key: "eval_max_usd", Title: "Eval run dollar cap", About: "The most one eval run may spend, estimated at a conservative price (at most 50).", Type: module.SettingInt, Default: defaultMaxUSD},
			{Key: "eval_turn_tokens", Title: "Eval case token cap", About: "The most tokens one case's turn may use, its subtasks included.", Type: module.SettingInt, Default: defaultTurnTokens},
			{Key: "eval_run_tokens", Title: "Eval run token budget", About: "A run stops when its cases together have used this many tokens.", Type: module.SettingInt, Default: defaultRunTokens},
		},
		Slots: []module.Slot{{
			Name: "workbench", About: "making and improving agents: their prompts, skills, tools, reflexes, context, evals",
			Views: []string{"workbench"},
			Tools: []string{"workbench_list", "workbench_get", "workbench_write_file", "workbench_validate", "workbench_runs", "workbench_run_get"},
			Where: "the agent library (workbench_list)",
			Instructions: "The owner is working on an agent in the Workbench. Read its files before proposing a change, and say what a change is for. " +
				"Eval runs and improvement proposals call a model and cost money: start one only when the owner asks. Accepting a proposal is the owner's alone.",
		}},
	}
}

func (m *mod) Start(_ context.Context, h module.Host) error {
	m.h = h
	m.lib = NewLibrary(func() string {
		if r := strings.TrimSpace(h.Config().String("root")); r != "" {
			return r
		}
		return filepath.Join(h.DataDir(), "agents")
	})
	if m.rt == nil {
		m.rt = &naniteRuntime{}
	}
	m.run = &Runner{lib: m.lib, rt: m.rt, h: h, now: time.Now}
	// After a crash: the library's leftovers put right, and runs that were going when the app
	// stopped marked as interrupted (their eval process went with it).
	if err := m.lib.Recover(); err != nil {
		h.Log().Warn("workbench: library not recovered", "err", err)
	}
	m.run.recoverRuns()
	m.routes(h)
	return nil
}

func (m *mod) Stop(context.Context) error {
	if m.run != nil {
		m.run.mu.Lock()
		for _, c := range m.run.running {
			c()
		}
		m.run.mu.Unlock()
		m.run.Wait()
	}
	return nil
}

// changed tells the views an agent's files or library changed.
func (m *mod) changed(id string, extra map[string]any) {
	data := map[string]any{"agent": id}
	for k, v := range extra {
		data[k] = v
	}
	m.h.Events().Publish("workbench.changed", data)
}

// create makes an agent from a template.
func (m *mod) create(id, name, template, author string) (Agent, error) {
	if template == "" {
		template = "embedded"
	}
	if id == "" {
		want := name
		if want == "" {
			want = template + "-agent"
		}
		free, err := m.lib.FreeID(want)
		if err != nil {
			return Agent{}, err
		}
		id = free
	}
	t, suites, err := fromTemplate(template, id)
	if err != nil {
		return Agent{}, err
	}
	a, _, err := m.lib.Create(id, t, Meta{Source: "template:" + template, Template: template}, "created from the "+template+" template", author)
	if err != nil {
		return a, err
	}
	for _, raw := range suites {
		if _, err := m.run.SaveSuite(id, raw); err != nil {
			m.h.Log().Warn("workbench: template suite not saved", "agent", id, "err", err)
		}
	}
	m.changed(id, map[string]any{"created": true})
	return a, nil
}

// importBundle adds an imported bundle to the library under a free id.
func (m *mod) importBundle(b Bundle, want, source, author string) (Agent, Report, []string, error) {
	rep := Validate(b.Tree)
	if !rep.OK {
		return Agent{}, rep, b.Skipped, &ErrInvalid{rep}
	}
	if want == "" {
		if b.Manifest != nil && b.Manifest.ID != "" {
			want = b.Manifest.ID
		} else if rep.Summary != nil {
			want = rep.Summary.Name
		}
	}
	id, err := m.lib.FreeID(want)
	if err != nil {
		return Agent{}, rep, b.Skipped, err
	}
	var notes []string
	if b.Manifest != nil && b.Manifest.Digest != "" && b.Manifest.Digest != rep.Digest {
		notes = append(notes, "the files differ from what workbench.json recorded (digest "+short(b.Manifest.Digest)+")")
	}
	a, rep, err := m.lib.Create(id, b.Tree, Meta{Source: source}, "imported from "+source, author)
	if err != nil {
		return a, rep, b.Skipped, err
	}
	for name, raw := range b.Suites {
		if _, err := m.run.SaveSuite(id, raw); err != nil {
			notes = append(notes, fmt.Sprintf("suite %s not kept: %v", name, err))
		}
	}
	m.changed(id, map[string]any{"created": true})
	return a, rep, append(notes, prefixed("skipped: ", b.Skipped)...), nil
}

func prefixed(p string, xs []string) []string {
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, p+x)
	}
	return out
}

func short(d string) string {
	if len(d) > 12 {
		return d[:12]
	}
	return d
}
