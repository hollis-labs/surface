package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hollis-labs/surface/module"
)

// mcp is a small Model Context Protocol endpoint (JSON-RPC over HTTP POST, no session):
// initialize, tools/list and tools/call. It is how an outside agent drives the app with
// the same tools the embedded one has.
//
// Read and act tools run at once. Write and confirm tools ask the owner first, with a card
// (mcpApprove): a write tool's "always" answer stands for that caller and tool until the app
// restarts; a confirm tool asks every time. The caller is the X-Surface-Agent header (a seat
// host sends its seat), or "an MCP client".
func (s *server) mcp(w http.ResponseWriter, r *http.Request) {
	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := read(r, &req); err != nil {
		write(w, http.StatusOK, rpcErr(nil, -32700, err.Error()))
		return
	}
	// A notification gets no reply.
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	switch req.Method {
	case "initialize":
		ok(w, rpcOK(req.ID, map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "surface-" + s.a.AppName(), "title": s.a.Title(), "version": "0.1.0"},
			"instructions":    "This is a Surface app: a desk of windows a person and agents share. Use desk_windows and window_context to see where the person is, desk_open and desk_arrange to arrange the desk, and each module's tools to work with its data.",
		}))
	case "ping":
		ok(w, rpcOK(req.ID, map[string]any{}))
	case "tools/list":
		type tool struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
			Annotations map[string]any  `json:"annotations,omitempty"`
		}
		tools := []tool{}
		for _, t := range s.a.Tools() {
			tools = append(tools, tool{Name: t.Name, Description: t.Description, InputSchema: t.Input,
				Annotations: map[string]any{"readOnlyHint": t.Level == "read", "destructiveHint": t.Level == "confirm"}})
		}
		ok(w, rpcOK(req.ID, map[string]any{"tools": tools}))
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			ok(w, rpcErr(req.ID, -32602, err.Error()))
			return
		}
		out, err := s.callMCP(r.Context(), mcpCaller(r), p.Name, p.Arguments)
		if err != nil {
			ok(w, rpcOK(req.ID, map[string]any{"isError": true, "content": []map[string]string{{"type": "text", "text": err.Error()}}}))
			return
		}
		ok(w, rpcOK(req.ID, map[string]any{"content": []map[string]string{{"type": "text", "text": out}}}))
	default:
		ok(w, rpcErr(req.ID, -32601, "method not found: "+req.Method))
	}
}

func rpcOK(id json.RawMessage, result any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
}

func rpcErr(id json.RawMessage, code int, msg string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}}
}

// mcpCaller names who is calling, for the approval card and standing grants.
func mcpCaller(r *http.Request) string {
	c := strings.TrimSpace(r.Header.Get("X-Surface-Agent"))
	if c == "" || len(c) > 120 {
		return "an MCP client"
	}
	return c
}

// mcpGrants are the standing "always" answers: caller -> tool. In memory: they last until the
// app restarts.
var (
	mcpGrantsMu sync.Mutex
	mcpGrants   = map[string]map[string]bool{}
)

// mcpApproveTimeout is how long a call waits for the owner's answer.
var mcpApproveTimeout = 10 * time.Minute

// errDenied is a call the owner declined, or did not answer in time.
var errDenied = errors.New("the owner did not approve this call")

func (s *server) callMCP(ctx context.Context, caller, name string, input json.RawMessage) (string, error) {
	for _, t := range s.a.Tools() {
		if t.Name != name {
			continue
		}
		if t.Level != module.LevelRead && t.Level != module.LevelAct {
			if err := s.mcpApprove(ctx, caller, t.Tool, input); err != nil {
				return "", err
			}
		}
		out, err := t.Call(ctx, input)
		if err != nil {
			return "", err
		}
		return Render(out), nil
	}
	return "", fmt.Errorf("no tool %q in this app", name)
}

// mcpApprove asks the owner before a write or confirm tool runs for an MCP caller, and waits.
func (s *server) mcpApprove(ctx context.Context, caller string, t module.Tool, input json.RawMessage) error {
	confirm := t.Level == module.LevelConfirm
	if !confirm {
		mcpGrantsMu.Lock()
		granted := mcpGrants[caller][t.Name]
		mcpGrantsMu.Unlock()
		if granted {
			return nil
		}
	}
	what := ""
	if t.Describe != nil {
		what = strings.TrimSpace(t.Describe(input))
	}
	if what == "" {
		what = t.Name
	}
	title := fmt.Sprintf("%s wants to: %s", caller, what)
	var c module.Card
	if confirm {
		payload, _ := json.Marshal(map[string]any{
			"summary":      fmt.Sprintf("%s asks to run %s. This kind of action asks every time.", caller, t.Name),
			"requested_by": caller,
			"changes":      []map[string]any{{"id": "call", "label": t.Name, "detail": string(input)}},
		})
		c = module.Card{Kind: "approval", Title: title, Source: "agent", Subject: "mcp:" + caller + ":" + t.Name,
			Risk: "high", Blocking: true, Payload: payload}
	} else {
		payload, _ := json.Marshal(map[string]any{
			"question": title,
			"body":     fmt.Sprintf("Tool %s. Input: %s", t.Name, truncate(string(input), 600)),
			"options": []map[string]string{
				{"id": "once", "label": "Allow once"},
				{"id": "always", "label": fmt.Sprintf("Always allow %s for %s", t.Name, caller), "description": "Until the app restarts."},
				{"id": "deny", "label": "Deny"},
			},
			"allow_other": false,
		})
		c = module.Card{Kind: "choice", Title: "Allow " + t.Name + "?", Source: "agent", Subject: "mcp:" + caller + ":" + t.Name,
			Risk: "medium", Blocking: true, Payload: payload}
	}
	card, err := s.a.Cards().Show(ctx, c)
	if err != nil {
		return fmt.Errorf("could not ask the owner: %w", err)
	}
	wctx, cancel := context.WithTimeout(ctx, mcpApproveTimeout)
	defer cancel()
	done, err := s.a.Cards().Await(wctx, card.ID)
	if err != nil {
		_, _ = s.a.Cards().End(context.Background(), card.ID, "expired")
		return fmt.Errorf("%w (no answer within %s)", errDenied, mcpApproveTimeout)
	}
	if done.Outcome == nil || done.Status != "submitted" {
		return errDenied
	}
	if confirm {
		for _, d := range done.Outcome.Decisions {
			if d.Action == "approve" {
				return nil
			}
		}
		return errDenied
	}
	for _, a := range done.Outcome.Answers {
		switch fmt.Sprint(a.Value) {
		case "once":
			return nil
		case "always":
			mcpGrantsMu.Lock()
			if mcpGrants[caller] == nil {
				mcpGrants[caller] = map[string]bool{}
			}
			mcpGrants[caller][t.Name] = true
			mcpGrantsMu.Unlock()
			return nil
		}
	}
	return errDenied
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
