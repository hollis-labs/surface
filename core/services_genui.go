package core

import (
	"context"

	"github.com/hollis-labs/surface/core/internal/genui"
)

// Generated UI: the agent shows one of three reviewed patterns and the owner's action goes
// through Substrate's GenUI gateway. The tool goes to every agent (embedded, /api/tools, /mcp).
func init() {
	serviceFactories = append(serviceFactories, func(ctx context.Context, a *App) (service, error) {
		s, err := genui.New(ctx, a.k, a.k.Cards())
		if err != nil {
			return nil, err
		}
		if err := a.k.AddTools(s.Tools()...); err != nil {
			return nil, err
		}
		return s, nil
	})
}
