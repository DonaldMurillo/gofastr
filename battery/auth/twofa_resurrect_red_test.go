//go:build red

// RED TESTS — open findings, 2026-09-03 adversarial pass round 4 (tests-only;
// no fix applied).
//
// Family: a committed disable (DeleteTwoFA) must win over racing
// read-modify-write writes — a stale SetTwoFA that started before the delete
// must not resurrect the factor. The backup-codes transition got the fix
// (CompareAndSetTwoFA, twofa.go:747) and the pin
// (twofa_security_test.go TestTwoFARegenerateDoesNotResurrect); these reds
// cover the other three transitions, which all still end in the same blind
// full-state SetTwoFA upsert.
//
// Distinct from TestTwoFARedConcurrentCodeSingleUse (twofa_red_test.go):
// that pin is the REPLAY property (two concurrent presentations of one code);
// these are the RESURRECT property (a delete racing one presentation). Both
// stand independently.

package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// setupRedResurrect mounts core + 2FA over a racingDisableTwoFAStore (the
// deterministic interleaving rig from twofa_security_test.go: arming `fire`
// makes the next SetTwoFA first apply the concurrent DeleteTwoFA that
// disableHandler committed between the handler's Get and its Set, then lets
// the stale write proceed — no scheduling luck involved), seeds the enrolled
// user with the given 2FA state, and logs in. The login session's shape
// follows the seed: Enabled=true mints a PendingTwoFactor session (the
// challenge shape), a pending seed mints a full session (the
// enroll/verify step-up shape).
func setupRedResurrect(t *testing.T, seed *TwoFAState) (*MemoryTwoFAStore, *racingDisableTwoFAStore, *router.Router, string) {
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

// RED TEST — open finding, 2026-09-03 adversarial pass round 4 (tests-only; no fix applied).
// Property: a disable that commits wins over a racing read-modify-write —
// the deleted factor stays deleted (no delete+re-enrol ABA resurrection).
// Surfaces: twofa.go:enrollHandler:392-411 — GetTwoFA (:392, the
// "already enabled" guard) → generate secret → blind SetTwoFA (:411), racing
// disableHandler's DeleteTwoFA (:702). The CompareAndSetTwoFA seam exists
// (twofa.go:167) but only backupCodesHandler uses it (:747).
// Finding: with the disable landing between enroll's Get and its Set, the
// stale Set re-creates the row the user just deleted: a pending enrollment
// reappears after an explicit disable. Honest scoping: enroll always writes
// Enabled=false, so this resurrects the PENDING row (the user's disable did
// not stick; the enrollment survives), not a live Enabled factor — weaker
// than the verify/challenge siblings below, same lost-delete window.
// Severity: P3 — pending-state resurrection only; availability/UX plus the
// principle that a committed delete must win.
// Fix direction: route enrollHandler's write through TwoFACompareAndSetter
// (refuse with 409 when the row changed or vanished), the fix shape
// backupCodesHandler already demonstrates.
func TestTwoFAEnrollRedNoResurrect(t *testing.T) {
	inner, wrapped, r, cookie := setupRedResurrect(t, &TwoFAState{
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
		t.Errorf("SECURITY: [twofa-resurrect] a concurrent disable landed between enroll's Get and its blind SetTwoFA, and the stale write re-created the deleted 2FA row (pending enrollment resurrected, Enabled=%v; handler answered %d): the user's disable did not stick", got.Enabled, w.Code)
	}
}

// RED TEST — open finding, 2026-09-03 adversarial pass round 4 (tests-only; no fix applied).
// Property: a disable that commits wins over a racing read-modify-write —
// the deleted factor stays deleted.
// Surfaces: twofa.go:verifyHandler:456-488 — GetTwoFA (:456) → ValidateTOTP
// (:472) → generate backup codes → blind SetTwoFA (:488) writing
// Enabled=true, racing disableHandler's DeleteTwoFA (:702).
// Finding: with the disable landing between verify's Get and its Set, the
// stale Set writes an Enabled=true, Verified=true row straight back over the
// delete: 2FA is silently ON again moments after the user turned it off.
// The disable path (step-up gated) and the enroll-confirm path (same session
// or a second one mid-enrollment) can interleave exactly this way — the
// enroll/confirm and disable flows are both driven by the user in a settings
// screen, and the interleaving is the same window ConsumeBackupCode's CAS
// exists to prevent (entity_twofa_store.go documents this ABA at :218).
// Severity: P2 — a security factor the user explicitly removed comes back
// enabled; every subsequent login demands the TOTP code the user believed
// retired (lockout), and the resurrected row re-arms 2FA the user may no
// longer have their authenticator entry for.
// Fix direction: route verifyHandler's write through
// TwoFACompareAndSetter (409 on row-changed/row-vanished), mirroring
// backupCodesHandler.
func TestTwoFAVerifyRedNoResurrect(t *testing.T) {
	inner, wrapped, r, cookie := setupRedResurrect(t, &TwoFAState{
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
		t.Errorf("SECURITY: [twofa-resurrect] a concurrent disable landed between verify's Get and its blind SetTwoFA, and the stale write resurrected an Enabled=true 2FA row (handler answered %d): 2FA re-enabled itself after the user disabled it", w.Code)
	}
}

// RED TEST — open finding, 2026-09-03 adversarial pass round 4 (tests-only; no fix applied).
// Property: a disable that commits wins over a racing read-modify-write —
// the deleted factor stays deleted.
// Surfaces: twofa.go:challengeHandler:548-561 — GetTwoFA (:548) →
// ValidateTOTPStep + step>LastUsedStep (:559) → blind SetTwoFA (:561, the
// step-consume write), racing disableHandler's DeleteTwoFA (:702).
// Finding: with the disable landing between challenge's Get and its Set, the
// step-consume write re-creates the deleted Enabled=true row: a login
// challenge completing moments after the user disabled 2FA resurrects the
// factor. Same shape as the backup-codes pin, different handler.
// Severity: P2 — a security factor the user explicitly removed comes back
// enabled; logins keep demanding a retired TOTP code (lockout) and the
// factor's enforcement the user meant to drop stays armed.
// Fix direction: route challengeHandler's step-consume write through
// TwoFACompareAndSetter (fail the challenge with 409 when the row changed
// or vanished), mirroring backupCodesHandler.
func TestTwoFAChallengeRedNoResurrect(t *testing.T) {
	inner, wrapped, r, cookie := setupRedResurrect(t, &TwoFAState{
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
		t.Errorf("SECURITY: [twofa-resurrect] a concurrent disable landed between challenge's Get and its blind SetTwoFA, and the stale write resurrected an Enabled=true 2FA row (handler answered %d): 2FA re-enabled itself after the user disabled it", w.Code)
	}
}
