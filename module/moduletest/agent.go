package moduletest

import (
	"context"
	"fmt"
	"sync"

	"github.com/hollis-labs/surface/module"
)

// Agent records the errands a module hands the agent. Err, when set, is what Ask returns
// (module.ErrAgentBusy, module.ErrNoAgent) instead of taking the errand.
type Agent struct {
	mu   sync.Mutex
	Asks []module.Ask
	Err  error
	// GW is what Gateway returns; nil gives module.ErrNoAgent.
	GW *module.Gateway
}

func (a *Agent) Ask(_ context.Context, x module.Ask) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.Err != nil {
		return "", a.Err
	}
	a.Asks = append(a.Asks, x)
	return fmt.Sprintf("run-%d", len(a.Asks)), nil
}

// Taken is a copy of the errands taken so far.
func (a *Agent) Taken() []module.Ask {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]module.Ask(nil), a.Asks...)
}

// Gateway returns GW, or module.ErrNoAgent when it is nil.
func (a *Agent) Gateway(context.Context) (module.Gateway, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.GW == nil {
		return module.Gateway{}, module.ErrNoAgent
	}
	return *a.GW, nil
}
