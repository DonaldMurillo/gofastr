package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// VerifiedCSRFSubmission answers only for a request the check passed: a
// re-dispatch presents what it returns, so a safe method or a skipped
// request must have nothing to present.
func TestVerifiedCSRFSubmission(t *testing.T) {
	type seen struct {
		header, token string
		ok            bool
	}
	var got seen
	record := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.header, got.token, got.ok = VerifiedCSRFSubmission(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
	skip := false
	h := CSRF(CSRFConfig{
		HeaderName: "X-Check",
		SecretKey:  []byte("0123456789abcdef0123456789abcdef"), // not-a-secret: test-only HMAC key
		Skip:       func(*http.Request) bool { return skip },
	})(record)
	c := csrfCookie(t, h)
	if got.ok {
		t.Fatalf("a GET reports a verified submission: %+v", got)
	}

	post := func() int {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.AddCookie(c)
		r.Header.Set("X-Check", MaskCSRFToken(c.Value))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if code := post(); code != http.StatusNoContent {
		t.Fatalf("POST with the token: status %d", code)
	}
	// The raw token, unmasked, under the header the check read.
	if want := (seen{"X-Check", c.Value, true}); got != want {
		t.Fatalf("verified POST = %+v, want %+v", got, want)
	}

	got = seen{}
	skip = true
	if code := post(); code != http.StatusNoContent {
		t.Fatalf("skipped POST: status %d", code)
	}
	if got.ok {
		t.Fatalf("a skipped POST reports a verified submission: %+v", got)
	}
	if _, _, ok := VerifiedCSRFSubmission(context.Background()); ok {
		t.Fatal("an empty context reports a verified submission")
	}
}
