package core

import "context"

// addServices starts the core's own services. The agent is wired in services_agent.go.
func (a *App) addServices(ctx context.Context) error {
	// The agent takes the tools that exist when it is built, so it is built after every
	// service that brings tools (ADE, generated UI), whatever order their files load in.
	for _, add := range append(append([]serviceFactory(nil), serviceFactories...), lateServiceFactories...) {
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

type serviceFactory = func(ctx context.Context, a *App) (service, error)

// serviceFactories are appended to by the files that wire a service in.
// lateServiceFactories run after them: for a service that uses what the others add.
var serviceFactories, lateServiceFactories []serviceFactory
