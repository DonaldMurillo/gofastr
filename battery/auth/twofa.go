package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
	"golang.org/x/crypto/bcrypt"
)

// ─── Configuration ─────────────────────────────────────────────────────

// TwoFAConfig holds optional settings for the TwoFAPlugin.
type TwoFAConfig struct {
	// Issuer is the name shown in authenticator apps. Defaults to "GoFastr".
	Issuer string

	// Period is the TOTP time-step period in seconds. Defaults to 30.
	Period uint

	// Digits is the number of digits in generated TOTP codes. Defaults to 6.
	Digits uint

	// Skew is the number of time-steps allowed before/after the current step.
	// Defaults to 1 (allows ±1 period window).
	Skew uint

	// BackupCodeCount is how many backup codes to generate. Defaults to 10.
	BackupCodeCount int

	// Store is the persistence backend for 2FA state. If nil, an in-memory
	// store is used (dev/test only).
	Store TwoFAStore

	// RateLimit applies a per-IP rate limit to /2fa/challenge and
	// /2fa/verify. It defaults to 10 attempts/min with a 15-minute block
	// (the register floor): without any, an attacker who has stolen a
	// session can brute-force the 6-digit TOTP (~333k expected attempts
	// at skew=1). Loosen by passing a config with a large MaxAttempts.
	RateLimit *RateLimiterConfig
}

func (c *TwoFAConfig) defaults() {
	if c.Issuer == "" {
		c.Issuer = "GoFastr"
	}
	if c.Period == 0 {
		c.Period = 30
	}
	if c.Digits == 0 {
		c.Digits = 6
	}
	if c.Skew == 0 {
		c.Skew = 1
	}
	if c.BackupCodeCount == 0 {
		c.BackupCodeCount = 10
	}
	// Default per-IP throttle on the code-guessing surfaces, like the
	// login/register floors: a stolen session brute-forcing the 6-digit
	// code needs ~333k expected attempts at skew=1, and 10/min makes that
	// a multi-week project instead of an afternoon. Opt out by passing a
	// config with a huge MaxAttempts, not by leaving it nil.
	if c.RateLimit == nil {
		c.RateLimit = &RateLimiterConfig{
			MaxAttempts:   10,
			Window:        time.Minute,
			BlockDuration: 15 * time.Minute,
		}
	}
}

// ─── State & Store ─────────────────────────────────────────────────────

// TwoFAState holds the per-user 2FA enrollment and verification status.
type TwoFAState struct {
	Enabled     bool     // true after successful verify step
	Secret      string   // base32 TOTP secret (plaintext, stored encrypted at rest in production)
	BackupCodes []string // bcrypt-hashed backup codes
	Verified    bool     // true once the user has proven they can generate a valid code

	// LastUsedStep is the TOTP time-step of the most recently accepted
	// code. A code stays valid for the whole ±skew window, so without
	// this one code works several times over ~90s — long enough for
	// anyone who saw it (shoulder-surfed, phished, logged by a proxy) to
	// spend it on a second session. Zero means nothing accepted yet.
	LastUsedStep uint64
}

// TwoFAStore is the interface for persisting 2FA state per user.
// TwoFACompareAndSetter is the optional TwoFAStore extension that writes
// only while 2FA is still enabled for the user.
//
// Regenerating backup codes is a read-modify-write, and a /2fa/disable
// landing between the read and the write is enough for the write to
// resurrect the row the disable just removed. Checking before writing does
// not help — the condition has to be part of the write. A store that
// cannot offer that keeps the blind Set.
type TwoFACompareAndSetter interface {
	// CompareAndSetTwoFA stores next only if the user still has an
	// enabled 2FA row, reporting whether the write happened.
	CompareAndSetTwoFA(ctx context.Context, userID string, next *TwoFAState) (bool, error)
}

// TwoFAStateSwapper is the optional TwoFAStore extension that writes next
// only while the stored row still EQUALS the state the caller read (a nil
// expect means the row must be absent). It is the atomic read-modify-write
// primitive for every handler transition:
//
//   - challenge's step consume: two sessions presenting the same TOTP code
//     both read LastUsedStep=0; the first swap wins, the second's expect no
//     longer matches, so one code authenticates exactly one session
//     (RFC 6238 §5.2) — and a disable that committed between read and
//     write deletes the row, failing the swap instead of resurrecting it.
//   - verify's enable: the write lands only over the pending row it read;
//     a racing disable (row gone) or re-enroll (row changed) refuses with
//     409 instead of silently re-enabling a factor the user removed.
//   - enroll's fresh pending row: inserted only while the row is still in
//     the state the guard checked.
//
// A store that cannot offer this keeps the blind Set and the lost-update
// window it comes with.
type TwoFAStateSwapper interface {
	CompareAndSwapTwoFA(ctx context.Context, userID string, expect, next *TwoFAState) (bool, error)
}

