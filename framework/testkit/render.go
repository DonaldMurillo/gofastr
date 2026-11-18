package testkit

import (
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/renderdiag"
)

// AllowRenderPanics returns next answering a recovered render panic with
// its production status instead of the test-only 500, until t's cleanup.
// It panics outside a test binary; never share the handler across tests.
func AllowRenderPanics(t testing.TB, next http.Handler) http.Handler {
	t.Helper()
	return renderdiag.ProductionStatus(t, next)
}
