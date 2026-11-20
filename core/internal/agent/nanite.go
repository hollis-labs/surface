package agent

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	libreflex "github.com/hollis-labs/libs/agent/core/reflexes"
	nanite "github.com/hollis-labs/nanite/agent"
	"github.com/hollis-labs/nanite/chatrunner"
)

// The Nanite runner for the conversation wire: the app's agent is a Nanite agent opened in
// this process, behind the same wire the scripted runner exercises.
//
// What is Surface's and what is Nanite's:
//
//   - The agent's standing instructions are authored files in its agent directory
//     (agent.md, host.yaml), kept beside the app's agent config. Surface writes a starter
//     the first time and never again: the files are the owner's to edit.
//   - Its run-time state (conversations, the lock) is under the app's data directory.
//   - Its tools are the app's, run here, with the app's levels. Nanite's own tool groups
//     are off unless the agent config turns some on.
//   - What is in front of the owner is Surface's context block, written for each turn and
//     placed in one slot for that turn only. Layout and slot instructions travel in it.
//   - The model, the gateway and the key file are the ones in the app's agent config.

// RunnerNanite names the Nanite runner.
const RunnerNanite = "nanite"

//go:embed seat/agent.md seat/host.yaml
var seatFiles embed.FS

// AgentDir is the authored directory of the app's agent: <agents.dir>/<app>, where
// agents.dir defaults to agents/ beside the agent config file.
func (c FileConfig) AgentDir(app string) string {
	dir := c.Agents.Dir
	if v := os.Getenv("SURFACE_AGENTS_DIR"); v != "" {
		dir = v
	}
	if rest, ok := strings.CutPrefix(dir, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, rest)
		}
	}
	if dir == "" {
		base := c.Path
		if base == "" {
			base = ConfigPath(app)
		}
		if base == "" {
			return ""
		}
		dir = filepath.Join(filepath.Dir(base), "agents")
	}
	return filepath.Join(dir, app)
}

// plantSeat writes the starter agent directory when there is none. An existing directory
// is left exactly as it is.
func plantSeat(dir, app string) (bool, error) {
	if _, err := os.Stat(dir); err == nil {
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	for _, name := range []string{"agent.md", "host.yaml"} {
		raw, err := seatFiles.ReadFile("seat/" + name)
		if err != nil {
			return false, err
		}
		body := strings.ReplaceAll(string(raw), "{{app}}", app)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			return false, err
		}
	}
	return true, nil
}

// builtinGroups is the agent config's list of Nanite tool groups, checked. None by default.
func builtinGroups(names []string) ([]nanite.BuiltinGroup, error) {
	known := map[string]nanite.BuiltinGroup{}
	var all []string
	for _, g := range nanite.AllBuiltins() {
		known[string(g)] = g
		all = append(all, string(g))
	}
	out := []nanite.BuiltinGroup{}
	for _, n := range names {
		n = strings.ToLower(strings.TrimSpace(n))
		if n == "all" {
			return nanite.AllBuiltins(), nil
		}
		g, ok := known[n]
		if !ok {
			return nil, fmt.Errorf("agent: no built-in tool group %q (known: %s, or all)", n, strings.Join(all, ", "))
		}
		out = append(out, g)
	}
	return out, nil
}

// asks is which tool a routing rule marked ask-first for the turn in progress, by
// conversation. A rule that is not automatic tells the agent what was asked; the call it
// then makes asks the owner first, whatever the tool's level.
type asks struct {
	mu sync.Mutex
	by map[string]string
}

func (a *asks) set(conversation, tool string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.by == nil {
		a.by = map[string]string{}
	}
	if tool == "" {
		delete(a.by, conversation)
		return
	}
	a.by[conversation] = tool
}

func (a *asks) has(conversation, tool string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return tool != "" && a.by[conversation] == tool
}

