package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// loginSessionCookie logs the seeded newTwoFATestEnv user in again and
// returns the fresh session cookie. After 2FA is enabled every new login
// mints a PendingTwoFactor session, which is exactly the attacker-held
// session shape the replay attack needs.
func loginSessionCookie(t *testing.T, r http.Handler) string {
	t.Helper()
	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"email":"alice@test.com","password":"testpass"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "session_id" {
			return c.Value
		}
	}
	t.Fatal("no session_id cookie after login")
	return ""
}

// Property: a one-time credential is single-use. RFC 6238 §5.2 requires the
// verifier to reject a second attempt after a successful validation.
//
// Attack: TOTP codes are replayable within the ±skew window. A code
// observed once (phishing relay, form MITM, shoulder-surf) steps up ANY
// pending session for that user: the same 6 digits mark victim and attacker
// sessions verified in sequence. The battery burns every sibling one-time
// credential on use (oauth2_test.go state nonce, password reset tokens,
// magic links, backup codes via ConsumeBackupCode); TOTP is the only
// replayable one because neither challengeHandler nor verifyHandler records
// the accepted timestep.
//
// Minimal fix (not applied): persist the consumed timestep on TwoFAState
// (field + store round-trip, mirroring ConsumeBackupCode) and refuse it in
// ValidateTOTP's callers.
func TestTOTPCodeNotReusableWithinWindow(t *testing.T) {
	mgr, _, _, cookie := newTwoFATestEnv(t)
	_ = mgr
	r := mountRoutes(mgr)

	// Enroll, then verify with the code for the current step (enables 2FA).
	req := httptest.NewRequest("POST", "/auth/2fa/enroll", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("enroll: %d %s", w.Code, w.Body.String())
	}
	var enrollResp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&enrollResp); err != nil {
		t.Fatal(err)
	}
	secret := enrollResp["secret"].(string)

	verifyCode := GenerateTOTP(secret, uint64(time.Now().Unix())/30)
	req = httptest.NewRequest("POST", "/auth/2fa/verify", strings.NewReader(fmt.Sprintf(`{"code":"%s"}`, verifyCode)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("verify: %d %s", w.Code, w.Body.String())
	}

	// Victim logs in (pending session) and steps up with a live code.
	victim := loginSessionCookie(t, r)
	code := GenerateTOTP(secret, uint64(time.Now().Unix())/30)
	req = httptest.NewRequest("POST", "/auth/2fa/challenge", strings.NewReader(fmt.Sprintf(`{"code":"%s"}`, code)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: victim})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("victim challenge: %d %s", w.Code, w.Body.String())
	}

	// Attacker holds a second pending session and replays the SAME code.
	attacker := loginSessionCookie(t, r)
	req = httptest.NewRequest("POST", "/auth/2fa/challenge", strings.NewReader(fmt.Sprintf(`{"code":"%s"}`, code)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: attacker})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [totp-replay] used code re-verified on a second session within the ±skew window: got %d (%s), want 401. The timestep is never consumed (RFC 6238 §5.2), so one observed code steps up unlimited sessions for ~90s", w.Code, w.Body.String())
	}
}

