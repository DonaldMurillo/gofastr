package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// ============================================================================
// MemoryMagicLinkTokenStore tests
// ============================================================================

func TestMemoryMagicLinkTokenStore_CreateAndRedeem(t *testing.T) {
	store := NewMemoryMagicLinkTokenStore()
	ctx := context.Background()

	token, err := store.CreateToken(ctx, "alice@example.com", 15*time.Minute)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if token == "" {
		t.Fatal("token should not be empty")
	}
	if len(token) != 64 { // 32 bytes hex-encoded = 64 chars
		t.Errorf("expected 64-char hex token, got %d chars", len(token))
	}

	email, err := store.RedeemToken(ctx, token)
	if err != nil {
		t.Fatalf("RedeemToken: %v", err)
	}
	if email != "alice@example.com" {
		t.Errorf("expected email alice@example.com, got %q", email)
	}
}

func TestMemoryMagicLinkTokenStore_DoubleRedeemFails(t *testing.T) {
	store := NewMemoryMagicLinkTokenStore()
	ctx := context.Background()

	token, _ := store.CreateToken(ctx, "alice@example.com", 15*time.Minute)

	// First redeem succeeds
	_, err := store.RedeemToken(ctx, token)
	if err != nil {
		t.Fatalf("first RedeemToken: %v", err)
	}

	// Second redeem fails
	_, err = store.RedeemToken(ctx, token)
	if err == nil {
		t.Fatal("second RedeemToken should fail")
	}
	if err != ErrTokenNotFound {
		t.Errorf("expected ErrTokenNotFound, got %v", err)
	}
}

func TestMemoryMagicLinkTokenStore_ExpiredTokenFails(t *testing.T) {
	store := NewMemoryMagicLinkTokenStore()
	ctx := context.Background()

	token, _ := store.CreateToken(ctx, "alice@example.com", -1*time.Second)

	_, err := store.RedeemToken(ctx, token)
	if err == nil {
		t.Fatal("expired token should fail redeem")
	}
	if err != ErrTokenNotFound {
		t.Errorf("expected ErrTokenNotFound, got %v", err)
	}
}

func TestMemoryMagicLinkTokenStore_UnknownTokenFails(t *testing.T) {
	store := NewMemoryMagicLinkTokenStore()
	ctx := context.Background()

	_, err := store.RedeemToken(ctx, "nonexistent-token")
	if err == nil {
		t.Fatal("unknown token should fail redeem")
	}
}

func TestMemoryMagicLinkTokenStore_Cleanup(t *testing.T) {
	store := NewMemoryMagicLinkTokenStore()
	ctx := context.Background()

	// Create one expired and one fresh token
	_, _ = store.CreateToken(ctx, "expired@example.com", -1*time.Second)
	_, _ = store.CreateToken(ctx, "fresh@example.com", 15*time.Minute)

	n, err := store.Cleanup(ctx)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 expired token cleaned up, got %d", n)
	}

	// Fresh token should still be redeemable
	store.mu.RLock()
	count := len(store.tokens)
	store.mu.RUnlock()
	if count != 1 {
		t.Fatalf("expected 1 remaining token, got %d", count)
	}
}

// ============================================================================
// MagicLinkPlugin registration and init
// ============================================================================

func TestMagicLinkPlugin_Name(t *testing.T) {
	p := NewMagicLinkPlugin(MagicLinkConfig{})
	if p.Name() != "magic-link" {
		t.Errorf("expected name 'magic-link', got %q", p.Name())
	}
}

func TestMagicLinkPlugin_Init(t *testing.T) {
	p := NewMagicLinkPlugin(MagicLinkConfig{BaseURL: "http://localhost:8080"})
	mgr := New(AuthConfig{})
	if err := p.Init(mgr); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if p.mgr == nil {
		t.Fatal("mgr should be set after Init")
	}
}

func TestMagicLinkPlugin_Defaults(t *testing.T) {
	p := NewMagicLinkPlugin(MagicLinkConfig{})
	if p.config.TokenLength != 32 {
		t.Errorf("default TokenLength should be 32, got %d", p.config.TokenLength)
	}
	if p.config.TokenTTL != 15*time.Minute {
		t.Errorf("default TokenTTL should be 15m, got %v", p.config.TokenTTL)
	}
	if p.config.OnSuccessURL != "/" {
		t.Errorf("default OnSuccessURL should be '/', got %q", p.config.OnSuccessURL)
	}
}