type TwoFAStore interface {
	// GetTwoFA retrieves the 2FA state for a user. Returns nil if not enrolled.
	GetTwoFA(ctx context.Context, userID string) (*TwoFAState, error)

	// SetTwoFA persists the 2FA state for a user.
	SetTwoFA(ctx context.Context, userID string, state *TwoFAState) error

	// DeleteTwoFA removes the 2FA state for a user.
	DeleteTwoFA(ctx context.Context, userID string) error

	// ConsumeBackupCode checks if the given code matches any stored (hashed)
	// backup code for the user. If it matches, that code is removed and true
	// is returned. Otherwise returns false.
	ConsumeBackupCode(ctx context.Context, userID string, code string) (bool, error)
}

// MemoryTwoFAStore is a goroutine-safe in-memory TwoFAStore for dev/test.
type MemoryTwoFAStore struct {
	mu     sync.RWMutex
	states map[string]*TwoFAState
}

// NewMemoryTwoFAStore creates a fresh, empty MemoryTwoFAStore.
func NewMemoryTwoFAStore() *MemoryTwoFAStore {
	return &MemoryTwoFAStore{states: make(map[string]*TwoFAState)}
}

func (m *MemoryTwoFAStore) GetTwoFA(_ context.Context, userID string) (*TwoFAState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneTwoFAState(m.states[userID]), nil
}

func (m *MemoryTwoFAStore) SetTwoFA(_ context.Context, userID string, state *TwoFAState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[userID] = cloneTwoFAState(state)
	return nil
}

// cloneTwoFAState deep-copies the state so the store never shares a
// pointer with its callers, matching MemorySessionStore.
//
// A struct copy alone is not enough here: BackupCodes is a slice, so a
// shallow copy still shares the backing array and two concurrent
// regenerations would write the same memory. Handing out the stored
// pointer let callers mutate the map's value with no lock at all, which
// the race detector flags on two simultaneous backup-code requests.
func cloneTwoFAState(st *TwoFAState) *TwoFAState {
	if st == nil {
		return nil
	}
	cp := *st
	if st.BackupCodes != nil {
		cp.BackupCodes = append([]string(nil), st.BackupCodes...)
	}
	return &cp
}

// CompareAndSetTwoFA writes next only while the row is still present and
// still Enabled. Implements [TwoFACompareAndSetter].
func (m *MemoryTwoFAStore) CompareAndSetTwoFA(_ context.Context, userID string, next *TwoFAState) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.states[userID]
	if !ok || cur == nil || !cur.Enabled {
		return false, nil
	}
	cp := *next
	m.states[userID] = &cp
	return true, nil
}

func (m *MemoryTwoFAStore) DeleteTwoFA(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.states, userID)
	return nil
}

func (m *MemoryTwoFAStore) ConsumeBackupCode(_ context.Context, userID string, code string) (bool, error) {
	// Snapshot the hashes under the read lock so other readers (GetTwoFA)
	// aren't blocked by the bcrypt loop. With 10 codes at default cost
	// the loop holds the lock for ~600ms in the previous implementation,
	// freezing every other 2FA call in the process.
	m.mu.RLock()
	state, ok := m.states[userID]
	if !ok || len(state.BackupCodes) == 0 {
		m.mu.RUnlock()
		return false, nil
	}
	hashes := append([]string(nil), state.BackupCodes...) // copy
	m.mu.RUnlock()

	// Bcrypt comparisons happen WITHOUT holding any lock.

	matchedHash := ""
	for _, hashed := range hashes {
		if bcrypt.CompareHashAndPassword([]byte(hashed), []byte(code)) == nil {
			matchedHash = hashed
			break
		}
	}
	if matchedHash == "" {
		return false, nil
	}

	// Re-acquire the write lock and remove the matched hash. Re-check
	// the state in case another goroutine mutated it while we were
	// hashing (e.g. concurrent successful consume).
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.states[userID]
	if !ok {
		return false, nil
	}
	for i, h := range cur.BackupCodes {
		if h == matchedHash {
			cur.BackupCodes = append(cur.BackupCodes[:i], cur.BackupCodes[i+1:]...)
			m.states[userID] = cur
			return true, nil
		}
	}
	// Already consumed by another goroutine.
	return false, nil
}

