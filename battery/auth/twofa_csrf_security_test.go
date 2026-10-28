package auth

// CSRF posture of the bodyless 2FA routes.
//
// /2fa/disable, /2fa/backup-codes and /2fa/enroll read no body, so a
// cross-site page could auto-submit them with no Content-Type (a
// CORS-simple request) while the victim's stepped-up session cookie rode
// along: one hidden form turned the victim's second factor off, burned
// their backup codes, or parked a pending secret on the account
// (2026-10-27 A36). /2fa/verify and /2fa/challenge demand a JSON body,
// which a cross-site page cannot send without a preflight.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

func TestTwoFARoutesRejectCrossSite(t *testing.T) {
	_, twofa, r := setupP17(t)
	session := loginP17(t, r)
	state, err := twofa.store.GetTwoFA(context.Background(), "u-1")
	if err != nil || state == nil {
		t.Fatalf("setup: GetTwoFA = %v, %v", state, err)
	}
	body, _ := json.Marshal(map[string]string{"code": GenerateTOTP(state.Secret, uint64(time.Now().Unix())/30)})
	req := httptest.NewRequest(http.MethodPost, "/auth/2fa/challenge", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: session})
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("setup: challenge = %d (body=%s)", rr.Code, rr.Body.String())
	}
	for _, c := range rr.Result().Cookies() {
		if c.Name == "session_id" && c.Value != "" {
			session = c.Value
		}
	}

	post := func(path string, crossSite bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(&http.Cookie{Name: "session_id", Value: session})
		req.Header.Set("Sec-Fetch-Mode", "navigate")
		if crossSite {
			req.Header.Set("Origin", "https://evil.example")
			req.Header.Set("Sec-Fetch-Site", "cross-site")
		} else {
			req.Header.Set("Origin", "http://example.com")
			req.Header.Set("Sec-Fetch-Site", "same-origin")
		}
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		return rr
	}
	current := func() *TwoFAState {
		t.Helper()
		s, err := twofa.store.GetTwoFA(context.Background(), "u-1")
		if err != nil {
			t.Fatalf("GetTwoFA: %v", err)
		}
		return s
	}

	before := current()
	for _, path := range []string{"/auth/2fa/backup-codes", "/auth/2fa/disable"} {
		if rr := post(path, true); rr.Code != http.StatusForbidden {
			t.Errorf("cross-site POST %s = %d, want 403", path, rr.Code)
		}
	}
	if after := current(); after == nil || !after.Enabled || !slices.Equal(after.BackupCodes, before.BackupCodes) {
		t.Fatalf("SECURITY: a cross-site POST changed the factor: %+v", after)
	}

	// Positive controls: the same session, same origin, does both.
	if rr := post("/auth/2fa/backup-codes", false); rr.Code != http.StatusOK {
		t.Fatalf("same-origin backup-codes = %d (body=%s), want 200", rr.Code, rr.Body.String())
	}
	if rr := post("/auth/2fa/disable", false); rr.Code != http.StatusOK {
		t.Fatalf("same-origin disable = %d (body=%s), want 200", rr.Code, rr.Body.String())
	}

	if rr := post("/auth/2fa/enroll", true); rr.Code != http.StatusForbidden {
		t.Errorf("cross-site POST /auth/2fa/enroll = %d, want 403", rr.Code)
	}
	if s := current(); s != nil {
		t.Fatalf("SECURITY: a cross-site enroll stored a secret: %+v", s)
	}
	if rr := post("/auth/2fa/enroll", false); rr.Code != http.StatusOK {
		t.Fatalf("same-origin enroll = %d (body=%s), want 200", rr.Code, rr.Body.String())
	}
}
