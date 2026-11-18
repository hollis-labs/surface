package core

import (
	"context"

	"github.com/hollis-labs/surface/core/internal/agent"
)

// The embedded agent: one for the whole desk, with context slots fed by the modules.
func init() {
	serviceFactories = append(serviceFactories, func(ctx context.Context, a *App) (service, error) {
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
		return s, nil
	})
}
