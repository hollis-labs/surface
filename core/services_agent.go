package core

import (
	"context"

	"github.com/hollis-labs/surface/core/internal/agent"
	"github.com/hollis-labs/surface/module"
)

// The embedded agent: one for the whole desk, with context slots fed by the modules.
func init() {
	lateServiceFactories = append(lateServiceFactories, func(ctx context.Context, a *App) (service, error) {
		// The model gateway the agent config names, for a module that calls a model itself
		// on the owner's action (Host.Agent().Gateway). Read at the moment of use.
		app := a.k.AppName()
		a.k.SetGateway(func(context.Context) (module.Gateway, error) { return agent.ModuleGateway(app) })
		s, err := agent.New(a.k)
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