// Property: state-changing operations must not be reachable via CSRF-exempt
// safe methods. csrf.go enforces double-submit protection on
// POST/PUT/PATCH/DELETE only and lets GET/HEAD/OPTIONS through by design,
// so a mutating GET escapes the CSRF model entirely: for sessions minted by
// the OAuth callback or magic-link verify (SameSite=Lax, unlike password
// login's Strict), a top-level cross-site GET navigation carries the cookie
// and silently regenerates the victim's backup codes, invalidating the
// codes they saved (lockout/availability attack).
//
// CONTRACT note: the route shape is documented as GET
// ("GET {basePath}/2fa/backup-codes", twofa.go:641), so flipping it to POST
// is a contract change that belongs to the human, per the adversarial-tests
// policy. This test pins the security property (a GET must not regenerate
// credentials); the fix is the method flip (or an equivalent refusal).
func TestBackupCodesRejectsGetRequests(t *testing.T) {
	mgr, _, _, cookie := newTwoFATestEnv(t)
	r := mountRoutes(mgr)

	// Enroll + verify so the session is stepped up.
	req := httptest.NewRequest("POST", "/auth/2fa/enroll", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var enrollResp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&enrollResp); err != nil {
		t.Fatal(err)
	}
	code := GenerateTOTP(enrollResp["secret"].(string), uint64(time.Now().Unix())/30)
	req = httptest.NewRequest("POST", "/auth/2fa/verify", strings.NewReader(fmt.Sprintf(`{"code":"%s"}`, code)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("verify: %d %s", w.Code, w.Body.String())
	}

	// The attack: a bare top-level GET regenerates the credential set.
	req = httptest.NewRequest("GET", "/auth/2fa/backup-codes", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusOK || strings.Contains(w.Body.String(), "backup_codes") {
		t.Fatalf("SECURITY: [csrf-safe-method] GET /auth/2fa/backup-codes regenerated backup codes (%d): %s. csrf.go exempts safe methods, so no token is ever checked for this state-changing route; a SameSite=Lax session (OAuth/magic-link login) rides any top-level cross-site link", w.Code, w.Body.String())
	}
}

// racingDisableTwoFAStore injects the disable-side of the read-modify-write
// race: the first SetTwoFA after arming first applies the concurrent
// DeleteTwoFA that disableHandler committed between backupCodesHandler's Get
// and its Set, then lets the stale write proceed. This mirrors the
// casTestHook seam EntityTwoFAStore uses to exercise ConsumeBackupCode's CAS
// against the same interleaving (entity_twofa_store.go). A fix that adds a
// CAS write path must route through this wrapper for the test to stay
// honest.
type racingDisableTwoFAStore struct {
	TwoFAStore
	fire bool
}

func (s *racingDisableTwoFAStore) SetTwoFA(ctx context.Context, userID string, state *TwoFAState) error {
	if s.fire {
		s.fire = false
		if err := s.TwoFAStore.DeleteTwoFA(ctx, userID); err != nil {
			return err
		}
	}
	return s.TwoFAStore.SetTwoFA(ctx, userID, state)
}

// Property: read-modify-write transitions on persisted auth state must not
// blind-write over concurrent mutations (lost update / resurrection).
//
// Attack: backupCodesHandler does Get → mutate → SetTwoFA while both
// MemoryTwoFAStore.SetTwoFA and EntityTwoFAStore.SetTwoFA are blind upserts
// (the entity store's INSERT arm even recreates the row at version 0). When
// a regenerate races the user disabling 2FA on another session, the stale
// Set re-creates an Enabled=true row AFTER DeleteTwoFA landed: 2FA is
// silently re-enabled after the user disabled it — the exact delete+re-enrol
// ABA shape ConsumeBackupCode's version+bytes CAS exists to prevent.
//
// Minimal fix (not applied): a compare-and-swap write for handler
// read-modify-write cycles (expected version, mirroring ConsumeBackupCode),
// or a re-read-and-refuse when the row changed or vanished.
func TestTwoFARegenerateDoesNotResurrect(t *testing.T) {
	inner := NewMemoryTwoFAStore()
	wrapped := &racingDisableTwoFAStore{TwoFAStore: inner}
	userStore := newMemoryUserStore()
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret",
		AllowInMemoryStores: true,
		SessionCookie:       "session_id",
		SessionTTL:          time.Hour,
		UserStore:           userStore,
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewTwoFAPlugin(TwoFAConfig{Store: wrapped}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	seedUser(t, userStore, "alice@test.com", "testpass")
	r := mountRoutes(mgr)
	cookie := loginSessionCookie(t, r)
	sess, err := mgr.SessionStore().Get(context.Background(), cookie)
	if err != nil || sess == nil {
		t.Fatalf("session lookup: %v", err)
	}
	userID := sess.UserID

	// Enroll + verify to enable 2FA and step the session up.
	req := httptest.NewRequest("POST", "/auth/2fa/enroll", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var enrollResp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&enrollResp); err != nil {
		t.Fatal(err)
	}
	code := GenerateTOTP(enrollResp["secret"].(string), uint64(time.Now().Unix())/30)
	req = httptest.NewRequest("POST", "/auth/2fa/verify", strings.NewReader(fmt.Sprintf(`{"code":"%s"}`, code)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("verify: %d %s", w.Code, w.Body.String())
	}

	// Arm the race: the disable lands between the handler's Get and Set.
	wrapped.fire = true
	req = httptest.NewRequest("GET", "/auth/2fa/backup-codes", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Whatever the handler answered, 2FA must stay disabled: the stale
	// write must not resurrect an Enabled row over the delete.
	got, err := inner.GetTwoFA(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetTwoFA: %v", err)
	}
	if got != nil && got.Enabled {
		t.Fatalf("SECURITY: [twofa-resurrect] backup-code regenerate resurrected an Enabled 2FA row after DeleteTwoFA landed between its Get and its blind SetTwoFA (handler answered %d): 2FA re-enabled itself after the user disabled it", w.Code)
	}
}
