package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FileConfig is the agent and harness configuration. It lives in a file, not the app's
// database (the app's own settings do): ~/.config/<app>/agent.json, or the file
// SURFACE_AGENT_CONFIG names. Environment variables override single fields for a
// development run: SURFACE_AGENT_GATEWAY, SURFACE_AGENT_MODEL, SURFACE_AGENT_KEY_FILE.
//
// The key itself is never in this file: KeyFile names a file that holds it, read on every
// request, so it can be rotated without a restart.
type FileConfig struct {
	// Model is the gateway's model name ("anthropic-opus").
	Model string `json:"model"`
	// GatewayURL is an Anthropic-compatible endpoint; the engine posts to <url>/v1/messages.
	GatewayURL string `json:"gateway_url"`
	// KeyFile holds the bearer key. "~/" is the home directory.
	KeyFile string `json:"key_file"`
	// System is added to the agent's standing instructions.
	System    string `json:"system,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
	MaxSteps  int    `json:"max_steps,omitempty"`
	CompactAt int    `json:"compact_at,omitempty"`
	// MCP lists outside MCP servers whose tools the agent gets. Reserved: the engine does
	// not speak MCP yet (libs agent/host has no MCP client).
	MCP map[string]json.RawMessage `json:"mcp,omitempty"`

	// Path is the file this was read from ("" when there was none).
	Path string `json:"-"`
}

// ConfigPath is where the agent config of app is read from.
func ConfigPath(app string) string {
	if p := os.Getenv("SURFACE_AGENT_CONFIG"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if home, herr := os.UserHomeDir(); herr == nil {
		// ~/.config on every platform: the same place the harness configs live.
		dir, err = filepath.Join(home, ".config"), nil
	}
	if err != nil {
		return ""
	}
	return filepath.Join(dir, app, "agent.json")
}

// LoadConfig reads the agent config of app. A missing file is not an error: the agent is
// then not ready until one is written, and says so.
func LoadConfig(app string) (FileConfig, error) {
	var c FileConfig
	path := ConfigPath(app)
	if path != "" {
		raw, err := os.ReadFile(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			return c, err
		default:
			if err := json.Unmarshal(raw, &c); err != nil {
				return c, fmt.Errorf("%s: %w", path, err)
			}
			c.Path = path
		}
	}
	if v := os.Getenv("SURFACE_AGENT_GATEWAY"); v != "" {
		c.GatewayURL = v
	}
	if v := os.Getenv("SURFACE_AGENT_MODEL"); v != "" {
		c.Model = v
	}
	if v := os.Getenv("SURFACE_AGENT_KEY_FILE"); v != "" {
		c.KeyFile = v
	}
	if rest, ok := strings.CutPrefix(c.KeyFile, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			c.KeyFile = filepath.Join(home, rest)
		}
	}
	return c, nil
}