// ─── Plugin ────────────────────────────────────────────────────────────

// TwoFAPlugin implements AuthPlugin and AuthPluginRoutes for TOTP-based
// two-factor authentication.
type TwoFAPlugin struct {
	config         TwoFAConfig
	mgr            *AuthManager
	store          TwoFAStore
	challengeLimit *RateLimiter
}

// NewTwoFAPlugin creates a new 2FA plugin with the given (optional) config.
func NewTwoFAPlugin(config TwoFAConfig) *TwoFAPlugin {
	config.defaults()
	p := &TwoFAPlugin{config: config}
	if config.RateLimit != nil {
		p.challengeLimit = newScopedRateLimiter(*config.RateLimit, "twofa")
	}
	return p
}

// twoFAStateEqual reports deep equality of two states (nil-aware): the
// field set a swap predicate needs, including the backup-code slice.
func twoFAStateEqual(a, b *TwoFAState) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Enabled == b.Enabled &&
		subtle.ConstantTimeCompare([]byte(a.Secret), []byte(b.Secret)) == 1 &&
		a.Verified == b.Verified &&
		a.LastUsedStep == b.LastUsedStep &&
		slices.Equal(a.BackupCodes, b.BackupCodes)
}

// CompareAndSwapTwoFA writes next only while the stored row still equals
// expect (nil expect = row absent), under the same mutex every other
// mutation takes. Implements [TwoFAStateSwapper].
func (m *MemoryTwoFAStore) CompareAndSwapTwoFA(_ context.Context, userID string, expect, next *TwoFAState) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !twoFAStateEqual(m.states[userID], expect) {
		return false, nil
	}
	switch {
	case next == nil:
		delete(m.states, userID)
	default:
		cp := *next
		m.states[userID] = &cp
	}
	return true, nil
}

// Name returns the plugin identifier.
func (p *TwoFAPlugin) Name() string { return "twofa" }

// Init stores a reference to the AuthManager, selects the store, and
// self-migrates its schema when the store supports it.
//
// Init fails closed when DevMode=false and no durable store is
// configured: in-memory 2FA state in production is worse than a scaling
// gap, a restart wipes enrollment, silently reverting every 2FA account
// to password-only auth. A security control that quietly stops applying
// is not a warning-grade condition, so the app refuses to boot unless
// the host acknowledges a deliberate single-node deployment via
// AuthConfig.AllowInMemoryStores.
func (p *TwoFAPlugin) Init(mgr *AuthManager) error {
	p.mgr = mgr
	if p.config.Store != nil {
		p.store = p.config.Store
	} else {
		cfg := mgr.Config()
		if !cfg.DevMode && !cfg.AllowInMemoryStores {
			return fmt.Errorf("auth: production mode refuses the in-memory 2FA store: a restart wipes enrollment, silently reverting every 2FA account to password-only auth; set TwoFAConfig.Store (e.g. auth.NewEntityTwoFAStore(db, \"auth_twofa\")), or set AuthConfig.AllowInMemoryStores: true to acknowledge a deliberate single-node deployment")
		}
		p.store = NewMemoryTwoFAStore()
		if !cfg.DevMode {
			// Acknowledged single-node: still leave a trace in the log.
			slog.Default().Warn("auth: production mode is running on the in-memory 2FA store (acknowledged via AllowInMemoryStores): a restart wipes enrollment, reverting 2FA accounts to password-only auth")
		}
	}
	// The battery owns its table: create it if absent so hosts never
	// hand-roll the 2FA DDL. Custom stores without a managed schema
	// simply don't implement the optional interface.
	if se, ok := p.store.(interface {
		EnsureSchema(context.Context) error
	}); ok {
		if err := se.EnsureSchema(context.Background()); err != nil {
			return fmt.Errorf("auth: 2FA store EnsureSchema: %w", err)
		}
	}
	return nil
}

// RegisterRoutes mounts the 2FA HTTP endpoints.
func (p *TwoFAPlugin) RegisterRoutes(r *router.Router, basePath string) {
	r.Post(basePath+"/2fa/enroll", http.HandlerFunc(p.enrollHandler))
	r.Post(basePath+"/2fa/verify", http.HandlerFunc(p.verifyHandler))
	r.Post(basePath+"/2fa/challenge", http.HandlerFunc(p.challengeHandler))
	r.Post(basePath+"/2fa/disable", http.HandlerFunc(p.disableHandler))
	// POST, not GET: this route REGENERATES the code set, invalidating the
	// previous one. csrf.go exempts safe methods by design, so as a GET no
	// token was ever checked — and a SameSite=Lax session, which is what
	// OAuth and magic-link logins leave behind, rides a plain top-level
	// cross-site link. A hidden <img> was enough to burn a user's codes.
	r.Post(basePath+"/2fa/backup-codes", http.HandlerFunc(p.backupCodesHandler))
}

