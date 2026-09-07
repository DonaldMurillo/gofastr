package analyzers_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1413 exists because core/middleware/idempotency.go's
// body-too-large bypass arm wrote Set("Vary", "Idempotency-Key")
// over the CORS middleware's Add("Vary", "Origin") — the
// TestIdempotencyVaryEatsCors probe showed the composed response
// carrying ACAO while Vary listed only Idempotency-Key, so a shared
// cache had no way to know the body varied. Fixtures reduce the Set
// and pin the append-only posture.

// The bypass arm, reduced.
func TestVarySetIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"idempotency.go": `package middleware

import "net/http"

func bypass(w http.ResponseWriter) {
	w.Header().Set("Vary", "Idempotency-Key")
	w.Header().Set("Idempotent-Bypass", "body-too-large")
}
`,
	})
	d := assertHas(t, ds, contracts.RuleVarySet)
	if !strings.Contains(d.Message, "Add") {
		t.Errorf("message must name the append-only spelling: %q", d.Message)
	}
}

// Add is the posture every correct Vary writer uses (cors.go,
// wellknown.go, embed, uihost, the auth BFF); Set of another header
// is none of this rule's business.
func TestVaryAddAndOtherHeaderSetsAreQuiet(t *testing.T) {
	ds := fixture(t, map[string]string{
		"cors.go": `package middleware

import "net/http"

func cors(w http.ResponseWriter, origin string) {
	w.Header().Add("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Origin", origin)
}
`,
	})
	assertNot(t, ds, contracts.RuleVarySet,
		"Add is the append-only spelling and other headers are not Vary")
}
