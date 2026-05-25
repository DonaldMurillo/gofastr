package builtins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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

	client := w.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: webFetchTimeout}
	}
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
