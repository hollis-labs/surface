package hostrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// The tap stands between the host's model client and the gateway, on a loopback port. It
// forwards every request as it is (the host's own client adds the key; the tap adds
// nothing) and reads the usage the gateway reports in the streamed answer, which the host
// sums into one input count: uncached input, cache reads and cache writes are what the run
// log keeps apart. The host names the thread a request is for (Config.SessionHeader); the
// tap takes that header off before forwarding.
//
// It exists because libs agent/host has no hook for the model client or its usage. When
// the host reports the split itself, the tap goes.

type tap struct {
	url  string
	srv  *http.Server
	ln   net.Listener
	step func(thread string, summary bool) *usageStep
}

type tapThreadKey struct{}

type tapSummaryKey struct{}

// summaryRequest reports whether a request body is the host's compaction request: the only
// one it sends with no tools and its summary prompt as the system prompt.
func summaryRequest(body []byte) bool {
	var req struct {
		System []struct {
			Text string `json:"text"`
		} `json:"system"`
		Tools []json.RawMessage `json:"tools"`
	}
	if json.Unmarshal(body, &req) != nil || len(req.Tools) > 0 || len(req.System) == 0 {
		return false
	}
	return strings.HasPrefix(req.System[0].Text, "You are writing the running summary")
}

func openTap(target string, step func(string, bool) *usageStep, logger *slog.Logger) (*tap, error) {
	to, err := url.Parse(strings.TrimRight(target, "/"))
	if err != nil || (to.Scheme != "http" && to.Scheme != "https") || to.Host == "" {
		return nil, fmt.Errorf("hostrunner: the gateway URL %q is not an HTTP(S) URL", target)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("hostrunner: tap: %w", err)
	}
	t := &tap{url: "http://" + ln.Addr().String(), ln: ln, step: step}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			thread := r.In.Header.Get(threadHeader)
			r.SetURL(to)
			r.Out.Host = to.Host
			r.Out.Header.Del(threadHeader)
			// The usage frames must be readable here: no compressed answer. (The transport
			// still asks for one itself and undoes it before the tap reads.)
			r.Out.Header.Del("Accept-Encoding")
			summary := false
			if r.In.Body != nil {
				if body, err := io.ReadAll(io.LimitReader(r.In.Body, 64<<20)); err == nil {
					summary = summaryRequest(body)
					r.Out.Body = io.NopCloser(bytes.NewReader(body))
					r.Out.ContentLength = int64(len(body))
				}
			}
			ctx := context.WithValue(r.Out.Context(), tapThreadKey{}, thread)
			r.Out = r.Out.WithContext(context.WithValue(ctx, tapSummaryKey{}, summary))
		},
		// A model stream goes through as it arrives.
		FlushInterval: -1,
		ModifyResponse: func(res *http.Response) error {
			if res.StatusCode/100 != 2 || !strings.Contains(res.Header.Get("Content-Type"), "event-stream") {
				return nil
			}
			thread, _ := res.Request.Context().Value(tapThreadKey{}).(string)
			summary, _ := res.Request.Context().Value(tapSummaryKey{}).(bool)
			res.Body = &usageReader{rc: res.Body, step: t.step(thread, summary)}
			return nil
		},
		ErrorLog: log.New(io.Discard, "", 0),
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			if !errors.Is(err, context.Canceled) {
				logger.Warn("hostrunner: the model gateway could not be reached", "err", err)
			}
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":{"message":"the model gateway could not be reached"}}`))
		},
	}
	t.srv = &http.Server{Handler: proxy, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = t.srv.Serve(ln) }()
	return t, nil
}

func (t *tap) close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if t.srv.Shutdown(ctx) != nil {
		_ = t.srv.Close()
	}
}

// usageReader passes a streamed answer through and notes the usage in it.
type usageReader struct {
	rc   io.ReadCloser
	step *usageStep
	line []byte
}

func (u *usageReader) Read(p []byte) (int, error) {
	n, err := u.rc.Read(p)
	if n > 0 {
		u.feed(p[:n])
	}
	if err != nil {
		u.flush()
	}
	return n, err
}

func (u *usageReader) Close() error { return u.rc.Close() }

func (u *usageReader) feed(b []byte) {
	for len(b) > 0 {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			if len(u.line)+len(b) <= 1<<20 { // a usage frame is small; a long one is text
				u.line = append(u.line, b...)
			}
			return
		}
		u.line = append(u.line, b[:i]...)
		u.parse(u.line)
		u.line = u.line[:0]
		b = b[i+1:]
	}
}

func (u *usageReader) flush() {
	if len(u.line) > 0 {
		u.parse(u.line)
		u.line = u.line[:0]
	}
	// A stream that broke off is over too: what it used is reported.
	u.step.mu.Lock()
	u.step.stopped = true
	u.step.mu.Unlock()
}

type wireUsage struct {
	Input      int `json:"input_tokens"`
	Output     int `json:"output_tokens"`
	CacheWrite int `json:"cache_creation_input_tokens"`
	CacheRead  int `json:"cache_read_input_tokens"`
}

func (u *usageReader) parse(line []byte) {
	data, ok := bytes.CutPrefix(bytes.TrimRight(line, "\r"), []byte("data:"))
	if !ok {
		return
	}
	data = bytes.TrimSpace(data)
	// Only the frames that carry usage are decoded.
	if !bytes.Contains(data, []byte(`"message_`)) {
		return
	}
	var f struct {
		Type    string `json:"type"`
		Message struct {
			Usage wireUsage `json:"usage"`
		} `json:"message"`
		Usage wireUsage `json:"usage"`
	}
	if json.Unmarshal(data, &f) != nil {
		return
	}
	s := u.step
	s.mu.Lock()
	defer s.mu.Unlock()
	switch f.Type {
	case "message_start":
		s.in, s.read, s.write, s.started = f.Message.Usage.Input, f.Message.Usage.CacheRead, f.Message.Usage.CacheWrite, true
		s.out = f.Message.Usage.Output
	case "message_delta":
		s.out = f.Usage.Output
	case "message_stop":
		s.stopped = true
	}
}
