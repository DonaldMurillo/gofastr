package builtins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

// webFetchImpl performs an HTTP GET against a URL and returns the
// response body as text. Per the threat model:
//
//   - Authorization and X-Harness-Token headers are stripped from any
//     user-supplied URL (a malicious URL containing `?api_key=…` is
//     still possible — that's a separate user-data exfil hazard the
//     redaction middleware addresses on the response side).
//   - Refuses non-http(s) schemes (no file://, no gopher://).
//   - Hard 10s timeout, 5 MiB response cap.
type webFetchImpl struct {
	// HTTPClient is the client used for fetches. Tests inject a
	// stub; production wires the engine's shared client.
	HTTPClient *http.Client

	// AllowPrivateHosts disables the SSRF preflight that rejects
	// loopback/link-local/private/unspecified destinations on the
	// INITIAL URL. It exists for tests that must reach an httptest
	// server (which listens on loopback). It is false in production,
	// so production fails closed. Redirect targets are re-validated
	// on every hop regardless of this flag.
	AllowPrivateHosts bool
}

func (webFetchImpl) Name() string        { return "WebFetch" }
func (webFetchImpl) Description() string { return "Fetch the body of a URL via HTTP GET." }
func (webFetchImpl) Mutating() bool      { return false }
func (webFetchImpl) InputSchema() []byte {
	return []byte(`{
  "type": "object",
  "properties": {
    "url":     {"type": "string", "description": "HTTP(S) URL to fetch"},
    "headers": {"type": "object", "description": "Extra request headers", "additionalProperties": {"type": "string"}}
  },
  "required": ["url"],
  "additionalProperties": false
}`)
}

type webFetchArgs struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

const (
	webFetchTimeout = 10 * time.Second
	webFetchMaxBody = 5 << 20 // 5 MiB — raw download cap (DoS guard)
	// webFetchModelMaxBody is how much we surface to the LLM. 5 MiB
	// of HTML would silently blow past most model context windows
	// (GLM-5.1: 128K tokens ≈ 500 KiB), and the model returns an
	// empty response when overflowed — exactly the bug from
	// sess_01KSC4S1D. 32 KiB is enough text for the model to
	// understand a page; users can inspect the full body via the
	// session log.
	webFetchModelMaxBody = 32 << 10 // 32 KiB
)

// strippedHeaders is the set of headers the WebFetch tool refuses to
// forward. Names compared case-insensitively.
var strippedHeaders = map[string]struct{}{
	"authorization":   {},
	"x-harness-token": {},
	"cookie":          {}, // a tool-driven cookie request is almost never what the user wanted
}

func (w webFetchImpl) Run(ctx context.Context, call tool.ToolCall, _ tool.EventSink) (*tool.ToolResult, error) {
	var args webFetchArgs
	if err := json.Unmarshal(call.Input, &args); err != nil {
		return nil, fmt.Errorf("WebFetch: invalid arguments: %w", err)
	}
	if args.URL == "" {
		return nil, errors.New("WebFetch: url is required")
	}
	u, err := url.Parse(args.URL)
	if err != nil {
		return errorResult(fmt.Sprintf("WebFetch: invalid URL: %v", err)), nil
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return errorResult(fmt.Sprintf("WebFetch: scheme %q is not allowed (http/https only)", u.Scheme)), nil
	}

	// SSRF preflight on the initial URL. Resolve the host and reject
	// loopback/link-local/private/unspecified destinations. Skipped only
	// when AllowPrivateHosts is set (tests reaching httptest servers);
	// production fails closed. Redirect hops are re-validated below
	// regardless of this flag.
	if !w.AllowPrivateHosts {
		if err := assertPublicHost(u.Host); err != nil {
			return errorResult(fmt.Sprintf("WebFetch: refusing to reach internal address: %v", err)), nil
		}
	}

	client := w.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: webFetchTimeout}
	}
	// Re-validate every redirect target. We never relax this — a vetted
	// public URL must not be able to 302 us into the metadata service or
	// loopback, even when the test injects AllowPrivateHosts on the
	// initial hop. We copy the client so we don't mutate a shared one.
	safeClient := *client
	// Dial-time SSRF guard. The CheckRedirect / preflight checks validate
	// the host string and a *separate* resolution; the dialer re-resolves,
	// so a DNS-rebinding host (public at preflight, internal at dial) would
	// otherwise reach the internal IP. Reject the ACTUAL connected IP at
	// connect time — this closes rebinding on the initial fetch and every
	// redirect hop. Only installed when the client carries no custom
	// transport (tests that inject httptest.Client keep their transport).
	if safeClient.Transport == nil {
		safeClient.Transport = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   webFetchTimeout,
				KeepAlive: 30 * time.Second,
				Control:   ssrfDialControl,
			}).DialContext,
		}
	}
	safeClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if err := assertPublicHost(req.URL.Host); err != nil {
			return fmt.Errorf("redirect into internal address blocked: %w", err)
		}
		return nil
	}
	client = &safeClient
	reqCtx, cancel := context.WithTimeout(ctx, webFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, args.URL, nil)
	if err != nil {
		return errorResult(fmt.Sprintf("WebFetch: build request: %v", err)), nil
	}
	for k, v := range args.Headers {
		if _, stripped := strippedHeaders[strings.ToLower(k)]; stripped {
			// Skip; threat-model rule.
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return errorResult(fmt.Sprintf("WebFetch: %v", err)), nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, webFetchMaxBody))
	if err != nil {
		return errorResult(fmt.Sprintf("WebFetch: read body: %v", err)), nil
	}
	fullLen := len(body)
	if fullLen > webFetchModelMaxBody {
		body = body[:webFetchModelMaxBody]
	}
	header := fmt.Sprintf("HTTP %d %s\n", resp.StatusCode, resp.Status)
	suffix := ""
	if fullLen > webFetchModelMaxBody {
		suffix = fmt.Sprintf("\n\n[... truncated %d more bytes of body — total %d. Increase webFetchModelMaxBody if needed.]",
			fullLen-webFetchModelMaxBody, fullLen)
	}
	if resp.StatusCode >= 400 {
		return errorResult(header + string(body) + suffix), nil
	}
	return textResult(header + string(body) + suffix), nil
}

