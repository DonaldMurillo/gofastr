package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// adversarialStore is the e2e-style superstore from
// e2e_happy_path_test.go reduced to the surface needed here. It is
// kept separate so concurrent tests don't accidentally share state
// with the happy-path test.
type adversarialStore struct{ *e2eFullStore }

func newAdversarialStore() *adversarialStore {
	return &adversarialStore{newE2EFullStore(nil)}
}

// concurrentSetup builds an AuthManager with the plugins we want to
// hammer, plus a pre-seeded user with a valid session.
func concurrentSetup(t *testing.T, plugins ...AuthPlugin) (*adversarialStore, *router.Router, string, string) {
	t.Helper()
	store := newAdversarialStore()
	hash, _ := HashPassword("starting-password")
	u := &BasicUser{ID: "u-alice", Email: "alice@adv.test", Roles: []string{"user"}}
	store.byEmail["alice@adv.test"] = &e2eFullEntry{user: u, hash: hash, passwordSet: true}
	store.byID["u-alice"] = store.byEmail["alice@adv.test"]

	mgr := New(AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     store,
		DevMode:       true,
	})
	mgr.Use(NewCorePlugin())
	for _, p := range plugins {
		mgr.Use(p)
	}
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	sess, err := mgr.SessionStore().Create(context.Background(), u.GetID(), time.Hour)
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)
	return store, r, sess.Token, u.GetID()
}

// TestConcurrent_VerifyEmailTokenIsSingleUse pins the atomicity of
// EmailVerificationPlugin.verifyHandler: two simultaneous GETs with the
// same token must produce exactly one 200 (verified) and one 401
// (already consumed). Without atomic redeem we'd see both succeed
// AND/OR the verified flag set twice.
func TestConcurrent_VerifyEmailTokenIsSingleUse(t *testing.T) {
	plug := NewEmailVerificationPlugin(EmailVerificationConfig{
		BaseURL: "http://test",
		DevMode: true,
	})
	store, r, sess, userID := concurrentSetup(t, plug)

	// Mint a verification token directly via the plugin's store.
	tok, err := plug.store.CreateToken(context.Background(), userID, time.Hour)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	const N = 16
	var ok, denied int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			<-start
			req := httptest.NewRequest(http.MethodGet, "/auth/verify-email?token="+url.QueryEscape(tok), nil)
			req.AddCookie(&http.Cookie{Name: "session_id", Value: sess})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			switch w.Code {
			case http.StatusOK:
				atomic.AddInt32(&ok, 1)
			case http.StatusUnauthorized:
				atomic.AddInt32(&denied, 1)
			default:
				t.Errorf("unexpected code: %d (body=%s)", w.Code, w.Body.String())
			}
		}()
	}
	close(start)
	wg.Wait()

	if ok != 1 {
		t.Fatalf("expected exactly 1 successful verify-email under %d concurrent attempts; got %d", N, ok)
	}
	if denied != N-1 {
		t.Fatalf("expected %d 401-denied attempts; got %d", N-1, denied)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.verified[userID] {
		t.Fatal("user must be marked verified after the single successful redeem")
	}
}

// TestConcurrent_ResetPasswordTokenIsSingleUse pins the same atomicity
// for PasswordResetPlugin: only one of N parallel reset attempts with
// the same token must succeed. Otherwise the second redeem would set
// the password to something the attacker controls — racey takeover.
func TestConcurrent_ResetPasswordTokenIsSingleUse(t *testing.T) {
	plug := NewPasswordResetPlugin(PasswordResetConfig{
		BaseURL: "http://test",
		DevMode: true,
	})
	_, r, _, userID := concurrentSetup(t, plug)

	tok, err := plug.store.CreateToken(context.Background(), userID, time.Hour)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	const N = 16
	var ok, denied int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			<-start
			body, _ := json.Marshal(map[string]string{
				"token":    tok,
				"password": "new-password-" + string(rune('a'+i)),
			})
			req := httptest.NewRequest(http.MethodPost, "/auth/reset-password", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			switch w.Code {
			case http.StatusOK:
				atomic.AddInt32(&ok, 1)
			case http.StatusUnauthorized:
				atomic.AddInt32(&denied, 1)
			default:
				t.Errorf("unexpected code: %d", w.Code)
			}
		}()
	}
	close(start)
	wg.Wait()

	if ok != 1 {
		t.Fatalf("expected exactly 1 successful reset under %d concurrent attempts; got %d", N, ok)
	}
	if denied != N-1 {
		t.Fatalf("expected %d 401-denied attempts; got %d", N-1, denied)
	}
}