// ─── Route helpers ─────────────────────────────────────────────────────

// getSessionUser extracts the user ID from the session cookie. It also
// reports whether the session is still in the PendingTwoFactor (pre-step-up)
// state, callers that mutate the second factor MUST refuse pending sessions
// (see requireStepUpUser). A pending session proves only the password.
func (p *TwoFAPlugin) getSessionUser(r *http.Request) (userID string, pending bool, err error) {
	sess, err := p.sessionFrom(r)
	if err != nil {
		return "", false, err
	}
	return sess.UserID, sess.PendingTwoFactor, nil
}

// swapTwoFAState performs a handler's read-modify-write through the
// TwoFAStateSwapper seam when the store offers it: the write lands only
// over the state the handler read, so a racing disable (or a competing
// presentation of the same TOTP step) loses cleanly to the caller with a
// 409 instead of a blind write resurrecting or double-spending state.
// Stores without the seam keep the blind Set and its documented window.
// Writes the error response and returns false when the caller must abort.
func (p *TwoFAPlugin) swapTwoFAState(w http.ResponseWriter, r *http.Request, userID string, expect, next *TwoFAState) bool {
	swapper, ok := p.store.(TwoFAStateSwapper)
	if !ok {
		if err := p.store.SetTwoFA(r.Context(), userID, next); err != nil {
			writeAuthError(w, http.StatusInternalServerError, "failed to save 2FA state")
			return false
		}
		return true
	}
	wrote, err := swapper.CompareAndSwapTwoFA(r.Context(), userID, expect, next)
	if err != nil {
		writeAuthError(w, http.StatusInternalServerError, "failed to save 2FA state")
		return false
	}
	if !wrote {
		writeAuthError(w, http.StatusConflict, "2FA state changed concurrently; retry")
		return false
	}
	return true
}

// sessionFrom resolves the session behind the request's cookie.
func (p *TwoFAPlugin) sessionFrom(r *http.Request) (*Session, error) {
	cfg := p.mgr.Config()
	cookie, err := r.Cookie(cfg.SessionCookie)
	if err != nil {
		return nil, fmt.Errorf("no session cookie")
	}
	sess, err := p.mgr.SessionStore().Get(r.Context(), cookie.Value)
	if err != nil || sess == nil {
		return nil, fmt.Errorf("invalid session")
	}
	return sess, nil
}

// requireStepUpUser resolves the session user and refuses any session that
// is still PendingTwoFactor. Used by every 2FA self-service handler except
// challengeHandler, a pending session (password only) must not be able to
// disable, re-enroll, verify, or refresh backup codes, which would defeat
// 2FA with the password alone. Writes the 401/403 response and returns ok=false
// when the caller must abort.
func (p *TwoFAPlugin) requireStepUpUser(w http.ResponseWriter, r *http.Request) (userID string, ok bool) {
	sess, err := p.sessionFrom(r)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, "not authenticated")
		return "", false
	}
	if sess.PendingTwoFactor {
		writeAuthError(w, http.StatusForbidden, "two-factor verification required")
		return "", false
	}
	// Positive check, not only the absence of the pending flag. A session
	// minted BEFORE the user enrolled carries PendingTwoFactor=false
	// forever, so the negative test alone left it "stepped up" for its
	// whole lifetime, able to disable a factor it never proved. Only a
	// session that actually passed the challenge (or the enrolling
	// session, which verifyHandler marks) may mutate the factor.
	state, err := p.store.GetTwoFA(r.Context(), sess.UserID)
	if err != nil {
		// Unreadable 2FA state is refused, never assumed absent,
		// assuming it would hand exactly the bypass back.
		writeAuthError(w, http.StatusInternalServerError, "2FA state lookup failed")
		return "", false
	}
	if state != nil && state.Enabled && !sess.TwoFactorVerified {
		writeAuthError(w, http.StatusForbidden, "two-factor verification required")
		return "", false
	}
	return sess.UserID, true
}

// ─── Route handlers ────────────────────────────────────────────────────

