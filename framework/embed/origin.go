package embed

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// NormalizeOrigin reduces an origin string to its canonical scheme://host[:port]
// form, or reports why it is not an origin at all.
//
// Normalization matters because an origin is a tuple and the customer types a
// string: https://acme.com, https://acme.com/, https://ACME.com and
// https://acme.com:443 are the same origin written four ways. Comparing the raw
// strings means a customer's trailing slash silently never matches and the embed
// "just doesn't work" with no diagnosable cause.
//
// Rejected outright: anything carrying a path, query, fragment or userinfo.
// Those are not part of an origin, and accepting them would mean two configs
// that differ only in ignored bytes compare unequal.
func NormalizeOrigin(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("embed: empty origin")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("embed: origin %q is not a URL: %w", raw, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("embed: origin %q must be http or https", raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("embed: origin %q has no host", raw)
	}
	if u.User != nil {
		return "", fmt.Errorf("embed: origin %q must not carry userinfo", raw)
	}
	if u.Path != "" && u.Path != "/" {
		return "", fmt.Errorf("embed: origin %q must not carry a path", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("embed: origin %q must not carry a query or fragment", raw)
	}
	// Opaque data ("https:foo") parses with an empty Host, already rejected
	// above, but be explicit that only the hierarchical form is an origin.
	if u.Opaque != "" {
		return "", fmt.Errorf("embed: origin %q must be an absolute http(s) origin", raw)
	}

	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", fmt.Errorf("embed: origin %q has no host", raw)
	}
	// url.Parse is permissive about what may sit in an authority — it happily
	// accepts "*.acme.com". An allowlist entry that looks like a wildcard but
	// is compared literally is the worst of both readings: the author believes
	// subdomains are covered and none are. Reject anything that is not a
	// literal hostname or IP.
	if !validOriginHost(host) {
		return "", fmt.Errorf("embed: origin %q has an invalid host — exact hosts only, no wildcards", raw)
	}
	// Ports are compared as NUMBERS, not as text. A browser serializes an
	// origin's port canonically, so ":0443" and ":443" both arrive as the
	// default-port-free form — while a config keeping ":0443" verbatim can
	// never match anything the browser sends. Same for a port outside the
	// valid range, which is not an origin at all.
	port := u.Port()
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("embed: origin %q has an invalid port", raw)
		}
		if (scheme == "https" && n == 443) || (scheme == "http" && n == 80) {
			port = ""
		} else {
			port = strconv.Itoa(n)
		}
	}
	// Canonicalize an IPv6 literal to the compressed form the browser sends.
	// "https://[0:0:0:0:0:0:0:1]" and "https://[::1]" are one origin; keeping
	// the expanded spelling means the frame's browser-attested origin never
	// matches the one the app minted for.
	if ip := net.ParseIP(host); ip != nil && strings.Contains(host, ":") {
		host = "[" + ip.String() + "]"
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		return scheme + "://" + host + ":" + port, nil
	}
	return scheme + "://" + host, nil
}

// validOriginHost reports whether host is a literal hostname or IP address.
// host arrives lowercased and with IPv6 brackets already stripped.
func validOriginHost(host string) bool {
	if len(host) > 253 {
		return false
	}
	// IPv6 literal: Hostname() strips the brackets, leaving colons behind.
	// net.ParseIP is the authority on the rest of the grammar.
	if strings.Contains(host, ":") {
		return net.ParseIP(host) != nil
	}
	// Trailing-dot FQDNs are the same name as their dotless form but a
	// different string, so they would split one origin into two allowlist
	// entries. Reject rather than silently strip.
	if strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return false
	}
	// A browser normalizes every numeric host to dotted-quad: "127.1",
	// "2130706433" and "0177.0.0.1" all serialize as "127.0.0.1". Go's URL
	// parser treats them as ordinary DNS labels, so such an allowlist entry is
	// accepted and then never matches. Rejecting is the honest answer — the
	// author gets an error at boot instead of an embed that silently never
	// completes its handshake.
	if looksNumericHost(host) && net.ParseIP(host) == nil {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			switch {
			case c >= 'a' && c <= 'z':
			case c >= '0' && c <= '9':
			case c == '-':
			default:
				// Includes '*', '_' and every non-ASCII byte. Internationalized
				// domains must be supplied in their punycode (xn--) form, which
				// is what the browser sends anyway.
				return false
			}
		}
	}
	return true
}

// looksNumericHost reports whether every label is decimal digits — the shape a
// browser reads as an IPv4 address however many parts it has.
func looksNumericHost(host string) bool {
	if host == "" {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" {
			return false
		}
		for i := 0; i < len(label); i++ {
			if label[i] < '0' || label[i] > '9' {
				return false
			}
		}
	}
	return true
}

// originSet is a normalized allowlist. Lookup is by normalized origin, so a
// caller must normalize the candidate first — Has does it for you.
type originSet struct {
	// order preserves declaration order so the frame-ancestors directive is
	// stable across boots (a shuffling CSP header defeats HTTP caching and
	// makes header diffs in tests meaningless).
	order []string
	set   map[string]struct{}
}

// newOriginSet normalizes and de-duplicates an allowlist.
//
// An empty allowlist is an error, never "allow everything": a surface with no
// declared framers is a configuration mistake, and the fail-open reading of it
// would publish the surface to the whole internet.
func newOriginSet(raw []string) (*originSet, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("embed: at least one allowed origin is required")
	}
	s := &originSet{set: make(map[string]struct{}, len(raw))}
	bytes := len("frame-ancestors")
	for _, r := range raw {
		o, err := NormalizeOrigin(r)
		if err != nil {
			return nil, err
		}
		if _, dup := s.set[o]; dup {
			continue
		}
		s.set[o] = struct{}{}
		s.order = append(s.order, o)
		bytes += len(o) + 1 // the separating space in the CSP directive
	}
	// Every origin is joined into ONE frame-ancestors directive on every shell
	// response, so the customer count is encoded directly into response-header
	// size. Past a few hundred origins that directive exceeds the response
	// header limits common in proxies and cloud load balancers, and the failure
	// is not graceful: the shell is rejected or the CSP is truncated, and the
	// surface stops loading for EVERY customer at once, including the ones that
	// worked yesterday.
	//
	// Refuse at boot with the arithmetic in the message rather than let it be
	// discovered by whichever customer's onboarding crossed the line.
	if bytes > maxFrameAncestorsBytes {
		return nil, fmt.Errorf(
			"embed: %d origins produce a %d-byte frame-ancestors directive, over the %d-byte "+
				"limit — that directive ships on every shell response and proxies reject or "+
				"truncate outsized response headers, which breaks the surface for every "+
				"customer at once. Split the customers across separate surfaces",
			len(s.order), bytes, maxFrameAncestorsBytes)
	}
	return s, nil
}

// maxFrameAncestorsBytes bounds the joined origin list. 4 KiB sits under the
// 8 KiB response-header ceiling common in proxies and cloud load balancers,
// leaving room for the rest of the CSP and the other response headers.
const maxFrameAncestorsBytes = 4 << 10

// Has reports whether candidate names an allowed origin. A candidate that is
// not a well-formed origin is not allowed — it is never normalized "close
// enough" into a match.
func (s *originSet) Has(candidate string) bool {
	if s == nil {
		return false
	}
	o, err := NormalizeOrigin(candidate)
	if err != nil {
		return false
	}
	_, ok := s.set[o]
	return ok
}

// List returns the normalized origins in declaration order.
func (s *originSet) List() []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s.order))
	copy(out, s.order)
	return out
}
