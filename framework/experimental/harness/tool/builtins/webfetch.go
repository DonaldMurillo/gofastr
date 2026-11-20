package builtins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/DonaldMurillo/gofastr/core/netguard"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

type webFetchImpl struct {
	// HTTPClient is copied for each fetch; nil uses a default client with the guarded transport.
	HTTPClient *http.Client
	// AllowPrivateHosts skips the first URL's preflight and keeps a host *http.Transport unguarded, for tests; redirects are still checked.
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
	webFetchTimeout      = 10 * time.Second
	webFetchMaxBody      = 5 << 20
	webFetchModelMaxBody = 32 << 10
)

var strippedHeaders = map[string]struct{}{
	"authorization":   {},
	"x-harness-token": {},
	"cookie":          {},
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

	if !w.AllowPrivateHosts {
		if err := assertPublicHost(u.Host); err != nil {
			return errorResult(fmt.Sprintf("WebFetch: refusing to reach internal address: %v", err)), nil
		}
	}

	client, err := fetchClient(w.HTTPClient, w.AllowPrivateHosts)
	if err != nil {
		return errorResult(fmt.Sprintf("WebFetch: %v", err)), nil
	}
	reqCtx, cancel := context.WithTimeout(ctx, webFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, args.URL, nil)
	if err != nil {
		return errorResult(fmt.Sprintf("WebFetch: build request: %v", err)), nil
	}
	for k, v := range args.Headers {
		if _, stripped := strippedHeaders[strings.ToLower(k)]; stripped {
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
		suffix = fmt.Sprintf("\n\n[... truncated %d more bytes of body, total %d. Increase webFetchModelMaxBody if needed.]",
			fullLen-webFetchModelMaxBody, fullLen)
	}
	if resp.StatusCode >= 400 {
		return errorResult(header + string(body) + suffix), nil
	}
	return textResult(header + string(body) + suffix), nil
}

func fetchClient(client *http.Client, allowPrivate bool) (*http.Client, error) {
	if client == nil {
		client = &http.Client{Timeout: webFetchTimeout}
	}
	safeClient := *client
	switch tr := safeClient.Transport.(type) {
	case nil:
		safeClient.Transport = webFetchTransport()
	case *http.Transport:
		if !allowPrivate {
			guarded, ok := netguard.GuardPlainTransport(tr, "WebFetch")
			if !ok {
				return nil, errors.New("the HTTP client's transport has its own proxy or dialer, which the dial-time address check cannot reach")
			}
			safeClient.Transport = guarded
		}
	default:
		if !allowPrivate {
			return nil, fmt.Errorf("the HTTP client's transport %T cannot carry the dial-time address check; use a nil or *http.Transport", tr)
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
	return &safeClient, nil
}

func webFetchTransport() *http.Transport {
	return &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   webFetchTimeout,
			KeepAlive: 30 * time.Second,
			Control:   ssrfDialControl,
		}).DialContext,
	}
}

func ssrfDialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("unresolved dial address %q", address)
	}
	if isInternalIP(ip) {
		return fmt.Errorf("refusing to dial internal address %s", ip)
	}
	return nil
}

func assertPublicHost(host string) error {
	h := host
	if hostOnly, _, err := net.SplitHostPort(host); err == nil {
		h = hostOnly
	}
	h = strings.Trim(h, "[]")
	if h == "" {
		return errors.New("empty host")
	}
	if ip := net.ParseIP(h); ip != nil {
		if isInternalIP(ip) {
			return fmt.Errorf("%s is a non-public address", ip)
		}
		return nil
	}
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

func isInternalIP(ip net.IP) bool {
	return netguard.IsInternal(ip)
}
