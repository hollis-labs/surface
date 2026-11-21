package core

import (
	"context"
	"fmt"

	"github.com/hollis-labs/surface/core/internal/agent"
	"github.com/hollis-labs/surface/core/internal/agent/profile"
	"github.com/hollis-labs/surface/module"
)

// The embedded agent: one for the whole desk, with context slots fed by the modules.
func init() {
	lateServiceFactories = append(lateServiceFactories, func(ctx context.Context, a *App) (service, error) {
		// The model gateway the agent config names, for a module that calls a model itself
		// on the owner's action (Host.Agent().Gateway). Read at the moment of use.
		app := a.k.AppName()
		a.k.SetGateway(func(context.Context) (module.Gateway, error) { return agent.ModuleGateway(app) })
		modules := map[string]string{}
		for _, m := range a.k.Modules() {
			modules[m.ID] = m.Title
		}
		validate := func(v any) error {
			bindings, err := agent.ParseBindings(v)
			if err != nil {
				return err
			}
			for id, dir := range bindings {
				title, ok := modules[id]
				if !ok {
					return fmt.Errorf("unknown module %q", id)
				}
				if _, err := profile.LoadBinding(id, title, dir); err != nil {
					return err
				}
			}
			return nil
		}
		if err := a.k.DeclareSettings("agent", []module.Setting{{Key: "bindings", Title: "Module agents", About: "Choose a Nanite agent folder for each module.", Type: module.SettingObject, Default: map[string]any{}, Owner: true, Validate: validate}}); err != nil {
			return nil, err
		}
		logIgnored := func(v any) {
			bindings, _ := agent.ParseBindings(v)
			for id, dir := range bindings {
				p, err := profile.LoadBinding(id, modules[id], dir)
				if err == nil && len(p.IgnoredRuntime) > 0 {
					a.k.Log().Info("bound agent host.yaml runtime keys are ignored; the app runtime applies", "module", id, "agent_dir", dir, "ignored_runtime_keys", p.IgnoredRuntime)
				}
			}
		}
		value, _ := a.k.Settings().Get("agent.bindings")
		if err := validate(value); err != nil {
			return nil, fmt.Errorf("agent.bindings: %w", err)
		}
		logIgnored(value)
		// Watch callbacks run only after a successful write, not during validation.
		a.k.Settings().Watch("agent.bindings", logIgnored)
		s, err := agent.NewWith(a.k, agent.Options{Modules: modules, Bindings: func() map[string]string {
			value, _ := a.k.Settings().Get("agent.bindings")
			bindings, _ := agent.ParseBindings(value)
			return bindings
		}})
		if err != nil {
			return nil, err
		}
		// The agent's own tools (slots, reflexes, routing rules) are offered to outside
		// agents too, at /api/tools and /mcp: the Interaction Builder's work, from anywhere.
		if err := a.k.AddTools(s.Tools()...); err != nil {
			_ = s.Close(ctx)
			return nil, err
		}
		// Modules hand the agent errands of their own (Host.Agent).
		a.k.SetAsker(s)
		return s, nil
	})
}