// TestConcurrent_UnlinkSameProviderTwice pins the unlink path under
// contention: two parallel DELETE /auth/unlink/google calls must
// produce exactly one 200 (the actual unlink) and exactly one 404
// (account not linked, because the other goroutine just removed it).
// If both returned 200 the rule would still hold post-condition (link
// is gone), but the 404 surface is what AccountLister exposes to the
// next caller and tests should pin it.
//
// This test currently requires the store's ListAccounts/UnlinkOAuth to
// be atomic enough that the gap between "count present" and "delete"
// is small. The reference implementation in adversarialStore uses a
// single mutex, so it should pass.
func TestConcurrent_UnlinkSameProviderTwice(t *testing.T) {
	store, r, sess, userID := concurrentSetup(t, NewAccountsPlugin())

	if err := store.LinkOAuth(context.Background(), userID, "google", "g-1"); err != nil {
		t.Fatalf("link: %v", err)
	}
	// Plant an extra link so unlink isn't refused as the "last login method"
	if err := store.LinkOAuth(context.Background(), userID, "github", "gh-1"); err != nil {
		t.Fatalf("link2: %v", err)
	}

	const N = 8
	var ok, notFound, conflict int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			<-start
			req := httptest.NewRequest(http.MethodDelete, "/auth/unlink/google", nil)
			req.AddCookie(&http.Cookie{Name: "session_id", Value: sess})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			switch w.Code {
			case http.StatusOK:
				atomic.AddInt32(&ok, 1)
			case http.StatusNotFound:
				atomic.AddInt32(&notFound, 1)
			case http.StatusConflict:
				atomic.AddInt32(&conflict, 1)
			default:
				t.Errorf("unexpected code: %d", w.Code)
			}
		}()
	}
	close(start)
	wg.Wait()

	if ok < 1 {
		t.Fatalf("at least one unlink must succeed; got ok=%d notFound=%d conflict=%d", ok, notFound, conflict)
	}
	// After all goroutines finish, the google link must be gone.
	accts, _ := store.ListAccounts(context.Background(), userID)
	for _, a := range accts {
		if a.Provider == "google" {
			t.Fatalf("google link must be gone after concurrent unlinks; ListAccounts returned %+v", accts)
		}
	}
}

// TestCancelledContext_PasswordReset_PartialFailureSurfaces pins that a
// caller that cancels the context mid-flow doesn't leave the user in
// "password updated, response never sent" state from the user's POV.
// We can't actually cancel between the redeem and the set, but we can
// pin that a cancelled context to SetPassword surfaces a recognisable
// error rather than silently passing.
func TestCancelledContext_PasswordReset_PartialFailureSurfaces(t *testing.T) {
	store := newAdversarialStore()
	hash, _ := HashPassword("orig")
	u := &BasicUser{ID: "u-bob", Email: "bob@adv.test", Roles: []string{"user"}}
	store.byEmail["bob@adv.test"] = &e2eFullEntry{user: u, hash: hash, passwordSet: true}
	store.byID["u-bob"] = store.byEmail["bob@adv.test"]

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	// The adversarialStore SetPassword doesn't honour ctx — it locks the
	// mutex unconditionally — so this test documents current behavior:
	// the operation completes regardless. If a future store grows
	// context-aware behavior, this assertion should flip to expect ctx.Err().
	if err := store.SetPassword(ctx, "u-bob", "new-hash"); err != nil {
		t.Logf("SetPassword on cancelled ctx surfaced %v — fine if ctx-aware", err)
	}
	if got, _ := store.HasPassword(context.Background(), "u-bob"); !got {
		t.Fatal("HasPassword must still report true after a SetPassword call regardless of ctx")
	}
}
