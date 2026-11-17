package embed

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// NormalizeOrigin reduces an origin string to its canonical scheme://host[:port]
// form, the one a browser serializes, or reports why it is not an origin: it
// refuses a path, query, fragment, userinfo, wildcard or invalid port.
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
	if u.Opaque != "" {
		return "", fmt.Errorf("embed: origin %q must be an absolute http(s) origin", raw)
	}

	host := asciicase.Lower(u.Hostname())
	if host == "" {
		return "", fmt.Errorf("embed: origin %q has no host", raw)
	}
	if !validOriginHost(host) {
		return "", fmt.Errorf("embed: origin %q has an invalid host: exact hosts only, no wildcards", raw)
	}
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
	if ip := net.ParseIP(host); ip != nil && strings.Contains(host, ":") {
		if ip.To4() != nil {
			return "", fmt.Errorf("embed: origin %q uses an IPv4-mapped IPv6 host; use the IPv4 origin or a plain IPv6 one", raw)
		}
		host = "[" + ip.String() + "]"
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		return scheme + "://" + host + ":" + port, nil
	}
	return scheme + "://" + host, nil
}

func validOriginHost(host string) bool {
	if len(host) > 253 {
		return false
	}
	if strings.Contains(host, ":") {
		return net.ParseIP(host) != nil
	}
	if strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return false
	}
	if endsInNumber(host) && net.ParseIP(host) == nil {
		return false
	}
	for label := range strings.SplitSeq(host, ".") {
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
				return false
			}
		}
	}
	return true
}

func endsInNumber(host string) bool {
	last := host[strings.LastIndexByte(host, '.')+1:]
	if last == "" {
		return false
	}
	digits, hex := last, false
	if rest, ok := strings.CutPrefix(last, "0x"); ok {
		digits, hex = rest, true
	}
	for i := 0; i < len(digits); i++ {
		c := digits[i]
		if (c < '0' || c > '9') && !(hex && c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

type originSet struct {
	order []string
	set   map[string]struct{}
}

const maxSourceRows = 4096

func normalizeOrigins(raw []string) (*originSet, int, error) {
	if len(raw) > maxSourceRows {
		return nil, 0, fmt.Errorf("embed: origin list has %d entries, over the %d-row limit", len(raw), maxSourceRows)
	}
	s := &originSet{set: make(map[string]struct{}, len(raw))}
	bytes := len("frame-ancestors")
	for _, r := range raw {
		o, err := NormalizeOrigin(r)
		if err != nil {
			return nil, 0, err
		}
		if _, dup := s.set[o]; dup {
			continue
		}
		s.set[o] = struct{}{}
		s.order = append(s.order, o)
		bytes += len(o) + 1 // the separating space in the CSP directive
	}
	return s, bytes, nil
}

func newOriginSet(raw []string) (*originSet, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("embed: at least one allowed origin is required")
	}
	s, bytes, err := normalizeOrigins(raw)
	if err != nil {
		return nil, err
	}
	if bytes > maxFrameAncestorsBytes {
		return nil, fmt.Errorf(
			"embed: %d origins produce a %d-byte frame-ancestors directive, over the %d-byte "+
				"limit: that directive ships on every shell response and proxies reject or "+
				"truncate outsized response headers, which breaks the surface for every "+
				"customer at once. Split the customers across separate surfaces",
			len(s.order), bytes, maxFrameAncestorsBytes)
	}
	return s, nil
}

func buildCustomerOriginSet(raw []string) (*originSet, error) {
	s, bytes, err := normalizeOrigins(raw)
	if err != nil {
		return nil, err
	}
	if len(s.order) == 0 {
		return nil, fmt.Errorf("embed: origin source returned no origins: a customer with no framers fails closed rather than widening to everyone")
	}
	if bytes > maxFrameAncestorsBytes {
		return nil, fmt.Errorf(
			"embed: customer produces a %d-byte frame-ancestors directive, over the %d-byte "+
				"limit: failing this customer closed rather than shipping a directive a proxy "+
				"will truncate or reject",
			bytes, maxFrameAncestorsBytes)
	}
	return s, nil
}

const maxFrameAncestorsBytes = 4 << 10

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

func (s *originSet) List() []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s.order))
	copy(out, s.order)
	return out
}
