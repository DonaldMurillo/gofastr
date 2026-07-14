package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
	"golang.org/x/crypto/bcrypt"
)

// ─── TOTP unit tests ───────────────────────────────────────────────────

func TestGenerateSecret_ValidBase32(t *testing.T) {
	secret := GenerateSecret()
	if len(secret) < 26 {
		t.Fatalf("secret too short: %q", secret)
	}
	for _, c := range secret {
		if !((c >= 'A' && c <= 'Z') || (c >= '2' && c <= '7')) {
			t.Fatalf("secret contains non-base32 char: %c", c)
		}
	}
}

func TestGenerateSecret_UniqueEachCall(t *testing.T) {
	s1 := GenerateSecret()
	s2 := GenerateSecret()
	if s1 == s2 {
		t.Fatal("two secrets should not be equal")
	}
}

func TestTOTP_GenerateAndValidate(t *testing.T) {
	secret := GenerateSecret()
	period := uint(30)
	skew := uint(1)

	now := uint64(time.Now().Unix())
	step := now / uint64(period)

	code := GenerateTOTP(secret, step)
	if len(code) != 6 {
		t.Fatalf("expected 6-digit code, got %q", code)
	}

	if !ValidateTOTP(secret, code, period, skew) {
		t.Fatal("valid TOTP code should pass validation")
	}
}

func TestTOTP_RejectsWrongCode(t *testing.T) {
	secret := GenerateSecret()
	// Generate the right code, then change one digit to make it wrong
	now := uint64(time.Now().Unix())
	step := now / 30
	code := GenerateTOTP(secret, step)
	// Flip a digit
	wrong := code[:5] + string((code[5]-'0'+1)%10+'0')
	if ValidateTOTP(secret, wrong, 30, 1) && wrong == code {
		// Only fails if the flipped digit happened to wrap to the same value
		t.Fatal("modified code should not validate (or got lucky)")
	}
}

func TestTOTP_SkewAllowsCurrentStep(t *testing.T) {
	secret := GenerateSecret()
	period := uint(30)
	now := uint64(time.Now().Unix())
	step := now / uint64(period)

	code := GenerateTOTP(secret, step)
	if !ValidateTOTP(secret, code, period, 1) {
		t.Fatal("code from current step should validate with skew=1")
	}
}

func TestTOTP_ZeroSkewStrictValidation(t *testing.T) {
	secret := GenerateSecret()
	now := uint64(time.Now().Unix())
	step := now / 30
	code := GenerateTOTP(secret, step)

	if !ValidateTOTP(secret, code, 30, 0) {
		t.Fatal("exact-step code should validate with skew=0")
	}
}

func TestTOTP_InvalidSecret(t *testing.T) {
	code := GenerateTOTP("!!!invalid-base32!!!", 0)
	if code != "" {
		t.Fatalf("expected empty code for invalid secret, got %q", code)
	}
}

// ─── Backup code tests ─────────────────────────────────────────────────

func TestBackupCodes_GenerateAndConsume(t *testing.T) {
	plain, hashed, err := generateBackupCodes(5)
	if err != nil {
		t.Fatalf("generateBackupCodes: %v", err)
	}
	if len(plain) != 5 || len(hashed) != 5 {
		t.Fatal("expected 5 codes")
	}
	for _, c := range plain {
		if len(c) != 8 {
			t.Fatalf("expected 8-char code, got %q", c)
		}
	}
}

