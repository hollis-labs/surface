package browser

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

var errPrivateTarget = errors.New("browser: target must resolve exclusively to public addresses")

// Deny special-purpose IPv4 and IPv6 space, including translation/tunnel ranges.
// IsGlobalUnicast alone includes private, documentation and shared address space.
var deniedNetworks = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/3", "::/96", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20", "5f00::/16", "fc00::/7", "fe80::/10", "ff00::/8"} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

func publicAddress(ip netip.Addr, local []netip.Prefix) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.Zone() != "" || !ip.IsGlobalUnicast() {
		return false
	}
	for _, p := range deniedNetworks {
		if p.Contains(ip) {
			return false
		}
	}
	// Only currently allocated IPv6 global unicast space; fail closed for new ranges.
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, p := range local {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}
func interfaceNetworks() ([]netip.Prefix, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	var out []netip.Prefix
	for _, a := range addrs {
		p, e := netip.ParsePrefix(a.String())
		if e == nil {
			out = append(out, p.Masked())
		}
	}
	return out, nil
}

type publicTransport struct {
	lookup func(context.Context, string, string) ([]netip.Addr, error)
	dial   func(context.Context, string, string) (net.Conn, error)
	locals func() ([]netip.Prefix, error)
	slots  chan struct{}
}

func newPublicTransport() *publicTransport {
	return &publicTransport{lookup: net.DefaultResolver.LookupNetIP, dial: (&net.Dialer{Timeout: connectTimeout}).DialContext, locals: interfaceNetworks, slots: make(chan struct{}, maxFetchConcurrency)}
}
func validFetchURL(u *url.URL, public bool) error {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return errors.New("browser: expected an HTTP(S) URL without credentials")
	}
	if !public {
		return nil
	}
	if port := u.Port(); port != "" && port != "80" && port != "443" {
		return errors.New("browser: only public HTTP ports 80 and 443 are supported")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if ip, err := netip.ParseAddr(host); err == nil {
		if !publicAddress(ip, nil) {
			return errPrivateTarget
		}
		return nil
	}
	if !strings.Contains(host, ".") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".ts.net") {
		return errPrivateTarget
	}
	// Exclude ambiguous numeric IPv4 spellings and non-ASCII/escaped hostnames.
	allNumeric := true
	for _, r := range host {
		if (r < '0' || r > '9') && r != '.' {
			allNumeric = false
		}
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '.' && r != '-' {
			return errPrivateTarget
		}
	}
	last := host[strings.LastIndex(host, ".")+1:]
	if strings.HasPrefix(host, "0x") || (last != "" && last[0] >= '0' && last[0] <= '9') {
		return errPrivateTarget
	}
	if allNumeric {
		return errPrivateTarget
	}
	return nil
}
func (t *publicTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		return nil, errors.New("browser: guarded fetches are GET only")
	}
	if err := validFetchURL(req.URL, true); err != nil {
		return nil, err
	}
	select {
	case t.slots <- struct{}{}:
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
	// The slot remains held until the bounded body is closed.
	release := func() { <-t.slots }
	ctx, cancel := context.WithTimeout(req.Context(), dnsTimeout)
	ips, err := t.lookup(ctx, "ip", req.URL.Hostname())
	cancel()
	if err != nil {
		release()
		return nil, errors.New("browser: DNS lookup failed")
	}
	local, err := t.locals()
	if err != nil {
		release()
		return nil, errors.New("browser: cannot verify local networks")
	}
	if len(ips) == 0 {
		release()
		return nil, errPrivateTarget
	}
	for _, ip := range ips {
		if !publicAddress(ip, local) {
			release()
			return nil, errPrivateTarget
		}
	}
	port := req.URL.Port()
	if port == "" {
		port = "80"
		if req.URL.Scheme == "https" {
			port = "443"
		}
	}
	pinned := net.JoinHostPort(ips[0].Unmap().String(), port)
	tr := &http.Transport{Proxy: nil, DisableKeepAlives: true, DisableCompression: true, TLSHandshakeTimeout: connectTimeout, ResponseHeaderTimeout: resourceTimeout, MaxResponseHeaderBytes: maxResponseHeaderBytes, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: req.URL.Hostname()}, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return t.dial(ctx, "tcp", pinned) }}
	clean := cleanRequest(req)
	resp, err := tr.RoundTrip(clean)
	if err != nil {
		release()
		tr.CloseIdleConnections()
		return nil, errors.New("browser: connection failed")
	}
	resp.Header.Del("Set-Cookie")
	resp.Body = &releaseBody{ReadCloser: resp.Body, release: func() { release(); tr.CloseIdleConnections() }}
	return resp, nil
}
func cleanRequest(req *http.Request) *http.Request {
	r := req.Clone(req.Context())
	r.Header = make(http.Header)
	r.Host = ""
	r.Header.Set("User-Agent", userAgent)
	r.Header.Set("Accept", "text/html,text/css,image/*;q=0.9,*/*;q=0.5")
	r.Header.Set("Accept-Encoding", "identity")
	r.Header.Set("Accept-Language", "en")
	return r
}

// The owner path intentionally permits internal destinations, but never credentials/proxies.
type ownerTransport struct{ base *http.Transport }

func (t ownerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := validFetchURL(r.URL, false); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(cleanRequest(r))
}
func browserClient(public bool) *http.Client {
	var tr http.RoundTripper
	if public {
		tr = newPublicTransport()
	} else {
		tr = ownerTransport{base: &http.Transport{Proxy: nil, DisableCompression: true, TLSHandshakeTimeout: connectTimeout, ResponseHeaderTimeout: resourceTimeout}}
	}
	return &http.Client{Transport: tr, Timeout: resourceTimeout, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > maxRedirects {
			return fmt.Errorf("browser: redirect limit (%d) exceeded", maxRedirects)
		}
		return validFetchURL(r.URL, public)
	}}
}
