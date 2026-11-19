package approvals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The Gatecrasher contract (gatecrasher api/openapi/gate-v1.yaml, pkg/approvalapi) as far
// as a surface needs it: the event stream, the card, decide.

const (
	pathApprovals = "/api/gate/v1/approvals"
	pathEvents    = "/api/gate/v1/events"
	cardVersion   = "gatecrasher.card/v1"
)

// feedEvent is one entry of the event stream.
type feedEvent struct {
	Seq        int64  `json:"seq"`
	ApprovalID string `json:"approval_id"`
	Type       string `json:"type"` // submitted, decided, shadow_decided, withdrawn, expired, secret_provided
	At         string `json:"at"`
	Status     string `json:"status"` // the approval's status now
	Tier       string `json:"tier"`
	Kind       string `json:"kind"`
	Source     string `json:"source"`
}

// gateCard is Gatecrasher's rendering of one approval.
type gateCard struct {
	Version    string `json:"version"`
	ApprovalID string `json:"approval_id"`
	Type       string `json:"type"` // approve, choose, answer, request_secret
	Status     string `json:"status"`
	Tier       string `json:"tier"`
	Kind       string `json:"kind"`
	Source     string `json:"source"`
	Title      string `json:"title"`
	Summary    string `json:"summary,omitempty"`
	Principal  struct {
		Sub  string `json:"sub"`
		Kind string `json:"kind"`
	} `json:"principal"`
	Requested *struct {
		Audience   string   `json:"audience"`
		Scopes     []string `json:"scopes"`
		TTLSeconds int64    `json:"ttl_seconds"`
	} `json:"requested,omitempty"`
	Evidence map[string]string `json:"evidence"`
	Prompt   *struct {
		Question string `json:"question"`
		Choices  []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"choices,omitempty"`
	} `json:"prompt,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
	Deciders  string    `json:"deciders"`
	Actions   []struct {
		Kind    string `json:"kind"`
		Label   string `json:"label"`
		Outcome string `json:"outcome,omitempty"`
		Decide  string `json:"decide,omitempty"`
	} `json:"actions"`
	Secret *struct {
		IntakeID  string    `json:"intake_id"`
		PostURL   string    `json:"post_url"`
		Field     string    `json:"field"`
		ExpiresAt time.Time `json:"expires_at"`
		WriteOnly bool      `json:"write_only"`
	} `json:"secret,omitempty"`
}

// decideRequest is the body of POST /approvals/{id}/decide.
type decideRequest struct {
	Outcome  string    `json:"outcome"` // approved, denied
	Narrowed *narrowed `json:"narrowed,omitempty"`
	Note     string    `json:"note,omitempty"`
	Choice   string    `json:"choice,omitempty"`
	Answer   string    `json:"answer,omitempty"`
}

type narrowed struct {
	Scopes     []string `json:"scopes,omitempty"`
	TTLSeconds int64    `json:"ttl_seconds,omitempty"`
}

// gate is a client of one Gatecrasher, acting as the owner with the key in keyFile.
type gate struct {
	base    string
	keyFile string
	client  *http.Client
}

var errNotConfigured = errors.New("approvals: set approvals.url and approvals.key_file in Settings")

// errTerminal: someone else decided first, or it expired or was withdrawn.
var errTerminal = errors.New("approvals: the approval is already closed")

func newGate(base, keyFile string) (*gate, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" || strings.TrimSpace(keyFile) == "" {
		return nil, errNotConfigured
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || !secure(u) {
		return nil, errors.New("approvals: the Gatecrasher URL must be https, or http on loopback")
	}
	return &gate{base: base, keyFile: expand(keyFile), client: &http.Client{Timeout: 20 * time.Second, CheckRedirect: noRedirect}}, nil
}

// noRedirect: a redirect is refused, so the owner's key goes only to the configured address.
func noRedirect(*http.Request, []*http.Request) error {
	return errors.New("approvals: Gatecrasher answered with a redirect")
}

// permanent is an approval that cannot be shown as it is (an unknown card, a missing one, an
// intake that is not https): retrying will not help, so it is reported and passed over.
type permanent struct{ err error }

func (p permanent) Error() string { return p.err.Error() }
func (p permanent) Unwrap() error { return p.err }

// secure: https, or http on loopback.
func secure(u *url.URL) bool {
	return u.Scheme == "https" || (u.Scheme == "http" && loopback(u.Hostname()))
}

func loopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func expand(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// key reads the owner's key at the moment of use. It is never logged or returned.
func (g *gate) key() (string, error) {
	b, err := os.ReadFile(g.keyFile)
	if err != nil {
		return "", errors.New("approvals: the owner key file cannot be read")
	}
	k := strings.TrimSpace(string(b))
	if k == "" {
		return "", errors.New("approvals: the owner key file is empty")
	}
	return k, nil
}

func (g *gate) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	key, err := g.key()
	if err != nil {
		return nil, err
	}
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.base+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return g.client.Do(req)
}

// card reads one approval's card.
func (g *gate) card(ctx context.Context, id string) (gateCard, error) {
	var c gateCard
	res, err := g.do(ctx, http.MethodGet, pathApprovals+"/"+url.PathEscape(id)+"/card", nil)
	if err != nil {
		return c, fmt.Errorf("approvals: Gatecrasher cannot be reached: %w", redact(err))
	}
	defer func() { _ = res.Body.Close() }()
	switch {
	case res.StatusCode == http.StatusNotFound:
		return c, permanent{fmt.Errorf("approvals: Gatecrasher has no card for %s", id)}
	case res.StatusCode != http.StatusOK:
		return c, fmt.Errorf("approvals: the card of %s: %s", id, res.Status)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&c); err != nil {
		return c, permanent{errors.New("approvals: the card could not be read")}
	}
	if c.Version != cardVersion {
		return c, permanent{fmt.Errorf("approvals: card version %q is not %q", c.Version, cardVersion)}
	}
	return c, nil
}

// decide sends the owner's decision. errTerminal when it was already closed.
func (g *gate) decide(ctx context.Context, id string, d decideRequest) error {
	res, err := g.do(ctx, http.MethodPost, pathApprovals+"/"+url.PathEscape(id)+"/decide", d)
	if err != nil {
		return fmt.Errorf("approvals: Gatecrasher cannot be reached: %w", redact(err))
	}
	defer func() { _ = res.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	switch res.StatusCode {
	case http.StatusOK, http.StatusAccepted:
		return nil
	case http.StatusConflict:
		return errTerminal
	default:
		return fmt.Errorf("approvals: decide on %s: %s", id, res.Status)
	}
}

// redact keeps a transport error to its kind: a URL error names the URL and nothing else.
func redact(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}