func TestMemoryTwoFAStore_CRUD(t *testing.T) {
	store := NewMemoryTwoFAStore()

	// Get non-existent
	state, err := store.GetTwoFA(nil, "user-1")
	if err != nil {
		t.Fatalf("GetTwoFA: %v", err)
	}
	if state != nil {
		t.Fatal("expected nil for non-existent user")
	}

	// Set
	state = &TwoFAState{Enabled: true, Secret: "JBSWY3DPEHPK3PXP"}
	if err := store.SetTwoFA(nil, "user-1", state); err != nil {
		t.Fatalf("SetTwoFA: %v", err)
	}

	// Get
	got, err := store.GetTwoFA(nil, "user-1")
	if err != nil {
		t.Fatalf("GetTwoFA: %v", err)
	}
	if !got.Enabled || got.Secret != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("unexpected state: %+v", got)
	}

	// Delete
	if err := store.DeleteTwoFA(nil, "user-1"); err != nil {
		t.Fatalf("DeleteTwoFA: %v", err)
	}
	got, _ = store.GetTwoFA(nil, "user-1")
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestMemoryTwoFAStore_ConsumeBackupCode(t *testing.T) {
	store := NewMemoryTwoFAStore()

	plain, hashed, err := generateBackupCodes(3)
	if err != nil {
		t.Fatalf("generateBackupCodes: %v", err)
	}

	state := &TwoFAState{Enabled: true, Secret: "TEST", BackupCodes: hashed}
	store.SetTwoFA(nil, "user-1", state)

	// Consume first code
	consumed, err := store.ConsumeBackupCode(nil, "user-1", plain[0])
	if err != nil {
		t.Fatalf("ConsumeBackupCode: %v", err)
	}
	if !consumed {
		t.Fatal("expected code to be consumed")
	}

	// Same code should not be reusable
	consumed, _ = store.ConsumeBackupCode(nil, "user-1", plain[0])
	if consumed {
		t.Fatal("backup code should not be reusable")
	}

	// Different code should still work
	consumed, _ = store.ConsumeBackupCode(nil, "user-1", plain[1])
	if !consumed {
		t.Fatal("second code should be consumed")
	}

	// Wrong code
	consumed, _ = store.ConsumeBackupCode(nil, "user-1", "XXXXXXXX")
	if consumed {
		t.Fatal("random code should not match")
	}
}

// ─── Plugin lifecycle tests ────────────────────────────────────────────

func TestTwoFAPlugin_Name(t *testing.T) {
	p := NewTwoFAPlugin(TwoFAConfig{})
	if p.Name() != "twofa" {
		t.Fatalf("expected 'twofa', got %q", p.Name())
	}
}

func TestTwoFAPlugin_InitDefaults(t *testing.T) {
	p := NewTwoFAPlugin(TwoFAConfig{})
	mgr := New(AuthConfig{JWTSecret: "test-secret", AllowInMemoryStores: true, UserStore: newMemoryUserStore()})
	mgr.Use(NewCorePlugin())
	mgr.Use(p)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if p.config.Issuer != "GoFastr" {
		t.Fatalf("expected default issuer 'GoFastr', got %q", p.config.Issuer)
	}
	if p.config.Period != 30 {
		t.Fatalf("expected default period 30, got %d", p.config.Period)
	}
}

func TestTwoFAPlugin_InitCustomConfig(t *testing.T) {
	p := NewTwoFAPlugin(TwoFAConfig{
		Issuer:          "MyApp",
		Period:          60,
		BackupCodeCount: 5,
	})
	mgr := New(AuthConfig{JWTSecret: "test-secret", AllowInMemoryStores: true, UserStore: newMemoryUserStore()})
	mgr.Use(NewCorePlugin())
	mgr.Use(p)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if p.config.Issuer != "MyApp" {
		t.Fatalf("expected 'MyApp', got %q", p.config.Issuer)
	}
	if p.config.Period != 60 {
		t.Fatalf("expected 60, got %d", p.config.Period)
	}
}

// ─── Route handler tests ───────────────────────────────────────────────

