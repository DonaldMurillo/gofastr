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
// Adding paths after the middleware is constructed is safe — Skip
// reads under an RWMutex, so plugins / OnStart hooks can register
// per-route exemptions late and the next request honors them. This is
// the per-route-skip surface called out in V3 #9: hosts list their
// exemptions centrally instead of scattering closures that inspect
// r.URL.Path.
//
// Path prefix matching is literal string-prefix (no globbing). A
// trailing "/" pins the prefix to a directory; without it a registered
// "/api" also skips "/apis/v1/...". Be deliberate — and prefer the
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

// Skip reports whether r.URL.Path starts with any registered prefix.
// Use as CSRFConfig.Skip directly when no other predicates apply, or
// compose with SkipAny.
func (s *CSRFSkipper) Skip(r *http.Request) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.prefixes {
		if strings.HasPrefix(r.URL.Path, p) {
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
