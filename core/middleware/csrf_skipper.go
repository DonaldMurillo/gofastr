package middleware

import (
	"net/http"
	"strings"
	"sync"
)

// CSRFSkipper holds path prefixes that bypass CSRF, matched literally against the escaped path, so a trailing slash pins a prefix to a directory; Add is safe while requests are served.
type CSRFSkipper struct {
	mu       sync.RWMutex
	prefixes []string
}

// NewCSRFSkipper returns an empty skipper.
func NewCSRFSkipper() *CSRFSkipper {
	return &CSRFSkipper{}
}

// Add registers path prefixes; it is safe to call concurrently with Skip.
func (s *CSRFSkipper) Add(prefixes ...string) {
	if len(prefixes) == 0 {
		return
	}
	s.mu.Lock()
	s.prefixes = append(s.prefixes, prefixes...)
	s.mu.Unlock()
}

// Skip reports whether the request's escaped path starts with a registered prefix.
func (s *CSRFSkipper) Skip(r *http.Request) bool {
	path := r.URL.EscapedPath()
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.prefixes {
		if strings.HasPrefix(path, p) { //gofastr:allow(GOFASTR1006) documented literal-prefix semantics: the trailing slash is the caller's boundary knob (see the CSRFSkipper doc)
			return true
		}
	}
	return false
}

// SkipAny returns a Skip predicate that is true when any of predicates is; with none it is always false.
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