// POST {basePath}/2fa/enroll
// Generates a new TOTP secret for the authenticated user and returns the
// otpauth:// URL (for QR code apps) along with the plaintext secret.
func (p *TwoFAPlugin) enrollHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := p.requireStepUpUser(w, r)
	if !ok {
		return
	}

	// Refuse to overwrite an already-enabled factor without a fresh step-up.
	// Re-enrolling silently clobbers the live secret (Enabled=false below),
	// which would let an attacker with a non-pending but un-stepped-up
	// session disable the victim's working second factor. Callers must
	// disable (which itself requires step-up) before re-enrolling.
	//
	// Fail CLOSED on a store error: a durable store can now error (the
	// memory store never did), and treating an unreadable state as
	// "not enabled" would skip this guard and overwrite a live factor.
	existing, err := p.store.GetTwoFA(r.Context(), userID)
	if err != nil {
		writeAuthError(w, http.StatusInternalServerError, "could not read 2FA state")
		return
	}
	if existing != nil && existing.Enabled {
		writeAuthError(w, http.StatusConflict, "2FA already enabled; disable it before re-enrolling")
		return
	}

	secret := GenerateSecret()

	// Persist a pending state (Enabled=false until verified), only over
	// the state the guard above read: a /2fa/disable committing between
	// the Get and this write deletes the row, and a blind Set would
	// re-create it — the user's disable would not stick.
	state := &TwoFAState{
		Enabled:     false,
		Secret:      secret,
		BackupCodes: nil,
		Verified:    false,
	}
	if !p.swapTwoFAState(w, r, userID, existing, state) {
		return
	}

	// Build otpauth URL. Fall back to userID if UserStore is not configured.
	email := userID
	if us := p.mgr.UserStore(); us != nil {
		if user, err := us.FindByID(r.Context(), userID); err == nil && user != nil {
			email = user.GetEmail()
		}
	}
	otpauthURL := buildOTPAuthURL(p.config.Issuer, email, secret)

	writeCredentialHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"secret": secret,
		"url":    otpauthURL,
	})
}

// POST {basePath}/2fa/verify
// Verifies a TOTP code from the enrollment flow. If valid, enables 2FA
// and generates backup codes.
func (p *TwoFAPlugin) verifyHandler(w http.ResponseWriter, r *http.Request) {
	if p.challengeLimit != nil && !p.challengeLimit.guard(w, r) {
		return
	}
	userID, ok := p.requireStepUpUser(w, r)
	if !ok {
		return
	}

	var body struct {
		Code string `json:"code"`
	}
	if !decodeJSONLimited(w, r, &body) {
		return
	}
	if body.Code == "" {
		writeAuthError(w, http.StatusBadRequest, "code required")
		return
	}

	state, err := p.store.GetTwoFA(r.Context(), userID)
	if err != nil || state == nil {
		writeAuthError(w, http.StatusBadRequest, "2FA not enrolled")
		return
	}
	if state.Enabled {
		writeAuthError(w, http.StatusBadRequest, "2FA already enabled")
		return
	}

	// Enrollment deliberately does NOT consume the step. This is a
	// one-time proof inside the user's own authenticated session, and the
	// same code must still work for the step-up challenge moments later —
	// consuming it here would make enrolment and first login mutually
	// exclusive within one window. The replay that matters is a second
	// SESSION on one code, and that is the challenge path's to refuse.
	if !ValidateTOTP(state.Secret, body.Code, p.config.Period, p.config.Skew) {
		writeAuthError(w, http.StatusUnauthorized, "invalid code")
		return
	}

	// Generate backup codes (plaintext to return, hashed to store).
	plainCodes, hashedCodes, err := generateBackupCodes(p.config.BackupCodeCount)
	if err != nil {
		writeAuthError(w, http.StatusInternalServerError, "failed to generate backup codes")
		return
	}

	// The enable write lands only over the pending row this handler
	// read: a /2fa/disable committing between the read and the write
	// deletes the row, and a blind Set would write Enabled=true straight
	// back over the delete — 2FA silently ON again after the user
	// turned it off.
	enabled := cloneTwoFAState(state)
	enabled.Enabled = true
	enabled.Verified = true
	enabled.BackupCodes = hashedCodes
	if !p.swapTwoFAState(w, r, userID, state, enabled) {
		return
	}

	// The caller just proved the factor with a live TOTP code, so mark
	// this session verified. Without it, requireStepUpUser's positive
	// check would lock the enrolling user out of their own 2FA settings
	// until they logged in again, the session that did the enrolling
	// would have Enabled=true and TwoFactorVerified=false.
	if marker, ok := p.mgr.SessionStore().(SessionTwoFAMarker); ok {
		if sess, err := p.sessionFrom(r); err == nil {
			if err := marker.MarkTwoFactorVerified(r.Context(), sess.Token); err != nil {
				writeAuthError(w, http.StatusInternalServerError, "failed to record verification")
				return
			}
		}
	}

	p.mgr.emitSecurity(r.Context(), SecurityEvent{
		Kind:   "2fa.enrolled",
		UserID: userID,
		Remote: remoteHost(r),
	})

	writeCredentialHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"enabled":      true,
		"backup_codes": plainCodes,
	})
}

