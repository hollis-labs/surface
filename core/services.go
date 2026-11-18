package core

import "context"

// addServices starts the core's own services. The agent is wired in services_agent.go.
func (a *App) addServices(ctx context.Context) error {
	for _, add := range serviceFactories {
		s, err := add(ctx, a)
		if err != nil {
			return err
		}
		if s != nil {
			a.services = append(a.services, s)
		}
	}
	return nil
}

// serviceFactories are appended to by the files that wire a service in.
var serviceFactories []func(ctx context.Context, a *App) (service, error)
