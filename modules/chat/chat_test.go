package chat

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hollis-labs/surface/module/moduletest"
)

func TestProxyAddsTokenAndStreams(t *testing.T) {
	var gotAuth, gotPath, gotQuery, gotCookie string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath, gotQuery, gotCookie = r.Header.Get("Authorization"), r.URL.Path, r.URL.RawQuery, r.Header.Get("Cookie")
		if strings.HasSuffix(r.URL.Path, "/events") {
			w.Header().Set("Content-Type", "text/event-stream")
			w.(http.Flusher).Flush()
			_, _ = w.Write([]byte("id: 1\ndata: {\"verb\":\"run.start\"}\n\n"))
			w.(http.Flusher).Flush()
			return
		}
		_, _ = w.Write([]byte(`{"host":"x","seats":[]}`))
	}))
	defer upstream.Close()
	tok := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tok, []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := moduletest.Start(t, &mod{})
	ctx := t.Context()
	_ = h.Settings.Set(ctx, settingURL, upstream.URL)
	_ = h.Settings.Set(ctx, settingKeyFile, tok)

	req := httptest.NewRequest("GET", "/api/m/chat/h/seats", nil)
	req.Header.Set("Cookie", "session=abc")
	req.Header.Set("Authorization", "Bearer browser")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || gotPath != "/seats" || gotAuth != "Bearer s3cret" || gotCookie != "" {
		t.Fatalf("code=%d path=%q auth=%q cookie=%q body=%s", rec.Code, gotPath, gotAuth, gotCookie, rec.Body)
	}

	srv := httptest.NewServer(h)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/m/chat/h/chat/v1/conversations/c1/events?attachment=a1&after=0")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	line, _ := bufio.NewReader(resp.Body).ReadString('\n')
	if gotPath != "/chat/v1/conversations/c1/events" || gotQuery != "attachment=a1&after=0" || !strings.HasPrefix(line, "id: 1") {
		t.Fatalf("path=%q query=%q first line=%q", gotPath, gotQuery, line)
	}

	if code, _ := h.Do("GET", "/h/../etc", ""); code < 300 || (code >= 400 && code != http.StatusBadRequest) {
		t.Errorf("traversal reached the proxy: %d", code) // the mux cleans it (307) or the proxy refuses (400)
	}
}

func TestStatusUnreachable(t *testing.T) {
	h := moduletest.Start(t, &mod{})
	_ = h.Settings.Set(t.Context(), settingURL, "http://127.0.0.1:1")
	_ = h.Settings.Set(t.Context(), settingKeyFile, "")
	code, body := h.Do("GET", "/status", "")
	if code != 200 || !strings.Contains(body, `"reachable":false`) {
		t.Fatalf("%d %s", code, body)
	}
}
