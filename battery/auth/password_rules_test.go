package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func (h *changeHarness) changeWith(ctx context.Context, body map[string]string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/auth/password", bytes.NewReader(b)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: h.sess.Token})
	w := httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	return w
}

func TestPasswordChangeRefusesTokenAndEmbedCallers(t *testing.T) {
	h := newChangeHarness(t, nil)
	body := map[string]string{"current_password": "oldpw123", "password": "brandnewpw1"}
	tokenCtx := context.WithValue(context.Background(), tokenScopesKey{}, []string{"*:*"})
	if w := h.changeWith(tokenCtx, body); w.Code != http.StatusUnauthorized {
		t.Errorf("API-token caller = %d, want 401", w.Code)
	}
	if w := h.changeWith(grantCtx(t), body); w.Code != http.StatusUnauthorized {
		t.Errorf("embed-grant caller = %d, want 401", w.Code)
	}
	if !h.passwordIs("oldpw123") {
		t.Fatal("a token or embed caller changed the password")
	}
}

func TestPasswordChangeRefusesOverlongPassword(t *testing.T) {
	h := newChangeHarness(t, nil)
	w := h.change(h.sess.Token, map[string]string{"current_password": "oldpw123", "password": strings.Repeat("a", 129)})
	if w.Code != http.StatusUnprocessableEntity || fieldError(t, w, "password") == "" {
		t.Fatalf("129-byte password: %d %s", w.Code, w.Body.String())
	}
}

func TestPasswordChangeTwoFALookupErrorRefuses(t *testing.T) {
	h := newChangeHarness(t, nil)
	h.mgr.Use(errorCheckerPlugin{})
	w := h.change(h.sess.Token, map[string]string{"current_password": "oldpw123", "password": "brandnewpw1"})
	if w.Code != http.StatusInternalServerError || !h.passwordIs("oldpw123") {
		t.Fatalf("a failed 2FA-state lookup let the change through: %d", w.Code)
	}
}

func (h *changeHarness) reset(t *testing.T, password string) *httptest.ResponseRecorder {
	t.Helper()
	p, _ := h.mgr.Plugin("password-reset")
	tok, err := createPurposeToken(context.Background(), p.(*PasswordResetPlugin).store, purposeReset, h.user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]string{"token": tok, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/auth/reset-password", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	return w
}

func TestPasswordResetMailsANotice(t *testing.T) {
	h := newChangeHarness(t, nil)
	if w := h.reset(t, "brandnewpw1"); w.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", w.Code, w.Body.String())
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.sender.mu.Lock()
		to, body := h.sender.lastTo, h.sender.lastBody
		h.sender.mu.Unlock()
		if to == "a@example.com" && strings.Contains(body, resetNoticeDone.English()) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("a completed reset mailed the account no notice")
}

func TestPasswordResetRefusesOverlongPasswordKeepingToken(t *testing.T) {
	h := newChangeHarness(t, nil)
	if w := h.reset(t, strings.Repeat("a", 129)); w.Code != http.StatusBadRequest || !h.passwordIs("oldpw123") {
		t.Fatalf("129-byte reset password: %d", w.Code)
	}
}

func TestPasswordResetDefaultTTLIsOneHour(t *testing.T) {
	if got := NewPasswordResetPlugin(PasswordResetConfig{}).cfg.TokenTTL; got != time.Hour {
		t.Errorf("default reset TTL = %v, want 1h", got)
	}
}

func TestForgotPasswordCrossSiteRefused(t *testing.T) {
	h := newChangeHarness(t, nil)
	req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", strings.NewReader("email=a%40example.com"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("cross-site forgot-password form = %d, want 403", w.Code)
	}
}

func TestPasswordChangeDropsOutstandingResetLinks(t *testing.T) {
	h := newChangeHarness(t, nil)
	p, _ := h.mgr.Plugin("password-reset")
	reset := p.(*PasswordResetPlugin)
	tok, err := createPurposeToken(context.Background(), reset.store, purposeReset, h.user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if w := h.change(h.sess.Token, map[string]string{"current_password": "oldpw123", "password": "brandnewpw1"}); w.Code != http.StatusOK {
		t.Fatalf("change: %d %s", w.Code, w.Body.String())
	}
	if _, err := redeemPurposeToken(context.Background(), reset.store, purposeReset, tok); err == nil {
		t.Error("a reset link mailed before the change still redeems after it")
	}
}
