package core

import (
	"context"

	"github.com/hollis-labs/surface/core/internal/attend"
)

// What needs the owner, or has not been seen by them, in one vocabulary (docs/attention.md).
// Its tool goes to every agent (embedded, /api/tools, /mcp).
func init() {
	serviceFactories = append(serviceFactories, func(ctx context.Context, a *App) (service, error) {
		s, err := attend.New(ctx, a.k.DB(), a.k.Events(), a.k.Desk().Windows)
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
