package browser

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
)

type releaseBody struct {
	io.ReadCloser
	once    sync.Once
	release func()
}

func (b *releaseBody) Close() error { err := b.ReadCloser.Close(); b.once.Do(b.release); return err }
func limitedBytes(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("browser: response exceeds size limit")
	}
	return b, nil
}
func responseBytes(resp *http.Response, limit int64) ([]byte, error) {
	raw, err := limitedBytes(resp.Body, limit)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))) {
	case "", "identity":
		return raw, nil
	case "gzip":
		gz, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		defer func() { _ = gz.Close() }()
		return limitedBytes(gz, limit)
	default:
		return nil, errors.New("browser: unsupported content encoding")
	}
}

// Transport errors can embed an entire redirect URL; expose only safe categories.
func fetchError(err error) error {
	for _, safe := range []error{errPrivateTarget, context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, safe) {
			return safe
		}
	}
	return errors.New("browser: fetch failed (connection, TLS, URL or redirect policy)")
}
