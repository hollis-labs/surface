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

	// Agents says where the agent's authored directory is kept, for a runner that has one
	// (Nanite): <dir>/<app>. Default: agents/ beside this file.
	Agents struct {
		Dir string `json:"dir,omitempty"`
	} `json:"agents,omitempty"`
	// Builtins turns on tool groups of the runner's own (Nanite: files-read, files-write,
	// shell, background, subagents, skills, notes, or all). None by default: the agent has
	// the app's tools.
	Builtins []string `json:"builtins,omitempty"`

	// Classifier configures the classifier's async tier: a decision gateway (JEV) asked
	// about each message after the turn. Without GatewayURL there is no async tier.
	Classifier ClassifierConfig `json:"classifier,omitempty"`

	// Path is the file this was read from ("" when there was none).
	Path string `json:"-"`
}

// ClassifierConfig is the classifier's async tier: a model gateway asked about each message
// after the turn, off its path. With no GatewayURL there is no async tier.
type ClassifierConfig struct {
	// GatewayURL is the gateway's base URL (or unix:///path.sock).
	GatewayURL string `json:"gateway_url,omitempty"`
	// Dialect is how the gateway is spoken to:
	//   - "llm-gateway" (default): JEV's System-1 API at <url>/v1/systemone and
	//     OpenAI-compatible <url>/v1/embeddings, with the gateway's model aliases.
	//   - "tether": a decision gateway's <url>/ai/decide and <url>/ai/embeddings.
	Dialect string `json:"dialect,omitempty"`
	// KeyFile holds the gateway's bearer key ("~/" is the home directory). The key needs
	// the decide scope for DecideModel and the embed scope for EmbedModel.
	KeyFile string `json:"key_file,omitempty"`
	// DecideModel turns on the JEV judge: the gateway's decision route ("jev"). Pinned
	// models keep results comparable.
	DecideModel string `json:"decide_model,omitempty"`
	// EmbedModel turns on the centroid judges and the emerging-label report: the gateway's
	// embedding route ("embed-local", "embed-openai"). Vectors from different models do not
	// compare, so changing it refits the centroids from scratch.
	EmbedModel string `json:"embed_model,omitempty"`
	// Purpose is what a decision gateway accounts the spend to; default surface_classify.
	Purpose string `json:"purpose,omitempty"`
}

// Classifier gateway dialects.
const (
	DialectLLMGateway = "llm-gateway"
	DialectTether     = "tether"
)

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
		raw, err := os.ReadFile(path) //nolint:gosec // the operator's own config file, by design
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
	c.KeyFile = expandHome(c.KeyFile)
	c.Classifier.KeyFile = expandHome(c.Classifier.KeyFile)
	return c, nil
}

func expandHome(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return p
}
