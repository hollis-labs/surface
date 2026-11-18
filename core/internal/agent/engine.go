package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/hollis-labs/libs/agent/host"
)

// Engine is the seam the embedded agent plugs into. Everything else in Surface (slots,
// router, reflexes, the panel's wire) talks to this, so the agent underneath can change
// without them: today libs agent/host, later Nanite's embeddable package.
//
// The wire it serves is agent-host/1 (libs agent/host WIRE.md), which the page's
// @hollis-labs/ade-agent client speaks.
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

// hostEngine is the Engine over libs agent/host.
type hostEngine struct {
	h *host.Host
}

// newHostEngine builds the agent/host engine with its conversation tables in db.
func newHostEngine(ctx context.Context, cfg FileConfig, system string, db *sql.DB) (*hostEngine, error) {
	store, err := host.NewSQLite(ctx, db)
	if err != nil {
		return nil, err
	}
	h := host.New(host.Config{
		Model:     cfg.Model,
		BaseURL:   cfg.GatewayURL,
		KeyFile:   cfg.KeyFile,
		System:    system,
		MaxTokens: cfg.MaxTokens,
		MaxSteps:  cfg.MaxSteps,
		CompactAt: cfg.CompactAt,
		// Surface assembles the block itself (slots.Assemble); the host only unwraps it.
		DescribeContext: describe,
	}, store)
	return &hostEngine{h: h}, nil
}

// describe returns the block Surface put in the turn's context.
func describe(raw json.RawMessage) string {
	var c struct {
		Block string `json:"block"`
	}
	if json.Unmarshal(raw, &c) != nil {
		return ""
	}
	return c.Block
}

func (e *hostEngine) Ready() bool   { return e.h.Config().Ready() }
func (e *hostEngine) Model() string { return e.h.Config().Model }
func (e *hostEngine) Close()        { e.h.Close() }

func (e *hostEngine) AddTools(tools ...EngineTool) error {
	out := make([]host.ServerTool, 0, len(tools))
	for _, t := range tools {
		run := t.Run
		out = append(out, host.ServerTool{
			Spec: host.ToolSpec{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema, Level: t.Level},
			Run: func(ctx context.Context, _ string, input map[string]any) (string, error) {
				raw, err := json.Marshal(input)
				if err != nil {
					return "", err
				}
				return run(ctx, raw)
			},
		})
	}
	return e.h.AddServerTools(out...)
}

func (e *hostEngine) Start(ctx context.Context, t Turn) (string, error) {
	id, err := e.h.Start(ctx, host.Turn{Owner: t.Owner, Text: t.Text, ClientID: t.ClientID, Context: t.Context, Auto: t.Auto})
	return id, engineErr(err)
}

func (e *hostEngine) History(ctx context.Context, owner string) ([]Said, error) {
	st := e.h.Store()
	thread, err := st.MainThread(ctx, owner)
	if err != nil {
		return nil, err
	}
	msgs, err := st.Messages(ctx, thread)
	if err != nil {
		return nil, err
	}
	out := make([]Said, 0, len(msgs))
	for _, m := range msgs {
		s := Said{Role: m.Role, CreatedAt: m.CreatedAt}
		var meta struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(m.Meta, &meta)
		var words []string
		for _, b := range m.Blocks {
			switch b.Type {
			case "text":
				words = append(words, b.Text)
			case "tool_use":
				s.ToolNames = append(s.ToolNames, b.Name)
			}
		}
		if m.Role == "user" && meta.Text != "" {
			s.Text = meta.Text // what the owner typed, without the context block
		} else {
			for i, w := range words {
				if i > 0 {
					s.Text += "\n"
				}
				s.Text += w
			}
		}
		out = append(out, s)
	}
	return out, nil
}

func (e *hostEngine) Handler(prefix string, owner func(*http.Request) (string, error)) http.Handler {
	return e.h.Handler(prefix, owner)
}
