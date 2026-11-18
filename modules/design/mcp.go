package design

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

// mcpCaller calls one tool on an MCP server and returns its text result.
type mcpCaller interface {
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

var initParams = map[string]any{
	"protocolVersion": "2025-06-18",
	"capabilities":    map[string]any{},
	"clientInfo":      map[string]any{"name": "surface-design", "version": "0.1.0"},
}

// toolText reads a tools/call result: its text content, or the error it reports.
func toolText(raw json.RawMessage) (string, error) {
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
	var b strings.Builder
	for _, c := range r.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	if r.IsError {
		return "", errRefused
	}
	return b.String(), nil
}

// errRefused is a tool's own failure. Its text is not passed on: what Tether says may carry
// more than a reason, and nothing from the far side is echoed into the conversation.
var errRefused = errors.New("tether: the call was refused")

// allowed are the only tools the dispatcher may call. Whatever the far side offers, nothing
// else is reachable through it.
var allowed = map[string]bool{"tether_message_send": true, "torque_task_create": true}

func checkTool(tool string) error {
	if !allowed[tool] {
		return fmt.Errorf("tether: %s is not a tool the Design module calls", tool)
	}
	return nil
}

// readToken reads a bearer token from a file. The token is never logged or returned in an error.
func readToken(path string) (string, error) {
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

// httpMCP speaks MCP's streamable HTTP transport: JSON-RPC posted to one URL, answered with
// JSON or with an event stream carrying the answer.
type httpMCP struct {
	url       string
	tokenFile string
	client    *http.Client
	seq       atomic.Int64
}

func (c *httpMCP) post(ctx context.Context, session, token string, req rpcRequest) (*rpcResponse, string, error) {
	body, _ := json.Marshal(req)
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body)) //nolint:gosec // the URL is the owner's own setting
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
	res, err := c.client.Do(hr) //nolint:gosec // see above
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

func (c *httpMCP) Call(ctx context.Context, tool string, args map[string]any) (string, error) {
	if err := checkTool(tool); err != nil {
		return "", err
	}
	token := ""
	if c.tokenFile != "" {
		t, err := readToken(c.tokenFile)
		if err != nil {
			return "", err
		}
		token = t
	}
	init, session, err := c.post(ctx, "", token, rpcRequest{JSONRPC: "2.0", ID: c.seq.Add(1), Method: "initialize", Params: initParams})
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
		return "", errRefused
	}
	return toolText(res.Result)
}

// stdioMCP runs a command that speaks MCP over stdio (newline-delimited JSON-RPC), one
// process per call: calls are rare and a process is the simplest thing to get right.
type stdioMCP struct {
	argv    []string
	timeout time.Duration
}

func (c *stdioMCP) Call(ctx context.Context, tool string, args map[string]any) (string, error) {
	if err := checkTool(tool); err != nil {
		return "", err
	}
	if len(c.argv) == 0 {
		return "", errors.New("tether: no command")
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.argv[0], c.argv[1:]...) //nolint:gosec // the command is the owner's own setting
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
			return nil, fmt.Errorf("tether: the command did not answer in %s", c.timeout)
		}
		return nil, errors.New("tether: the command ended without answering")
	}
	if err = enc.Encode(rpcRequest{JSONRPC: "2.0", ID: 1, Method: "initialize", Params: initParams}); err != nil {
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
		return "", errRefused
	}
	return toolText(r.Result)
}
