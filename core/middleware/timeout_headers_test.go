package middleware

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

// TestTimeoutKeepsUpstreamHeaders: the handler behind Timeout works on
// the response headers earlier middleware set, the way it would with no
// timeout in the chain. A handler that read an empty map could not patch
// the security policy (an inlined page then shipped blocks the policy
// refused), its Vary Add replaced the upstream Vary instead of adding to
// it, and its Del of an upstream header did nothing.
func TestTimeoutKeepsUpstreamHeaders(t *testing.T) {
	var seen string
	h := Timeout(time.Second)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		seen = w.Header().Get("Content-Security-Policy")
		w.Header().Set("Content-Security-Policy", seen+"; style-src 'self'")
		w.Header().Add("Vary", "Cookie")
		w.Header().Del("X-Upstream")
		_, _ = w.Write([]byte("ok"))
	}))
	w := httptest.NewRecorder()
	w.Header().Set("Content-Security-Policy", "default-src 'self'")
	w.Header().Set("Vary", "Accept-Encoding")
	w.Header().Set("X-Upstream", "1")
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))

	if seen != "default-src 'self'" {
		t.Errorf("handler saw policy %q, want the upstream one", seen)
	}
	if got := w.Header().Get("Content-Security-Policy"); got != "default-src 'self'; style-src 'self'" {
		t.Errorf("policy = %q", got)
	}
	if got := w.Header().Values("Vary"); !slices.Equal(got, []string{"Accept-Encoding", "Cookie"}) {
		t.Errorf("Vary = %q, want the upstream value kept", got)
	}
	if w.Header().Get("X-Upstream") != "" {
		t.Error("the handler's Del of an upstream header was undone")
	}
}

// TestTimeoutStreamKeepsUpstreamHeaders: the same on the streaming path,
// where the first Flush commits the headers.
func TestTimeoutStreamKeepsUpstreamHeaders(t *testing.T) {
	h := Timeout(time.Second)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Vary", "Cookie")
		w.Header().Del("X-Upstream")
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte("data: x\n\n"))
	}))
	w := httptest.NewRecorder()
	w.Header().Set("Vary", "Accept-Encoding")
	w.Header().Set("X-Upstream", "1")
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if got := w.Header().Values("Vary"); !slices.Equal(got, []string{"Accept-Encoding", "Cookie"}) {
		t.Errorf("Vary = %q", got)
	}
	if w.Header().Get("X-Upstream") != "" {
		t.Error("the handler's Del of an upstream header was undone")
	}
}
