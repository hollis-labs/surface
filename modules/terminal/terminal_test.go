package terminal

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/hollis-labs/surface/module/moduletest"
)

// A shell over the socket: it starts at the size sent first, runs what is typed, and a second
// attach gets the replay of what the first one saw.
func TestShellOverTheSocket(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	m := &mod{}
	h := moduletest.Start(t, m)
	srv := httptest.NewServer(h)
	defer srv.Close()

	res, err := http.Post(srv.URL+"/api/m/terminal/sessions", "application/json", strings.NewReader(`{"title":"test"}`))
	if err != nil || res.StatusCode != 201 {
		t.Fatalf("create: %v %v", res.Status, err)
	}
	var info struct{ ID string }
	_ = json.NewDecoder(res.Body).Decode(&info)
	_ = res.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/m/terminal/sessions/" + info.ID + "/ws"
	read := func(c *websocket.Conn, want string) string {
		t.Helper()
		var got bytes.Buffer
		for !strings.Contains(got.String(), want) {
			typ, data, rerr := c.Read(ctx)
			if rerr != nil {
				t.Fatalf("waiting for %q, got %q: %v", want, got.String(), rerr)
			}
			if typ == websocket.MessageBinary {
				got.Write(data)
			}
		}
		return got.String()
	}

	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":90,"rows":20}`))
	_ = c.Write(ctx, websocket.MessageBinary, []byte("echo surface-$((40+2))\n"))
	read(c, "surface-42")
	_ = c.Close(websocket.StatusNormalClosure, "")

	// Another window: the replay comes first, the shell is the same one.
	c2, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = c2.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":90,"rows":20}`))
	read(c2, "surface-42")
	_ = c2.Close(websocket.StatusNormalClosure, "")

	if code, body := h.Do("GET", "/sessions", ""); code != 200 || !strings.Contains(body, info.ID) {
		t.Fatalf("list: %d %s", code, body)
	}
	if code, _ := h.Do("DELETE", "/sessions/"+info.ID, ""); code != 204 {
		t.Fatalf("close: %d", code)
	}
}

// A page on another site cannot open a shell.
func TestCrossOriginRefused(t *testing.T) {
	m := &mod{}
	h := moduletest.Start(t, m)
	srv := httptest.NewServer(h)
	defer srv.Close()
	req, _ := http.NewRequest("GET", srv.URL+"/api/m/terminal/sessions/x/ws", nil)
	req.Header.Set("Origin", "https://evil.example")
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 403 {
		t.Fatalf("cross-origin: %v %v", res.Status, err)
	}
}