// ============================================================================
// Helpers for route tests
// ============================================================================

// mockEmailSender captures the magic link URL for inspection.
type mockEmailSender struct {
	lastEmail string
	lastURL   string
	err       error
}

func (m *mockEmailSender) SendMagicLink(_ context.Context, email, magicLinkURL string) error {
	m.lastEmail = email
	m.lastURL = magicLinkURL
	return m.err
}

func newMagicLinkManager(t *testing.T, sender MagicLinkEmailSender) (*AuthManager, *memoryUserStore, *MagicLinkPlugin) {
	t.Helper()
	userStore := newMemoryUserStore()
	mgr := New(AuthConfig{
		JWTSecret:     "test-secret", // prod-mode Init fails closed without one
		SessionTTL:    24 * time.Hour,
		SessionCookie: "session_id",
		UserStore:     userStore,
	})

	plugin := NewMagicLinkPlugin(MagicLinkConfig{
		BaseURL:      "http://localhost:8080",
		OnSuccessURL: "/dashboard",
		TokenTTL:     15 * time.Minute,
		EmailSender:  sender,
		// Tests using a nil sender want the legacy "log to stdout" path;
		// production code must opt in via DevMode explicitly. See
		// TestMagicLink_NilSenderWithoutDevMode_FailsClosed.
		DevMode: sender == nil,
	})
	mgr.Use(plugin)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return mgr, userStore, plugin
}

func mountMagicLinkRoutes(mgr *AuthManager) *router.Router {
	r := router.New()
	mgr.RegisterRoutes(r)
	return r
}

// ============================================================================
// Send route tests
// ============================================================================

func TestMagicLink_Send_Returns200(t *testing.T) {
	sender := &mockEmailSender{}
	mgr, _, _ := newMagicLinkManager(t, sender)
	r := mountMagicLinkRoutes(mgr)

	body, _ := json.Marshal(map[string]string{"email": "alice@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/send", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Errorf("expected success=true, got %v", resp["success"])
	}

	if sender.lastEmail != "alice@example.com" {
		t.Errorf("expected email alice@example.com, got %q", sender.lastEmail)
	}
	if !strings.Contains(sender.lastURL, "/auth/magic-link/verify?token=") {
		t.Errorf("expected magic link URL with token, got %q", sender.lastURL)
	}
}

func TestMagicLink_Send_NoEmail_Returns400(t *testing.T) {
	sender := &mockEmailSender{}
	mgr, _, _ := newMagicLinkManager(t, sender)
	r := mountMagicLinkRoutes(mgr)

	body, _ := json.Marshal(map[string]string{"email": ""})
	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/send", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestMagicLink_Send_DevMode_LogsURL(t *testing.T) {
	// EmailSender is nil — dev mode
	mgr, _, _ := newMagicLinkManager(t, nil)
	r := mountMagicLinkRoutes(mgr)

	body, _ := json.Marshal(map[string]string{"email": "alice@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/send", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 in dev mode, got %d", w.Code)
	}
}

// ============================================================================
// Verify route tests
// ============================================================================

func TestMagicLink_Verify_ValidToken_SetsCookieAndRedirects(t *testing.T) {
	sender := &mockEmailSender{}
	mgr, userStore, plugin := newMagicLinkManager(t, sender)

	// Pre-create the user so we test the "find existing user" path
	seedUser(t, userStore, "alice@example.com", "irrelevant")

	r := mountMagicLinkRoutes(mgr)

	// Create a token via the plugin's token store
	token, err := plugin.tokenStore.CreateToken(context.Background(), "alice@example.com", 15*time.Minute)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/magic-link/verify?token="+token, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "/dashboard" {
		t.Errorf("expected redirect to /dashboard, got %q", loc)
	}

	// Verify session cookie is set
	var sessionCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "session_id" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session_id cookie to be set")
	}

	// Verify session exists in store
	sess, err := mgr.SessionStore().Get(context.Background(), sessionCookie.Value)
	if err != nil {
		t.Fatalf("session should exist: %v", err)
	}
	if sess.UserID == "" {
		t.Fatal("session UserID should not be empty")
	}
}

