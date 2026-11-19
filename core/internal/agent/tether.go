package agent

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/hollis-labs/libs/agent/chat"
	"github.com/hollis-labs/surface/core/internal/kernel"
	"github.com/hollis-labs/surface/core/internal/slots"
	"github.com/hollis-labs/surface/module"
)

const tetherPrefix = Prefix + "/tether"

// mountTether keeps the page on its own origin, including SSE reconnects. Credentials
// and per-turn context are added here, never in the browser or conversation event log.
func (s *Service) mountTether(mux *http.ServeMux) {
	target, _ := url.Parse(s.cfg.Tether.URL) // validated by LoadConfig
	proxy := &httputil.ReverseProxy{
		Rewrite: func(p *httputil.ProxyRequest) {
			p.Out.URL.Path = strings.TrimPrefix(p.In.URL.Path, tetherPrefix)
			p.Out.URL.RawPath = ""
			p.SetURL(target)
			p.Out.Header.Del("Origin")
			p.Out.Header.Del("Cookie")
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			fail(w, http.StatusBadGateway, "The Tether chat endpoint is unavailable.")
		},
	}
	forward := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seat := r.PathValue("seat"); seat != "" && seat != s.cfg.Tether.Seat {
			fail(w, http.StatusNotFound, "This app is attached to a different Tether seat.")
			return
		}
		r = r.Clone(r.Context())
		r.Header.Del("Authorization")
		if s.cfg.Tether.KeyFile != "" {
			key, err := os.ReadFile(s.cfg.Tether.KeyFile) //nolint:gosec // operator-configured credential file
			if err != nil || len(bytes.TrimSpace(key)) == 0 {
				fail(w, http.StatusBadGateway, "The Tether credential file cannot be read.")
				return
			}
			r.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(key)))
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/turns") {
			var body map[string]json.RawMessage
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil || body == nil {
				fail(w, http.StatusBadRequest, "Invalid chat turn.")
				return
			}
			var text string
			if err := json.Unmarshal(body["text"], &text); err != nil {
				fail(w, http.StatusBadRequest, "A chat turn needs text.")
				return
			}
			sit := s.k.Situation(r.Context())
			sit.Input = text
			sel := s.selector.Select(r.Context(), sit, s.k.Slots())
			available := []kernel.BoundTool{}
			for _, tool := range s.allTools() {
				if tool.Level == module.LevelRead || tool.Level == module.LevelAct {
					available = append(available, tool)
				}
			}
			asm := slots.Assemble(r.Context(), sit, sel, available, s.notes, slots.Extra{Instructions: s.layoutInstructions(r.Context(), sit)})
			block := asm.Block
			if extra := strings.TrimSpace(s.cfg.System); extra != "" {
				block = extra + "\n\n" + block
			}
			raw, _ := json.Marshal(block)
			if len(raw) > chat.MaxContextBytes {
				fail(w, http.StatusRequestEntityTooLarge, "The app context is too large for a chat turn.")
				return
			}
			body["context"] = raw
			encoded, _ := json.Marshal(body)
			r.Body = io.NopCloser(bytes.NewReader(encoded))
			r.ContentLength = int64(len(encoded))
			r.Header.Set("Content-Type", "application/json")
		}
		proxy.ServeHTTP(w, r)
	})
	// Only the chat protocol is forwarded; this is not a general Tether API proxy.
	for _, route := range []string{
		"POST /agents/{seat}/conversation", "POST /agents/{seat}/conversation/reset",
		"GET /conversations/{id}", "POST /conversations/{id}/attachments",
		"DELETE /conversations/{id}/attachments/{attachment}",
		"POST /conversations/{id}/lease", "POST /conversations/{id}/lease/takeover",
		"GET /conversations/{id}/events", "POST /conversations/{id}/turns",
		"POST /conversations/{id}/runs/{run}/cancel", "POST /conversations/{id}/approvals/{approval}",
	} {
		method, path, _ := strings.Cut(route, " ")
		mux.Handle(method+" "+tetherPrefix+path, forward)
	}
}