// POST {basePath}/2fa/challenge
// Verifies a TOTP code (or backup code) during login. Called after the
// core login flow if the user has 2FA enabled.
func (p *TwoFAPlugin) challengeHandler(w http.ResponseWriter, r *http.Request) {
	if p.challengeLimit != nil && !p.challengeLimit.guard(w, r) {
		return
	}
	// challengeHandler is the ONLY endpoint a PendingTwoFactor session may
	// reach, it is how the session completes step-up. Hence it uses the raw
	// getSessionUser (pending is allowed here) rather than requireStepUpUser.
	userID, _, err := p.getSessionUser(r)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	var body struct {
		Code string `json:"code"`
	}
	if !decodeJSONLimited(w, r, &body) {
		return
	}
	if body.Code == "" {
		writeAuthError(w, http.StatusBadRequest, "code required")
		return
	}

	state, err := p.store.GetTwoFA(r.Context(), userID)
	if err != nil || state == nil || !state.Enabled {
		writeAuthError(w, http.StatusBadRequest, "2FA not enabled")
		return
	}

	// Try TOTP code first. A code is valid across the whole ±skew window,
	// so accepting the same step twice lets anyone who observed one code
	// open a second session inside that window (RFC 6238 §5.2 forbids it).
	// The consume rides the compare-and-swap: the write lands only over
	// the state this handler read, so the second concurrent presentation
	// of the same step fails the swap (one code, one session) and a
	// racing disable finds the row already gone instead of resurrected.
	// Recording the step BEFORE marking the session means a store failure
	// leaves the code spent rather than replayable.
	if step, ok := ValidateTOTPStep(state.Secret, body.Code, p.config.Period, p.config.Skew); ok && step > state.LastUsedStep {
		consumed := cloneTwoFAState(state)
		consumed.LastUsedStep = step
		if !p.swapTwoFAState(w, r, userID, state, consumed) {
			return
		}
		if err := p.markSessionTwoFA(r); err != nil {
			// The session is still pending: answering 200 here
			// would report a step-up that did not happen.
			writeAuthError(w, http.StatusInternalServerError, "could not complete the step-up")
			return
		}
		p.mgr.emitSecurity(r.Context(), SecurityEvent{
			Kind:   "2fa.challenge_succeeded",
			UserID: userID,
			Remote: remoteHost(r),
			Meta:   map[string]string{"method": "totp"},
		})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"verified": true})
		return
	}

	// Fallback to backup code.
	consumed, err := p.store.ConsumeBackupCode(r.Context(), userID, body.Code)
	if err != nil {
		writeAuthError(w, http.StatusInternalServerError, "backup code check failed")
		return
	}
	if consumed {
		if err := p.markSessionTwoFA(r); err != nil {
			// The session is still pending: answering 200 here
			// would report a step-up that did not happen.
			writeAuthError(w, http.StatusInternalServerError, "could not complete the step-up")
			return
		}
		p.mgr.emitSecurity(r.Context(), SecurityEvent{
			Kind:   "2fa.challenge_succeeded",
			UserID: userID,
			Remote: remoteHost(r),
			Meta:   map[string]string{"method": "backup_code"},
		})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"verified": true, "backup_code": true})
		return
	}

	p.mgr.emitSecurity(r.Context(), SecurityEvent{
		Kind:   "2fa.challenge_failed",
		UserID: userID,
		Remote: remoteHost(r),
	})
	writeAuthError(w, http.StatusUnauthorized, "invalid code")
}

// HasTwoFactorEnabled implements TwoFactorChecker. Returns true when the
// user has 2FA enrolled and enabled. CorePlugin's loginHandler queries
// this to decide whether to mint a PendingTwoFactor session.
func (p *TwoFAPlugin) HasTwoFactorEnabled(ctx context.Context, userID string) (bool, error) {
	state, err := p.store.GetTwoFA(ctx, userID)
	if err != nil {
		return false, err
	}
	if state == nil {
		return false, nil
	}
	return state.Enabled, nil
}

