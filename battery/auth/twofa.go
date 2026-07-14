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

	// RateLimit, when non-nil, applies a per-IP rate limit to
	// /2fa/challenge and /2fa/verify. Without this, an attacker who has
	// stolen a session can brute-force the 6-digit TOTP (~333k expected
	// attempts at skew=1).
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
}

// ─── State & Store ─────────────────────────────────────────────────────

// TwoFAState holds the per-user 2FA enrollment and verification status.
type TwoFAState struct {
	Enabled     bool     // true after successful verify step
	Secret      string   // base32 TOTP secret (plaintext, stored encrypted at rest in production)
	BackupCodes []string // bcrypt-hashed backup codes
	Verified    bool     // true once the user has proven they can generate a valid code
}

// TwoFAStore is the interface for persisting 2FA state per user.
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
	return m.states[userID], nil
}

func (m *MemoryTwoFAStore) SetTwoFA(_ context.Context, userID string, state *TwoFAState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[userID] = state
	return nil
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

// Name returns the plugin identifier.
func (p *TwoFAPlugin) Name() string { return "twofa" }

// Init stores a reference to the AuthManager, selects the store, and
// self-migrates its schema when the store supports it.
//
// Init fails closed when DevMode=false and no durable store is
// configured: in-memory 2FA state in production is worse than a scaling
// gap — a restart wipes enrollment, silently reverting every 2FA account
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
			return fmt.Errorf("auth: production mode refuses the in-memory 2FA store — a restart wipes enrollment, silently reverting every 2FA account to password-only auth; set TwoFAConfig.Store (e.g. auth.NewEntityTwoFAStore(db, \"auth_twofa\")), or set AuthConfig.AllowInMemoryStores: true to acknowledge a deliberate single-node deployment")
		}
		p.store = NewMemoryTwoFAStore()
		if !cfg.DevMode {
			// Acknowledged single-node: still leave a trace in the log.
			slog.Default().Warn("auth: production mode is running on the in-memory 2FA store (acknowledged via AllowInMemoryStores) — a restart wipes enrollment, reverting 2FA accounts to password-only auth")
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
	r.Get(basePath+"/2fa/backup-codes", http.HandlerFunc(p.backupCodesHandler))
}

// ─── Route helpers ─────────────────────────────────────────────────────

// getSessionUser extracts the user ID from the session cookie. It also
// reports whether the session is still in the PendingTwoFactor (pre-step-up)
// state — callers that mutate the second factor MUST refuse pending sessions
// (see requireStepUpUser). A pending session proves only the password.
func (p *TwoFAPlugin) getSessionUser(r *http.Request) (userID string, pending bool, err error) {
	cfg := p.mgr.Config()
	cookie, err := r.Cookie(cfg.SessionCookie)
	if err != nil {
		return "", false, fmt.Errorf("no session cookie")
	}
	sess, err := p.mgr.SessionStore().Get(r.Context(), cookie.Value)
	if err != nil {
		return "", false, fmt.Errorf("invalid session")
	}
	return sess.UserID, sess.PendingTwoFactor, nil
}

// requireStepUpUser resolves the session user and refuses any session that
// is still PendingTwoFactor. Used by every 2FA self-service handler except
// challengeHandler — a pending session (password only) must not be able to
// disable, re-enroll, verify, or refresh backup codes, which would defeat
// 2FA with the password alone. Writes the 401/403 response and returns ok=false
// when the caller must abort.
func (p *TwoFAPlugin) requireStepUpUser(w http.ResponseWriter, r *http.Request) (userID string, ok bool) {
	uid, pending, err := p.getSessionUser(r)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, "not authenticated")
		return "", false
	}
	if pending {
		writeAuthError(w, http.StatusForbidden, "two-factor verification required")
		return "", false
	}
	return uid, true
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

	// Persist a pending state (Enabled=false until verified).
	state := &TwoFAState{
		Enabled:     false,
		Secret:      secret,
		BackupCodes: nil,
		Verified:    false,
	}
	if err := p.store.SetTwoFA(r.Context(), userID, state); err != nil {
		writeAuthError(w, http.StatusInternalServerError, "failed to save 2FA state")
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

	state.Enabled = true
	state.Verified = true
	state.BackupCodes = hashedCodes

	if err := p.store.SetTwoFA(r.Context(), userID, state); err != nil {
		writeAuthError(w, http.StatusInternalServerError, "failed to save 2FA state")
		return
	}

	p.mgr.emitSecurity(r.Context(), SecurityEvent{
		Kind:   "2fa.enrolled",
		UserID: userID,
		Remote: remoteHost(r),
	})

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
	// reach — it is how the session completes step-up. Hence it uses the raw
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

	// Try TOTP code first.
	if ValidateTOTP(state.Secret, body.Code, p.config.Period, p.config.Skew) {
		p.markSessionTwoFA(r)
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
		p.markSessionTwoFA(r)
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
func (p *TwoFAPlugin) markSessionTwoFA(r *http.Request) {
	cfg := p.mgr.Config()
	cookie, err := r.Cookie(cfg.SessionCookie)
	if err != nil {
		return
	}
	if marker, ok := p.mgr.SessionStore().(SessionTwoFAMarker); ok {
		_ = marker.MarkTwoFactorVerified(r.Context(), cookie.Value)
	}
}

// RequireTwoFA returns middleware that:
//
//   - Lets requests through if the user has not enrolled in 2FA.
//   - Lets requests through if the session has TwoFactorVerified=true.
//   - Returns 403 in all other cases (enrolled but not verified, or no session).
//
// Install this on every route that requires step-up authentication. Note
// that it relies on the SessionStore implementing SessionTwoFAMarker —
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
			// Not enrolled — bypass.
			if state == nil || !state.Enabled {
				next.ServeHTTP(w, r)
				return
			}
			// Enrolled — must have verified for this session.
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

// GET {basePath}/2fa/backup-codes
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

	state.BackupCodes = hashedCodes
	if err := p.store.SetTwoFA(r.Context(), userID, state); err != nil {
		writeAuthError(w, http.StatusInternalServerError, "failed to save backup codes")
		return
	}

	p.mgr.emitSecurity(r.Context(), SecurityEvent{
		Kind:   "2fa.backup_codes_regenerated",
		UserID: userID,
		Remote: remoteHost(r),
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"backup_codes": plainCodes,
	})
}

// ─── TOTP implementation (RFC 6238, HMAC-SHA1) ─────────────────────────

// GenerateSecret creates a cryptographically random 20-byte secret and
// returns it as a base32-encoded string (no padding). Panics if crypto/rand
// fails — entropy starvation makes the rest of the auth system unsound.
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
func ValidateTOTP(secret, code string, period, skew uint) bool {
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
			return true
		}
	}
	return false
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
	for i := 0; i < n; i++ {
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
