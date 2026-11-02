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

// Header.Set canonicalizes its key, so a lowercase or mixed-case
// "vary" is the same clobbering write.
func TestVarySetAnyCaseIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"cache.go": `package middleware

import "net/http"

func mark(w http.ResponseWriter, h http.Header) {
	w.Header().Set("vary", "Accept")
	h.Set("VARY", "Cookie")
}
`,
	})
	if found := countRule(t, ds, contracts.RuleVarySet); len(found) != 2 {
		t.Fatalf("want 2 findings (lower + upper case), got %d: %v", len(found), found)
	}
}

// The clobber has two more spellings than Set (2026-11-02 guard bash):
// assigning the canonical map key outright replaces every entry the
// same way, and Del drops them all before any Add that follows. A map
// write under a non-canonical key ("vary") is a separate header line,
// not a clobber, and stays quiet.
func TestVaryMapWriteAndDelAreReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"cache.go": `package middleware

import "net/http"

func mark(w http.ResponseWriter, h http.Header) {
	w.Header()["Vary"] = []string{"Accept"}
	h.Del("vary")
	h.Add("Vary", "Cookie")
	h["vary"] = []string{"Cookie"}
}
`,
	})
	if found := countRule(t, ds, contracts.RuleVarySet); len(found) != 2 {
		t.Fatalf("want 2 findings (map write + Del), got %d: %v", len(found), found)
	}
}

// h["Vary"] = append(h["Vary"], v) is Add spelled as a map write: it
// keeps every entry the slice already held and clobbers nothing (the
// round-3 review's false positive). An append onto a DIFFERENT slice,
// or onto an empty one, still replaces what earlier middlewares added.
func TestVaryMapAppendToItselfIsQuiet(t *testing.T) {
	ds := fixture(t, map[string]string{
		"cache.go": `package middleware

import "net/http"

func mark(w http.ResponseWriter, h http.Header, other []string) {
	h["Vary"] = append(h["Vary"], "Origin")
	w.Header()["Vary"] = append(w.Header()["Vary"], "Accept", "Cookie")
	h["Vary"] = append(other, "Origin")
	h["Vary"] = append([]string(nil), "Origin")
	h["Vary"] = append(w.Header()["Vary"], "Origin")
}
`,
	})
	found := countRule(t, ds, contracts.RuleVarySet)
	if len(found) != 3 {
		t.Fatalf("want 3 findings (append onto another slice, onto nil, onto another header), got %d: %v", len(found), found)
	}
	for _, d := range found {
		if d.Line <= 7 {
			t.Errorf("self-append on line %d must stay quiet: %v", d.Line, d)
		}
	}
}
