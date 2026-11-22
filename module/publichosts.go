package module

import (
	"net"
	"regexp"
	"strings"
)

// PublicHosts lets a module have the app answer at more host names, for its module.Public
// routes only, while it runs. The app's host guard (against DNS rebinding) otherwise answers
// only to IPs, localhost and the names in SURFACE_ALLOWED_HOSTS. A module uses this when a name
// it no longer serves is still dialed by its own clients: a hub whose public URL changed keeps
// taking its runners' connections at the old name until they have moved, then drops it.
//
// Only use names the app's operator configured (a previous public URL's), never one a request
// supplied. The page, sign-in and every route not marked module.Public are not answered at them.
type PublicHosts interface {
	// Set replaces the names this module adds (host names, no port), live. Nil removes them.
	// Invalid names (IPs need no listing; wildcards, ports and empty names are refused) are
	// skipped and returned.
	Set(names []string) (skipped []string)
}

type publicHostsProvider interface{ PublicHosts() PublicHosts }

// PublicHostsOf returns the host's PublicHosts, and false when the app has none (an older core,
// or a test host without it).
func PublicHostsOf(h Host) (PublicHosts, bool) {
	p, ok := h.(publicHostsProvider)
	if !ok {
		return nil, false
	}
	ph := p.PublicHosts()
	return ph, ph != nil
}

var hostNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?)*$`)

// NormalHostName is a host name as PublicHosts compares it (lowercase, no trailing dot), and
// false for anything that is not a plain DNS name (an IP, a port, a wildcard, empty).
func NormalHostName(h string) (string, bool) {
	h = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(h), "."))
	if h == "" || len(h) > 253 || net.ParseIP(h) != nil || !hostNameRe.MatchString(h) {
		return "", false
	}
	return h, true
}
