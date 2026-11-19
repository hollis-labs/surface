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
	"sync"
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
// tool's own JSON; ask for less (a smaller limit) to get it whole, or use CallAll.
func ToolText(raw json.RawMessage) (string, error) {
	text, _, err := blocks(raw)
	return text, err
}

// ErrRefused is a tool's own failure. Its text is not passed on: what Tether says may carry
// more than a reason, and nothing from the far side is echoed into the conversation.
var ErrRefused = errors.New("tether: the call was refused")

// RefusedError is a tool's own failure with what the far side said. It reads as ErrRefused
// (errors.Is holds, and Error() says no more), so a module that reports err.Error() passes
// nothing on. Detail is for a caller that hands the result to a model as material, such as
// the agent's own Tether tools, where the reason a call failed is what lets it try again.
type RefusedError struct{ Detail string }

func (e *RefusedError) Error() string        { return ErrRefused.Error() }
func (e *RefusedError) Is(target error) bool { return target == ErrRefused }

// ToolInfo is one tool as the server lists it.
type ToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// Lister lists the server's tools that the client's Allowed names (tools/list, every page).
type Lister interface {
	List(ctx context.Context) ([]ToolInfo, error)
}

// listResult reads one page of tools/list, keeping the allowed tools.
func listResult(raw json.RawMessage, allowed Allowed, into []ToolInfo) ([]ToolInfo, string, error) {
	var page struct {
		Tools      []ToolInfo `json:"tools"`
		NextCursor string     `json:"nextCursor"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		return into, "", errors.New("tether: the tool list could not be read")
	}
	for _, t := range page.Tools {
		if allowed[t.Name] {
			into = append(into, t)
		}
	}
	return into, page.NextCursor, nil
}

// maxListPages bounds tools/list paging against a server that never stops.
const maxListPages = 50

func rpcRefused(e *struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}) error {
	return &RefusedError{Detail: e.Message}
}

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

// open starts a session: the token, and the session id the server gave.
func (c *HTTP) open(ctx context.Context) (session, token string, err error) {
	if c.TokenFile != "" {
		if token, err = ReadToken(c.TokenFile); err != nil {
			return "", "", err
		}
	}
	init, session, err := c.post(ctx, "", token, rpcRequest{JSONRPC: "2.0", ID: c.seq.Add(1), Method: "initialize", Params: initParams("surface-" + c.Module)})
	if err != nil {
		return "", "", err
	}
	if init.Error != nil {
		return "", "", errors.New("tether: the MCP endpoint refused to start a session")
	}
	if _, _, err = c.post(ctx, session, token, rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"}); err != nil {
		return "", "", err
	}
	return session, token, nil
}

func (c *HTTP) Call(ctx context.Context, tool string, args map[string]any) (string, error) {
	return c.run(ctx, tool, args, false)
}

// CallAll is Call that reads the whole of a result the gateway cut short (see CallAll).
func (c *HTTP) CallAll(ctx context.Context, tool string, args map[string]any) (string, error) {
	return c.run(ctx, tool, args, true)
}

func (c *HTTP) run(ctx context.Context, tool string, args map[string]any, all bool) (string, error) {
	if err := c.Allowed.check(c.Module, tool); err != nil {
		return "", err
	}
	session, token, err := c.open(ctx)
	if err != nil {
		return "", err
	}
	call := func(tool string, args map[string]any) (json.RawMessage, error) {
		res, _, err := c.post(ctx, session, token, rpcRequest{JSONRPC: "2.0", ID: c.seq.Add(1), Method: "tools/call", Params: map[string]any{"name": tool, "arguments": args}})
		if err != nil {
			return nil, err
		}
		if res.Error != nil {
			return nil, rpcRefused(res.Error)
		}
		return res.Result, nil
	}
	return finish(tool, args, call, all)
}

// List lists the allowed tools the endpoint has.
func (c *HTTP) List(ctx context.Context) ([]ToolInfo, error) {
	session, token, err := c.open(ctx)
	if err != nil {
		return nil, err
	}
	var out []ToolInfo
	cursor := ""
	for range maxListPages {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		res, _, err := c.post(ctx, session, token, rpcRequest{JSONRPC: "2.0", ID: c.seq.Add(1), Method: "tools/list", Params: params})
		if err != nil {
			return nil, err
		}
		if res.Error != nil {
			return nil, rpcRefused(res.Error)
		}
		if out, cursor, err = listResult(res.Result, c.Allowed, out); err != nil || cursor == "" {
			return out, err
		}
	}
	return out, nil
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
	return c.run(ctx, tool, args, false)
}

// CallAll is Call that reads the whole of a result the gateway cut short (see CallAll).
func (c *Stdio) CallAll(ctx context.Context, tool string, args map[string]any) (string, error) {
	return c.run(ctx, tool, args, true)
}

func (c *Stdio) run(ctx context.Context, tool string, args map[string]any, all bool) (string, error) {
	if err := c.Allowed.check(c.Module, tool); err != nil {
		return "", err
	}
	var text string
	err := c.session(ctx, func(request func(method string, params any) (json.RawMessage, error)) error {
		call := func(tool string, args map[string]any) (json.RawMessage, error) {
			return request("tools/call", map[string]any{"name": tool, "arguments": args})
		}
		var err error
		text, err = finish(tool, args, call, all)
		return err
	})
	return text, err
}

// List lists the allowed tools the command's server has.
func (c *Stdio) List(ctx context.Context) ([]ToolInfo, error) {
	var out []ToolInfo
	err := c.session(ctx, func(request func(method string, params any) (json.RawMessage, error)) error {
		cursor := ""
		for range maxListPages {
			params := map[string]any{}
			if cursor != "" {
				params["cursor"] = cursor
			}
			res, err := request("tools/list", params)
			if err != nil {
				return err
			}
			if out, cursor, err = listResult(res, c.Allowed, out); err != nil || cursor == "" {
				return err
			}
		}
		return nil
	})
	return out, err
}

// session starts the command, opens an MCP session on it, and gives use a way to make
// requests on it; the process ends when use returns.
func (c *Stdio) session(ctx context.Context, use func(request func(method string, params any) (json.RawMessage, error)) error) error {
	if len(c.Argv) == 0 {
		return errors.New("tether: no command")
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	pl := poolFor(c.Argv)
	p, err := pl.lease(ctx)
	if err != nil {
		return err
	}
	defer pl.release(p)
	return use(func(method string, params any) (json.RawMessage, error) {
		r, err := p.request(ctx, method, params, timeout)
		if err != nil {
			return nil, err
		}
		if r.Error != nil {
			return nil, rpcRefused(r.Error)
		}
		return r.Result, nil
	})
}

// ---- Kept sessions ----
//
// A stdio command is started once and kept: its MCP session is opened when it starts, and
// each call after that is one request on it, not a process, an ssh handshake and an MCP
// handshake. Commands are shared by every client with the same command line (modules make a
// client per call), up to maxProcs at once so calls in parallel do not wait on each other. A
// command that has been idle for idleFor is closed when next looked at; one that fails, or
// whose call ran out of time, is killed and not used again. A call is never sent twice: a
// command that died with a request in flight answers that call with an error.

const (
	// maxProcs bounds the commands kept for one command line.
	maxProcs = 4
	// idleFor is how long an unused command is kept.
	idleFor = 5 * time.Minute
)

var (
	poolsMu sync.Mutex
	pools   = map[string]*procPool{}
)

func poolFor(argv []string) *procPool {
	key := strings.Join(argv, "\x00")
	poolsMu.Lock()
	defer poolsMu.Unlock()
	pl := pools[key]
	if pl == nil {
		pl = &procPool{argv: append([]string(nil), argv...), slots: make(chan struct{}, maxProcs)}
		pools[key] = pl
	}
	return pl
}

type procPool struct {
	argv  []string
	slots chan struct{}
	mu    sync.Mutex
	idle  []*proc
	// started counts the commands this pool started (tests).
	started atomic.Int64
}

// lease takes an idle command, or starts one, holding one of the pool's slots until release.
func (pl *procPool) lease(ctx context.Context) (*proc, error) {
	select {
	case pl.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, errors.New("tether: every session to the command is busy")
	}
	pl.mu.Lock()
	for len(pl.idle) > 0 {
		p := pl.idle[len(pl.idle)-1]
		pl.idle = pl.idle[:len(pl.idle)-1]
		if p.usable() {
			pl.mu.Unlock()
			return p, nil
		}
		p.kill()
	}
	pl.mu.Unlock()
	p, err := pl.start(ctx)
	if err != nil {
		<-pl.slots
		return nil, err
	}
	return p, nil
}

// release puts a command back for the next call, or ends it when it can no longer be used.
func (pl *procPool) release(p *proc) {
	defer func() { <-pl.slots }()
	if !p.usable() {
		p.kill()
		return
	}
	p.used = time.Now()
	pl.mu.Lock()
	pl.idle = append(pl.idle, p)
	pl.mu.Unlock()
}

func (pl *procPool) start(ctx context.Context) (*proc, error) {
	cmd := exec.Command(pl.argv[0], pl.argv[1:]...) //nolint:gosec // the command is the owner's own setting
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	// The command's own output stays out of everything the module reports.
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		return nil, errors.New("tether: the command did not start")
	}
	pl.started.Add(1)
	p := &proc{cmd: cmd, stdin: stdin, enc: json.NewEncoder(stdin), lines: make(chan []byte, 16), used: time.Now()}
	go p.read(stdout)
	init, err := p.request(ctx, "initialize", initParams("surface"), 0)
	if err != nil {
		p.kill()
		return nil, err
	}
	if init.Error != nil {
		p.kill()
		return nil, errors.New("tether: the command refused to start a session")
	}
	if err = p.enc.Encode(rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"}); err != nil {
		p.kill()
		return nil, err
	}
	return p, nil
}

// proc is one running command with an open MCP session. One call uses it at a time.
type proc struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	enc   *json.Encoder
	lines chan []byte
	seq   int64
	used  time.Time
	dead  atomic.Bool
	ended sync.Once
}

// read hands the command's lines to the call waiting on them; at the end of its output the
// command is dead.
func (p *proc) read(stdout io.Reader) {
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	for sc.Scan() {
		p.lines <- append([]byte(nil), sc.Bytes()...)
	}
	p.dead.Store(true)
	close(p.lines)
}

func (p *proc) usable() bool { return !p.dead.Load() && time.Since(p.used) < idleFor }

func (p *proc) kill() {
	p.dead.Store(true)
	p.ended.Do(func() {
		_ = p.stdin.Close()
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		go func() { _ = p.cmd.Wait() }()
	})
}

// request sends one request and waits for its answer. When the time runs out the command is
// killed: its answer may still come, and the next call must not read it.
func (p *proc) request(ctx context.Context, method string, params any, timeout time.Duration) (*rpcResponse, error) {
	p.seq++
	id := p.seq
	if err := p.enc.Encode(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		p.dead.Store(true)
		return nil, errors.New("tether: the command is gone")
	}
	for {
		select {
		case line, ok := <-p.lines:
			if !ok {
				return nil, errors.New("tether: the command ended without answering")
			}
			var r rpcResponse
			if json.Unmarshal(line, &r) == nil && r.ID == id {
				return &r, nil
			}
		case <-ctx.Done():
			p.kill()
			if timeout > 0 {
				return nil, fmt.Errorf("tether: the command did not answer in %s", timeout)
			}
			return nil, errors.New("tether: the command did not answer")
		}
	}
}

// ---- Results the gateway cut short ----

// ResultNext is the gateway's tool that hands out the rest of a result it cut. Its cursor is
// good only in the session that got the first part, so CallAll asks for the rest in the same
// session. It reads only what an allowed call already returned, so it is not in Allowed.
const ResultNext = "tether_result_next"

// maxPages bounds how many continuations one CallAll reads.
const maxPages = 64

// AllCaller is a Caller that can read the whole of a result the gateway cut short.
type AllCaller interface {
	CallAll(ctx context.Context, tool string, args map[string]any) (string, error)
}

// CallAll calls a tool and, when the gateway cut its answer short (a long list), reads the
// rest in the same session and returns the tool's JSON whole. A Caller that cannot (a test's
// stand-in) is simply called.
func CallAll(ctx context.Context, c Caller, tool string, args map[string]any) (string, error) {
	if a, ok := c.(AllCaller); ok {
		return a.CallAll(ctx, tool, args)
	}
	return c.Call(ctx, tool, args)
}

// truncation is the gateway's note on a result it cut: which list (path) and where the rest
// starts.
type truncation struct {
	Path string `json:"path"`
	Next string `json:"next_cursor"`
}

// blocks reads a tools/call result into its text and the gateway's note, if any.
func blocks(raw json.RawMessage) (string, *truncation, error) {
	var r struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", nil, fmt.Errorf("tether: unreadable tool result: %w", err)
	}
	var out []string
	var note *truncation
	for _, c := range r.Content {
		if c.Type != "text" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(c.Text), `{"gateway_truncated"`) {
			var n struct {
				G *truncation `json:"gateway_truncated"`
			}
			if json.Unmarshal([]byte(c.Text), &n) == nil && n.G != nil {
				note = n.G
			}
			continue
		}
		out = append(out, c.Text)
	}
	if r.IsError {
		return "", nil, &RefusedError{Detail: strings.Join(out, "")}
	}
	return strings.Join(out, ""), note, nil
}

// finish makes the call and, for CallAll, reads the rest of a cut list into the first part.
func finish(tool string, args map[string]any, call func(string, map[string]any) (json.RawMessage, error), all bool) (string, error) {
	raw, err := call(tool, args)
	if err != nil {
		return "", err
	}
	text, note, err := blocks(raw)
	if err != nil || !all || note == nil || note.Path == "" || note.Next == "" {
		return text, err
	}
	var whole map[string]json.RawMessage
	if err = json.Unmarshal([]byte(text), &whole); err != nil {
		return "", fmt.Errorf("tether: %s: the cut result is unreadable", tool)
	}
	path := note.Path
	var list []json.RawMessage
	if err = json.Unmarshal(whole[path], &list); err != nil {
		return "", fmt.Errorf("tether: %s: the cut list is unreadable", tool)
	}
	for page := 0; note != nil && note.Next != "" && page < maxPages; page++ {
		raw, nerr := call(ResultNext, map[string]any{"cursor": note.Next})
		if nerr != nil {
			return "", fmt.Errorf("tether: %s: the rest of the result: %w", tool, nerr)
		}
		var more string
		if more, note, err = blocks(raw); err != nil {
			return "", err
		}
		var obj map[string]json.RawMessage
		var rest []json.RawMessage
		if json.Unmarshal([]byte(more), &obj) != nil || json.Unmarshal(obj[path], &rest) != nil {
			return "", fmt.Errorf("tether: %s: the rest of the result is unreadable", tool)
		}
		list = append(list, rest...)
	}
	whole[path], _ = json.Marshal(list)
	out, err := json.Marshal(whole)
	return string(out), err
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
