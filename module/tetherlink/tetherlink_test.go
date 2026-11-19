package tetherlink

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A result the gateway cut short is two text blocks; the note is set aside and the tool's JSON
// is what is returned.
func TestToolTextSetsTheGatewayNoteAside(t *testing.T) {
	raw := json.RawMessage(`{"content":[{"type":"text","text":"{\"messages\":[{\"id\":\"m1\"}]}"},{"type":"text","text":"{\"gateway_truncated\":{\"truncated\":true,\"next_cursor\":\"c.15\"}}"}]}`)
	text, err := ToolText(raw)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Messages []struct{ ID string } `json:"messages"`
	}
	if err := json.Unmarshal([]byte(text), &v); err != nil || len(v.Messages) != 1 {
		t.Fatalf("%q: %v", text, err)
	}
	if _, err := ToolText(json.RawMessage(`{"isError":true,"content":[{"type":"text","text":"secret detail"}]}`)); !errors.Is(err, ErrRefused) {
		t.Fatalf("a refusal: %v", err)
	}
}

// CallAll reads the rest of a list the gateway cut, in the session that got the first part.
func TestCallAllReadsTheRestInTheSameSession(t *testing.T) {
	var sessions []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		text := func(blocks ...string) {
			content := []map[string]string{}
			for _, b := range blocks {
				content = append(content, map[string]string{"type": "text", "text": b})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"content": content}})
		}
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "s1")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{}})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/call":
			sessions = append(sessions, r.Header.Get("Mcp-Session-Id"))
			switch req.Params.Name {
			case "caster_ls":
				text(`{"items":[{"ref":"a"},{"ref":"b"}]}`, `{"gateway_truncated":{"truncated":true,"path":"items","next_cursor":"c.2"}}`)
			case ResultNext:
				if req.Params.Arguments["cursor"] == "c.2" {
					text(`{"items":[{"ref":"c"}]}`, `{"gateway_truncated":{"truncated":true,"path":"items","next_cursor":"c.3"}}`)
				} else {
					text(`{"items":[{"ref":"d"}]}`)
				}
			}
		}
	}))
	defer srv.Close()
	c := &HTTP{Module: "test", URL: srv.URL, Client: srv.Client(), Allowed: Allowed{"caster_ls": true}}
	out, err := CallAll(context.Background(), c, "caster_ls", map[string]any{"ns": "x"})
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Items []struct{ Ref string } `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil || len(v.Items) != 4 || v.Items[3].Ref != "d" {
		t.Fatalf("%s: %v", out, err)
	}
	for _, s := range sessions {
		if s != "s1" {
			t.Fatalf("a continuation left the session: %v", sessions)
		}
	}
	// Call alone keeps the first part.
	out, _ = c.Call(context.Background(), "caster_ls", nil)
	if strings.Count(out, "ref") != 2 {
		t.Fatalf("Call: %s", out)
	}
}
