package frag

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Delivery: the worker takes one due capture at a time, freezes its fe.capture.v1 manifest
// with a random upstream id, sends it to FE (POST /v1/captures, then /complete) with the
// owner's personal key, and records the receipt. It is Basalisk's fragment worker in
// personal mode, for one owner.

// failure is a delivery that did not finish. A retryable one backs off and goes again; any
// other waits for the owner (needs_attention).
type failure struct {
	Code      string
	Retryable bool
}

func (e *failure) Error() string { return e.Code }
func fail(code string, retry bool) error {
	return &failure{code, retry}
}

// personalPrincipal is FE's principal for a personal-mode capture, as fe-clipper's default.
const personalPrincipal = "local-user"

// attemptTimeout bounds one delivery attempt, below the lease (tests shorten it).
var attemptTimeout = 40 * time.Second

// origin checks the configured FE address: https, or http to this machine only; no path,
// query or credentials. It returns the origin without a trailing slash.
func origin(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("the Fragments Engine address is an origin like https://fragments.example.test")
	}
	switch u.Scheme {
	case "https":
	case "http":
		host := u.Hostname()
		if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return "", errors.New("the Fragments Engine address must be https (plain http only to this machine)")
		}
	default:
		return "", errors.New("the Fragments Engine address must be https")
	}
	return u.Scheme + "://" + u.Host, nil
}

// readToken reads the owner's key file at the moment of use. Its contents are never logged,
// stored or returned; a failure names only the reason.
func readToken(path string) (string, error) {
	path = strings.TrimSpace(path)
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fail("credential_unavailable", false)
		}
		path = filepath.Join(home, path[2:])
	}
	if path == "" || !filepath.IsAbs(path) {
		return "", fail("credential_unavailable", false)
	}
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > 8192 {
		return "", fail("credential_unavailable", false)
	}
	raw, err := os.ReadFile(path) //nolint:gosec // the owner's own setting names the file
	if err != nil {
		return "", fail("credential_unavailable", false)
	}
	tok := strings.TrimSpace(string(raw))
	if tok == "" || strings.ContainsAny(tok, "\r\n") {
		return "", fail("credential_unavailable", false)
	}
	return tok, nil
}

// client speaks FE's public capture contract: no cookies, no redirects, a short timeout.
type client struct{ http *http.Client }

func newClient(base *http.Client) *client {
	if base == nil {
		base = &http.Client{}
	}
	c := *base
	c.Timeout = 10 * time.Second
	c.Jar = nil
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client{&c}
}

func (c *client) request(ctx context.Context, method, endpoint, bearer string, body []byte, out any, accepted ...int) error {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, rd)
	if err != nil {
		return fail("invalid_configuration", false)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return fail("connection_unavailable", true)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		switch res.StatusCode {
		case 401, 403:
			return fail("authorization_required", false)
		case 409:
			return fail("capture_conflict", false)
		case 400, 404, 413, 422:
			return fail("capture_rejected", false)
		case 408, 429:
			return fail("service_busy", true)
		default:
			return fail("service_unavailable", res.StatusCode >= 500)
		}
	}
	if !slices.Contains(accepted, res.StatusCode) {
		return fail("invalid_receipt", false)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1024*1024+1))
	if err != nil {
		return fail("connection_unavailable", true)
	}
	if len(raw) > 1024*1024 || json.Unmarshal(raw, out) != nil {
		return fail("invalid_receipt", false)
	}
	return nil
}

// receipt is FE's answer to a capture (fe.capture.result.v1, fe.capture.status.v1).
type receipt struct {
	SchemaVersion     string            `json:"schema_version"`
	CaptureID         string            `json:"capture_id"`
	FragmentID        string            `json:"fragment_id"`
	RevisionID        string            `json:"fragment_revision_id"`
	AttemptID         string            `json:"capture_attempt_id"`
	Completion        string            `json:"completion"`
	AssetInstructions []json.RawMessage `json:"asset_instructions"`
	Assets            []json.RawMessage `json:"assets"`
}