// ssrfDialControl is a net.Dialer.Control hook that rejects a
// connection whose resolved peer address is an internal IP. Because it
// runs AFTER name resolution but BEFORE the connect completes, it
// closes the DNS-rebinding TOCTOU that assertPublicHost (a separate,
// earlier resolution) cannot. address is "ip:port".
func ssrfDialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// Should not happen — Control receives an already-resolved
		// numeric address. Fail closed.
		return fmt.Errorf("unresolved dial address %q", address)
	}
	if isInternalIP(ip) {
		return fmt.Errorf("refusing to dial internal address %s", ip)
	}
	return nil
}

// assertPublicHost resolves host (host[:port]) and returns an error if
// it is missing, an outright internal IP literal, or resolves to any
// loopback/link-local/private/unspecified/multicast address. SSRF
// guard for WebFetch: a model-chosen or prompt-injected URL must not
// reach the cloud metadata service (169.254.169.254), loopback, or the
// host's private network. Fails closed on resolution error.
func assertPublicHost(host string) error {
	h := host
	if hostOnly, _, err := net.SplitHostPort(host); err == nil {
		h = hostOnly
	}
	h = strings.Trim(h, "[]")
	if h == "" {
		return errors.New("empty host")
	}
	// If the host is an IP literal, check it directly.
	if ip := net.ParseIP(h); ip != nil {
		if isInternalIP(ip) {
			return fmt.Errorf("%s is a non-public address", ip)
		}
		return nil
	}
	// Otherwise resolve and reject if ANY resolved address is internal.
	ips, err := net.LookupIP(h)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", h, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("no addresses for %q", h)
	}
	for _, ip := range ips {
		if isInternalIP(ip) {
			return fmt.Errorf("%s resolves to non-public address %s", h, ip)
		}
	}
	return nil
}

// cgnatRange is the RFC 6598 carrier-grade NAT block (100.64.0.0/10),
// which IsPrivate() does not cover but is non-routable internal space.
var cgnatRange = net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// isInternalIP reports whether ip is loopback, link-local, private,
// unspecified, multicast, or CGNAT (RFC 6598). IPv4-mapped IPv6
// addresses (`::ffff:a.b.c.d`) are normalized to their v4 form first
// so a mapped internal literal cannot slip past the v4 range checks.
func isInternalIP(ip net.IP) bool {
	// Normalize IPv4-mapped IPv6 to its 4-byte form so range checks
	// (IsPrivate / CGNAT) that key off the v4 representation apply.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsPrivate() {
		return true
	}
	if cgnatRange.Contains(ip) {
		return true
	}
	return false
}
