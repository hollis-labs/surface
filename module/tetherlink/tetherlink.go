// Package tetherlink calls Tether's tools over MCP for a module: a stdio command (the local
// bridge, one process per call) or MCP's streamable HTTP. Each client names the only tools it
// may call; nothing else is reachable through it, whatever the far side offers. A tool's own
// failure is reported without its text: what Tether says may carry more than a reason.
//
// Extracted from the Design module's client (modules/design/mcp.go), which it will replace.
package tetherlink

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

// Caller calls one tool on an MCP server and returns its text result.
type Caller interface {
	Call(ctx context.Context, tool string, args map[string]any) (string, error)
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func initParams(client string) map[string]any {
	return map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": client, "version": "0.1.0"},
	}
}

// ToolText reads a tools/call result: its text content, or the error it reports. When the
// gateway cut a large result short, the answer is two text blocks: the result (as much of it as
// was kept) and a {"gateway_truncated": ...} note. The note is set aside, so the text is still the
// tool's own JSON; ask for less (a smaller limit) to get it whole.
func ToolText(raw json.RawMessage) (string, error) {
	var r struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("tether: unreadable tool result: %w", err)
	}
	if r.IsError {
		return "", ErrRefused
	}
	var blocks []string
	for _, c := range r.Content {
		if c.Type != "text" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(c.Text), `{"gateway_truncated"`) {
			continue
		}
		blocks = append(blocks, c.Text)
	}
	return strings.Join(blocks, ""), nil
}

// ErrRefused is a tool's own failure. Its text is not passed on: what Tether says may carry
// more than a reason, and nothing from the far side is echoed into the conversation.
var ErrRefused = errors.New("tether: the call was refused")

// Allowed is the set of tools a client may call.
type Allowed map[string]bool

func (a Allowed) check(module, tool string) error {
	if !a[tool] {
		return fmt.Errorf("tether: %s is not a tool the %s module calls", tool, module)
	}
	return nil
}

// ReadToken reads a bearer token from a file. The token is never logged or returned in an error.
func ReadToken(path string) (string, error) {
	b, err := os.ReadFile(path) //nolint:gosec // the path is the owner's own setting
	if err != nil {
		return "", fmt.Errorf("tether: the token file cannot be read: %w", errors.Unwrap(err))
	}
	t := strings.TrimSpace(string(b))
	if t == "" {
		return "", errors.New("tether: the token file is empty")
	}
	return t, nil
}

// HTTP speaks MCP's streamable HTTP transport: JSON-RPC posted to one URL, answered with JSON
// or with an event stream carrying the answer.
type HTTP struct {
	// Module names the caller in errors and to the server.
	Module    string
	URL       string
	TokenFile string
	Client    *http.Client
	Allowed   Allowed
	seq       atomic.Int64
}

func (c *HTTP) post(ctx context.Context, session, token string, req rpcRequest) (*rpcResponse, string, error) {
	body, _ := json.Marshal(req)
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body)) //nolint:gosec // the URL is the owner's own setting
	if err != nil {
		return nil, "", err
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		hr.Header.Set("Authorization", "Bearer "+token)
	}
	if session != "" {
		hr.Header.Set("Mcp-Session-Id", session)
	}
	res, err := c.Client.Do(hr) //nolint:gosec // see above
	if err != nil {
		return nil, "", errors.New("tether: the MCP endpoint cannot be reached")
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return nil, "", fmt.Errorf("tether: the MCP endpoint refused the token (%s)", res.Status)
	}
	if res.StatusCode == http.StatusAccepted || req.ID == 0 {
		return nil, res.Header.Get("Mcp-Session-Id"), nil
	}
	if res.StatusCode/100 != 2 {
		return nil, "", fmt.Errorf("tether: the MCP endpoint answered %s", res.Status)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, "", err
	}
	if strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		data = lastEventData(data, req.ID)
	}
	var out rpcResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, "", errors.New("tether: the MCP answer could not be read")
	}
	return &out, res.Header.Get("Mcp-Session-Id"), nil
}

