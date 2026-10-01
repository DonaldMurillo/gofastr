package testkit

import (
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/renderdiag"
)

// AllowRenderPanics returns a handler that keeps production HTTP status behavior
// after recovered render panics. Use it only to test intentional panic recovery.
// It does not silence error logs or TestHarness failure reporters.
//
// The exemption ends at t.Cleanup; later requests through the returned handler
// again receive the test-only 500. Requests already admitted may finish with
// production status. Register server cleanup after calling this helper.
// It is safe with t.Parallel when each test owns its wrapper and server; never
// share the returned handler or server with tests that did not opt out.
// It panics outside a Go test binary. No request header or query opts out.
func AllowRenderPanics(t testing.TB, next http.Handler) http.Handler {
	t.Helper()
	return renderdiag.ProductionStatus(t, next)
}
