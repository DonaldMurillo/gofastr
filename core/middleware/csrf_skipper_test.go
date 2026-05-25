package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCSRFSkipper_PathPrefix_SkipsConfiguredPrefixes pins V3 #9: a
// host can list per-route CSRF exemptions without writing a closure
// that inspects r.URL.Path. Skipper.Add("/webhooks/") makes a POST to
// /webhooks/stripe bypass the CSRF check, while /save still enforces.
func TestCSRFSkipper_PathPrefix_SkipsConfiguredPrefixes(t *testing.T) {
	skipper := NewCSRFSkipper()
	skipper.Add("/webhooks/")

	mw := CSRF(CSRFConfig{
		SecretKey: mustRandomKey(),
		Skip:      skipper.Skip,
	})

	// /webhooks/* — must reach handler (no cookie, no token).
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/webhooks/stripe", nil))
	if rec.Code != http.StatusTeapot {
		t.Errorf("/webhooks/stripe POST: skipper should bypass CSRF, got %d / %s",
			rec.Code, rec.Body.String())
	}

	// /save — must 403 (no cookie).
	rec2 := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})).ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/save", nil))
	if rec2.Code != http.StatusForbidden {
		t.Errorf("/save POST should still 403, got %d", rec2.Code)
	}
}

// TestCSRFSkipper_AddMultiple covers the multi-path case central to
// V3 #9: a single skipper aggregates a list of exempted prefixes —
// the host registers them in one place instead of scattering closures.
func TestCSRFSkipper_AddMultiple(t *testing.T) {
	skipper := NewCSRFSkipper()
	skipper.Add("/webhooks/", "/health", "/api/v1/")

	for _, path := range []string{"/webhooks/x", "/health", "/api/v1/users"} {
		if !skipper.Skip(httptest.NewRequest(http.MethodPost, path, nil)) {
			t.Errorf("path %q should be skipped", path)
		}
	}
	for _, path := range []string{"/", "/api/v2/users", "/foo"} {
		if skipper.Skip(httptest.NewRequest(http.MethodPost, path, nil)) {
			t.Errorf("path %q should NOT be skipped", path)
		}
	}
}

// TestSkipAny_ComposesPredicates pins the composition helper: hosts
// can chain SkipBearerAuth() and a path-skipper without writing their
// own boolean glue.
func TestSkipAny_ComposesPredicates(t *testing.T) {
	skipper := NewCSRFSkipper()
	skipper.Add("/webhooks/")
	combined := SkipAny(SkipBearerAuth(), skipper.Skip)

	// Bearer-auth path — skipped via SkipBearerAuth.
	bearerReq := httptest.NewRequest(http.MethodPost, "/api/users", nil)
	bearerReq.Header.Set("Authorization", "Bearer xxx")
	if !combined(bearerReq) {
		t.Error("bearer-auth should be skipped via SkipBearerAuth")
	}

	// Webhook path — skipped via skipper.
	if !combined(httptest.NewRequest(http.MethodPost, "/webhooks/stripe", nil)) {
		t.Error("/webhooks/stripe should be skipped via path skipper")
	}

	// Plain form-POST — NOT skipped, falls through to CSRF enforcement.
	plain := httptest.NewRequest(http.MethodPost, "/save", strings.NewReader(""))
	if combined(plain) {
		t.Error("plain POST should not be skipped by SkipAny")
	}
}

// TestSkipAny_EmptyReturnsAlwaysFalse: zero predicates means
// nothing is skipped — safer default than panicking.
func TestSkipAny_EmptyReturnsAlwaysFalse(t *testing.T) {
	pred := SkipAny()
	if pred == nil {
		t.Fatal("SkipAny() should return a non-nil predicate")
	}
	if pred(httptest.NewRequest(http.MethodPost, "/", nil)) {
		t.Error("empty SkipAny should never skip")
	}
}

// TestCSRFSkipper_ConcurrentAddAndSkip guards against the obvious
// race: a long-lived skipper read by the CSRF middleware on every
// request while a startup goroutine is still adding prefixes
// (legitimate when batteries / plugins contribute exemptions in
// OnStart hooks). Mirrors the Router.Use concurrency guarantee.
func TestCSRFSkipper_ConcurrentAddAndSkip(t *testing.T) {
	skipper := NewCSRFSkipper()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			skipper.Add("/p" + string(rune('0'+(i%10))) + "/")
		}
		close(done)
	}()
	for i := 0; i < 1000; i++ {
		skipper.Skip(httptest.NewRequest(http.MethodPost, "/p3/foo", nil))
	}
	<-done
}