// lastEventData picks the data of the event that answers id out of an event stream.
func lastEventData(stream []byte, id int64) []byte {
	var found []byte
	for _, line := range bytes.Split(stream, []byte("\n")) {
		d, ok := bytes.CutPrefix(bytes.TrimRight(line, "\r"), []byte("data:"))
		if !ok {
			continue
		}
		d = bytes.TrimSpace(d)
		var probe struct {
			ID int64 `json:"id"`
		}
		if json.Unmarshal(d, &probe) == nil && probe.ID == id {
			found = d
		}
	}
	return found
}

func (c *HTTP) Call(ctx context.Context, tool string, args map[string]any) (string, error) {
	if err := c.Allowed.check(c.Module, tool); err != nil {
		return "", err
	}
	token := ""
	if c.TokenFile != "" {
		t, err := ReadToken(c.TokenFile)
		if err != nil {
			return "", err
		}
		token = t
	}
	init, session, err := c.post(ctx, "", token, rpcRequest{JSONRPC: "2.0", ID: c.seq.Add(1), Method: "initialize", Params: initParams("surface-" + c.Module)})
	if err != nil {
		return "", err
	}
	if init.Error != nil {
		return "", errors.New("tether: the MCP endpoint refused to start a session")
	}
	if _, _, err = c.post(ctx, session, token, rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"}); err != nil {
		return "", err
	}
	res, _, err := c.post(ctx, session, token, rpcRequest{JSONRPC: "2.0", ID: c.seq.Add(1), Method: "tools/call", Params: map[string]any{"name": tool, "arguments": args}})
	if err != nil {
		return "", err
	}
	if res.Error != nil {
		return "", ErrRefused
	}
	return ToolText(res.Result)
}

// Stdio runs a command that speaks MCP over stdio (newline-delimited JSON-RPC), one process per
// call: calls are rare and a process is the simplest thing to get right.
type Stdio struct {
	Module  string
	Argv    []string
	Timeout time.Duration
	Allowed Allowed
}

func (c *Stdio) Call(ctx context.Context, tool string, args map[string]any) (string, error) {
	if err := c.Allowed.check(c.Module, tool); err != nil {
		return "", err
	}
	if len(c.Argv) == 0 {
		return "", errors.New("tether: no command")
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Argv[0], c.Argv[1:]...) //nolint:gosec // the command is the owner's own setting
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	// The command's own output stays out of everything the module reports.
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		return "", errors.New("tether: the command did not start")
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	}()
	enc := json.NewEncoder(stdin)
	lines := bufio.NewScanner(stdout)
	lines.Buffer(make([]byte, 0, 64<<10), 4<<20)
	await := func(id int64) (*rpcResponse, error) {
		for lines.Scan() {
			var r rpcResponse
			if json.Unmarshal(lines.Bytes(), &r) == nil && r.ID == id {
				return &r, nil
			}
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("tether: the command did not answer in %s", timeout)
		}
		return nil, errors.New("tether: the command ended without answering")
	}
	if err = enc.Encode(rpcRequest{JSONRPC: "2.0", ID: 1, Method: "initialize", Params: initParams("surface-" + c.Module)}); err != nil {
		return "", err
	}
	r, err := await(1)
	if err != nil {
		return "", err
	}
	if r.Error != nil {
		return "", errors.New("tether: the command refused to start a session")
	}
	if err = enc.Encode(rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"}); err != nil {
		return "", err
	}
	if err = enc.Encode(rpcRequest{JSONRPC: "2.0", ID: 2, Method: "tools/call", Params: map[string]any{"name": tool, "arguments": args}}); err != nil {
		return "", err
	}
	r, err = await(2)
	if err != nil {
		return "", err
	}
	if r.Error != nil {
		return "", ErrRefused
	}
	return ToolText(r.Result)
}

// Config is how a module reaches Tether: an MCP URL (with a token file) or a stdio command.
type Config struct {
	URL       string
	TokenFile string
	Command   string
}

// New is a client for cfg, or nil when it names no way to reach Tether.
func New(module string, cfg Config, allowed Allowed) Caller {
	if cfg.URL != "" {
		return &HTTP{Module: module, URL: cfg.URL, TokenFile: cfg.TokenFile, Client: &http.Client{Timeout: 20 * time.Second}, Allowed: allowed}
	}
	if argv := strings.Fields(cfg.Command); len(argv) > 0 {
		return &Stdio{Module: module, Argv: argv, Timeout: 30 * time.Second, Allowed: allowed}
	}
	return nil
}