// markSessionTwoFA flips the TwoFactorVerified flag on the caller's
// session, if the session store supports SessionTwoFAMarker. No-op
// otherwise (RequireTwoFA fails closed in that case).
// markSessionTwoFA records that this session cleared its second factor.
//
// The error is returned, not swallowed. RequireTwoFA gates on the marker,
// so a failed write means the session is still pending — answering the
// challenge with 200 verified:true would tell the caller they are through
// a door that is still shut, and every later request would bounce with no
// explanation. A store without SessionTwoFAMarker keeps its old behaviour:
// there is no marker to fail to write.
func (p *TwoFAPlugin) markSessionTwoFA(r *http.Request) error {
	cfg := p.mgr.Config()
	cookie, err := r.Cookie(cfg.SessionCookie)
	if err != nil {
		return nil
	}
	if marker, ok := p.mgr.SessionStore().(SessionTwoFAMarker); ok {
		return marker.MarkTwoFactorVerified(r.Context(), cookie.Value)
	}
	return nil
}

// RequireTwoFA returns middleware that:
//
//   - Lets requests through if the user has not enrolled in 2FA.
//   - Lets requests through if the session has TwoFactorVerified=true.
//   - Returns 403 in all other cases (enrolled but not verified, or no session).
//
// Install this on every route that requires step-up authentication. Note
// that it relies on the SessionStore implementing SessionTwoFAMarker,
// otherwise RequireTwoFA fails closed (always 403 for enrolled users).
func (p *TwoFAPlugin) RequireTwoFA() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cfg := p.mgr.Config()
			cookie, err := r.Cookie(cfg.SessionCookie)
			if err != nil {
				writeAuthError(w, http.StatusUnauthorized, "no session")
				return
			}
			sess, err := p.mgr.SessionStore().Get(r.Context(), cookie.Value)
			if err != nil {
				writeAuthError(w, http.StatusUnauthorized, "invalid session")
				return
			}
			state, err := p.store.GetTwoFA(r.Context(), sess.UserID)
			if err != nil {
				writeAuthError(w, http.StatusInternalServerError, "2FA state lookup failed")
				return
			}
			// Not enrolled, bypass.
			if state == nil || !state.Enabled {
				next.ServeHTTP(w, r)
				return
			}
			// Enrolled, must have verified for this session.
			if !sess.TwoFactorVerified {
				writeAuthError(w, http.StatusForbidden, "two-factor verification required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// POST {basePath}/2fa/disable
// Disables 2FA for the authenticated user.
func (p *TwoFAPlugin) disableHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := p.requireStepUpUser(w, r)
	if !ok {
		return
	}

	if err := p.store.DeleteTwoFA(r.Context(), userID); err != nil {
		writeAuthError(w, http.StatusInternalServerError, "failed to disable 2FA")
		return
	}

	p.mgr.emitSecurity(r.Context(), SecurityEvent{
		Kind:   "2fa.disabled",
		UserID: userID,
		Remote: remoteHost(r),
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"disabled": true})
}

// POST {basePath}/2fa/backup-codes
// Generates a fresh set of backup codes, invalidating any previous ones.
func (p *TwoFAPlugin) backupCodesHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := p.requireStepUpUser(w, r)
	if !ok {
		return
	}

	state, err := p.store.GetTwoFA(r.Context(), userID)
	if err != nil || state == nil || !state.Enabled {
		writeAuthError(w, http.StatusBadRequest, "2FA not enabled")
		return
	}

	plainCodes, hashedCodes, err := generateBackupCodes(p.config.BackupCodeCount)
	if err != nil {
		writeAuthError(w, http.StatusInternalServerError, "failed to generate backup codes")
		return
	}

	// Generating codes takes real time, and a /2fa/disable landing in that
	// gap deletes the row — after which a blind Set writes an Enabled
	// state straight back, so 2FA silently re-enables itself moments after
	// the user turned it off, holding codes only the earlier request saw.
	//
	// A re-read before the write does not fix this: the delete can still
	// land between the check and the write. The condition has to travel
	// WITH the write, which is what TwoFACompareAndSetter is for. A store
	// without it keeps the blind write and the window it comes with.
	state.BackupCodes = hashedCodes
	if cas, ok := p.store.(TwoFACompareAndSetter); ok {
		wrote, err := cas.CompareAndSetTwoFA(r.Context(), userID, state)
		if err != nil {
			writeAuthError(w, http.StatusInternalServerError, "failed to save backup codes")
			return
		}
		if !wrote {
			writeAuthError(w, http.StatusConflict, "2FA is no longer enabled")
			return
		}
	} else if err := p.store.SetTwoFA(r.Context(), userID, state); err != nil {
		writeAuthError(w, http.StatusInternalServerError, "failed to save backup codes")
		return
	}

	p.mgr.emitSecurity(r.Context(), SecurityEvent{
		Kind:   "2fa.backup_codes_regenerated",
		UserID: userID,
		Remote: remoteHost(r),
	})

	writeCredentialHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"backup_codes": plainCodes,
	})
}

