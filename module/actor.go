package module

import "context"

// Actor is who is doing something to the app: the owner at the page, the embedded agent, a
// reflex, or a client from outside (MCP, the HTTP tools, the CLI). The desk keeps it on the
// windows it opens, and holds to the owner's say over windows that are theirs
// (docs/desk-rules.md).
//
// The core sets it where a request comes in; a module only reads it. A context that carries
// none is treated as the owner's, which is how the desk behaved before actors existed: a
// module acting on its own (a timer, a background job) is not held to the agent's etiquette
// unless it says so with WithActor.
type Actor string

const (
	ActorOwner   Actor = "owner"
	ActorAgent   Actor = "agent"
	ActorReflex  Actor = "reflex"
	ActorOutside Actor = "outside"
)

type actorKey struct{}

// WithActor returns a context that says who is acting.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

// ActorOf says who is acting, or "" when the context does not say.
func ActorOf(ctx context.Context) Actor {
	a, _ := ctx.Value(actorKey{}).(Actor)
	return a
}

// AgentRef names an agent from outside the app that is acting through it (a seat behind a
// seat host, over MCP): Name is how the owner knows it, ReplyTo is the asker's own handle for
// routing an answer back (a seat host's session id). The embedded agent has none.
type AgentRef struct {
	Name    string
	ReplyTo string
}

type agentKey struct{}

// WithAgent returns a context that names the outside agent acting.
func WithAgent(ctx context.Context, a AgentRef) context.Context {
	return context.WithValue(ctx, agentKey{}, a)
}

// AgentOf names the outside agent acting, or the zero AgentRef when the context does not.
func AgentOf(ctx context.Context) AgentRef {
	a, _ := ctx.Value(agentKey{}).(AgentRef)
	return a
}

// Delegate reports whether the actor acts for the owner rather than as the owner: the agent,
// a reflex, or an outside client. The owner and an unnamed actor are not delegates.
func (a Actor) Delegate() bool {
	return a == ActorAgent || a == ActorReflex || a == ActorOutside
}
