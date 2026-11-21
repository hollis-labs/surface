package kernel

import (
	"context"
	"testing"

	"github.com/hollis-labs/surface/module"
)

// The agent's worker can ask about a window before the app has started the module that
// owns it (Studio crashed in Workbench's Describe on v0.10.1). Describe runs only after Start.
func TestDescribeWaitsForTheModuleToStart(t *testing.T) {
	called := 0
	h := &host{id: "m", man: module.Manifest{Views: []module.View{{ID: "v",
		Describe: func(context.Context, module.Host, module.Window) (module.WindowContext, error) {
			called++
			return module.WindowContext{Kind: "data", Ref: "m:x"}, nil
		}}}}}
	a := &App{byModule: map[string]*host{"m": h}}
	w := module.Window{Module: "m", View: "v"}
	if _, ok := a.describe(t.Context(), w); ok || called != 0 {
		t.Fatalf("described before start: ok=%v called=%d", ok, called)
	}
	h.started.Store(true)
	if wc, ok := a.describe(t.Context(), w); !ok || called != 1 || wc.Ref != "m:x" {
		t.Fatalf("after start: %+v ok=%v called=%d", wc, ok, called)
	}
}
