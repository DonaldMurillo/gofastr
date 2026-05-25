package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestFormAuthError_FallsBackToConfiguredPath pins the operator-visible
// fix: when there's no usable Referer (e.g. browser with no-referrer
// policy), writeFormAuthError must NOT emit raw JSON in the response
// body. Instead, redirect to a configured login-error path that the
// operator wires up.
func TestFormAuthError_FallsBackToConfiguredPath(t *testing.T) {
	SetDefaultLoginErrorPath("/login") // operator-configured fallback
	t.Cleanup(func() { SetDefaultLoginErrorPath("") })

	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	// no Referer header
	rec := httptest.NewRecorder()
	writeFormAuthError(rec, req, http.StatusUnauthorized, "invalid_credentials")

	if rec.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want 303 (redirect to login-error path)", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/login") {
		t.Errorf("Location = %q, want /login...", loc)
	}
	if !strings.Contains(loc, "error=invalid_credentials") {
		t.Errorf("error code missing from fallback redirect: %q", loc)
	}
	// And the body should NOT contain raw JSON the user sees as a page.
	if strings.Contains(rec.Body.String(), `"error"`) {
		t.Errorf("body emitted JSON instead of redirect: %s", rec.Body.String())
	}
}

// TestFormAuthError_NoConfigStillFallsToJSON confirms the legacy path
// when the host hasn't configured a login-error path: emit JSON (the
// pre-fix behaviour), keeping backward compatibility.
// TestDefaultLoginErrorPath_ConcurrentSettersDontRace pins the
// race-free contract of SetDefaultLoginErrorPath. Run with
// `go test -race` to confirm. A plain string assignment is not
// atomic in Go's memory model — concurrent setters can produce a
// torn read that panics on bounds-check.
func TestDefaultLoginErrorPath_ConcurrentSettersDontRace(t *testing.T) {
	prev := getDefaultLoginErrorPath()
	t.Cleanup(func() { SetDefaultLoginErrorPath(prev) })

	done := make(chan struct{})
	for i := 0; i < 100; i++ {
		go func(i int) {
			SetDefaultLoginErrorPath("/path-" + string(rune('a'+i%26)))
			_ = getDefaultLoginErrorPath()
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 100; i++ {
		<-done
	}
}

func TestFormAuthError_NoConfigStillFallsToJSON(t *testing.T) {
	SetDefaultLoginErrorPath("")

	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	rec := httptest.NewRecorder()
	writeFormAuthError(rec, req, http.StatusUnauthorized, "invalid_credentials")

	if rec.Code == http.StatusSeeOther {
		t.Errorf("status = 303 when no fallback configured; want JSON 401")
	}
	if !strings.Contains(rec.Body.String(), "invalid_credentials") {
		t.Errorf("expected JSON error in body, got: %s", rec.Body.String())
	}
}
