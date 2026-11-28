// Package chat is the agent chat module: chat with the seats a seat host runs
// (hollis-skunkworks/seat-host). A seat host serves each seat's sessions on the
// agent/chat conversation wire, the same wire packages/chat speaks, plus a
// small seat and lifecycle API. This module proxies both, adding the host's
// bearer token server-side, so the browser never holds it.
//
// Routes (under /api/m/chat):
//
//	GET  /status        whether the host is configured and answers
//	*    /h/...         proxied to the seat host: /h/seats, /h/sessions/..., /h/chat/v1/...
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/hollis-labs/surface/module"
)

func init() { module.Register(&mod{}) }

const (
	viewID          = "chat"
	defaultURL      = "http://127.0.0.1:8790"
	defaultKeyFile  = "/srv/data/agent-system/secrets/seat-host-token"
	settingURL      = "url"
	settingKeyFile  = "key_file"
	maxSeatsBody    = 4 << 20
	statusTimeout   = 3 * time.Second
	proxyHeaderAuth = "Authorization"
)

type mod struct {
	module.Base
	h module.Host
}

func (m *mod) Manifest() module.Manifest {
	return module.Manifest{
		ID:      "chat",
		Title:   "Agent chat",
		Version: "0.1.0",
		About:   "Chat with agent seats run by a seat host: continuity seats and concurrent seats with nested sessions, with state, restart and logs.",
		Views: []module.View{{
			ID: viewID, Title: "Agent chat", Icon: "messages-square", Width: 1100, Height: 760,
			Menu: true, Bare: true,
			Render:   module.Component("chat/main"),
			Describe: m.describeWindow,
		}},
		Commands: []module.Command{{
			ID: "chat.open", Title: "Agent chat: open", Section: "Agents",
			Keywords: "chat agents seats sessions agent designer skill designer acp",
			Run: func(ctx context.Context, h module.Host, args map[string]any) (any, error) {
				return h.Desk().Open(ctx, viewID, module.OpenOptions{Args: args})
			},
		}},
		Settings: []module.Setting{
			{Key: settingURL, Title: "Seat host URL", About: "The seat host's base URL (http on loopback, or https).",
				Type: module.SettingString, Default: defaultURL, Owner: true},
			{Key: settingKeyFile, Title: "Seat host token file", About: "File holding the seat host's bearer token. Never sent to the browser.",
				Type: module.SettingString, Default: defaultKeyFile, Owner: true},
			{Key: "history_messages", Title: "Messages to load", About: "How many recent messages a chat draws when opened (a turn is your message and the reply); older ones load as you scroll up.",
				Type: module.SettingInt, Default: 25},
			{Key: "recent_minutes", Title: "Recently active (minutes)", About: "A seat that answered within this many minutes sits at the top of the roster, most recent first.",
				Type: module.SettingInt, Default: 15},
			{Key: "pinned_seats", Title: "Pinned seats", About: "Seat ids pinned to the top of the roster, in order, comma separated. Pin and drag in the roster to change it.",
				Type: module.SettingString, Default: ""},
			{Key: "seat_names", Title: "Seat names", About: "Display names for seats, by seat id (only how they show here; the seat's definition is unchanged). Rename from a seat's menu.",
				Type: module.SettingObject, Default: map[string]any{}},
			{Key: "hidden_seats", Title: "Hidden seats", About: "Seat ids left out of the roster, comma separated. Hide and show from a seat's menu.",
				Type: module.SettingString, Default: ""},
			{Key: "approvals_auto_open", Title: "Open approvals automatically", About: "Open the approvals queue when an agent asks for a tool approval.",
				Type: module.SettingBool, Default: true},
			{Key: "approvals_snooze_minutes", Title: "Approvals snooze (minutes)", About: "How long Snooze keeps the approvals queue closed.",
				Type: module.SettingInt, Default: 10},
		},
	}
}

func (m *mod) Start(_ context.Context, h module.Host) error {
	m.h = h
	h.Handle("GET /status", http.HandlerFunc(m.status))
	h.Handle("/h/", http.HandlerFunc(m.proxy))
	return nil
}

func (m *mod) describeWindow(_ context.Context, _ module.Host, w module.Window) (module.WindowContext, error) {
	sid, _ := w.Args["session"].(string)
	wc := module.WindowContext{Kind: "chat"}
	if sid != "" {
		wc.Ref = "chat:" + sid
	}
	return wc, nil
}

var errNotConfigured = errors.New("chat: the seat host URL is not set")

// target is the seat host's base URL and token, read at each call so a
// settings change takes effect at once.
func (m *mod) target() (*url.URL, string, error) {
	cfg := m.h.Config()
	raw := strings.TrimSpace(cfg.String(settingURL))
	if raw == "" {
		return nil, "", errNotConfigured
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, "", fmt.Errorf("chat: bad seat host URL %q", raw)
	}
	token := ""
	if f := strings.TrimSpace(cfg.String(settingKeyFile)); f != "" {
		b, err := os.ReadFile(f) //nolint:gosec // owner-configured token file path
		if err != nil {
			return nil, "", fmt.Errorf("chat: read the token file: %w", err)
		}
		token = strings.TrimSpace(string(b))
	}
	return u, token, nil
}

func (m *mod) status(w http.ResponseWriter, r *http.Request) {
	u, _, err := m.target()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false, "error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), statusTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(u.String(), "/")+"/health", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"configured": true, "reachable": false, "url": u.String(), "error": err.Error()})
		return
	}
	defer func() { _ = resp.Body.Close() }()
	var health map[string]any
	_ = json.NewDecoder(http.MaxBytesReader(w, resp.Body, maxSeatsBody)).Decode(&health)
	writeJSON(w, http.StatusOK, map[string]any{"configured": true, "reachable": resp.StatusCode == http.StatusOK, "url": u.String(), "health": health})
}

// proxy forwards /h/<path> to the seat host's /<path>. Event streams are
// flushed as they arrive.
func (m *mod) proxy(w http.ResponseWriter, r *http.Request) {
	u, token, err := m.target()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"code": "not_configured", "message": err.Error()})
		return
	}
	// The kernel strips /api/m/chat; a test host may not.
	rest := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/api/m/chat"), "/h")
	if rest == "" || !strings.HasPrefix(rest, "/") || strings.Contains(rest, "..") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"code": "bad_path", "message": "bad path"})
		return
	}
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(u)
			pr.Out.URL.Path = strings.TrimRight(u.Path, "/") + rest
			pr.Out.URL.RawPath = ""
			pr.Out.URL.RawQuery = r.URL.RawQuery
			pr.Out.Host = u.Host
			pr.Out.Header.Del("Cookie")
			pr.Out.Header.Del(proxyHeaderAuth)
			if token != "" {
				pr.Out.Header.Set(proxyHeaderAuth, "Bearer "+token)
			}
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeJSON(w, http.StatusBadGateway, map[string]any{"code": "seat_host_unreachable", "message": err.Error()})
		},
	}
	rp.ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
