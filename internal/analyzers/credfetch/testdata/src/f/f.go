// Package f pins the 2026-10-27 bug-bash credfetch gaps.
//
// I5: battery/log/webhook.go:92 builds its default client as
// &http.Client{Timeout: opts.Timeout} with no CheckRedirect, and
// postOnce (:233-252) copies every configured opts.Headers entry (an
// X-API-Key, say) onto the request by ranging over the map. The header
// name is a range key, not a constant, so the constant-only header read
// never saw a credential.
//
// I14: a CheckRedirect key whose value is nil follows redirects exactly
// like an unset one, and the key's presence alone read as a guard.
package f

import (
	"bytes"
	"context"
	"net/http"
	"time"
)

type webhookOpts struct {
	Timeout time.Duration
	Headers map[string]string
}

type webhookSink struct {
	url    string
	opts   webhookOpts
	client *http.Client
}

func newWebhookSink(url string, opts webhookOpts) *webhookSink {
	return &webhookSink{url: url, opts: opts, client: &http.Client{Timeout: opts.Timeout}} // want `no CheckRedirect on a credential-bearing fetch.* \(configured header map s.opts.Headers\)`
}

func (s *webhookSink) postOnce(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range s.opts.Headers {
		if k == "Content-Type" {
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// headerAdder takes its header map as an http.Header parameter and
// copies it with Add and an index write: the same forwarding.
func headerAdder(ctx context.Context, url string, extraHeaders http.Header) error {
	client := &http.Client{Timeout: time.Second} // want `no CheckRedirect on a credential-bearing fetch.* \(configured header map extraHeaders\)`
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for k, vs := range extraHeaders {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

func headerIndexed(ctx context.Context, url string, headers http.Header) error {
	client := &http.Client{} // want `no CheckRedirect on a credential-bearing fetch.* \(configured header map headers\)`
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header[k] = v
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// guardedWebhook is the fix: refuse redirects, keep forwarding the map.
func guardedWebhook(ctx context.Context, url string, opts webhookOpts) error {
	client := &http.Client{
		Timeout: opts.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// proxyForward ranges over the inbound request's own headers: a
// pass-through of a request this function did not configure, which a
// proxy owns differently. Quiet: the map is not configuration.
func proxyForward(ctx context.Context, url string, in *http.Request) error {
	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for k, v := range in.Header {
		req.Header[k] = v
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// labelsNotHeaders ranges a map whose name says nothing about headers
// and copies it into query parameters: quiet.
func labelsNotHeaders(ctx context.Context, url string, labels map[string]string) error {
	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	q := req.URL.Query()
	for k, v := range labels {
		q.Set(k, v)
	}
	req.URL.RawQuery = q.Encode()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// nilCheckRedirect: the key is present, its value follows redirects.
func nilCheckRedirect(ctx context.Context, url, token string) error {
	client := &http.Client{CheckRedirect: nil} // want `no CheckRedirect on a credential-bearing fetch.* \(Authorization header\)`
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// nilAssigned clears CheckRedirect after construction: still unset.
func nilAssigned(ctx context.Context, url, token string) error {
	client := &http.Client{} // want `no CheckRedirect on a credential-bearing fetch.* \(Authorization header\)`
	client.CheckRedirect = nil
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// nilReturned: a constructor whose only client carries a nil
// CheckRedirect proves an unset client at every caller.
func newNilClient() *http.Client {
	return &http.Client{CheckRedirect: nil} // want `no CheckRedirect on a credential-bearing fetch.* \(Authorization header\)`
}

func viaNilClient(ctx context.Context, url, token string) error {
	c := newNilClient()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// assignedGuard is the assignment spelling of the fix: quiet.
func assignedGuard(ctx context.Context, url, token string) error {
	client := &http.Client{}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// tagsIntoHeaders ranges a map whose name does not say headers: the
// rule will not guess that a tags map carries a credential. Quiet.
func tagsIntoHeaders(ctx context.Context, url string, tags map[string]string) error {
	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for k, v := range tags {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}