func TestMagicLink_Verify_InvalidToken_Returns401(t *testing.T) {
	sender := &mockEmailSender{}
	mgr, _, _ := newMagicLinkManager(t, sender)
	r := mountMagicLinkRoutes(mgr)

	req := httptest.NewRequest(http.MethodGet, "/auth/magic-link/verify?token=badtoken", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestMagicLink_Verify_ExpiredToken_Returns401(t *testing.T) {
	sender := &mockEmailSender{}
	mgr, _, plugin := newMagicLinkManager(t, sender)
	r := mountMagicLinkRoutes(mgr)

	token, _ := plugin.tokenStore.CreateToken(context.Background(), "alice@example.com", -1*time.Second)

	req := httptest.NewRequest(http.MethodGet, "/auth/magic-link/verify?token="+token, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestMagicLink_Verify_NoToken_Returns401(t *testing.T) {
	sender := &mockEmailSender{}
	mgr, _, _ := newMagicLinkManager(t, sender)
	r := mountMagicLinkRoutes(mgr)

	req := httptest.NewRequest(http.MethodGet, "/auth/magic-link/verify", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestMagicLink_Verify_CreatesUserIfNotExists(t *testing.T) {
	sender := &mockEmailSender{}
	mgr, userStore, plugin := newMagicLinkManager(t, sender)
	r := mountMagicLinkRoutes(mgr)

	email := "newuser@example.com"

	// Confirm user does NOT exist
	_, _, err := userStore.FindByEmail(context.Background(), email)
	if err == nil {
		t.Fatal("user should not exist yet")
	}

	token, _ := plugin.tokenStore.CreateToken(context.Background(), email, 15*time.Minute)

	req := httptest.NewRequest(http.MethodGet, "/auth/magic-link/verify?token="+token, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", w.Code, w.Body.String())
	}

	// Confirm user was created
	user, _, err := userStore.FindByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("user should exist after verify: %v", err)
	}
	if user.GetEmail() != email {
		t.Errorf("expected email %q, got %q", email, user.GetEmail())
	}
}

func TestMagicLink_Verify_FindsExistingUser(t *testing.T) {
	sender := &mockEmailSender{}
	mgr, userStore, plugin := newMagicLinkManager(t, sender)
	r := mountMagicLinkRoutes(mgr)

	email := "existing@example.com"
	existingUser := seedUser(t, userStore, email, "irrelevant")
	existingID := existingUser.GetID()

	token, _ := plugin.tokenStore.CreateToken(context.Background(), email, 15*time.Minute)

	req := httptest.NewRequest(http.MethodGet, "/auth/magic-link/verify?token="+token, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}

	// Verify the session is for the existing user
	var sessionCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "session_id" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie")
	}

	sess, _ := mgr.SessionStore().Get(context.Background(), sessionCookie.Value)
	if sess.UserID != existingID {
		t.Errorf("expected session for user %q, got %q", existingID, sess.UserID)
	}

	// Confirm no duplicate user was created — FindByEmail should succeed
	// and FindByID should return the same user as the original.
	found, _, findErr := userStore.FindByEmail(context.Background(), email)
	if findErr != nil {
		t.Fatalf("user should exist: %v", findErr)
	}
	if found.GetID() != existingID {
		t.Errorf("expected same user ID %q, got %q — user was duplicated", existingID, found.GetID())
	}
}

func TestMagicLink_Verify_TokenConsumedAfterUse(t *testing.T) {
	sender := &mockEmailSender{}
	mgr, _, plugin := newMagicLinkManager(t, sender)
	r := mountMagicLinkRoutes(mgr)

	token, _ := plugin.tokenStore.CreateToken(context.Background(), "alice@example.com", 15*time.Minute)

	// First verify succeeds
	req := httptest.NewRequest(http.MethodGet, "/auth/magic-link/verify?token="+token, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("first verify: expected 302, got %d", w.Code)
	}

	// Second verify fails — token already consumed
	req2 := httptest.NewRequest(http.MethodGet, "/auth/magic-link/verify?token="+token, nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("second verify: expected 401, got %d", w2.Code)
	}
}

// ============================================================================
// Integration: send → verify end-to-end
// ============================================================================

func TestMagicLink_SendThenVerify_EndToEnd(t *testing.T) {
	sender := &mockEmailSender{}
	userStore := newMemoryUserStore()
	mgr := New(AuthConfig{
		JWTSecret:     "test-secret", // prod-mode Init fails closed without one
		SessionTTL:    24 * time.Hour,
		SessionCookie: "session_id",
		UserStore:     userStore,
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewMagicLinkPlugin(MagicLinkConfig{
		BaseURL:      "http://localhost:8080",
		OnSuccessURL: "/dashboard",
		TokenTTL:     15 * time.Minute,
		EmailSender:  sender,
	}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := mountMagicLinkRoutes(mgr)

	// 1. Send magic link
	body, _ := json.Marshal(map[string]string{"email": "e2e@example.com"})
	sendReq := httptest.NewRequest(http.MethodPost, "/auth/magic-link/send", bytes.NewReader(body))
	sendReq.Header.Set("Content-Type", "application/json")
	sendW := httptest.NewRecorder()
	r.ServeHTTP(sendW, sendReq)

	if sendW.Code != http.StatusOK {
		t.Fatalf("send: expected 200, got %d", sendW.Code)
	}

	// Extract token from the URL that was "sent"
	if !strings.Contains(sender.lastURL, "token=") {
		t.Fatal("magic link URL should contain token parameter")
	}
	token := strings.TrimPrefix(sender.lastURL, "http://localhost:8080/auth/magic-link/verify?token=")

	// 2. Verify with that token
	verifyReq := httptest.NewRequest(http.MethodGet, "/auth/magic-link/verify?token="+token, nil)
	verifyW := httptest.NewRecorder()
	r.ServeHTTP(verifyW, verifyReq)

	if verifyW.Code != http.StatusFound {
		t.Fatalf("verify: expected 302, got %d: %s", verifyW.Code, verifyW.Body.String())
	}

	// Session cookie should be set
	var cookie *http.Cookie
	for _, c := range verifyW.Result().Cookies() {
		if c.Name == "session_id" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("expected session cookie after verify")
	}

	// User should have been auto-created
	meReq := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	meReq.AddCookie(cookie)
	meW := httptest.NewRecorder()
	r.ServeHTTP(meW, meReq)

	if meW.Code != http.StatusOK {
		t.Fatalf("me: expected 200, got %d: %s", meW.Code, meW.Body.String())
	}

	fmt.Printf("e2e me response: %s\n", meW.Body.String())
}

// First-time OAuth/magic-link signups must NOT recompute a fresh bcrypt
// hash per request — that's ~50ms of CPU + a hash allocation just to
// store a value nobody will ever try to verify against (the user logs
// in via OAuth/magic-link, never via password).
//
// Test: time the magic-link verify path for a brand-new email. With the
// fix, latency is dominated by token redeem + session create (low single-
// digit ms). With the bug, every first signup pays a full bcrypt.

func TestMagicLinkVerify_NewUser_DoesNotRunBcryptPerSignup(t *testing.T) {
	userStore := newMemoryUserStore()
	mgr := New(AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     userStore,
		DevMode:       true,
	})
	plugin := NewMagicLinkPlugin(MagicLinkConfig{
		BaseURL:  "http://localhost",
		TokenTTL: time.Hour,
		DevMode:  true,
	})
	mgr.Use(plugin)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)

	// Warm-up so init costs don't bias.
	tok0, _ := plugin.tokenStore.CreateToken(context.Background(), "warm@example.com", time.Hour)
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/auth/magic-link/verify?token="+tok0, nil))

	const samples = 5
	var total time.Duration
	for i := 0; i < samples; i++ {
		email := "fresh" + string(rune('a'+i)) + "@example.com"
		tok, err := plugin.tokenStore.CreateToken(context.Background(), email, time.Hour)
		if err != nil {
			t.Fatalf("CreateToken: %v", err)
		}
		req := httptest.NewRequest(http.MethodGet, "/auth/magic-link/verify?token="+tok, nil)
		w := httptest.NewRecorder()
		start := time.Now()
		r.ServeHTTP(w, req)
		total += time.Since(start)
		if w.Code != http.StatusFound {
			t.Fatalf("verify: expected 302, got %d (body=%s)", w.Code, w.Body.String())
		}
	}
	mean := total / samples
	t.Logf("magic-link verify mean latency = %v", mean)

	// Bcrypt at default cost is ≥30ms even on fast hardware. With the
	// per-signup hash bug, mean ≥ 30ms. With the fix (one shared
	// placeholder), mean is single-digit ms.
	if mean > 20*time.Millisecond {
		t.Fatalf("magic-link verify mean=%v — likely doing bcrypt per signup (target: shared placeholder hash)", mean)
	}
}

// MagicLink dev mode (logging the token URL) must be opt-in. Today
// nil EmailSender silently logs live tokens — a production deploy
// missing the email sender ships token-in-logs account takeover.

func newMagicLinkPluginWithDev(t *testing.T, sender MagicLinkEmailSender, devMode bool) (*AuthManager, *MagicLinkPlugin) {
	t.Helper()
	mgr := New(AuthConfig{
		JWTSecret:     "test-secret", // prod-mode Init fails closed without one
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     newMemoryUserStore(),
	})
	plugin := NewMagicLinkPlugin(MagicLinkConfig{
		BaseURL:     "http://localhost",
		TokenTTL:    time.Minute,
		EmailSender: sender,
		DevMode:     devMode,
	})
	mgr.Use(plugin)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return mgr, plugin
}

func sendMagicLink(t *testing.T, mgr *AuthManager) *httptest.ResponseRecorder {
	t.Helper()
	r := router.New()
	mgr.RegisterRoutes(r)
	body, _ := json.Marshal(map[string]string{"email": "alice@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/auth/magic-link/send", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestMagicLink_NilSenderWithoutDevMode_FailsClosed(t *testing.T) {
	mgr, _ := newMagicLinkPluginWithDev(t, nil, false)
	w := sendMagicLink(t, mgr)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 (no sender, no dev mode), got %d body=%s", w.Code, w.Body.String())
	}
}

func TestMagicLink_NilSenderWithDevMode_LogsAndReturns200(t *testing.T) {
	mgr, _ := newMagicLinkPluginWithDev(t, nil, true)
	w := sendMagicLink(t, mgr)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 in dev mode, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestMagicLink_RealSender_NoDevMode_Works(t *testing.T) {
	sender := &mockEmailSender{}
	mgr, _ := newMagicLinkPluginWithDev(t, sender, false)
	w := sendMagicLink(t, mgr)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with sender configured, got %d body=%s", w.Code, w.Body.String())
	}
	if sender.lastEmail == "" {
		t.Fatalf("sender was not called")
	}
}

// Compile guard: keep context import live in case helpers move.
var _ = context.Background

// TestMagicLinkUsesConfiguredRoles pins that the magic-link
// auto-create path stamps AuthManager.DefaultRoles() onto the new
// account instead of the hardcoded ["user"].
func TestMagicLinkUsesConfiguredRoles(t *testing.T) {
	sender := &mockEmailSender{}
	userStore := newMemoryUserStore()
	mgr := New(AuthConfig{
		JWTSecret:     "test-secret",
		SessionTTL:    24 * time.Hour,
		SessionCookie: "session_id",
		UserStore:     userStore,
		DefaultRoles:  []string{"member", "editor"},
	})
	plugin := NewMagicLinkPlugin(MagicLinkConfig{
		BaseURL:      "http://localhost:8080",
		OnSuccessURL: "/dashboard",
		TokenTTL:     15 * time.Minute,
		EmailSender:  sender,
	})
	mgr.Use(plugin)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := mountMagicLinkRoutes(mgr)

	email := "newuser@example.com"
	token, _ := plugin.tokenStore.CreateToken(context.Background(), email, 15*time.Minute)
	req := httptest.NewRequest(http.MethodGet, "/auth/magic-link/verify?token="+token, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", w.Code, w.Body.String())
	}
	u, _, err := userStore.FindByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("user should exist after verify: %v", err)
	}
	got := u.GetRoles()
	if len(got) != 2 || got[0] != "member" || got[1] != "editor" {
		t.Fatalf("expected configured [member editor], got %v", got)
	}
}
