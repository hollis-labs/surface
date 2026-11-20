package agent

import (
	"context"
	"encoding/json"
	"net/http"
)

// Engine is the optional injected engine seam used by custom integrations and tests.
// Production embedded agents use Nanite on the durable conversation wire.
type Engine interface {
	// Ready reports whether a model is configured, so a message can be answered.
	Ready() bool
	Model() string
	// AddTools gives the agent tools it runs itself. Call before serving.
	AddTools(tools ...EngineTool) error
	// Start takes the owner's message and begins answering it; the answer is the run's
	// event stream, served by Handler.
	Start(ctx context.Context, t Turn) (runID string, err error)
	// History is the owner's conversation so far, most recent last.
	History(ctx context.Context, owner string) ([]Said, error)
	// Handler serves the wire under prefix: the thread, the run streams, tool results,
	// approvals, cancel. Surface overrides the turn route itself.
	Handler(prefix string, owner func(*http.Request) (string, error)) http.Handler
	Close()
}

// Turn is one message from the owner.
type Turn struct {
	Owner string
	Text  string
	// ClientID identifies the page that sent it.
	ClientID string
	// Context is what the model sees with the message. Surface sends {"block": "<context>…"}
	// plus the assembly, for the panel to show.
	Context json.RawMessage
	// Auto marks a message the app wrote and the owner did not type.
	Auto string
}

// EngineTool is a tool the engine runs itself.
type EngineTool struct {
	Name        string
	Description string
	InputSchema map[string]any
	// Level is read, act, write or confirm.
	Level string
	Run   func(ctx context.Context, input json.RawMessage) (string, error)
}

// Said is one message of the conversation, as the reflexes see it.
type Said struct {
	Role      string
	Text      string
	ToolNames []string
	CreatedAt string
}

// unavailableEngine keeps setup endpoints usable when no embedded model is configured.
// It owns no runtime, threads, or persistence.
type unavailableEngine struct{}

func (unavailableEngine) Ready() bool                                     { return false }
func (unavailableEngine) Model() string                                   { return "" }
func (unavailableEngine) AddTools(...EngineTool) error                    { return nil }
func (unavailableEngine) Start(context.Context, Turn) (string, error)     { return "", ErrNotReady }
func (unavailableEngine) History(context.Context, string) ([]Said, error) { return nil, nil }
func (unavailableEngine) Close()                                          {}
func (unavailableEngine) Handler(prefix string, _ func(*http.Request) (string, error)) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+prefix, func(w http.ResponseWriter, _ *http.Request) { reply(w, 200, map[string]any{"ready": false}) })
	mux.HandleFunc("GET "+prefix+"/thread", func(w http.ResponseWriter, _ *http.Request) { reply(w, 200, map[string]any{"messages": []Said{}}) })
	return mux
}
