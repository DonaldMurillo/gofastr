package auth

import (
	"context"
	"errors"
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

// readFailsAt is a TwoFAStore without CompareAndSwapTwoFA whose at-th GetTwoFA errors.
type readFailsAt struct {
	TwoFAStore
	calls, at int
}

func (s *readFailsAt) GetTwoFA(ctx context.Context, userID string) (*TwoFAState, error) {
	s.calls++
	if s.calls == s.at {
		return nil, errors.New("read failed")
	}
	return s.TwoFAStore.GetTwoFA(ctx, userID)
}

func TestTwoFAEnrollFailsClosedOnReadError(t *testing.T) {
	mgr, store, users, cookie := newTwoFATestEnv(t)
	ctx := context.Background()
	u, _, err := users.FindByEmail(ctx, "alice@test.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetTwoFA(ctx, u.GetID(), &TwoFAState{Secret: "LIVESECRET", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.SessionStore().(SessionTwoFAMarker).MarkTwoFactorVerified(ctx, cookie); err != nil {
		t.Fatal(err)
	}
	plugin, err := PluginAs[*TwoFAPlugin](mgr, "twofa")
	if err != nil {
		t.Fatal(err)
	}
	plugin.store = &readFailsAt{TwoFAStore: store, at: 2}
	req := httptest.NewRequest(http.MethodPost, "/auth/2fa/enroll", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w := httptest.NewRecorder()
	mountRoutes(mgr).ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("enrol on a failed state read = %d, want 500", w.Code)
	}
	if st, _ := store.GetTwoFA(ctx, u.GetID()); st == nil || st.Secret != "LIVESECRET" || !st.Enabled {
		t.Errorf("SECURITY: enrol on a failed read overwrote the live factor: %+v", st)
	}
}