func safeID(s string) bool {
	if s == "" || len(s) > 255 {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

func (r receipt) valid(captureID string) bool {
	return r.CaptureID == captureID && safeID(r.FragmentID) && safeID(r.RevisionID) && safeID(r.AttemptID)
}

func (c *client) accept(ctx context.Context, origin, bearer, captureID string, manifest []byte) (receipt, error) {
	var r receipt
	if err := c.request(ctx, "POST", origin+"/v1/captures", bearer, manifest, &r, 200, 201); err != nil {
		return r, err
	}
	// A capture without media has nothing for FE to ask for.
	if r.SchemaVersion != "fe.capture.result.v1" || !r.valid(captureID) || (r.Completion != "accepting" && r.Completion != "complete") || len(r.AssetInstructions) != 0 {
		return r, fail("invalid_receipt", false)
	}
	return r, nil
}

// complete closes a capture that has no media. The idempotency key is the capture's, so a
// repeat is the same completion.
func (c *client) complete(ctx context.Context, origin, bearer string, accepted receipt) (receipt, error) {
	var r receipt
	body, _ := json.Marshal(map[string]any{"schema_version": "fe.capture.completion.v1", "idempotency_key": "complete-" + accepted.CaptureID, "assets": []any{}, "warnings": []any{}})
	if err := c.request(ctx, "POST", origin+"/v1/captures/"+url.PathEscape(accepted.CaptureID)+"/complete", bearer, body, &r, 200); err != nil {
		return r, err
	}
	if r.SchemaVersion != "fe.capture.status.v1" || !r.valid(accepted.CaptureID) || r.FragmentID != accepted.FragmentID || r.RevisionID != accepted.RevisionID || r.AttemptID != accepted.AttemptID || r.Completion != "complete" || len(r.Assets) != 0 {
		return r, fail("invalid_receipt", false)
	}
	return r, nil
}

// capabilities reads GET /v1/capabilities: a read that confirms the address and the key.
func (c *client) capabilities(ctx context.Context, origin, bearer string) (string, error) {
	var caps struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := c.request(ctx, "GET", origin+"/v1/capabilities", bearer, nil, &caps, 200); err != nil {
		return "", err
	}
	if caps.SchemaVersion != "fe.capabilities.v1" {
		return "", fail("invalid_capabilities", false)
	}
	return caps.SchemaVersion, nil
}

// manifest is the fe.capture.v1 document for one note: the shape Basalisk sends for the
// owner's captures. The owner's context is not in it.
func manifest(upstream, sourceURL string, p Payload) ([]byte, error) {
	tags := p.Tags
	if tags == nil {
		tags = []string{}
	}
	provenance := map[string]any{"capture_id": p.CaptureID, "device_id": "owner", "draft_at": p.DraftAt, "submitted_at": p.SubmittedAt}
	return json.Marshal(map[string]any{
		"schema_version": "fe.capture.v1", "capture_id": upstream, "captured_at": p.SubmittedAt, "principal_id": personalPrincipal,
		"client": map[string]string{"kind": "surface", "version": "1.0.0"},
		"source": map[string]any{"submitted_url": sourceURL, "canonical_url": sourceURL, "provider": "surface", "source_registration_id": "surface-notes",
			"source_item_key": "note:" + upstream, "segment_key": "root", "canonicalizer": map[string]string{"adapter": "surface", "version": "1.0.0"}},
		"document": map[string]any{"title": "Frag note", "content": map[string]string{"format": "markdown", "body": p.Content},
			"extensions": map[string]any{"provider.surface": provenance}},
		"tags": tags, "annotations": []any{}, "media": []any{},
		"extraction": map[string]any{"adapter": "surface", "adapter_version": "1.0.0", "observed_capabilities": []string{"title", "body"}, "warnings": []any{}},
	})
}

// sourceBase is the origin of a note's source link. FE requires an absolute http(s) URL there
// and does not fetch it.
const sourceBase = "http://localhost/api/m/frag/outbox/"

type job struct {
	seq                                                 int64
	payload, manifest, origin, upstream, receipt, lease string
	attempts                                            int
}

// worker delivers the queue. It does nothing while delivery is off or not configured.
type worker struct {
	st      *store
	cl      *client
	cfg     func() (fe, tokenFile string, on bool)
	changed func(seq int64)
}

// ready says whether delivery may run now, and why not.
func (w *worker) ready() (fe, tokenFile, reason string) {
	fe, tokenFile, on := w.cfg()
	switch {
	case !on:
		return "", "", "delivery is off (frag.delivery)"
	case strings.TrimSpace(fe) == "":
		return "", "", "no Fragments Engine address (frag.fe_url)"
	case strings.TrimSpace(tokenFile) == "":
		return "", "", "no key file (frag.fe_token_file)"
	}
	o, err := origin(fe)
	if err != nil {
		return "", "", err.Error()
	}
	return o, tokenFile, ""
}

func (w *worker) run(ctx context.Context, wake <-chan struct{}) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		for ctx.Err() == nil {
			did, err := w.once(ctx)
			if err != nil || !did {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-wake:
		}
	}
}

// once delivers at most one due capture. did is false when there was nothing to do.
func (w *worker) once(ctx context.Context) (did bool, err error) {
	fe, tokenFile, reason := w.ready()
	if reason != "" {
		return false, nil
	}
	j, err := w.claim(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	w.changed(j.seq)
	attemptCtx, cancel := context.WithTimeout(ctx, attemptTimeout)
	defer cancel()
	if err := w.deliver(attemptCtx, &j, fe, tokenFile); err != nil {
		// Keep the retry state even when the app is stopping.
		saveCtx, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer done()
		err = w.failed(saveCtx, j, err)
		w.changed(j.seq)
		return true, err
	}
	w.changed(j.seq)
	return true, nil
}

func (w *worker) claim(ctx context.Context) (job, error) {
	var j job
	tx, err := w.st.db.BeginTx(ctx, nil)
	if err != nil {
		return j, err
	}
	defer func() { _ = tx.Rollback() }()
	t := time.Now().Unix()
	err = tx.QueryRowContext(ctx, `SELECT sequence,payload,manifest,delivery_origin,upstream_capture_id,receipt,attempts FROM submissions
		WHERE state='queued' AND next_attempt<=? AND lease_until<=? ORDER BY next_attempt,sequence LIMIT 1`, t, t).
		Scan(&j.seq, &j.payload, &j.manifest, &j.origin, &j.upstream, &j.receipt, &j.attempts)
	if err != nil {
		return j, err
	}
	j.lease = randomHex(16)
	if _, err = tx.ExecContext(ctx, `UPDATE submissions SET lease_token=?,lease_until=?,code='delivering',updated_at=? WHERE sequence=?`, j.lease, t+leaseSeconds, stamp(), j.seq); err != nil {
		return j, err
	}
	return j, tx.Commit()
}

// update changes the row only while this attempt holds its lease.
func (w *worker) update(ctx context.Context, j job, set string, args ...any) error {
	args = append(args, j.seq, j.lease)
	//nolint:gosec // set is a constant column list from this file; values are bound
	res, err := w.st.db.ExecContext(ctx, `UPDATE submissions SET `+set+` WHERE sequence=? AND lease_token=? AND state='queued'`, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("delivery lease lost")
	}
	return nil
}

// held checks that the attempt still holds the row (the owner may have discarded it).
func (w *worker) held(ctx context.Context, j job) error {
	var one int
	return w.st.db.QueryRowContext(ctx, `SELECT 1 FROM submissions WHERE sequence=? AND state='queued' AND lease_token=? AND lease_until>?`, j.seq, j.lease, time.Now().Unix()).Scan(&one)
}

// failed records a failed attempt: a retryable one backs off from 5 s to 5 min, any other
// waits for the owner's review.
func (w *worker) failed(ctx context.Context, j job, cause error) error {
	code, state := "storage_unavailable", stateQueued
	var f *failure
	if errors.As(cause, &f) {
		code = f.Code
		if !f.Retryable {
			state = stateAttention
		}
	}
	delay := backoff(j.attempts)
	return w.update(ctx, j, `state=?,code=?,attempts=attempts+1,next_attempt=?,lease_token='',lease_until=0,updated_at=?`, state, code, time.Now().Unix()+int64(delay), stamp())
}

// backoff is the wait in seconds after a capture's nth failed attempt.
func backoff(attempts int) int { return min(maxBackoff, minBackoff*(1<<min(attempts, 6))) }

func (w *worker) deliver(ctx context.Context, j *job, fe, tokenFile string) error {
	if err := w.held(ctx, *j); err != nil {
		return err
	}
	// A manifest frozen for one FE is never sent to another.
	if j.manifest != "" && j.origin != fe {
		return fail("connection_changed", false)
	}
	if j.manifest == "" {
		var p Payload
		if json.Unmarshal([]byte(j.payload), &p) != nil {
			return fail("invalid_submission", false)
		}
		j.upstream = "surface-" + randomHex(13)
		j.origin = fe
		raw, err := manifest(j.upstream, sourceBase+strconv.FormatInt(j.seq, 10), p)
		if err != nil {
			return err
		}
		j.manifest = string(raw)
		if e := w.update(ctx, *j, `manifest=?,delivery_origin=?,upstream_capture_id=?`, j.manifest, j.origin, j.upstream); e != nil {
			return e
		}
	}
	bearer, err := readToken(tokenFile)
	if err != nil {
		return err
	}
	var r receipt
	if j.receipt == "" {
		// Set before the request: a lost answer may still mean FE saved it.
		if e := w.update(ctx, *j, `may_have_sent=1`); e != nil {
			return e
		}
		r, err = w.cl.accept(ctx, j.origin, bearer, j.upstream, []byte(j.manifest))
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(r)
		j.receipt = string(raw)
		if e := w.update(ctx, *j, `receipt=?`, j.receipt); e != nil {
			return e
		}
	} else if json.Unmarshal([]byte(j.receipt), &r) != nil || !r.valid(j.upstream) {
		return fail("invalid_receipt", false)
	}
	if e := w.held(ctx, *j); e != nil {
		return e
	}
	done, err := w.cl.complete(ctx, j.origin, bearer, r)
	if err != nil {
		return err
	}
	return w.update(ctx, *j, `state='saved',code='confirmed',fragment_id=?,fragment_revision_id=?,lease_token='',lease_until=0,updated_at=?`, done.FragmentID, done.RevisionID, stamp())
}
