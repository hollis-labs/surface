package core

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTetherAttachmentForwardsWireAndFreshContext(t *testing.T) {
	type request struct {
		method, path, query, cursor, auth, origin string
		body                                      map[string]json.RawMessage
	}
	seen := make(chan request, 10)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		seen <- request{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Last-Event-ID"), r.Header.Get("Authorization"), r.Header.Get("Origin"), body}
		if strings.HasSuffix(r.URL.Path, "/events") {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "id: 12\ndata: {\"seq\":12,\"verb\":\"part.delta\",\"delta\":{\"text\":\"hello\"}}\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	}))
	defer upstream.Close()
	key := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(key, []byte("test-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SURFACE_TETHER_URL", upstream.URL+"/chat/v1")
	t.Setenv("SURFACE_TETHER_SEAT", "surface-seat")
	t.Setenv("SURFACE_TETHER_KEY_FILE", key)
	// Tether takes precedence without constructing an embedded runner.
	t.Setenv("SURFACE_AGENT_RUNNER", "nanite")
	srv := boot(t, "scratch")
	var setup struct {
		Ready       bool
		Wire, Agent string
		Base        string `json:"wire_base"`
	}
	do(t, srv, "GET", "/api/agent/setup", "", &setup)
	if !setup.Ready || setup.Agent != "surface-seat" || setup.Wire != "conversation" || setup.Base != "/api/agent/tether" {
		t.Fatalf("setup: %+v", setup)
	}
	do(t, srv, "POST", setup.Base+"/agents/surface-seat/conversation", `{}`, nil)
	first := <-seen
	if first.path != "/chat/v1/agents/surface-seat/conversation" || first.auth != "Bearer test-token" {
		t.Fatalf("open: %+v", first)
	}
	if code := do(t, srv, "POST", setup.Base+"/agents/other/conversation", `{}`, nil); code != 404 {
		t.Fatalf("other seat: %d", code)
	}
	do(t, srv, "POST", "/api/desk/actions", `{"type":"open","view":"scratch"}`, nil)
	do(t, srv, "POST", setup.Base+"/conversations/c1/turns", `{"attachment":"a1","text":"look here","request_id":"r1","context":"untrusted stale context"}`, nil)
	turn := <-seen
	var block string
	if err := json.Unmarshal(turn.body["context"], &block); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(block, "scratch") || strings.Contains(block, "untrusted stale context") {
		t.Fatalf("context: %s", block)
	}
	if string(turn.body["text"]) != `"look here"` || string(turn.body["request_id"]) != `"r1"` {
		t.Fatalf("turn fields: %v", turn.body)
	}
	req, _ := http.NewRequest("GET", srv.URL+setup.Base+"/conversations/c1/events?attachment=a2&after=11", nil)
	req.Header.Set("Last-Event-ID", "11")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || !strings.Contains(string(raw), "part.delta") || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream: %s, %v", raw, err)
	}
	stream := <-seen
	if stream.cursor != "11" || stream.query != "attachment=a2&after=11" {
		t.Fatalf("resume: %+v", stream)
	}
	do(t, srv, "DELETE", setup.Base+"/conversations/c1/attachments/a1", "", nil)
	if detach := <-seen; detach.method != "DELETE" || !strings.HasSuffix(detach.path, "/attachments/a1") {
		t.Fatalf("detach: %+v", detach)
	}
	do(t, srv, "POST", setup.Base+"/conversations/c1/attachments", `{"role":"controller","stream":"deltas"}`, nil)
	<-seen
	if err := os.WriteFile(key, []byte("rotated-token"), 0600); err != nil {
		t.Fatal(err)
	}
	do(t, srv, "GET", setup.Base+"/conversations/c1", "", nil)
	if rotated := <-seen; rotated.auth != "Bearer rotated-token" {
		t.Fatal("credential was not reread")
	}
}

func TestMCPOnlyListsAndCallsReadAndAct(t *testing.T) {
	srv := boot(t, "scratch")
	var list struct {
		Result struct{ Tools []struct{ Name string } }
	}
	do(t, srv, "POST", "/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, &list)
	names := map[string]bool{}
	for _, tool := range list.Result.Tools {
		names[tool.Name] = true
	}
	if !names["scratch_list"] || !names["desk_open"] {
		t.Fatalf("read/act missing: %v", names)
	}
	for _, name := range []string{"scratch_add", "scratch_update", "scratch_delete", "agent_reflex_put", "agent_reflex_delete"} {
		if names[name] {
			t.Errorf("restricted tool listed: %s", name)
		}
		var result struct{ Result struct{ IsError bool } }
		do(t, srv, "POST", "/mcp", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"`+name+`","arguments":{}}}`, &result)
		if !result.Result.IsError {
			t.Errorf("restricted call accepted: %s", name)
		}
	}
}
