package core

import (
	"context"

	"github.com/hollis-labs/surface/core/internal/script"
)

// Scripts: one walk-through that runs as an onboarding guide, a recorded demo, or a test
// (docs/scripts.md). Its tools go to every agent (embedded, /api/tools, /mcp).
func init() {
	serviceFactories = append(serviceFactories, func(ctx context.Context, a *App) (service, error) {
		s, err := script.New(ctx, a.k)
		if err != nil {
			return nil, err
		}
		if err := a.k.AddTools(s.Tools()...); err != nil {
			_ = s.Close(ctx)
			return nil, err
		}
		return s, nil
	})
}
