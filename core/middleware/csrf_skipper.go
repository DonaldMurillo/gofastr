package middleware

import (
	"net/http"
	"strings"
	"sync"
)

// CSRFSkipper accumulates path prefixes that should bypass the CSRF
// middleware. It composes into CSRFConfig.Skip via Skipper.Skip:
//
//	skipper := middleware.NewCSRFSkipper()
//	skipper.Add("/webhooks/", "/health")
//	mw := middleware.CSRF(middleware.CSRFConfig{
//	    SecretKey: secret,
//	    Skip:      middleware.SkipAny(middleware.SkipBearerAuth(), skipper.Skip),
//	})
//
// Adding paths after the middleware is constructed is safe; Skip
// reads under an RWMutex, so plugins / OnStart hooks can register
// per-route exemptions late and the next request honors them. This is
// the per-route-skip surface called out in V3 #9: hosts list their
// exemptions centrally instead of scattering closures that inspect
// r.URL.Path.
//
// Path prefix matching is literal string-prefix (no globbing). A
// trailing "/" pins the prefix to a directory; without it a registered
// "/api" also skips "/apis/v1/...". Be deliberate, and prefer the
// trailing-slash form unless you specifically want the broader match.
type CSRFSkipper struct {
	mu       sync.RWMutex
	prefixes []string
}

// NewCSRFSkipper returns an empty skipper. Callers register prefixes
// with Add and pass Skip to CSRFConfig.Skip (typically via SkipAny).
func NewCSRFSkipper() *CSRFSkipper {
	return &CSRFSkipper{}
}

// Add registers one or more path prefixes for CSRF bypass. Safe to
// call concurrently with Skip.
func (s *CSRFSkipper) Add(prefixes ...string) {
	if len(prefixes) == 0 {
		return
	}
	s.mu.Lock()
	s.prefixes = append(s.prefixes, prefixes...)
	s.mu.Unlock()
}

// Skip reports whether the request path starts with any registered
// prefix. Use as CSRFConfig.Skip directly when no other predicates
// apply, or compose with SkipAny.
//
// SECURITY: the comparison is against r.URL.EscapedPath(), the path AS
// SENT, not the percent-decoded r.URL.Path. An exemption decision has
// to be made on the same bytes the router matched, and those differ:
// Go's mux matches on decoded segments but does not redirect when %2F
// or %2E leave the raw and decoded forms different. Matching the
// decoded path let "POST /api%2f..%2f..%2fadmin/wipe" present
// "/api/../../admin/wipe" here, inheriting the "/api/" exemption,
// while the mux dispatched whatever wildcard route actually matched.
// Comparing the escaped form fails closed: an encoded separator no
// longer reads as a directory boundary, so the request keeps its CSRF
// requirement. Ordinary encodings inside a genuinely-matching prefix
// (e.g. "/api/foo%20bar") are unaffected.
func (s *CSRFSkipper) Skip(r *http.Request) bool {
	path := r.URL.EscapedPath()
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// SkipAny returns a Skip predicate that reports true when ANY of the
// passed predicates does. Lets hosts compose CSRFSkipper.Skip alongside
// SkipBearerAuth without writing their own boolean glue. A zero-arg
// call returns a predicate that always reports false (no skips).
func SkipAny(predicates ...func(*http.Request) bool) func(*http.Request) bool {
	if len(predicates) == 0 {
		return func(*http.Request) bool { return false }
	}
	preds := make([]func(*http.Request) bool, len(predicates))
	copy(preds, predicates)
	return func(r *http.Request) bool {
		for _, p := range preds {
			if p != nil && p(r) {
				return true
			}
		}
		return false
	}
}