// openNanite opens the app's agent as a Nanite agent with the app's tools.
func (s *Service) openNanite(ctx context.Context, w *wire) (*chatrunner.Runner, error) {
	if s.cfgErr != nil {
		return nil, fmt.Errorf("agent: config: %w", s.cfgErr)
	}
	c := s.cfg
	if c.Model == "" || c.GatewayURL == "" || c.KeyFile == "" {
		return nil, fmt.Errorf("agent: the nanite runner needs model, gateway_url and key_file in %s", ConfigPath(s.k.AppName()))
	}
	dir := c.AgentDir(w.agent)
	if dir == "" {
		return nil, errors.New("agent: no place for the agent directory; set agents.dir in the agent config")
	}
	planted, err := plantSeat(dir, w.agent)
	if err != nil {
		return nil, fmt.Errorf("agent: agent directory %s: %w", dir, err)
	}
	if planted {
		s.log.Info("wrote a starter agent directory; it is yours to edit", "dir", dir)
	}
	groups, err := builtinGroups(c.Builtins)
	if err != nil {
		return nil, err
	}
	// With module profiles the agent opens on a staged directory: the seat, with the active
	// profile over it, written again before each turn (profiles.go).
	open := dir
	if len(s.prof.order) > 0 {
		s.prof.seat, s.prof.stage = dir, s.stageDir(w.agent)
		if err = s.stageProfile(ProfileChoice{Why: "default"}); err != nil {
			s.prof.stage = ""
			return nil, fmt.Errorf("agent: staged agent directory: %w", err)
		}
		open = s.prof.stage
		groups = s.profileBuiltins(groups)
		s.log.Info("module agent profiles are on", "profiles", s.prof.order, "staged", open)
	}
	// Nanite takes the model from host.yaml, or from this variable when the file names
	// none (the starter names none): the app's agent config stays the one place it is set.
	if os.Getenv("NANITE_DEFAULT_MODEL") == "" {
		_ = os.Setenv("NANITE_DEFAULT_MODEL", c.Model)
	}
	db := s.k.DB()
	r, err := chatrunner.Open(ctx, chatrunner.Options{
		Agent: nanite.Options{
			Dir:             open,
			DataDir:         filepath.Join(s.k.DataDir(), "agents", w.agent),
			GlobalSkillsDir: "-",
			// The key is a reference to the file; Nanite reads it when it calls the gateway.
			Gateway:  nanite.Gateway{BaseURL: c.GatewayURL, KeyRef: "file://" + c.KeyFile, Auth: "bearer"},
			Logger:   s.log.With("part", "nanite"),
			Builtins: groups,
		},
		Tools: s.runnerTools(),
		Ask: func(_ context.Context, call chatrunner.Call) bool {
			return w.asks.has(call.ConversationID, call.Tool)
		},
		Granted: func(ctx context.Context, call chatrunner.Call) bool {
			ok, gerr := granted(ctx, db, w.agent, call.Tool)
			if gerr != nil {
				s.log.Warn("standing approvals not read; asking", "err", gerr)
			}
			return ok
		},
	})
	if err != nil {
		if s.prof != nil {
			s.prof.stage = ""
		}
		return nil, fmt.Errorf("agent: nanite: %w", err)
	}
	s.log.Info("the agent is a Nanite agent", "dir", dir, "tools", len(s.engineTools()), "builtins", len(groups))
	return r, nil
}

// runnerTools is the app's tools as the runner takes them: the same list, levels and
// approver the engine gets.
func (s *Service) runnerTools() []chatrunner.Tool {
	var out []chatrunner.Tool
	seen := map[string]bool{}
	for _, t := range s.engineTools() {
		if seen[t.Name] {
			continue
		}
		seen[t.Name] = true
		schema, err := json.Marshal(t.InputSchema)
		if err != nil {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		run := t.Run
		out = append(out, chatrunner.Tool{Name: t.Name, Description: t.Description, InputSchema: schema,
			Level: chatrunner.Level(t.Level),
			Run: func(ctx context.Context, call nanite.ToolCall) (nanite.ToolResult, error) {
				input := call.Input
				if len(input) == 0 {
					input = json.RawMessage(`{}`)
				}
				text, err := run(ctx, input)
				if err != nil {
					// A failed call is the model's to read and recover from, not the turn's end.
					return nanite.ToolResult{Text: err.Error(), IsError: true}, nil //nolint:nilerr // the error is the tool's result
				}
				return nanite.ToolResult{Text: text}, nil
			}})
	}
	return out
}

// granted reports a kept standing approval for a tool.
func granted(ctx context.Context, db *sql.DB, agent, tool string) (bool, error) {
	if tool == "" {
		return false, nil
	}
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_grants WHERE agent = ? AND tool = ?`, agent, tool).Scan(&n)
	return n > 0, err
}

// naniteHistory is the conversation so far as the reflexes see it, most recent first, read
// from the agent's own history.
func (w *wire) naniteHistory(ctx context.Context, conversation string) []libreflex.MessageSignal {
	c, err := w.nanite.Agent().Conversation(ctx, chatrunner.ConversationID(conversation))
	if err != nil {
		return nil // no turn yet
	}
	hist, err := c.History(ctx)
	if err != nil {
		return nil
	}
	var prior []libreflex.MessageSignal
	for i := len(hist) - 1; i >= 0 && len(prior) < 40; i-- {
		m := hist[i]
		var text []string
		var tools []string
		for _, b := range m.Blocks {
			switch b.Type {
			case "text":
				text = append(text, b.Text)
			case "tool_use":
				tools = append(tools, b.Name)
			}
		}
		if m.Role == "user" && len(text) == 0 {
			continue // tool results only
		}
		prior = append(prior, libreflex.MessageSignal{Role: m.Role, Content: strings.Join(text, "\n"), CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339),
			ToolCalls: len(tools), ToolNames: tools})
	}
	return prior
}
