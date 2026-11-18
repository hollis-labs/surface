package core

import (
	"context"

	"github.com/hollis-labs/surface/core/internal/ade"
)

// ADE's overlays: the agent points at windows and anchors and runs short guides; the page
// draws them. The tools go to every agent (embedded, /api/tools, /mcp).
func init() {
	serviceFactories = append(serviceFactories, func(_ context.Context, a *App) (service, error) {
		s := ade.New(a.k)
		if err := a.k.AddTools(s.Tools()...); err != nil {
			return nil, err
		}
		return s, nil
	})
}
