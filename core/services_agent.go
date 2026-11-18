package core

import (
	"context"

	"github.com/hollis-labs/surface/core/internal/agent"
)

// The embedded agent: one for the whole desk, with context slots fed by the modules.
func init() {
	serviceFactories = append(serviceFactories, func(_ context.Context, a *App) (service, error) {
		return agent.New(a.k)
	})
}
