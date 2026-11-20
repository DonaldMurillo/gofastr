package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTwoFAEnrollRefusesOverwritingEnabledFactor(t *testing.T) {
	mgr, store, users, cookie := newTwoFATestEnv(t)
	ctx := context.Background()
	u, _, err := users.FindByEmail(ctx, "alice@test.com")
	if err != nil {
		t.Fatal(err)
	}
	live := &TwoFAState{Secret: "LIVESECRET", Enabled: true}
	if err := store.SetTwoFA(ctx, u.GetID(), live); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SessionStore().(SessionTwoFAMarker).MarkTwoFactorVerified(ctx, cookie); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/2fa/enroll", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w := httptest.NewRecorder()
	mountRoutes(mgr).ServeHTTP(w, req)
	if w.Code < 400 {
		t.Errorf("re-enroll over an enabled factor = %d, want a refusal", w.Code)
	}
	if st, _ := store.GetTwoFA(ctx, u.GetID()); st == nil || st.Secret != "LIVESECRET" || !st.Enabled {
		t.Errorf("re-enroll replaced the live factor: %+v", st)
	}
}
