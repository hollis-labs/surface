package app

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Serving beyond loopback, for an app with sign-in. Three settings, all from the environment
// (an installer or a service unit sets them), so the config stays simple:
//
//	<APP>_PUBLIC_URL      where people reach the app, e.g. https://app.example.com. Its
//	                      origin is accepted for sign-in, an https one makes the session
//	                      cookie Secure, its host is the passkey relying party, and the
//	                      page is served to that host name.
//	<APP>_TLS_CERT and    a certificate and key (PEM) to serve HTTPS with directly. They
//	<APP>_TLS_KEY         are read again when the files change (a renewal needs no restart).
//	<APP>_TRUSTED_PROXIES comma-separated CIDRs of a TLS-terminating proxy in front of the
//	                      app (a tunnel, tailscale serve, Caddy): only their
//	                      X-Forwarded-Proto is believed. 127.0.0.1/32 for one on this host.
//
// Behind a proxy set PUBLIC_URL to the proxy's https address and TRUSTED_PROXIES to where it
// connects from; serving TLS directly set PUBLIC_URL to the certificate's name.

// httpsSettings are the environment's serving settings.
type httpsSettings struct {
	publicURL         string
	certFile, keyFile string
	proxies           []string
}

func readHTTPS(name string, getenv func(string) string) (httpsSettings, error) {
	pre := envName(name) + "_"
	h := httpsSettings{
		publicURL: strings.TrimRight(strings.TrimSpace(getenv(pre+"PUBLIC_URL")), "/"),
		certFile:  strings.TrimSpace(getenv(pre + "TLS_CERT")),
		keyFile:   strings.TrimSpace(getenv(pre + "TLS_KEY")),
	}
	for _, p := range strings.Split(getenv(pre+"TRUSTED_PROXIES"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			h.proxies = append(h.proxies, p)
		}
	}
	if (h.certFile == "") != (h.keyFile == "") {
		return h, fmt.Errorf("%sTLS_CERT and %sTLS_KEY go together", pre, pre)
	}
	if h.publicURL != "" {
		u, err := url.Parse(h.publicURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return h, fmt.Errorf("%sPUBLIC_URL %q must be http(s)://host[:port]", pre, h.publicURL)
		}
	}
	return h, nil
}

// apply puts the settings into Substrate's config: identity.public_url and
// identity.trusted_proxies (an app without sign-in has no identity block and ignores them).
func (h httpsSettings) apply(cfg map[string]any) {
	id, ok := cfg["identity"].(map[string]any)
	if !ok {
		return
	}
	if h.publicURL != "" {
		if _, set := id["public_url"]; !set {
			id["public_url"] = h.publicURL
		}
	}
	if len(h.proxies) > 0 {
		list := make([]any, 0, len(h.proxies))
		for _, p := range h.proxies {
			list = append(list, p)
		}
		id["trusted_proxies"] = list
	}
}

// allowHost lets the core answer to the public URL's host name (its guard answers only IPs,
// localhost and listed names, against DNS rebinding).
func (h httpsSettings) allowHost() {
	if h.publicURL == "" {
		return
	}
	u, err := url.Parse(h.publicURL)
	if err != nil {
		return
	}
	host := strings.ToLower(u.Hostname())
	if net.ParseIP(host) != nil || host == "localhost" {
		return
	}
	cur := os.Getenv("SURFACE_ALLOWED_HOSTS")
	for _, h := range strings.Split(cur, ",") {
		if strings.EqualFold(strings.TrimSpace(h), host) {
			return
		}
	}
	if cur != "" {
		cur += ","
	}
	_ = os.Setenv("SURFACE_ALLOWED_HOSTS", cur+host)
}

// tlsListener listens on addr with TLS from the certificate files, re-reading them when they
// change (checked at most every 30 seconds), so a renewed certificate needs no restart.
func (h httpsSettings) tlsListener(addr string) (net.Listener, error) {
	if h.certFile == "" {
		return nil, nil
	}
	r := &certReloader{cert: h.certFile, key: h.keyFile}
	if _, err := r.load(); err != nil {
		return nil, fmt.Errorf("the TLS certificate: %w", err)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return tls.NewListener(ln, &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: r.get}), nil
}

type certReloader struct {
	cert, key string
	mu        sync.Mutex
	current   *tls.Certificate
	modified  time.Time
	checked   time.Time
}

func (r *certReloader) load() (*tls.Certificate, error) {
	c, err := tls.LoadX509KeyPair(r.cert, r.key)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(r.cert)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.current, r.modified, r.checked = &c, st.ModTime(), time.Now()
	r.mu.Unlock()
	return &c, nil
}

func (r *certReloader) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.Lock()
	cur, modified, due := r.current, r.modified, time.Since(r.checked) > 30*time.Second
	if due {
		r.checked = time.Now()
	}
	r.mu.Unlock()
	if due {
		if st, err := os.Stat(r.cert); err == nil && st.ModTime().After(modified) {
			if c, err := r.load(); err == nil {
				return c, nil
			}
		}
	}
	if cur == nil {
		return nil, errors.New("no certificate")
	}
	return cur, nil
}
