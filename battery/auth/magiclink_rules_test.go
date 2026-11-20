package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type recordingRateStore struct{ keys []string }

func (s *recordingRateStore) Allow(_ context.Context, key string, _ RateLimiterConfig) (bool, time.Duration, error) {
	s.keys = append(s.keys, key)
	return true, 0, nil
}

func TestMailAddressLimitKeyIsADigest(t *testing.T) {
	st := &recordingRateStore{}
	rl := newScopedRateLimiter(RateLimiterConfig{MaxAttempts: 1, Window: time.Minute, Store: st}, "mail_address")
	allowAddress(context.Background(), rl, "victim@example.com")
	for _, k := range st.keys {
		if strings.Contains(k, "victim") {
			t.Errorf("limiter key %q carries the address", k)
		}
	}
	if len(st.keys) == 0 {
		t.Fatal("the limiter store was never asked")
	}
}

func TestConfirmPageRefusesOtherFlowToken(t *testing.T) {
	h := newTokenPurposeHarness(t)
	req := httptest.NewRequest(http.MethodGet, "/auth/magic-link/verify?token="+h.mintResetToken(), nil)
	w := httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("confirm page for a reset token = %d, want 401", w.Code)
	}
}

func TestMagicLinkVerifyCapsBody(t *testing.T) {
	mgr, _, _ := newMagicLinkManager(t, &mockEmailSender{})
	r := mountMagicLinkRoutes(mgr)
	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/verify",
		strings.NewReader("token="+strings.Repeat("A", oversizedFieldBytes)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized verify body = %d, want 413", w.Code)
	}
}

func TestMailQueueRecoversSenderPanic(t *testing.T) {
	q := newMailQueue("test")
	done := make(chan struct{})
	q.enqueue(context.Background(), "a@example.com", func(context.Context) {
		defer close(done)
		panic("sender exploded")
	})
	<-done
	if err := q.stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if q.enqueue(context.Background(), "a@example.com", func(context.Context) {}) {
		t.Error("enqueue after stop was accepted")
	}
}

func TestMagicLinkStoresPurgeByPayload(t *testing.T) {
	ctx := context.Background()
	for name, st := range map[string]MagicLinkTokenStore{"memory": NewMemoryMagicLinkTokenStore(), "sql": newSQLStore(t)} {
		mine, _ := st.CreateToken(ctx, "magiclink:a@example.com", time.Minute)
		other, _ := st.CreateToken(ctx, "magiclink:b@example.com", time.Minute)
		n, err := st.(MagicLinkTokenPurger).DeleteTokensForPayload(ctx, "magiclink:a@example.com")
		if err != nil || n != 1 {
			t.Errorf("%s purge = %d, %v; want 1, nil", name, n, err)
		}
		if _, err := st.RedeemToken(ctx, mine); err == nil {
			t.Errorf("%s: a purged token still redeems", name)
		}
		if _, err := st.RedeemToken(ctx, other); err != nil {
			t.Errorf("%s: the purge took another address's token: %v", name, err)
		}
	}
}

func TestMagicLinkInitRefusesBadBaseURL(t *testing.T) {
	mgr := New(AuthConfig{JWTSecret: "s", DevMode: true, UserStore: newMemoryUserStore()})
	mgr.Use(NewMagicLinkPlugin(MagicLinkConfig{BaseURL: "javascript:alert(1)", DevMode: true}))
	if err := mgr.Init(nil); err == nil {
		t.Error("a javascript: BaseURL passed Init")
	}
}

func TestMagicLinkSendIsNotCached(t *testing.T) {
	mgr, _, _ := newMagicLinkManager(t, &mockEmailSender{})
	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/send", strings.NewReader(`{"email":"a@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mountMagicLinkRoutes(mgr).ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Errorf("send = %d Cache-Control %q, want 200 no-store", w.Code, w.Header().Get("Cache-Control"))
	}
}

func TestConfirmPageIsNotCachedOrReferred(t *testing.T) {
	w := httptest.NewRecorder()
	writeConfirmPage(w, httptest.NewRequest(http.MethodGet, "/", nil), nil, ConfirmPageData{Token: "t", Action: "/auth/magic-link/verify"})
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("confirm page headers = %v, want no-store and no-referrer", w.Header())
	}
}

func TestDefaultConfirmPageRefusesUnsafeAction(t *testing.T) {
	page := string(defaultConfirmPage(context.Background(), ConfirmPageData{Token: "t", Action: "javascript:alert(1)"}))
	if strings.Contains(page, "javascript:") {
		t.Errorf("an unsafe Action reached the form: %s", page)
	}
}

func TestMemoryMagicLinkPeekRefusesExpired(t *testing.T) {
	st := NewMemoryMagicLinkTokenStore()
	tok, _ := st.CreateToken(context.Background(), "a@example.com", time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	if _, err := st.PeekToken(context.Background(), tok); !errors.Is(err, ErrTokenNotFound) {
		t.Errorf("peek of an expired token = %v, want ErrTokenNotFound", err)
	}
}