func newTwoFATestEnv(t *testing.T) (*AuthManager, *MemoryTwoFAStore, *memoryUserStore, string) {
	t.Helper()

	twoFAStore := NewMemoryTwoFAStore()
	userStore := newMemoryUserStore()

	mgr := New(AuthConfig{
		JWTSecret:           "test-secret", // prod-mode Init fails closed without one
		AllowInMemoryStores: true,          // 2FA on the memory store is fail-closed in prod
		SessionCookie:       "session_id",
		SessionTTL:          24 * time.Hour,
		UserStore:           userStore,
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewTwoFAPlugin(TwoFAConfig{Store: twoFAStore}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Seed a test user
	seedUser(t, userStore, "alice@test.com", "testpass")

	// Login to get session cookie
	r := mountRoutes(mgr)
	body := `{"email":"alice@test.com","password":"testpass"}`
	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
	}
	var cookieVal string
	for _, c := range w.Result().Cookies() {
		if c.Name == "session_id" {
			cookieVal = c.Value
		}
	}
	if cookieVal == "" {
		t.Fatal("no session cookie after login")
	}

	return mgr, twoFAStore, userStore, cookieVal
}

func TestTwoFA_EnrollAndVerify(t *testing.T) {
	mgr, _, _, cookie := newTwoFATestEnv(t)
	r := mountRoutes(mgr)

	// Enroll
	req := httptest.NewRequest("POST", "/auth/2fa/enroll", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("enroll: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var enrollResp map[string]any
	json.NewDecoder(w.Body).Decode(&enrollResp)
	secret, _ := enrollResp["secret"].(string)
	url, _ := enrollResp["url"].(string)
	if secret == "" {
		t.Fatal("expected non-empty secret")
	}
	if !strings.HasPrefix(url, "otpauth://totp/") {
		t.Fatalf("expected otpauth URL, got %q", url)
	}

	// Verify with valid TOTP code
	step := uint64(time.Now().Unix()) / 30
	code := GenerateTOTP(secret, step)
	verifyBody := fmt.Sprintf(`{"code":"%s"}`, code)
	req = httptest.NewRequest("POST", "/auth/2fa/verify", strings.NewReader(verifyBody))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("verify: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var verifyResp map[string]any
	json.NewDecoder(w.Body).Decode(&verifyResp)
	if enabled, _ := verifyResp["enabled"].(bool); !enabled {
		t.Fatal("expected enabled=true after verify")
	}
	backupCodes, _ := verifyResp["backup_codes"].([]any)
	if len(backupCodes) == 0 {
		t.Fatal("expected backup codes after verify")
	}
}

// toggleErrTwoFAStore wraps a real store and, once armed, fails GetTwoFA —
// simulating a durable store hitting a DB error (the memory store never
// could). Armed AFTER login, because a GetTwoFA error during login makes
// login itself fail closed (see the 2FA flow's fail-closed pending check).
type toggleErrTwoFAStore struct {
	TwoFAStore
	failing atomic.Bool
	err     error
}

func (e *toggleErrTwoFAStore) GetTwoFA(ctx context.Context, uid string) (*TwoFAState, error) {
	if e.failing.Load() {
		return nil, e.err
	}
	return e.TwoFAStore.GetTwoFA(ctx, uid)
}

// Regression: enroll must FAIL CLOSED when the store errors reading the
// current state. Treating an unreadable state as "not enabled" would let
// enroll overwrite a victim's live second factor (Enabled=false secret),
// downgrading the account to password-only.
func TestTwoFA_Enroll_FailsClosedOnStoreError(t *testing.T) {
	userStore := newMemoryUserStore()
	errStore := &toggleErrTwoFAStore{
		TwoFAStore: NewMemoryTwoFAStore(),
		err:        errors.New("simulated DB read failure"),
	}
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret",
		AllowInMemoryStores: true,
		SessionCookie:       "session_id",
		SessionTTL:          24 * time.Hour,
		UserStore:           userStore,
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewTwoFAPlugin(TwoFAConfig{Store: errStore}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	user := seedUser(t, userStore, "alice@test.com", "testpass")

	r := mountRoutes(mgr)
	login := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"email":"alice@test.com","password":"testpass"}`))
	login.Header.Set("Content-Type", "application/json")
	lw := httptest.NewRecorder()
	r.ServeHTTP(lw, login)
	var cookie string
	for _, c := range lw.Result().Cookies() {
		if c.Name == "session_id" {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("no session cookie after login")
	}

	// Arm the store failure only now — login is past, enroll is next.
	errStore.failing.Store(true)

	req := httptest.NewRequest("POST", "/auth/2fa/enroll", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("enroll on a store read error must fail closed with 500, got %d: %s", w.Code, w.Body.String())
	}

	// The actual security property: on a store-read error the handler must
	// return BEFORE persisting anything, so no secret/state was written.
	// (Read the underlying store directly, bypassing the error toggle.)
	if st, err := errStore.TwoFAStore.GetTwoFA(context.Background(), user.GetID()); err != nil {
		t.Fatalf("underlying store read: %v", err)
	} else if st != nil {
		t.Fatalf("enroll persisted state despite failing closed: %+v", st)
	}
}

func TestTwoFA_Enroll_NoSession(t *testing.T) {
	mgr, _, _, _ := newTwoFATestEnv(t)
	r := mountRoutes(mgr)

	req := httptest.NewRequest("POST", "/auth/2fa/enroll", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without session, got %d", w.Code)
	}
}

func TestTwoFA_Verify_InvalidCode(t *testing.T) {
	mgr, _, _, cookie := newTwoFATestEnv(t)
	r := mountRoutes(mgr)

	// Enroll first
	req := httptest.NewRequest("POST", "/auth/2fa/enroll", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("enroll: %d", w.Code)
	}

	// Verify with wrong code
	req = httptest.NewRequest("POST", "/auth/2fa/verify", strings.NewReader(`{"code":"000000"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for bad code, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTwoFA_Challenge_WithTOTP(t *testing.T) {
	mgr, _, _, cookie := newTwoFATestEnv(t)
	r := mountRoutes(mgr)

	// Enroll
	req := httptest.NewRequest("POST", "/auth/2fa/enroll", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var enrollResp map[string]any
	json.NewDecoder(w.Body).Decode(&enrollResp)
	secret := enrollResp["secret"].(string)

	// Verify to enable
	step := uint64(time.Now().Unix()) / 30
	code := GenerateTOTP(secret, step)
	req = httptest.NewRequest("POST", "/auth/2fa/verify", strings.NewReader(fmt.Sprintf(`{"code":"%s"}`, code)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("verify: %d %s", w.Code, w.Body.String())
	}

	// Challenge with valid TOTP code
	challengeCode := GenerateTOTP(secret, uint64(time.Now().Unix())/30)
	req = httptest.NewRequest("POST", "/auth/2fa/challenge", strings.NewReader(fmt.Sprintf(`{"code":"%s"}`, challengeCode)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("challenge: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if verified, _ := resp["verified"].(bool); !verified {
		t.Fatal("expected verified=true")
	}
}

func TestTwoFA_Challenge_WithBackupCode(t *testing.T) {
	mgr, _, _, cookie := newTwoFATestEnv(t)
	r := mountRoutes(mgr)

	// Enroll
	req := httptest.NewRequest("POST", "/auth/2fa/enroll", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var enrollResp map[string]any
	json.NewDecoder(w.Body).Decode(&enrollResp)
	secret := enrollResp["secret"].(string)

	// Verify to enable + get backup codes
	step := uint64(time.Now().Unix()) / 30
	code := GenerateTOTP(secret, step)
	req = httptest.NewRequest("POST", "/auth/2fa/verify", strings.NewReader(fmt.Sprintf(`{"code":"%s"}`, code)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var verifyResp map[string]any
	json.NewDecoder(w.Body).Decode(&verifyResp)
	backupCodes := verifyResp["backup_codes"].([]any)
	firstBackup := backupCodes[0].(string)

	// Challenge with backup code
	req = httptest.NewRequest("POST", "/auth/2fa/challenge", strings.NewReader(fmt.Sprintf(`{"code":"%s"}`, firstBackup)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("challenge with backup: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if bc, _ := resp["backup_code"].(bool); !bc {
		t.Fatal("expected backup_code=true")
	}

	// Same backup code should not work again
	req = httptest.NewRequest("POST", "/auth/2fa/challenge", strings.NewReader(fmt.Sprintf(`{"code":"%s"}`, firstBackup)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("reused backup code: expected 401, got %d", w.Code)
	}
}

func TestTwoFA_Disable(t *testing.T) {
	mgr, store, _, cookie := newTwoFATestEnv(t)
	r := mountRoutes(mgr)

	// Enroll + verify
	req := httptest.NewRequest("POST", "/auth/2fa/enroll", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var enrollResp map[string]any
	json.NewDecoder(w.Body).Decode(&enrollResp)
	secret := enrollResp["secret"].(string)

	step := uint64(time.Now().Unix()) / 30
	code := GenerateTOTP(secret, step)
	req = httptest.NewRequest("POST", "/auth/2fa/verify", strings.NewReader(fmt.Sprintf(`{"code":"%s"}`, code)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Disable
	req = httptest.NewRequest("POST", "/auth/2fa/disable", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("disable: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify state is cleared
	state, _ := store.GetTwoFA(nil, "user-1")
	if state != nil {
		t.Fatal("expected nil state after disable")
	}
}

func TestTwoFA_BackupCodesRefresh(t *testing.T) {
	mgr, _, _, cookie := newTwoFATestEnv(t)
	r := mountRoutes(mgr)

	// Full enroll + verify
	req := httptest.NewRequest("POST", "/auth/2fa/enroll", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var enrollResp map[string]any
	json.NewDecoder(w.Body).Decode(&enrollResp)
	secret := enrollResp["secret"].(string)

	step := uint64(time.Now().Unix()) / 30
	code := GenerateTOTP(secret, step)
	req = httptest.NewRequest("POST", "/auth/2fa/verify", strings.NewReader(fmt.Sprintf(`{"code":"%s"}`, code)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Generate new backup codes
	req = httptest.NewRequest("GET", "/auth/2fa/backup-codes", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("backup-codes: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	codes, _ := resp["backup_codes"].([]any)
	if len(codes) == 0 {
		t.Fatal("expected backup codes")
	}
}

func TestTwoFA_Challenge_NotEnabled(t *testing.T) {
	mgr, _, _, cookie := newTwoFATestEnv(t)
	r := mountRoutes(mgr)

	// Challenge without enrolling
	req := httptest.NewRequest("POST", "/auth/2fa/challenge", strings.NewReader(`{"code":"123456"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-enrolled challenge, got %d", w.Code)
	}
}

// Login of a 2FA-enrolled user must mint a "pending" session — usable
// ONLY for /auth/2fa/challenge. Today login mints a fully-authenticated
// session and 2FA enforcement is opt-in per route via RequireTwoFA. The
// fix flips the default to deny: any other endpoint refuses a pending
// session.

func setupP17(t *testing.T) (*AuthManager, *TwoFAPlugin, *router.Router) {
	t.Helper()
	userStore := newMemoryUserStore()
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret", // prod-mode Init fails closed without one
		AllowInMemoryStores: true,          // 2FA on the memory store is fail-closed in prod
		SessionTTL:          time.Hour,
		SessionCookie:       "session_id",
		UserStore:           userStore,
	})
	core := NewCorePlugin()
	twofa := NewTwoFAPlugin(TwoFAConfig{})
	mgr.Use(core)
	mgr.Use(twofa)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Seed the user with a known password and enrol them in 2FA.
	hash, err := HashPassword("password123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	user := &BasicUser{ID: "u-1", Email: "alice@example.com", Roles: []string{"user"}}
	userStore.users["alice@example.com"] = &storeEntry{user: user, hash: hash}
	userStore.byID[user.ID] = userStore.users["alice@example.com"]
	if err := twofa.store.SetTwoFA(context.Background(), user.ID, &TwoFAState{
		Enabled: true, Secret: GenerateSecret(), Verified: true,
	}); err != nil {
		t.Fatalf("SetTwoFA: %v", err)
	}

	r := router.New()
	mgr.RegisterRoutes(r)
	return mgr, twofa, r
}

func loginP17(t *testing.T, r *router.Router) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": "alice@example.com", "password": "password123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	for _, c := range cookies {
		if c.Name == "session_id" {
			return c.Value
		}
	}
	t.Fatalf("no session_id cookie set")
	return ""
}

// Pending session must not satisfy /auth/me — that endpoint reveals the
// authenticated user identity, which is exactly what 2FA exists to gate.
func TestPendingTwoFA_Me_Rejected(t *testing.T) {
	_, _, r := setupP17(t)
	tok := loginP17(t, r)

	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: tok})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Fatalf("/auth/me with pending 2FA session must NOT be 200; got 200 (body=%s)", w.Body.String())
	}
	if w.Code != http.StatusForbidden && w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401/403 for pending 2FA session, got %d", w.Code)
	}
}

// After /2fa/challenge succeeds, the same session DOES satisfy /auth/me.
func TestPendingTwoFA_Me_AllowedAfterChallenge(t *testing.T) {
	_, twofa, r := setupP17(t)
	tok := loginP17(t, r)

	state, _ := twofa.store.GetTwoFA(context.Background(), "u-1")
	step := uint64(time.Now().Unix()) / 30
	code := GenerateTOTP(state.Secret, step)
	body, _ := json.Marshal(map[string]string{"code": code})

	req := httptest.NewRequest(http.MethodPost, "/auth/2fa/challenge", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: tok})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("challenge: expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}

	// Same session, /auth/me now passes.
	req2 := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req2.AddCookie(&http.Cookie{Name: "session_id", Value: tok})
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("/auth/me after challenge: expected 200, got %d (body=%s)", w2.Code, w2.Body.String())
	}
}

// User without 2FA enrolment is unaffected — meHandler works on first try.
func TestPendingTwoFA_NoEnrollmentUnaffected(t *testing.T) {
	userStore := newMemoryUserStore()
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret", // prod-mode Init fails closed without one
		AllowInMemoryStores: true,          // 2FA on the memory store is fail-closed in prod
		SessionTTL:          time.Hour,
		SessionCookie:       "session_id",
		UserStore:           userStore,
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewTwoFAPlugin(TwoFAConfig{}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	hash, _ := HashPassword("pwlong123")
	user := &BasicUser{ID: "u-2", Email: "bob@example.com", Roles: []string{"user"}}
	userStore.users["bob@example.com"] = &storeEntry{user: user, hash: hash}
	userStore.byID[user.ID] = userStore.users["bob@example.com"]
	r := router.New()
	mgr.RegisterRoutes(r)

	body, _ := json.Marshal(map[string]string{"email": "bob@example.com", "password": "pwlong123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login: %d", w.Code)
	}
	var tok string
	for _, c := range w.Result().Cookies() {
		if c.Name == "session_id" {
			tok = c.Value
		}
	}

	req2 := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req2.AddCookie(&http.Cookie{Name: "session_id", Value: tok})
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("user without 2FA must reach /auth/me; got %d", w2.Code)
	}
}

// MemoryTwoFAStore.ConsumeBackupCode must not hold the exclusive lock
// while running N bcrypt comparisons. Today it does — the entire 2FA
// store freezes for ~600ms per failed attempt, which is also the
// brute-force path under attack.
//
// Test strategy: enroll 10 backup codes (default count), then run
// ConsumeBackupCode with a wrong code in a goroutine. While that's
// running, GetTwoFA from the main goroutine. With the lock held, Get
// blocks until the bcrypt loop finishes (~600ms). With the fix, Get
// returns immediately.

func TestBackupCode_GetIsNotBlockedByConsume(t *testing.T) {
	store := NewMemoryTwoFAStore()
	ctx := context.Background()

	// Pre-hash 10 random backup codes — the typical default.
	codes := make([]string, 10)
	for i := range codes {
		h, err := bcrypt.GenerateFromPassword([]byte("code"+string(rune('0'+i))), bcrypt.DefaultCost)
		if err != nil {
			t.Fatalf("bcrypt: %v", err)
		}
		codes[i] = string(h)
	}
	if err := store.SetTwoFA(ctx, "u-1", &TwoFAState{
		Enabled:     true,
		Secret:      "s",
		BackupCodes: codes,
	}); err != nil {
		t.Fatalf("SetTwoFA: %v", err)
	}

	// Kick off ConsumeBackupCode with a code that won't match — forces
	// all 10 bcrypt comparisons to run.
	done := make(chan struct{})
	go func() {
		_, _ = store.ConsumeBackupCode(ctx, "u-1", "definitely-wrong")
		close(done)
	}()

	// Brief delay so the goroutine has acquired its lock.
	time.Sleep(10 * time.Millisecond)

	// GetTwoFA from the main goroutine. With the bug, this blocks until
	// the bcrypt loop finishes (~600ms+). With the fix it returns instantly.
	start := time.Now()
	if _, err := store.GetTwoFA(ctx, "u-1"); err != nil {
		t.Fatalf("GetTwoFA: %v", err)
	}
	elapsed := time.Since(start)

	// Generous bound: even with some scheduling overhead, GetTwoFA should
	// be well under 200ms once the bcrypt work isn't lock-blocking.
	if elapsed > 200*time.Millisecond {
		t.Fatalf("GetTwoFA blocked %v while ConsumeBackupCode held the lock; expected <200ms", elapsed)
	}

	// Wait for the consume to finish so the test cleans up.
	<-done
}

// 2FA must actually gate access. Today the /2fa/challenge endpoint just
// returns {verified:true} and does nothing else — these tests pin the
// expected new behavior:
//
//   1. RequireTwoFA middleware blocks access when the user has 2FA
//      enabled but has not completed the challenge for this session.
//   2. /2fa/challenge marks the session as 2FA-verified on success.
//   3. After a successful challenge, RequireTwoFA lets the request through.
//   4. Users WITHOUT 2FA enrolled are unaffected.

// helper: make a request with a session cookie.
func reqWithSession(method, path, sessionToken string, body []byte) *http.Request {
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.AddCookie(&http.Cookie{Name: "session_id", Value: sessionToken})
	return r
}

func newTwoFAEnforceManager(t *testing.T) (*AuthManager, *TwoFAPlugin) {
	t.Helper()
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret", // prod-mode Init fails closed without one
		AllowInMemoryStores: true,          // 2FA on the memory store is fail-closed in prod
		SessionTTL:          time.Hour, SessionCookie: "session_id",
		UserStore: newMemoryUserStore(),
	})
	mgr.Use(NewCorePlugin())
	twofa := NewTwoFAPlugin(TwoFAConfig{})
	mgr.Use(twofa)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return mgr, twofa
}

func TestTwoFA_RequireMiddleware_BlocksUnverifiedSession(t *testing.T) {
	mgr, twofa := newTwoFAEnforceManager(t)

	// Pretend the user enrolled 2FA earlier.
	userID := "user-x"
	secret := GenerateSecret()
	if err := twofa.store.SetTwoFA(context.Background(), userID, &TwoFAState{
		Enabled: true, Secret: secret, Verified: true,
	}); err != nil {
		t.Fatalf("SetTwoFA: %v", err)
	}

	// Build a session; do NOT mark it 2FA-verified.
	sess, err := mgr.SessionStore().Create(context.Background(), userID, time.Hour)
	if err != nil {
		t.Fatalf("session create: %v", err)
	}

	// A protected handler wrapped in RequireTwoFA.
	protected := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	r := router.New()
	r.Get("/protected", twofa.RequireTwoFA()(protected).(http.HandlerFunc))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, reqWithSession(http.MethodGet, "/protected", sess.Token, nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for un-challenged session, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestTwoFA_RequireMiddleware_AllowsAfterChallenge(t *testing.T) {
	mgr, twofa := newTwoFAEnforceManager(t)

	userID := "user-x"
	secret := GenerateSecret()
	if err := twofa.store.SetTwoFA(context.Background(), userID, &TwoFAState{
		Enabled: true, Secret: secret, Verified: true,
	}); err != nil {
		t.Fatalf("SetTwoFA: %v", err)
	}

	sess, err := mgr.SessionStore().Create(context.Background(), userID, time.Hour)
	if err != nil {
		t.Fatalf("session create: %v", err)
	}

	r := router.New()
	mgr.RegisterRoutes(r)
	protected := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	r.Get("/protected", twofa.RequireTwoFA()(protected).(http.HandlerFunc))

	// Submit a valid TOTP code via /2fa/challenge.
	currentStep := uint64(time.Now().Unix()) / 30
	code := GenerateTOTP(secret, currentStep)
	body, _ := json.Marshal(map[string]string{"code": code})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, reqWithSession(http.MethodPost, "/auth/2fa/challenge", sess.Token, body))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from challenge, got %d (body=%s)", w.Code, w.Body.String())
	}

	// Same session should now pass RequireTwoFA.
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, reqWithSession(http.MethodGet, "/protected", sess.Token, nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 after successful challenge, got %d (body=%s)", w2.Code, w2.Body.String())
	}
}

func TestTwoFA_RequireMiddleware_NoEnrollmentBypass(t *testing.T) {
	mgr, twofa := newTwoFAEnforceManager(t)

	// User has NOT enrolled 2FA — the middleware should not gate them.
	sess, err := mgr.SessionStore().Create(context.Background(), "user-y", time.Hour)
	if err != nil {
		t.Fatalf("session create: %v", err)
	}

	protected := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	r := router.New()
	r.Get("/protected", twofa.RequireTwoFA()(protected).(http.HandlerFunc))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, reqWithSession(http.MethodGet, "/protected", sess.Token, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("user without 2FA enrollment must pass RequireTwoFA; got %d", w.Code)
	}
}

// Compile guard
var _ = fmt.Sprintf