// ─── TOTP implementation (RFC 6238, HMAC-SHA1) ─────────────────────────

// GenerateSecret creates a cryptographically random 20-byte secret and
// returns it as a base32-encoded string (no padding). Panics if crypto/rand
// fails, entropy starvation makes the rest of the auth system unsound.
func GenerateSecret() string {
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		panic(fmt.Sprintf("auth: crypto/rand failed: %v", err))
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
}

// GenerateTOTP produces a TOTP code for the given base32 secret and
// time-step counter using HMAC-SHA1 as specified in RFC 6238.
func GenerateTOTP(secret string, timeStep uint64) string {
	// Decode the base32 secret.
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return ""
	}

	// Encode timeStep as an 8-byte big-endian value.
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, timeStep)

	// HMAC-SHA1.
	mac := hmac.New(sha1.New, key)
	mac.Write(buf)
	hmacResult := mac.Sum(nil)

	// Dynamic truncation (RFC 4226 §5.3).
	offset := hmacResult[len(hmacResult)-1] & 0x0F
	code := binary.BigEndian.Uint32(hmacResult[offset:]) & 0x7FFFFFFF

	// 6 digits.
	code = code % 1000000
	return fmt.Sprintf("%06d", code)
}

// ValidateTOTP checks whether the provided code is valid for the given
// secret within ±skew time periods of the current time.
//
// A caller enforcing single use wants [ValidateTOTPStep], which names the
// step that matched.
func ValidateTOTP(secret, code string, period, skew uint) bool {
	_, ok := ValidateTOTPStep(secret, code, period, skew)
	return ok
}

// ValidateTOTPStep reports the time-step a valid code belongs to, so a
// caller can refuse a step it has already accepted. RFC 6238 §5.2 requires
// exactly this: "the verifier MUST NOT accept the second attempt of the
// OTP generated for the same time window".
func ValidateTOTPStep(secret, code string, period, skew uint) (uint64, bool) {
	if period == 0 {
		period = 30
	}
	now := uint64(time.Now().Unix())
	currentStep := now / uint64(period)

	codeBytes := []byte(code)
	for i := -int(skew); i <= int(skew); i++ {
		step := int64(currentStep) + int64(i)
		if step < 0 {
			continue
		}
		// Constant-time compare so the verification doesn't leak which
		// digits matched. Mirrors framework/auth/mfa.go.
		expected := []byte(GenerateTOTP(secret, uint64(step)))
		if subtle.ConstantTimeCompare(expected, codeBytes) == 1 {
			return uint64(step), true
		}
	}
	return 0, false
}

// ─── Helpers ───────────────────────────────────────────────────────────

// buildOTPAuthURL constructs an otpauth://totp/ URL for authenticator apps.
func buildOTPAuthURL(issuer, accountName, secret string) string {
	u := url.URL{
		Scheme: "otpauth",
		Host:   "totp",
		Path:   fmt.Sprintf("%s:%s", issuer, accountName),
	}
	q := u.Query()
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", "6")
	q.Set("period", "30")
	u.RawQuery = q.Encode()
	return u.String()
}

// generateBackupCodes creates n cryptographically random 8-character
// alphanumeric codes. Returns both plaintext and bcrypt-hashed slices.
func generateBackupCodes(n int) (plain []string, hashed []string, err error) {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	plain = make([]string, n)
	hashed = make([]string, n)
	for i := range n {
		code, err := randomString(8, charset)
		if err != nil {
			return nil, nil, err
		}
		h, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
		if err != nil {
			return nil, nil, err
		}
		plain[i] = code
		hashed[i] = string(h)
	}
	return plain, hashed, nil
}

// randomString generates a cryptographically random string of the given
// length using characters from charset.
func randomString(length int, charset string) (string, error) {
	result := make([]byte, length)
	max := big.NewInt(int64(len(charset)))
	for i := range result {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		result[i] = charset[n.Int64()]
	}
	return string(result), nil
}
