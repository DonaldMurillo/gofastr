//go:build red

// RED TEST — open finding, 2026-09-02 adversarial pass (tests-only; no fix applied).
// Property: brute-force-sensitive auth endpoints ship a default per-IP throttle,
// like login/register do (AuthConfig.defaults installs those unconditionally).
// Surfaces: twofa.go:TwoFAConfig.defaults, twofa.go:NewTwoFAPlugin,
// twofa.go:challengeHandler, twofa.go:verifyHandler.
// Finding: TwoFAConfig.defaults installs no RateLimit, so with config left at
// defaults p.challengeLimit is nil, the guard in challengeHandler/verifyHandler
// is skipped, and 100 wrong 6-digit codes from one IP all get plain 401s —
// TwoFAConfig.RateLimit's own doc names the attack (stolen session,
// ~333k expected attempts at skew=1).
// Fix direction: install a default per-IP RateLimit in TwoFAConfig.defaults
// (opt-out, not opt-in), mirroring AuthConfig.defaults' login/register floors.

package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTwoFARedDefaultChallengeThrottle(t *testing.T) {
	_, twofa, r := setupP17(t)
	tok := loginP17(t, r)

	// A syntactically valid 6-digit code from step 0 (1970): always outside
	// the ±skew window, so every attempt fails validation deterministically.
	state, err := twofa.store.GetTwoFA(context.Background(), "u-1")
	if err != nil || state == nil || !state.Enabled {
		t.Fatalf("seeded 2FA state missing: state=%v err=%v", state, err)
	}
	wrongCode := GenerateTOTP(state.Secret, 0)

	throttled := 0
	for range 100 {
		body, _ := json.Marshal(map[string]string{"code": wrongCode})
		req := httptest.NewRequest(http.MethodPost, "/auth/2fa/challenge", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session_id", Value: tok})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		switch w.Code {
		case http.StatusTooManyRequests:
			throttled++
		case http.StatusUnauthorized, http.StatusBadRequest:
			// wrong code refused, as it must be
		default:
			t.Fatalf("unexpected status %d (body=%s)", w.Code, w.Body.String())
		}
	}
	if throttled == 0 {
		t.Errorf("100 wrong 6-digit codes from one IP against /auth/2fa/challenge were never throttled (0 of 100 responses were 429); the 2FA endpoints need a default per-IP rate limit like login/register")
	}
}

// RED TEST — open finding, 2026-09-02 adversarial pass (tests-only; no fix applied).
// Property: one TOTP code authenticates at most one session, even under
// concurrent presentation (RFC 6238 §5.2).
// Surfaces: twofa.go:challengeHandler (GetTwoFA → step > LastUsedStep check →
// blind SetTwoFA), twofa.go:TwoFACompareAndSetter (shipped, used only by
// backup-code regeneration).
// Finding: the LastUsedStep check and the write are separate store calls, so
// two concurrent challenge POSTs carrying the same valid code both read the
// stale state, both pass the check, both write, and both sessions get
// 200 verified:true.
// Fix direction: make the step-consume atomic (a compare-and-set on
// LastUsedStep, the TwoFACompareAndSetter seam the package already ships) so
// the second presentation of a step fails.

// rendezvousTwoFAStore pins the read→write race window in challengeHandler
// open deterministically: when armed, the FIRST GetTwoFA call reads its
// snapshot and then blocks until the SECOND arrives. Both callers therefore
// hold pre-write copies of the state before either can reach SetTwoFA — no
// scheduling luck involved.
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

func TestTwoFARedConcurrentCodeSingleUse(t *testing.T) {
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

// RED TEST — open finding, 2026-09-02 adversarial pass round 2 (tests-only;
// no fix applied). Pins the /2fa/verify half of the default-throttle finding
// above: the same nil p.challengeLimit guard also leaves verifyHandler
// unthrottled, and that endpoint is brute-force sensitive in its own right —
// a stolen session of an account mid-enrollment can guess the 6-digit code
// and enable 2FA on the victim's account (lockout/takeover).
// Property: brute-force-sensitive auth endpoints ship a default per-IP
// throttle, like login/register do.
// Surfaces: twofa.go:TwoFAConfig.defaults (no RateLimit installed),
// twofa.go:verifyHandler challengeLimit guard.
// Finding: with config left at defaults, 100 wrong 6-digit codes from one IP
// against /auth/2fa/verify all get plain 401s.
// Fix direction: same as TestTwoFARedDefaultChallengeThrottle — install a
// default per-IP RateLimit in TwoFAConfig.defaults (opt-out, not opt-in).
func TestTwoFAVerifyRedDefaultThrottle(t *testing.T) {
	twofa, r, session := setupRedTwoFAEnrollment(t)

	// A syntactically valid 6-digit code from step 0 (1970): always outside
	// the ±skew window, so every attempt fails validation deterministically.
	// The enrollment is pending (Enabled=false) so verifyHandler reaches the
	// TOTP check, not an early "already enabled" / auth refusal.
	state, err := twofa.store.GetTwoFA(context.Background(), "u-1")
	if err != nil || state == nil || state.Enabled {
		t.Fatalf("seeded pending 2FA enrollment missing: state=%v err=%v", state, err)
	}
	wrongCode := GenerateTOTP(state.Secret, 0)

	throttled := 0
	for range 100 {
		body, _ := json.Marshal(map[string]string{"code": wrongCode})
		req := httptest.NewRequest(http.MethodPost, "/auth/2fa/verify", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session_id", Value: session})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		switch w.Code {
		case http.StatusTooManyRequests:
			throttled++
		case http.StatusUnauthorized, http.StatusBadRequest:
			// wrong code refused, as it must be
		default:
			t.Fatalf("unexpected status %d (body=%s)", w.Code, w.Body.String())
		}
	}
	if throttled == 0 {
		t.Errorf("100 wrong 6-digit codes from one IP against /auth/2fa/verify were never throttled (0 of 100 responses were 429); the endpoint needs the same default per-IP rate limit as login/register")
	}
}
