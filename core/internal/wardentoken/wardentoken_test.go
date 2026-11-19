package wardentoken

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeWarden is a rotate-on-use token endpoint: each parent may be presented once; a second
// presentation revokes the family, as Warden does.
type fakeWarden struct {
	mu      sync.Mutex
	current string
	seen    map[string]bool
	revoked bool
	n       int
	calls   atomic.Int32
	ttl     time.Duration
	mode    atomic.Value // "", "5xx", "4xx", "cut", "hang"
	now     func() time.Time
}

func newFake(t *testing.T, first string) (*fakeWarden, *httptest.Server) {
	f := &fakeWarden{current: first, seen: map[string]bool{}, ttl: time.Hour, now: time.Now}
	f.mode.Store("")
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeWarden) serve(w http.ResponseWriter, r *http.Request) {
	f.calls.Add(1)
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if r.URL.Path != "/v1/token" || r.ParseForm() != nil || r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("audience") != "llm-gateway" {
		http.Error(w, `{"code":"bad_request"}`, http.StatusBadRequest)
		return
	}
	switch f.mode.Load().(string) {
	case "4xx":
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":"slow_down"}`))
		return
	case "hang":
		time.Sleep(300 * time.Millisecond)
	}
	parent := r.Form.Get("refresh_token")
	f.mu.Lock()
	if f.seen[parent] {
		f.revoked = true
	}
	f.seen[parent] = true
	if f.revoked || parent != f.current {
		f.mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"invalid_grant"}`))
		return
	}
	f.n++
	next := fmt.Sprintf("parent-%d-SECRETPARENT", f.n)
	tok := fmt.Sprintf("token-%d-SECRETTOKEN", f.n)
	f.current = next
	f.mu.Unlock()
	switch f.mode.Load().(string) {
	case "5xx":
		w.WriteHeader(http.StatusBadGateway)
		return
	case "cut":
		hj, ok := w.(http.Hijacker)
		if ok {
			c, _, _ := hj.Hijack()
			_ = c.Close()
		}
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": tok, "expires_at": f.now().Add(f.ttl), "refresh_token": next})
}

func setup(t *testing.T) (*fakeWarden, *Source, *bytes.Buffer, *time.Time) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "warden.token")
	if err := os.WriteFile(file, []byte("parent-0-SECRETPARENT\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, srv := newFake(t, "parent-0-SECRETPARENT")
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	f.now = func() time.Time { return now }
	var buf bytes.Buffer
	s := &Source{URL: srv.URL, CredentialFile: file, Audience: "llm-gateway", Log: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	s.now = func() time.Time { return now }
	return f, s, &buf, &now
}

func noSecrets(t *testing.T, buf *bytes.Buffer) {
	t.Helper()
	if strings.Contains(buf.String(), "SECRET") {
		t.Fatalf("a secret reached the log:\n%s", buf.String())
	}
}

func TestRotationSavedBeforeUse(t *testing.T) {
	_, s, buf, _ := setup(t)
	tok, err := s.Token(t.Context())
	if err != nil || tok != "token-1-SECRETTOKEN" {
		t.Fatalf("token %q err %v", tok, err)
	}
	raw, _ := os.ReadFile(s.CredentialFile)
	if strings.TrimSpace(string(raw)) != "parent-1-SECRETPARENT" {
		t.Fatalf("parent not rotated: %q", raw)
	}
	st, _ := os.Stat(s.CredentialFile)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
	if _, err := os.Stat(s.CredentialFile + ".inflight"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("marker left behind: %v", err)
	}
	noSecrets(t, buf)
}

func TestRenewsAtTwoThirds(t *testing.T) {
	f, s, buf, now := setup(t)
	if _, err := s.Token(t.Context()); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(39 * time.Minute)
	if tok, _ := s.Token(t.Context()); tok != "token-1-SECRETTOKEN" || f.calls.Load() != 1 {
		t.Fatalf("renewed early: %q after %d calls", tok, f.calls.Load())
	}
	*now = now.Add(2 * time.Minute) // 41 minutes
	if tok, _ := s.Token(t.Context()); tok != "token-2-SECRETTOKEN" || f.calls.Load() != 2 {
		t.Fatalf("not renewed at 2/3: %q after %d calls", tok, f.calls.Load())
	}
	noSecrets(t, buf)
}

func TestConcurrentCallersShareOneMint(t *testing.T) {
	f, s, _, _ := setup(t)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Token(t.Context()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if f.calls.Load() != 1 {
		t.Fatalf("%d mints for 20 callers", f.calls.Load())
	}
}

func TestUncertainOutcomeNeverReplays(t *testing.T) {
	for _, mode := range []string{"5xx", "cut"} {
		t.Run(mode, func(t *testing.T) {
			f, s, buf, now := setup(t)
			f.mode.Store(mode)
			if _, err := s.Token(t.Context()); err == nil {
				t.Fatal("no error")
			}
			if _, err := os.Stat(s.CredentialFile + ".inflight"); err != nil {
				t.Fatalf("marker missing: %v", err)
			}
			f.mode.Store("")
			*now = now.Add(2 * retryAfter)
			if _, err := s.Token(t.Context()); !errors.Is(err, ErrUncertain) {
				t.Fatalf("want ErrUncertain, got %v", err)
			}
			if f.calls.Load() != 1 || f.revoked {
				t.Fatalf("replayed: %d calls, revoked %v", f.calls.Load(), f.revoked)
			}
			noSecrets(t, buf)
		})
	}
}

func TestTimeoutIsUncertain(t *testing.T) {
	f, s, _, _ := setup(t)
	f.mode.Store("hang")
	s.HTTP = &http.Client{Timeout: 50 * time.Millisecond}
	if _, err := s.Token(t.Context()); err == nil {
		t.Fatal("no error")
	}
	if _, err := os.Stat(s.CredentialFile + ".inflight"); err != nil {
		t.Fatalf("a timeout must leave the marker: %v", err)
	}
}

func TestRefusalIsCertainAndCurrentTokenKept(t *testing.T) {
	f, s, buf, now := setup(t)
	if _, err := s.Token(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.mode.Store("4xx")
	*now = now.Add(45 * time.Minute)
	tok, err := s.Token(t.Context())
	if err != nil || tok != "token-1-SECRETTOKEN" {
		t.Fatalf("current token not kept: %q %v", tok, err)
	}
	if _, err := os.Stat(s.CredentialFile + ".inflight"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a definite refusal must not leave the marker")
	}
	*now = now.Add(20 * time.Minute) // past expiry, past retryAfter
	if _, err := s.Token(t.Context()); err == nil {
		t.Fatal("an expired token was handed out")
	}
	f.mode.Store("")
	*now = now.Add(2 * retryAfter)
	if tok, err := s.Token(t.Context()); err != nil || tok != "token-2-SECRETTOKEN" {
		t.Fatalf("no recovery: %q %v", tok, err)
	}
	noSecrets(t, buf)
}

func TestDialFailureIsCertain(t *testing.T) {
	_, s, _, _ := setup(t)
	s.URL = "http://127.0.0.1:1"
	if _, err := s.Token(t.Context()); err == nil {
		t.Fatal("no error")
	}
	if _, err := os.Stat(s.CredentialFile + ".inflight"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a request that was never sent must not leave the marker")
	}
}

func TestTransportSetsBearer(t *testing.T) {
	_, s, _, _ := setup(t)
	var got string
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.Header.Get("Authorization") }))
	defer gw.Close()
	hc := &http.Client{Transport: &Transport{Source: s}}
	res, err := hc.Get(gw.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if got != "Bearer token-1-SECRETTOKEN" {
		t.Fatalf("header %q", got)
	}
}

// Two processes minting from one parent file must never present the same parent.
func TestTwoProcessesShareOneFile(t *testing.T) {
	if os.Getenv("WARDENTOKEN_HELPER") != "" {
		t.Skip("helper")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "warden.token")
	if err := os.WriteFile(file, []byte("parent-0-SECRETPARENT\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, srv := newFake(t, "parent-0-SECRETPARENT")
	const procs, each = 2, 15
	var cmds []*exec.Cmd
	for range procs {
		c := exec.Command(os.Args[0], //nolint:gosec // G204: re-runs this test binary
			"-test.run=^TestHelperMint$", "-test.count=1")
		c.Env = append(os.Environ(), "WARDENTOKEN_HELPER=1", "WARDENTOKEN_URL="+srv.URL, "WARDENTOKEN_FILE="+file, "WARDENTOKEN_N="+strconv.Itoa(each))
		var out bytes.Buffer
		c.Stdout, c.Stderr = &out, &out
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		cmds = append(cmds, c)
		t.Cleanup(func() {
			if t.Failed() {
				t.Log(out.String())
			}
		})
	}
	for _, c := range cmds {
		if err := c.Wait(); err != nil {
			t.Fatalf("helper: %v", err)
		}
	}
	if f.revoked || f.n != procs*each {
		t.Fatalf("revoked %v, %d good mints of %d", f.revoked, f.n, procs*each)
	}
}

func TestHelperMint(t *testing.T) {
	if os.Getenv("WARDENTOKEN_HELPER") == "" {
		t.Skip("run by TestTwoProcessesShareOneFile")
	}
	n, _ := strconv.Atoi(os.Getenv("WARDENTOKEN_N"))
	for range n {
		// A fresh Source each time, so every call mints.
		s := &Source{URL: os.Getenv("WARDENTOKEN_URL"), CredentialFile: os.Getenv("WARDENTOKEN_FILE"), Audience: "llm-gateway"}
		if _, err := s.Token(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}
