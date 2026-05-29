package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Property: a session in the PendingTwoFactor (pre-step-up) state must not
// reach any 2FA self-service endpoint except /2fa/challenge. The pending
// state proves only the password, so it must not be able to disable, re-
// enroll, or refresh the second factor — doing so would defeat 2FA with
// the password alone (full account takeover).
//
// Surfaces: /2fa/disable, /2fa/enroll, /2fa/verify, /2fa/backup-codes.
// Each is reached via the same getSessionUser path, which previously did
// not inspect PendingTwoFactor. Contrast meHandler (gated) and the doc
// comment on Session.PendingTwoFactor ("ONLY valid for /auth/2fa/challenge").
func TestPendingTwoFA_MutationEndpointsRejected(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"disable", http.MethodPost, "/auth/2fa/disable", ""},
		{"enroll", http.MethodPost, "/auth/2fa/enroll", ""},
		{"verify", http.MethodPost, "/auth/2fa/verify", `{"code":"000000"}`},
		{"backup-codes", http.MethodGet, "/auth/2fa/backup-codes", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, twofa, r := setupP17(t)
			tok := loginP17(t, r) // pending-2FA session (password only)

			// Capture the victim's live secret to prove it is untouched.
			before, _ := twofa.store.GetTwoFA(context.Background(), "u-1")
			if before == nil || !before.Enabled {
				t.Fatalf("precondition: victim must have 2FA enabled")
			}

			var bodyReader *strings.Reader
			if tc.body != "" {
				bodyReader = strings.NewReader(tc.body)
			} else {
				bodyReader = strings.NewReader("")
			}
			req := httptest.NewRequest(tc.method, tc.path, bodyReader)
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			req.AddCookie(&http.Cookie{Name: "session_id", Value: tok})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusForbidden && w.Code != http.StatusUnauthorized {
				t.Fatalf("%s with pending session: expected 401/403, got %d (body=%s)",
					tc.path, w.Code, w.Body.String())
			}

			// The live second factor must survive unchanged.
			after, _ := twofa.store.GetTwoFA(context.Background(), "u-1")
			if after == nil || !after.Enabled {
				t.Fatalf("%s: pending session disabled the victim's live 2FA", tc.path)
			}
			if after.Secret != before.Secret {
				t.Fatalf("%s: pending session overwrote the victim's live 2FA secret", tc.path)
			}
			// Backup codes must not have been regenerated/returned.
			if strings.Contains(w.Body.String(), "backup_codes") {
				t.Fatalf("%s: pending session received fresh backup codes (2FA bypass)", tc.path)
			}
		})
	}
}
