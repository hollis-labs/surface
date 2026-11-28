package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hollis-labs/surface/module"
)

// mcp is a small Model Context Protocol endpoint (JSON-RPC over HTTP POST, no session):
// initialize, tools/list and tools/call. It is how an outside agent drives the app with
// the same tools the embedded one has.
//
// Read and act tools run at once for every client. A client that names itself (the
// X-Surface-Agent header; a seat host sends its seat) also gets write and confirm tools, which
// ask the owner first with a card (mcpApprove): a write tool's "always" answer stands for that
// caller and tool until revoked (mcp_grants); a confirm tool asks every time. An anonymous client
// sees and calls read and act tools only.
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
		caller := mcpCaller(r)
		for _, t := range s.a.Tools() {
			if caller == "" && t.Level != module.LevelRead && t.Level != module.LevelAct {
				continue
			}
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
		ctx := r.Context()
		if c := mcpCaller(r); c != "" {
			ctx = module.WithAgent(ctx, module.AgentRef{Name: c, ReplyTo: strings.TrimSpace(r.Header.Get("X-Surface-Reply-To"))})
		}
		out, err := s.callMCP(ctx, mcpCaller(r), p.Name, p.Arguments)
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

// mcpCaller names who is calling, for the approval card and standing grants; empty for an
// anonymous client.
func mcpCaller(r *http.Request) string {
	c := strings.TrimSpace(r.Header.Get("X-Surface-Agent"))
	if len(c) > 120 {
		return ""
	}
	return c
}

// The standing "always" answers live in the core database (mcp_grants) and last until revoked.

func (s *server) mcpGranted(ctx context.Context, caller, tool string) bool {
	var one int
	err := s.a.DB().QueryRowContext(ctx, `SELECT 1 FROM mcp_grants WHERE caller = ? AND tool = ?`, caller, tool).Scan(&one)
	return err == nil
}

func (s *server) mcpGrant(ctx context.Context, caller, tool string) error {
	_, err := s.a.DB().ExecContext(ctx, `INSERT OR IGNORE INTO mcp_grants(caller, tool, granted_at) VALUES (?, ?, ?)`,
		caller, tool, time.Now().UTC().Format(time.RFC3339))
	return err
}

// mcpGrants lists the standing answers (GET /api/mcp/grants).
func (s *server) mcpGrants(w http.ResponseWriter, r *http.Request) {
	rows, err := s.a.DB().QueryContext(r.Context(), `SELECT caller, tool, granted_at FROM mcp_grants ORDER BY caller, tool`)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	defer func() { _ = rows.Close() }()
	out := []map[string]string{}
	for rows.Next() {
		var c, t, at string
		if rows.Scan(&c, &t, &at) == nil {
			out = append(out, map[string]string{"caller": c, "tool": t, "granted_at": at})
		}
	}
	ok(w, out)
}

// mcpRevoke drops one standing answer (DELETE /api/mcp/grants?caller=&tool=).
func (s *server) mcpRevoke(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if _, err := s.a.DB().ExecContext(r.Context(), `DELETE FROM mcp_grants WHERE caller = ? AND tool = ?`, q.Get("caller"), q.Get("tool")); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

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
			if caller == "" {
				return "", fmt.Errorf("tool %q is not available to an anonymous MCP client (read and act only; name yourself with X-Surface-Agent to ask the owner)", name)
			}
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
		if s.mcpGranted(ctx, caller, t.Name) {
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
		c = module.Card{Kind: "approval", Title: title, Source: "agent", Agent: caller, Subject: "mcp:" + caller + ":" + t.Name,
			Risk: "high", Blocking: true, Payload: payload}
	} else {
		payload, _ := json.Marshal(map[string]any{
			"question": title,
			"body":     fmt.Sprintf("Tool %s. Input: %s", t.Name, truncate(string(input), 600)),
			"options": []map[string]string{
				{"id": "once", "label": "Allow once"},
				{"id": "always", "label": fmt.Sprintf("Always allow %s for %s", t.Name, caller), "description": "Until you revoke it (Approvals → Always allowed)."},
				{"id": "deny", "label": "Deny"},
			},
			"allow_other": false,
		})
		c = module.Card{Kind: "choice", Title: "Allow " + t.Name + "?", Source: "agent", Agent: caller, Subject: "mcp:" + caller + ":" + t.Name,
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
			if err := s.mcpGrant(context.WithoutCancel(ctx), caller, t.Name); err != nil {
				return fmt.Errorf("allowed once; the standing answer was not kept: %w", err)
			}
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
