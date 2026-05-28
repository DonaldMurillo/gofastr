package integration_test

import (
	"io"
	"net/http"
	"testing"
)

// Per-test HTTP clients with controlled connection lifecycles.
//
// Why this exists: the integration suite spins up many httptest servers
// across parallel test packages. Each test that used http.Get /
// http.Post / http.DefaultClient.Do dialed through the process-wide
// http.DefaultClient, whose Transport pools connections per host. Under
// `go test ./...` parallel-package execution on macOS, idle pooled
// connections from finished tests plus client-side TCP TIME_WAIT
// entries accumulate, exhaust the 49152-65535 ephemeral port range
// faster than TIME_WAIT clears (15s), and surface intermittently as
// `dial tcp 127.0.0.1:NNNNN: connect: can't assign requested address`.
//
// kilnHTTPClient(t) returns a fresh client whose Transport's
// CloseIdleConnections fires at test cleanup, so the connections this
// test allocated release promptly instead of riding the process-wide
// pool. Use kilnGet / kilnPost as drop-in shorthands.
//
// Note: a separate test-only helper `httpGet(t, url) (string, error)`
// already exists in browser_test.go for "fetch body as string" — these
// helpers are the `(*http.Response, error)` variants used by tests
// that need to inspect headers, status, or stream the body.

func kilnHTTPClient(t *testing.T) *http.Client {
	t.Helper()
	tr := &http.Transport{}
	t.Cleanup(tr.CloseIdleConnections)
	return &http.Client{Transport: tr}
}

func kilnGet(t *testing.T, url string) (*http.Response, error) {
	return kilnHTTPClient(t).Get(url)
}

func kilnPost(t *testing.T, url, contentType string, body io.Reader) (*http.Response, error) {
	return kilnHTTPClient(t).Post(url, contentType, body)
}
