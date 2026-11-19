package reader

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// Feeds are fetched from public addresses only, on ports 80 and 443, with no cookies and no
// referrer: a feed's address (or a redirect) that names a private or local address, or one
// whose name resolves to one, is refused at the connection. Ported from Basalisk's media
// client (internal/api/frag_media.go).

var (
	errNotPublic = errors.New("not a public address")
	errPort      = errors.New("port not allowed")
)

// Not public though not in Go's private ranges: carrier-grade NAT (and tailnets), IETF protocol
// assignments, benchmarking, and the NAT64 prefix.
var blockedPrefixes = []netip.Prefix{
	netip.PrefixFrom(netip.AddrFrom4([4]byte{100, 64, 0, 0}), 10), // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("64:ff9b::/96"),
	// Addresses that carry an IPv4 address inside them, or reach this host: blocked like the
	// address they carry could be.
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("::/96"),          // IPv4-compatible (::127.0.0.1)
	netip.MustParsePrefix("2002::/16"),      // 6to4
	netip.MustParsePrefix("64:ff9b:1::/48"), // local-use NAT64
}

// publicAddr says whether a feed may be fetched from an address: public unicast only.
func publicAddr(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

// publicClient is the feed fetcher's client.
func publicClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		// Checked on the address actually connected to, after DNS, on every connection (so on
		// every redirect hop too).
		Control: func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return errNotPublic
			}
			if p := ap.Port(); p != 80 && p != 443 {
				return errPort
			}
			if !publicAddr(ap.Addr()) {
				return errNotPublic
			}
			return nil
		},
	}
	transport := &http.Transport{Proxy: nil, DialContext: dialer.DialContext, ForceAttemptHTTP2: true, MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second}
	return &http.Client{
		Timeout:   45 * time.Second,
		Transport: transport,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if len(via) >= 5 || (r.URL.Scheme != "http" && r.URL.Scheme != "https") || r.URL.User != nil {
				return errors.New("redirect refused")
			}
			r.Header.Del("Referer")
			return nil
		},
	}
}
