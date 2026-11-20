package agent

import (
	"context"
	"encoding/json"
)

// ToolBinding is a server tool offered to Nanite.
type ToolBinding struct {
	Name        string
	Description string
	InputSchema map[string]any
	// Level is read, act, write or confirm.
	Level string
	Run   func(ctx context.Context, input json.RawMessage) (string, error)
}
