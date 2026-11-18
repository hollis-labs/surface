package api

import (
	"encoding/json"
	"net/http"
)

// mcp is a small Model Context Protocol endpoint (JSON-RPC over HTTP POST, no session):
// initialize, tools/list and tools/call. It is how an outside agent drives the app with
// the same tools the embedded one has.
//
// Tool levels are not enforced here: asking the owner is the embedded agent's panel's job
// (and later Gatecrasher's). The app listens on loopback only.
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
		out, err := s.call(r.Context(), p.Name, p.Arguments)
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
