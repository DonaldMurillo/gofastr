package auth

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/router"
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

// CompareAndSetTwoFA fires the same interleaving against the CAS path, so a
// fix that routes around SetTwoFA still meets the disable landing mid-write.
func (s *racingDisableTwoFAStore) CompareAndSetTwoFA(ctx context.Context, userID string, next *TwoFAState) (bool, error) {
	if s.fire {
		s.fire = false
		if err := s.TwoFAStore.DeleteTwoFA(ctx, userID); err != nil {
			return false, err
		}
	}
	cas, ok := s.TwoFAStore.(TwoFACompareAndSetter)
	if !ok {
		return false, nil
	}
	return cas.CompareAndSetTwoFA(ctx, userID, next)
}

// CompareAndSwapTwoFA fires the same interleaving against the state-swap
// path, so a fix that routes enroll/verify/challenge writes through
// CompareAndSwapTwoFA still meets the disable landing mid-write.
func (s *racingDisableTwoFAStore) CompareAndSwapTwoFA(ctx context.Context, userID string, expect, next *TwoFAState) (bool, error) {
	if s.fire {
		s.fire = false
		if err := s.TwoFAStore.DeleteTwoFA(ctx, userID); err != nil {
			return false, err
		}
	}
	sw, ok := s.TwoFAStore.(TwoFAStateSwapper)
	if !ok {
		return false, nil
	}
	return sw.CompareAndSwapTwoFA(ctx, userID, expect, next)
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
	req = httptest.NewRequest("POST", "/auth/2fa/backup-codes", nil)
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

// ─── Single-use TOTP under concurrent presentation ────────────────────────

// rendezvousTwoFAStore pins the read→write race window in challengeHandler
// open deterministically: when armed, the FIRST GetTwoFA call reads its
// snapshot and then blocks until the SECOND arrives. Both callers therefore
// hold pre-write copies of the state before either can reach the write — no
// scheduling luck involved. The fix (CompareAndSwapTwoFA on the read state)
// makes each write conditional, so the second presentation's swap fails.
type rendezvousTwoFAStore struct {
	*MemoryTwoFAStore
	armed     atomic.Bool
	entered   atomic.Int64
	release   chan struct{}
	closeOnce sync.Once
}

func (s *rendezvousTwoFAStore) GetTwoFA(ctx context.Context, userID string) (*TwoFAState, error) {
	st, err := s.MemoryTwoFAStore.GetTwoFA(ctx, userID)
	if err != nil {
		return nil, err
	}
	if s.armed.Load() {
		if s.entered.Add(1) == 1 {
			select {
			case <-s.release:
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(10 * time.Second):
				// Fall through rather than hang forever; the assertions
				// below report whatever actually happened.
			}
		} else {
			s.closeOnce.Do(func() { close(s.release) })
		}
	}
	return st, nil
}

// Property: one TOTP code authenticates at most one session, even under
// concurrent presentation (RFC 6238 §5.2). The step-consume write rides
// CompareAndSwapTwoFA on the state the handler read, so the second
// concurrent presentation of the same step loses.
func TestTwoFAChallengeCodeSingleUseConcurrent(t *testing.T) {
	_, twofa, r := setupP17(t)

	rendezvous := &rendezvousTwoFAStore{
		MemoryTwoFAStore: twofa.store.(*MemoryTwoFAStore),
		release:          make(chan struct{}),
	}
	twofa.store = rendezvous

	// Two distinct pending sessions for the same enrolled user.
	tok1 := loginP17(t, r)
	tok2 := loginP17(t, r)
	if tok1 == tok2 {
		t.Fatalf("expected two distinct pending sessions, got the same token")
	}

	state, err := rendezvous.GetTwoFA(context.Background(), "u-1")
	if err != nil || state == nil || !state.Enabled {
		t.Fatalf("seeded 2FA state missing: state=%v err=%v", state, err)
	}
	step := uint64(time.Now().Unix()) / 30
	if state.LastUsedStep >= step {
		t.Fatalf("LastUsedStep %d not below the current step %d", state.LastUsedStep, step)
	}
	code := GenerateTOTP(state.Secret, step)

	type outcome struct {
		status   int
		verified bool
	}
	post := func(tok string) outcome {
		body, _ := json.Marshal(map[string]string{"code": code})
		req := httptest.NewRequest(http.MethodPost, "/auth/2fa/challenge", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session_id", Value: tok})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var resp struct {
			Verified bool `json:"verified"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return outcome{status: w.Code, verified: resp.Verified}
	}

	rendezvous.armed.Store(true)
	outcomes := make(chan outcome, 2)
	for _, tok := range []string{tok1, tok2} {
		go func(tok string) { outcomes <- post(tok) }(tok)
	}
	got := []outcome{<-outcomes, <-outcomes}

	verified := 0
	for _, o := range got {
		if o.status == http.StatusOK && o.verified {
			verified++
		}
	}
	if verified != 1 {
		t.Errorf("one TOTP code presented concurrently to two pending sessions verified %d sessions; at most 1 is allowed (outcomes: %+v)", verified, got)
	}
}

// ─── A committed disable wins over racing read-modify-writes ──────────────
//
// The backup-codes transition is pinned above
// (TestTwoFARegenerateDoesNotResurrect); the three tests below cover the
// other transitions. All route their writes through TwoFAStateSwapper
// (CompareAndSwapTwoFA), so a DeleteTwoFA that committed between the
// handler's read and its write fails the swap instead of resurrecting the
// row.

// setupResurrectRace mounts core + 2FA over a racingDisableTwoFAStore (the
// deterministic interleaving rig above: arming `fire` makes the next
// state write first apply the concurrent DeleteTwoFA that disableHandler
// committed between the handler's Get and its write, then lets the stale
// write proceed — no scheduling luck involved), seeds the enrolled user
// with the given 2FA state, and logs in. The login session's shape
// follows the seed: Enabled=true mints a PendingTwoFactor session (the
// challenge shape), a pending seed mints a full session (the
// enroll/verify step-up shape).
func setupResurrectRace(t *testing.T, seed *TwoFAState) (*MemoryTwoFAStore, *racingDisableTwoFAStore, *router.Router, string) {
	t.Helper()
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
	hash, err := HashPassword("password123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	user := &BasicUser{ID: "u-1", Email: "alice@example.com", Roles: []string{"user"}}
	userStore.users["alice@example.com"] = &storeEntry{user: user, hash: hash}
	userStore.byID[user.ID] = userStore.users["alice@example.com"]
	if err := inner.SetTwoFA(context.Background(), user.ID, seed); err != nil {
		t.Fatalf("seed SetTwoFA: %v", err)
	}
	r := mountRoutes(mgr)
	return inner, wrapped, r, loginP17(t, r)
}

// Property: a disable that commits wins over enroll's read-modify-write —
// the deleted factor stays deleted. Scoping: enroll always writes
// Enabled=false, so this is the PENDING row's resurrection (the user's
// disable did not stick), not a live Enabled factor.
func TestTwoFAEnrollNoResurrect(t *testing.T) {
	inner, wrapped, r, cookie := setupResurrectRace(t, &TwoFAState{
		Enabled: false, Secret: GenerateSecret(), Verified: false,
	})

	wrapped.fire = true
	req := httptest.NewRequest(http.MethodPost, "/auth/2fa/enroll", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Whatever the handler answered, the row the disable deleted must not
	// come back.
	got, err := inner.GetTwoFA(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("GetTwoFA: %v", err)
	}
	if got != nil {
		t.Errorf("SECURITY: [twofa-resurrect] a concurrent disable landed between enroll's read and its write, and the stale write re-created the deleted 2FA row (pending enrollment resurrected, Enabled=%v; handler answered %d): the user's disable did not stick", got.Enabled, w.Code)
	}
}

// Property: a disable that commits wins over verify's enable write — the
// deleted factor stays deleted. Pre-fix, the stale Set wrote
// Enabled=true, Verified=true straight back over the delete: 2FA silently
// ON again moments after the user turned it off.
func TestTwoFAVerifyNoResurrect(t *testing.T) {
	inner, wrapped, r, cookie := setupResurrectRace(t, &TwoFAState{
		Enabled: false, Secret: GenerateSecret(), Verified: false,
	})

	state, err := inner.GetTwoFA(context.Background(), "u-1")
	if err != nil || state == nil {
		t.Fatalf("seeded pending enrollment missing: state=%v err=%v", state, err)
	}
	code := GenerateTOTP(state.Secret, uint64(time.Now().Unix())/30)

	wrapped.fire = true
	body, _ := json.Marshal(map[string]string{"code": code})
	req := httptest.NewRequest(http.MethodPost, "/auth/2fa/verify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Whatever the handler answered, 2FA must stay disabled: the stale
	// write must not resurrect an Enabled row over the delete.
	got, err := inner.GetTwoFA(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("GetTwoFA: %v", err)
	}
	if got != nil && got.Enabled {
		t.Errorf("SECURITY: [twofa-resurrect] a concurrent disable landed between verify's read and its write, and the stale write resurrected an Enabled=true 2FA row (handler answered %d): 2FA re-enabled itself after the user disabled it", w.Code)
	}
}

// Property: a disable that commits wins over challenge's step-consume
// write — the deleted factor stays deleted. Pre-fix, a login challenge
// completing moments after the user disabled 2FA re-created the
// Enabled=true row.
func TestTwoFAChallengeNoResurrect(t *testing.T) {
	inner, wrapped, r, cookie := setupResurrectRace(t, &TwoFAState{
		Enabled: true, Secret: GenerateSecret(), Verified: true,
	})

	state, err := inner.GetTwoFA(context.Background(), "u-1")
	if err != nil || state == nil || !state.Enabled {
		t.Fatalf("seeded enabled 2FA state missing: state=%v err=%v", state, err)
	}
	step := uint64(time.Now().Unix()) / 30
	if state.LastUsedStep >= step {
		t.Fatalf("LastUsedStep %d not below the current step %d", state.LastUsedStep, step)
	}
	code := GenerateTOTP(state.Secret, step)

	wrapped.fire = true
	body, _ := json.Marshal(map[string]string{"code": code})
	req := httptest.NewRequest(http.MethodPost, "/auth/2fa/challenge", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Whatever the handler answered, 2FA must stay disabled: the stale
	// write must not resurrect an Enabled row over the delete.
	got, err := inner.GetTwoFA(context.Background(), "u-1")
	if err != nil {
		t.Fatalf("GetTwoFA: %v", err)
	}
	if got != nil && got.Enabled {
		t.Errorf("SECURITY: [twofa-resurrect] a concurrent disable landed between challenge's read and its write, and the stale write resurrected an Enabled=true 2FA row (handler answered %d): 2FA re-enabled itself after the user disabled it", w.Code)
	}
}

// ─── TOTP secret sealed at rest ───────────────────────────────────────────

// newSealedTwoFAStore builds an EntityTwoFAStore with an EncryptionKey, the
// documented production posture for the secret column.
func newSealedTwoFAStore(t *testing.T) *EntityTwoFAStore {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s, err := NewEntityTwoFAStore(db, "auth_twofa", EntityTwoFAStoreConfig{
		EncryptionKey: []byte("test-sealing-key-not-a-secret"),
	})
	if err != nil {
		t.Fatalf("NewEntityTwoFAStore: %v", err)
	}
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return s
}

// Property: a password-equivalent credential (the TOTP seed authenticates
// every 2FA-gated route) is not recoverable from a raw DB read. The
// column is sealed with the same AES-GCM helper the OAuth token store
// uses when the store is built with an EncryptionKey.
func TestTwoFAStoreSealsSecretAtRest(t *testing.T) {
	s := newSealedTwoFAStore(t)
	ctx := context.Background()

	const secret = "JBSWY3DPEHPK3PXPSECRETSEED42"
	if err := s.SetTwoFA(ctx, "u-seal", &TwoFAState{
		Enabled: true, Verified: true, Secret: secret,
	}); err != nil {
		t.Fatalf("SetTwoFA: %v", err)
	}

	var raw sql.NullString
	if err := s.db.QueryRow("SELECT secret FROM "+s.table+" WHERE user_id = $1", "u-seal").Scan(&raw); err != nil {
		t.Fatalf("raw read: %v", err)
	}
	if !raw.Valid {
		t.Fatal("no secret row stored — setup no longer reaches the sink")
	}
	stored := raw.String
	if stored == secret || strings.Contains(stored, secret) || strings.Contains(secret, stored) {
		t.Errorf("SECURITY: [twofa-at-rest] auth_twofa.secret stores the live TOTP seed recoverable by a raw read "+
			"(stored %q vs plaintext %q); the column must be sealed when the store is built with an EncryptionKey", stored, secret)
	}

	// The sealed value still reads back usable through the store.
	got, err := s.GetTwoFA(ctx, "u-seal")
	if err != nil || got == nil || got.Secret != secret {
		t.Fatalf("sealed round-trip: state=%v err=%v, want secret %q", got, err, secret)
	}
}

// Property: rows written as plaintext before sealing was enabled still
// verify (read both forms), and the next write re-seals them.
func TestTwoFAStoreReadsLegacyPlaintext(t *testing.T) {
	s := newSealedTwoFAStore(t)
	ctx := context.Background()

	const secret = "JBSWY3DPEHPK3PXPLEGACYSEED7"
	if _, err := s.db.Exec(
		"INSERT INTO "+s.table+" (user_id, enabled, secret, backup_codes, verified, last_used_step, version) VALUES ($1, 1, $2, '[]', 1, 0, 0)",
		"u-legacy", secret,
	); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	// Read path: the plaintext row comes back verbatim and still verifies.
	got, err := s.GetTwoFA(ctx, "u-legacy")
	if err != nil || got == nil || got.Secret != secret || !got.Enabled {
		t.Fatalf("legacy plaintext read: state=%v err=%v, want enabled row with secret %q", got, err, secret)
	}
	if !ValidateTOTP(got.Secret, GenerateTOTP(secret, uint64(time.Now().Unix())/30), 30, 1) {
		t.Fatal("legacy secret no longer validates a live code")
	}

	// Write path: the next SetTwoFA seals the column.
	got.Verified = true
	if err := s.SetTwoFA(ctx, "u-legacy", got); err != nil {
		t.Fatalf("SetTwoFA re-write: %v", err)
	}
	var raw string
	if err := s.db.QueryRow("SELECT secret FROM "+s.table+" WHERE user_id = $1", "u-legacy").Scan(&raw); err != nil {
		t.Fatalf("raw read: %v", err)
	}
	if raw == secret {
		t.Fatalf("SECURITY: [twofa-at-rest] rewrite of a legacy row left the plaintext secret in the column")
	}
	again, err := s.GetTwoFA(ctx, "u-legacy")
	if err != nil || again == nil || again.Secret != secret {
		t.Fatalf("re-sealed round-trip: state=%v err=%v", again, err)
	}
}
