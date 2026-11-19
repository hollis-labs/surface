// Package wardentoken mints short-lived bearer tokens for one audience from a Warden parent
// credential kept in a file, and hands them to HTTP clients.
//
// Warden's parent credentials are rotate-on-use: every mint answers a new parent, and
// presenting a parent that was already spent revokes the whole family. So this package:
//   - holds an exclusive lock on <file>.lock around read, mint and save, so two processes (or
//     two apps) sharing one parent file never present the same parent;
//   - saves the new parent (temp file, fsync, rename, mode 0600) before the token is used;
//   - writes <file>.inflight before it sends a parent and removes it only once the outcome is
//     known. When the answer is lost (a timeout, a 5xx, a cut body) the parent may be spent,
//     and minting again would replay it, so the marker stays and every later mint refuses
//     until an operator re-enrolls and deletes the marker.
//
// Tokens are renewed at two thirds of their life. When a renewal fails, the current token is
// used until it expires. Neither the parent nor any token is ever logged.
package wardentoken

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ErrUncertain means an earlier mint's outcome is unknown, so the parent on file may be spent.
var ErrUncertain = errors.New("wardentoken: an earlier mint's outcome is unknown and the parent credential may be spent; re-enroll, store the new parent, then delete the .inflight marker")

// retryAfter is how long a failed mint is not retried.
const retryAfter = time.Minute

// Source mints and caches tokens. The zero value is not usable: set URL, CredentialFile and
// Audience.
type Source struct {
	// URL is Warden's base URL; the token endpoint is <URL>/v1/token.
	URL string
	// CredentialFile holds the parent credential. It is rewritten on every mint.
	CredentialFile string
	// Audience is the token's audience ("llm-gateway").
	Audience string
	// Scopes narrows the token; empty asks for what the grant allows.
	Scopes []string
	// HTTP is the client for Warden; default a 30s-timeout client.
	HTTP *http.Client
	// Log gets operator messages (never secrets); default slog.Default().
	Log *slog.Logger

	now func() time.Time

	mu       sync.Mutex
	token    string
	issued   time.Time
	expires  time.Time
	failedAt time.Time
	lastErr  error
}

func (s *Source) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *Source) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// Token answers a valid token, minting one when there is none or the current one is past two
// thirds of its life. Callers are serialized, so concurrent callers share one mint.
func (s *Source) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock()
	if s.token != "" && now.Before(s.issued.Add(s.expires.Sub(s.issued)*2/3)) {
		return s.token, nil
	}
	if !s.failedAt.IsZero() && now.Sub(s.failedAt) < retryAfter {
		if s.token != "" && now.Before(s.expires) {
			return s.token, nil
		}
		return "", s.lastErr
	}
	tok, exp, err := s.mint(ctx)
	if err != nil {
		s.failedAt, s.lastErr = now, err
		if s.token != "" && now.Before(s.expires) {
			s.log().Warn("wardentoken: renewal failed; using the current token until it expires", "audience", s.Audience, "expires", s.expires, "err", err)
			return s.token, nil
		}
		s.log().Error("wardentoken: no token", "audience", s.Audience, "err", err)
		return "", err
	}
	s.token, s.issued, s.expires, s.failedAt, s.lastErr = tok, now, exp, time.Time{}, nil
	return tok, nil
}

