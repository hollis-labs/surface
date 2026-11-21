package browser

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
)

// Positive fixtures use IANA special-purpose AS112 space (RFC7534); all dials use net.Pipe.
// https://www.iana.org/assignments/iana-ipv4-special-registry
func TestPublicAddressMatrix(t *testing.T) {
	for _, raw := range []string{"0.0.0.0", "10.1.2.3", "100.64.1.2", "127.0.0.1", "169.254.169.254", "172.16.1.1", "192.168.1.1", "192.0.2.1", "198.18.0.1", "198.51.100.1", "203.0.113.1", "224.1.1.1", "255.255.255.255", "::", "::1", "::ffff:127.0.0.1", "fc00::1", "fd7a:115c:a1e0::1", "fe80::1", "ff02::1", "64:ff9b::7f00:1", "2002:7f00:1::", "2001::1", "2001:db8::1", "3fff::1"} {
		if publicAddress(netip.MustParseAddr(raw), nil) {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, raw := range []string{"192.175.48.6", "192.175.48.42", "2620:4f:8000::6", "::ffff:192.175.48.6"} {
		if !publicAddress(netip.MustParseAddr(raw), nil) {
			t.Errorf("denied %s", raw)
		}
	}
	if publicAddress(netip.MustParseAddr("192.175.48.6"), []netip.Prefix{netip.MustParsePrefix("192.175.48.0/24")}) {
		t.Fatal("local interface network allowed")
	}
}
func fixtureTransport(t *testing.T, reply func(*http.Request) string) (*publicTransport, *atomic.Int32) {
	t.Helper()
	calls := &atomic.Int32{}
	tr := newPublicTransport()
	tr.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("192.175.48.6")}, nil
	}
	tr.locals = func() ([]netip.Prefix, error) { return nil, nil }
	tr.dial = func(_ context.Context, network, address string) (net.Conn, error) {
		calls.Add(1)
		if address != "192.175.48.6:80" || network != "tcp" {
			t.Errorf("un-pinned dial %s %s", network, address)
		}
		client, server := net.Pipe()
		go func() {
			defer func() { _ = server.Close() }()
			req, err := http.ReadRequest(bufio.NewReader(server))
			if err != nil {
				return
			}
			_, _ = io.WriteString(server, reply(req))
		}()
		return client, nil
	}
	return tr, calls
}
func testClient(tr *publicTransport) *http.Client {
	c := browserClient(true)
	c.Transport = tr
	return c
}
func TestGuardPinsDNSAndDropsCredentials(t *testing.T) {
	tr, calls := fixtureTransport(t, func(r *http.Request) string {
		if r.Host != "example.com" {
			t.Errorf("Host = %s", r.Host)
		}
		for _, h := range []string{"Cookie", "Authorization", "Proxy-Authorization", "Referer", "X-Owner"} {
			if r.Header.Get(h) != "" {
				t.Errorf("forwarded %s", h)
			}
		}
		return "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nSet-Cookie: secret=fixture\r\n\r\nok"
	})
	var lookup atomic.Int32
	tr.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		if lookup.Add(1) > 1 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("192.175.48.6")}, nil
	}
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	req, _ := http.NewRequest("GET", "http://example.com/", nil)
	for _, h := range []string{"Cookie", "Authorization", "Proxy-Authorization", "Referer", "X-Owner"} {
		req.Header.Set(h, "fixture")
	}
	resp, err := testClient(tr).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Header.Get("Set-Cookie") != "" {
		t.Fatal("Set-Cookie survived")
	}
	_, err = responseBytes(resp, 32)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || lookup.Load() != 1 || len(tr.slots) != 0 {
		t.Fatalf("calls%d lookups%d slots%d", calls.Load(), lookup.Load(), len(tr.slots))
	}
	// A subsequent request revalidates DNS; prior connection cannot bypass the new answer.
	if _, err = testClient(tr).Do(req); err == nil {
		t.Fatal("rebound DNS accepted")
	}
	if calls.Load() != 1 {
		t.Fatal("dialed after rebinding")
	}
}
func TestGuardRejectsBeforeConnecting(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1/", "http://[::1]/", "http://[fe80::1%25eth0]/", "http://2130706433/", "http://0177.0.0.1/", "http://localhost/", "http://box.local/", "http://box.example-tailnet.ts.net/", "http://example.com:8080/", "http://user:pass@example.com/", "file:///etc/hosts", "http://0x7f000001/"} {
		t.Run(raw, func(t *testing.T) {
			tr, calls := fixtureTransport(t, func(*http.Request) string { return "" })
			req, err := http.NewRequest("GET", raw, nil)
			if err == nil {
				_, err = testClient(tr).Do(req)
			}
			if err == nil || calls.Load() != 0 {
				t.Fatalf("accepted or dialed: %v %d", err, calls.Load())
			}
		})
	}
	for _, ips := range [][]netip.Addr{nil, {netip.MustParseAddr("192.175.48.6"), netip.MustParseAddr("10.0.0.1")}, {netip.MustParseAddr("2620:4f:8000::6"), netip.MustParseAddr("::ffff:127.0.0.1")}} {
		tr, calls := fixtureTransport(t, func(*http.Request) string { return "" })
		tr.lookup = func(context.Context, string, string) ([]netip.Addr, error) { return ips, nil }
		_, err := testClient(tr).Get("http://example.com/")
		if err == nil || calls.Load() != 0 {
			t.Fatalf("mixed/empty answers accepted: %v", ips)
		}
	}
}
func TestRedirectValidationAndLimit(t *testing.T) {
	for _, target := range []string{"http://127.0.0.1/", "http://user:pass@example.com/", "file:///x", "http://example.com:9000/"} {
		tr, calls := fixtureTransport(t, func(*http.Request) string {
			return "HTTP/1.1 302 Found\r\nLocation: " + target + "\r\nContent-Length: 0\r\n\r\n"
		})
		resp, err := testClient(tr).Get("http://example.com/")
		if resp != nil {
			_ = resp.Body.Close()
		}
		if err == nil || calls.Load() != 1 {
			t.Fatalf("redirect: %v calls%d", err, calls.Load())
		}
	}
	tr, calls := fixtureTransport(t, func(*http.Request) string {
		return "HTTP/1.1 302 Found\r\nLocation: /again\r\nContent-Length: 0\r\n\r\n"
	})
	resp, err := testClient(tr).Get("http://example.com/")
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil || calls.Load() != maxRedirects+1 {
		t.Fatalf("limit %v calls%d", err, calls.Load())
	}
}
func TestBoundedBodiesAndCancellation(t *testing.T) {
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	_, _ = gz.Write(bytes.Repeat([]byte("x"), 1024))
	_ = gz.Close()
	for _, tt := range []struct {
		body     []byte
		encoding string
	}{{bytes.Repeat([]byte("x"), 65), ""}, {compressed.Bytes(), "gzip"}, {[]byte("small"), "br"}} {
		r := &http.Response{Body: io.NopCloser(bytes.NewReader(tt.body)), Header: make(http.Header)}
		r.Header.Set("Content-Encoding", tt.encoding)
		if _, err := responseBytes(r, 64); err == nil {
			t.Fatal("unbounded/unsupported response accepted")
		}
	}
	tr, calls := fixtureTransport(t, func(*http.Request) string { return "" })
	tr.lookup = func(ctx context.Context, _, _ string) ([]netip.Addr, error) { <-ctx.Done(); return nil, ctx.Err() }
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://example.com/", nil)
	if _, err := testClient(tr).Do(req); err == nil || calls.Load() != 0 || len(tr.slots) != 0 {
		t.Fatal("cancel did not stop DNS/release slot")
	}
}
func TestOwnerAndAgentPoliciesAndCachesAreSeparate(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("credentials")
		}
		_, _ = io.WriteString(w, "<p>Owner private page</p>")
	}))
	defer srv.Close()
	m := &mod{}
	h := moduletest.Start(t, m)
	p, err := m.f.get(context.Background(), srv.URL)
	if err != nil || !strings.Contains(p.Text, "Owner private") {
		t.Fatalf("owner %v %+v", err, p)
	}
	for _, name := range []string{"browser_read", "browser_links"} {
		if _, err := h.Tool(name, fmt.Sprintf(`{"url":%q}`, srv.URL)); err == nil {
			t.Fatalf("agent %s got owner content", name)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("agent reached private service")
	}
	// Owner GET is not privileged: only the explicit owner POST opts into internal access.
	for _, tt := range []struct {
		method string
		actor  module.Actor
		want   bool
	}{{"GET", module.ActorOwner, false}, {"POST", module.ActorOutside, false}, {"POST", module.ActorOwner, true}} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(tt.method, "/api/m/browser/read?url="+srv.URL, nil)
		req = req.WithContext(module.WithActor(req.Context(), tt.actor))
		h.ServeHTTP(rr, req)
		if (rr.Code == 200) != tt.want {
			t.Errorf("%s %s: %d %s", tt.method, tt.actor, rr.Code, rr.Body.String())
		}
	}
}
func TestGuardConcurrencyBoundAndNoCredentialURL(t *testing.T) {
	tr, _ := fixtureTransport(t, func(*http.Request) string { return "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n" })
	var mu sync.Mutex
	var bodies []io.Closer
	for i := 0; i < maxFetchConcurrency; i++ {
		r, err := testClient(tr).Get("http://example.com/")
		if err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		bodies = append(bodies, r.Body)
		mu.Unlock()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://example.com/", nil)
	if _, err := testClient(tr).Do(req); !errors.Is(err, context.Canceled) {
		t.Fatalf("saturated cancellation: %v", err)
	}
	for _, b := range bodies {
		_ = b.Close()
	}
	if len(tr.slots) != 0 {
		t.Fatal("slots leaked")
	}
	if _, err := newFetcher().get(context.Background(), "http://user:pass@example.com/"); err == nil {
		t.Fatal("owner credentials URL allowed")
	}
}

func TestRedirectHostIsResolvedAndDenied(t *testing.T) {
	tr, calls := fixtureTransport(t, func(*http.Request) string {
		return "HTTP/1.1 302 Found\r\nLocation: http://private.example.test/\r\nContent-Length: 0\r\n\r\n"
	})
	tr.lookup = func(_ context.Context, _, host string) ([]netip.Addr, error) {
		if host == "private.example.test" {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("192.175.48.6")}, nil
	}
	if _, err := testClient(tr).Get("http://example.com/"); err == nil || calls.Load() != 1 {
		t.Fatalf("redirect DNS bypass: %v calls%d", err, calls.Load())
	}
}
func TestTLSCertificateIsVerified(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("untrusted TLS request reached handler")
		w.WriteHeader(200)
	}))
	defer server.Close()
	tr, _ := fixtureTransport(t, func(*http.Request) string { return "" })
	tr.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "192.175.48.6:443" {
			t.Errorf("pin = %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	if _, err := testClient(tr).Get("https://example.com/"); err == nil {
		t.Fatal("untrusted certificate accepted")
	}
}
func TestAgentSearchAndOpenedWindowsStayPublic(t *testing.T) {
	m := &mod{}
	h := moduletest.Start(t, m)
	tr, calls := fixtureTransport(t, func(*http.Request) string { return "" })
	tr.lookup = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	m.public.client = testClient(tr)
	if _, err := h.Tool("browser_search", `{"query":"example"}`); err == nil || calls.Load() != 0 {
		t.Fatalf("search bypassed guard: %v", err)
	}
	if _, err := h.Tool("browser_open", `{"url":"http://example.com/","new":true}`); err != nil {
		t.Fatal(err)
	}
	windows := h.Desk().Windows(context.Background())
	if len(windows) != 1 || windows[0].Args["public_only"] != true {
		t.Fatalf("agent window policy: %+v", windows)
	}
}
