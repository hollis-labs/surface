package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hollis-labs/libs/agent/chat"
	"github.com/hollis-labs/surface/core/internal/agent/profile"
)

func TestProvidersPathDefaultsUnderTheDataDir(t *testing.T) {
	if got := (FileConfig{}).ProvidersPath("/data"); got != "/data/agent/providers.yaml" {
		t.Fatalf("got %q", got)
	}
	if got := (FileConfig{ProvidersFile: "/x/p.yaml"}).ProvidersPath("/data"); got != "/x/p.yaml" {
		t.Fatalf("got %q", got)
	}
}

func TestAppProvidersChecksTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.yaml")
	if raw, chat, err := appProviders(path); raw != nil || chat || err != nil {
		t.Fatalf("missing file: %v %v %v", raw, chat, err)
	}
	write := func(s string) {
		if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("providers:\n  local:\n    wire: openai-chat\n    base_url: http://127.0.0.1:1/v1\n    auth: none\n")
	if _, chat, err := appProviders(path); chat || err != nil {
		t.Fatalf("no use.chat: %v %v", chat, err)
	}
	write("providers:\n  local:\n    wire: openai-chat\n    base_url: http://127.0.0.1:1/v1\n    auth: none\nuse:\n  chat: {provider: local, model: m}\n")
	if _, chat, err := appProviders(path); !chat || err != nil {
		t.Fatalf("use.chat: %v %v", chat, err)
	}
	write("providers:\n  anthropic:\n    wire: anthropic\n    base_url: http://x\n")
	if _, _, err := appProviders(path); err == nil {
		t.Fatal("redefining a built-in should not check out")
	}
}

func TestStageWithLaysAppFilesOverTheSeat(t *testing.T) {
	seat, stage := t.TempDir(), t.TempDir()
	for name, body := range map[string]string{"agent.md": "---\nname: a\n---\nhi\n", "providers.yaml": "seat's own"} {
		if err := os.WriteFile(filepath.Join(seat, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := profile.StageWith(stage, seat, nil, map[string][]byte{"providers.yaml": []byte("app's")}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(stage, "providers.yaml")); string(b) != "app's" {
		t.Fatalf("got %q", b)
	}
	// without app files the seat's comes back
	if err := profile.StageWith(stage, seat, nil, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(stage, "providers.yaml")); string(b) != "seat's own" {
		t.Fatalf("got %q", b)
	}
}

// fakeOpenAI is an OpenAI-compatible Chat Completions server that answers every request
// with reply, streamed or not as asked, and records what it was sent.
type fakeOpenAI struct {
	reply string
	mu    sync.Mutex
	auth  []string
	model []string
}

func (f *fakeOpenAI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
		http.NotFound(w, r)
		return
	}
	var req struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	body, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(body, &req)
	f.mu.Lock()
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	f.model = append(f.model, req.Model)
	f.mu.Unlock()
	if !req.Stream {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"c1","object":"chat.completion","created":1,"model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`, req.Model, f.reply)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	chunk := func(s string) { fmt.Fprintf(w, "data: %s\n\n", s) }
	chunk(fmt.Sprintf(`{"id":"c1","object":"chat.completion.chunk","created":1,"model":%q,"choices":[{"index":0,"delta":{"role":"assistant","content":%q},"finish_reason":null}]}`, req.Model, f.reply))
	chunk(fmt.Sprintf(`{"id":"c1","object":"chat.completion.chunk","created":1,"model":%q,"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`, req.Model))
	chunk("[DONE]")
}

// TestNaniteAnswersThroughTheAppsProvidersFile opens the Nanite runner with no gateway in the
// agent config, only the app's providers file naming an OpenAI-compatible server, and runs a
// turn through it.
func TestNaniteAnswersThroughTheAppsProvidersFile(t *testing.T) {
	s, k, _, _ := setup(t, Options{})
	fake := &fakeOpenAI{reply: "Hello from the local model."}
	llm := httptest.NewServer(fake)
	defer llm.Close()

	keyDir, err := filepath.EvalSymlinks(t.TempDir()) // the resolver refuses a path through a symlink
	if err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(keyDir, "local.key")
	if err := os.WriteFile(keyFile, []byte("sk-test-123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(k.dataDir, "agent"), 0o700); err != nil {
		t.Fatal(err)
	}
	providers := fmt.Sprintf("providers:\n  local:\n    wire: openai-chat\n    base_url: %s/v1\n    key_ref: file://%s\nuse:\n  chat: {provider: local, model: tiny-model}\n", llm.URL, keyFile)
	if err := os.WriteFile(filepath.Join(k.dataDir, "agent", "providers.yaml"), []byte(providers), 0o600); err != nil {
		t.Fatal(err)
	}

	s.wire.close(context.Background())
	s.cfg = FileConfig{Path: filepath.Join(t.TempDir(), "agent.json")} // no model, gateway or key
	s.wire, err = s.openWire(context.Background(), RunnerNanite)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Mount(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	view, err := s.wire.svc.EnsureConversation(t.Context(), s.wire.agent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.wire.svc.Submit(t.Context(), view.ConversationID, chat.Turn{Text: "Say hello."}); err != nil {
		t.Fatal(err)
	}
	raw, err := http.Post(srv.URL+"/api/agent/conversations/"+view.ConversationID+"/attachments", "application/json", strings.NewReader(`{"role":"viewer","stream":"committed"}`))
	if err != nil {
		t.Fatal(err)
	}
	var attachment struct {
		ID string `json:"attachment_id"`
	}
	_ = json.NewDecoder(raw.Body).Decode(&attachment)
	_ = raw.Body.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/agent/conversations/"+view.ConversationID+"/events?attachment="+attachment.ID+"&after=0", nil)
	ev, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer ev.Body.Close()
	var text strings.Builder
	end := ""
	sc := bufio.NewScanner(ev.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() && end == "" {
		line, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok {
			continue
		}
		var e struct {
			Verb, Text, Message, Reason string
			Final                       struct{ Text string }
		}
		_ = json.Unmarshal([]byte(line), &e)
		switch e.Verb {
		case "part.delta":
			text.WriteString(e.Text)
		case "part.end":
			text.WriteString(e.Final.Text)
		case "run.finish", "run.error", "run.abort":
			end = e.Verb + " " + e.Message + e.Reason
		}
	}
	if !strings.HasPrefix(end, "run.finish") {
		t.Fatalf("run ended %q (answer %q)", end, text.String())
	}
	if !strings.Contains(text.String(), "Hello from the local model.") {
		t.Fatalf("answer %q", text.String())
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.auth) == 0 || fake.auth[0] != "Bearer sk-test-123" || fake.model[0] != "tiny-model" {
		t.Fatalf("auth %v model %v", fake.auth, fake.model)
	}
}