type answer struct {
	AccessToken  string    `json:"access_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	TTL          int       `json:"ttl_seconds"`
	RefreshToken string    `json:"refresh_token"`
}

func (s *Source) mint(ctx context.Context) (string, time.Time, error) {
	unlock, err := lockFile(s.CredentialFile + ".lock")
	if err != nil {
		return "", time.Time{}, fmt.Errorf("wardentoken: lock the parent credential: %w", err)
	}
	defer unlock()
	marker := s.CredentialFile + ".inflight"
	if _, statErr := os.Stat(marker); statErr == nil {
		s.log().Error("wardentoken: refusing to mint: an earlier mint's outcome is unknown, so the parent may be spent and presenting it again could revoke the credential family. Re-enroll, store the new parent, then delete the marker.", "marker", marker)
		return "", time.Time{}, ErrUncertain
	}
	raw, err := os.ReadFile(s.CredentialFile)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("wardentoken: read the parent credential: %w", err)
	}
	parent := strings.TrimSpace(string(raw))
	if parent == "" {
		return "", time.Time{}, errors.New("wardentoken: the parent credential file is empty")
	}
	if err = writeFile(marker, []byte(time.Now().UTC().Format(time.RFC3339)+"\n")); err != nil {
		return "", time.Time{}, fmt.Errorf("wardentoken: write the in-flight marker: %w", err)
	}
	// From here the outcome is uncertain unless proven otherwise.
	certain := false
	defer func() {
		if certain {
			_ = os.Remove(marker)
		} else {
			s.log().Error("wardentoken: the mint's outcome is unknown; minting is stopped until an operator re-enrolls and deletes the marker", "marker", marker)
		}
	}()

	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {parent}}
	if s.Audience != "" {
		form.Set("audience", s.Audience)
	}
	if len(s.Scopes) > 0 {
		form.Set("scope", strings.Join(s.Scopes, " "))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.URL, "/")+"/v1/token", strings.NewReader(form.Encode()))
	if err != nil {
		certain = true
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	hc := s.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	res, err := hc.Do(req)
	if err != nil {
		if notSent(err) {
			certain = true
		}
		return "", time.Time{}, fmt.Errorf("wardentoken: mint: %w", redactErr(err, parent))
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("wardentoken: mint: reading the answer: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		// A 4xx is Warden's definite refusal: nothing rotated. A 5xx may have rotated.
		if res.StatusCode >= 400 && res.StatusCode < 500 {
			certain = true
		}
		return "", time.Time{}, fmt.Errorf("wardentoken: mint: Warden answered %d: %s", res.StatusCode, errorCode(body))
	}
	var a answer
	if err := json.Unmarshal(body, &a); err != nil || a.AccessToken == "" || a.RefreshToken == "" {
		return "", time.Time{}, errors.New("wardentoken: mint: the answer has no token or no next parent credential")
	}
	if err := writeFile(s.CredentialFile, []byte(a.RefreshToken+"\n")); err != nil {
		// The old parent is spent and the new one is not saved: the marker stays.
		return "", time.Time{}, fmt.Errorf("wardentoken: could not save the rotated parent credential (re-enroll): %w", err)
	}
	certain = true
	exp := a.ExpiresAt
	if exp.IsZero() && a.TTL > 0 {
		exp = s.clock().Add(time.Duration(a.TTL) * time.Second)
	}
	if exp.IsZero() {
		exp = s.clock().Add(time.Hour)
	}
	s.log().Info("wardentoken: minted", "audience", s.Audience, "expires", exp)
	return a.AccessToken, exp, nil
}

// notSent: the request certainly did not reach Warden (it could not be dialed).
func notSent(err error) bool {
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return true
	}
	var dns *net.DNSError
	return errors.As(err, &dns)
}

func redactErr(err error, secret string) error {
	if secret == "" || !strings.Contains(err.Error(), secret) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), secret, "[redacted]"))
}

// errorCode keeps only Warden's error code, never echoing a body that might carry a secret.
func errorCode(body []byte) string {
	var e struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	_ = json.Unmarshal(body, &e)
	switch {
	case e.Code != "":
		return e.Code
	case e.Error != "" && len(e.Error) < 64 && !strings.ContainsAny(e.Error, " ."):
		return e.Error
	}
	return "(no code)"
}

// writeFile replaces path atomically with mode 0600: temp file, fsync, rename, fsync dir.
func writeFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	tmp := filepath.Join(dir, "."+filepath.Base(path)+".tmp-"+hex.EncodeToString(b))
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // G304: beside the operator's configured file
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if d, derr := os.Open(dir); derr == nil { //nolint:gosec // G304: the configured file's directory
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// Transport sets each request's bearer token from a Source.
type Transport struct {
	Source *Source
	Base   http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	tok, err := t.Source.Token(req.Context())
	if err != nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, err
	}
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+tok)
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}
