package workbench

import (
	"context"
	"time"

	"github.com/hollis-labs/surface/module"
)

// execute keeps the app's cwd unchanged: Nanite gives each case its own WorkDir.
func (x *Runner) execute(ctx context.Context, run Run, s Suite, t Tree, gw module.Gateway) Run {
	graded := 0
	result := executeSuite(ctx, x.rt, run, s, t, gw, func(id string) {
		graded++
		x.h.Events().Publish("workbench.run", map[string]any{"agent": run.Agent, "run": run.ID, "status": "running", "graded": graded, "case": id})
	})
	if ctx.Err() != nil {
		result.Status, result.Stopped = "canceled", "canceled by the owner"
		result.Finished = x.now().UTC().Format(time.RFC3339)
	}
	return result
}
