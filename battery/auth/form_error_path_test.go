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
	writeFormAuthError(rec, req, http.StatusUnauthorized, reasonInvalidCredentials)

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
// atomic in Go's memory model, concurrent setters can produce a
// torn read that panics on bounds-check.
func TestDefaultLoginErrorPath_ConcurrentSettersDontRace(t *testing.T) {
	prev := getDefaultLoginErrorPath()
	t.Cleanup(func() { SetDefaultLoginErrorPath(prev) })

	done := make(chan struct{})
	for i := range 100 {
		go func(i int) {
			SetDefaultLoginErrorPath("/path-" + string(rune('a'+i%26)))
			_ = getDefaultLoginErrorPath()
			done <- struct{}{}
		}(i)
	}
	for range 100 {
		<-done
	}
}

func TestFormAuthError_NoConfigStillFallsToJSON(t *testing.T) {
	SetDefaultLoginErrorPath("")

	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	rec := httptest.NewRecorder()
	writeFormAuthError(rec, req, http.StatusUnauthorized, reasonInvalidCredentials)

	if rec.Code == http.StatusSeeOther {
		t.Errorf("status = 303 when no fallback configured; want JSON 401")
	}
	if !strings.Contains(rec.Body.String(), "invalid_credentials") {
		t.Errorf("expected JSON error in body, got: %s", rec.Body.String())
	}
}

func TestFormAuthError_QueryBeforeFragment(t *testing.T) {
	t.Cleanup(func() {
		SetDefaultLoginErrorPath("")
		SetDefaultRegisterErrorPath("")
	})

	cases := []struct {
		name     string
		register bool
		fallback string
		next     string
		want     string
	}{
		{
			name:     "login fragment only",
			fallback: "/auth#login",
			want:     "/auth?error=invalid_credentials#login",
		},
		{
			name:     "login fragment with next",
			fallback: "/auth#login",
			next:     "/dash",
			want:     "/auth?error=invalid_credentials&next=%2Fdash#login",
		},
		{
			name:     "login query and fragment",
			fallback: "/auth?tab=1#login",
			want:     "/auth?error=invalid_credentials&tab=1#login",
		},
		{
			name:     "question mark inside fragment",
			fallback: "/auth#step?a=1",
			want:     "/auth?error=invalid_credentials#step?a=1",
		},
		{
			name:     "register fragment only",
			register: true,
			fallback: "/auth#register",
			want:     "/auth?error=invalid_credentials#register",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.register {
				SetDefaultLoginErrorPath("")
				SetDefaultRegisterErrorPath(tc.fallback)
			} else {
				SetDefaultLoginErrorPath(tc.fallback)
				SetDefaultRegisterErrorPath("")
			}
			req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
			if tc.register {
				req = asRegisterForm(req)
			}
			rec := httptest.NewRecorder()
			redirectFormAuthError(rec, req, http.StatusUnauthorized, reasonInvalidCredentials, tc.next)

			if rec.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want 303", rec.Code)
			}
			if got := rec.Header().Get("Location"); got != tc.want {
				t.Errorf("Location = %q, want %q", got, tc.want)
			}
		})
	}
}
