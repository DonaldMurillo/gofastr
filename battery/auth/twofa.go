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
	"github.com/DonaldMurillo/gofastr/framework/ratelimit"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"
	"golang.org/x/crypto/bcrypt"
)

// TwoFAConfig holds optional settings for the TwoFAPlugin.
type TwoFAConfig struct {
	// Issuer is the name shown in authenticator apps. Defaults to "GoFastr".
	Issuer string

	// Period is the TOTP step in seconds (default 30); codes are six digits.
	Period uint

	// Skew is the number of time-steps allowed before/after the current step.
	Skew uint

	// BackupCodeCount is how many backup codes to generate. Defaults to 10.
	BackupCodeCount int

	// Store keeps 2FA state; nil is an in-memory store, refused in production.
	Store TwoFAStore

	// RateLimit is the per-IP limit on challenge and verify; nil means 10 a minute with a 15-minute block.
	RateLimit *RateLimiterConfig

	// ChallengeAttemptsPerSession caps the codes one pending login may try before it is deleted (default 10).
	ChallengeAttemptsPerSession int
}

func (c *TwoFAConfig) defaults() {
	if c.Issuer == "" {
		c.Issuer = "GoFastr"
	}
	if c.Period == 0 {
		c.Period = 30
	}
	if c.Skew == 0 {
		c.Skew = 1
	}
	if c.BackupCodeCount == 0 {
		c.BackupCodeCount = 10
	}
	if c.RateLimit == nil {
		c.RateLimit = &RateLimiterConfig{
			MaxAttempts:   10,
			Window:        time.Minute,
			BlockDuration: 15 * time.Minute,
		}
	}
	if c.ChallengeAttemptsPerSession <= 0 {
		c.ChallengeAttemptsPerSession = 10
	}
}

// TwoFAState holds the per-user 2FA enrollment and verification status.
type TwoFAState struct {
	Enabled     bool     // true after successful verify step
	Secret      string   // base32 TOTP secret (plaintext, stored encrypted at rest in production)
	BackupCodes []string // bcrypt-hashed backup codes
	Verified    bool     // true once the user has proven they can generate a valid code

	// LastUsedStep is the TOTP step of the last accepted code; a step is accepted once.
	LastUsedStep uint64
}

// TwoFAStateSwapper writes next only while the stored state equals expect (nil expect: no row), reporting whether it wrote.
type TwoFAStateSwapper interface {
	CompareAndSwapTwoFA(ctx context.Context, userID string, expect, next *TwoFAState) (bool, error)
}

// TwoFAStore is the interface for persisting 2FA state per user.
type TwoFAStore interface {
	// GetTwoFA retrieves the 2FA state for a user. Returns nil if not enrolled.
	GetTwoFA(ctx context.Context, userID string) (*TwoFAState, error)

	// SetTwoFA persists the 2FA state for a user.
	SetTwoFA(ctx context.Context, userID string, state *TwoFAState) error

	// DeleteTwoFA removes the 2FA state for a user.
	DeleteTwoFA(ctx context.Context, userID string) error

	// ConsumeBackupCode removes and reports a matching backup code.
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

func (m *MemoryTwoFAStore) DeleteTwoFA(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.states, userID)
	return nil
}

func (m *MemoryTwoFAStore) ConsumeBackupCode(_ context.Context, userID string, code string) (bool, error) {
	m.mu.RLock()
	state, ok := m.states[userID]
	if !ok || len(state.BackupCodes) == 0 {
		m.mu.RUnlock()
		return false, nil
	}
	hashes := append([]string(nil), state.BackupCodes...) // copy
	m.mu.RUnlock()

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
	return false, nil
}

// TwoFAPlugin serves TOTP enrolment, the sign-in challenge and backup codes.
type TwoFAPlugin struct {
	config         TwoFAConfig
	mgr            *AuthManager
	store          TwoFAStore
	challengeLimit *RateLimiter
	sessionGuesses *RateLimiter
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

// CompareAndSwapTwoFA implements TwoFAStateSwapper.
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

// Init selects the store, refusing the in-memory one in production, and ensures its schema.
func (p *TwoFAPlugin) Init(mgr *AuthManager) error {
	p.mgr = mgr
	ttl := mgr.Config().SessionTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	guessCfg := RateLimiterConfig{
		MaxAttempts:   p.config.ChallengeAttemptsPerSession,
		Window:        ttl,
		BlockDuration: ttl,
		Scope:         "twofa_session",
	}
	if p.config.RateLimit != nil {
		guessCfg.Store = p.config.RateLimit.Store
	}
	p.sessionGuesses = NewRateLimiter(guessCfg)
	if p.config.Store != nil {
		p.store = p.config.Store
	} else {
		cfg := mgr.Config()
		if !cfg.DevMode && !cfg.AllowInMemoryStores {
			return fmt.Errorf("auth: production mode refuses the in-memory 2FA store: a restart wipes enrollment, silently reverting every 2FA account to password-only auth; set TwoFAConfig.Store (e.g. auth.NewEntityTwoFAStore(db, \"auth_twofa\", auth.EntityTwoFAStoreConfig{EncryptionKey: key})), or set AuthConfig.AllowInMemoryStores: true to acknowledge a deliberate single-node deployment")
		}
		p.store = NewMemoryTwoFAStore()
		if !cfg.DevMode {
			slog.Default().Warn("auth: production mode is running on the in-memory 2FA store (acknowledged via AllowInMemoryStores): a restart wipes enrollment, reverting 2FA accounts to password-only auth")
		}
	}
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
	r = r.Group("", browserAccountOnly)
	r.Post(basePath+"/2fa/enroll", http.HandlerFunc(p.enrollHandler))
	r.Post(basePath+"/2fa/verify", http.HandlerFunc(p.verifyHandler))
	r.Post(basePath+"/2fa/challenge", http.HandlerFunc(p.challengeHandler))
	r.Post(basePath+"/2fa/disable", http.HandlerFunc(p.disableHandler))
	r.Post(basePath+"/2fa/backup-codes", http.HandlerFunc(p.backupCodesHandler))
}

func (p *TwoFAPlugin) swapTwoFAState(w http.ResponseWriter, r *http.Request, userID string, expect, next *TwoFAState) bool {
	swapper, ok := p.store.(TwoFAStateSwapper)
	if !ok {
		if err := p.store.SetTwoFA(r.Context(), userID, next); err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonFailedToSave2faState)
			return false
		}
		return true
	}
	wrote, err := swapper.CompareAndSwapTwoFA(r.Context(), userID, expect, next)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonFailedToSave2faState)
		return false
	}
	if !wrote {
		writeAuthError(w, r, http.StatusConflict, reason2faConflict)
		return false
	}
	return true
}

func (p *TwoFAPlugin) requireStepUpSession(w http.ResponseWriter, r *http.Request) (*Session, bool) {
	if rejectInProcessAccountOperation(w, r) {
		return nil, false
	}
	sess, err := p.mgr.requestSession(r, false)
	if err != nil {
		writeAuthError(w, r, http.StatusUnauthorized, reasonNotAuthenticated)
		return nil, false
	}
	if sess.PendingTwoFactor {
		writeAuthError(w, r, http.StatusForbidden, reason2faRequired)
		return nil, false
	}
	state, err := p.store.GetTwoFA(r.Context(), sess.UserID)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reason2faLookupFailed)
		return nil, false
	}
	if state != nil && state.Enabled && !sess.TwoFactorVerified {
		writeAuthError(w, r, http.StatusForbidden, reason2faRequired)
		return nil, false
	}
	return sess, true
}

func (p *TwoFAPlugin) requireStepUpUser(w http.ResponseWriter, r *http.Request) (userID string, ok bool) {
	sess, ok := p.requireStepUpSession(w, r)
	if !ok {
		return "", false
	}
	return sess.UserID, true
}

func (p *TwoFAPlugin) enrollHandler(w http.ResponseWriter, r *http.Request) {
	if rejectCrossSiteForm(w, r) {
		return
	}
	userID, ok := p.requireStepUpUser(w, r)
	if !ok {
		return
	}

	existing, err := p.store.GetTwoFA(r.Context(), userID)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonCouldNotRead2faState)
		return
	}
	if existing != nil && existing.Enabled {
		writeAuthError(w, r, http.StatusConflict, reason2faReenroll)
		return
	}

	secret := GenerateSecret()

	state := &TwoFAState{
		Enabled:     false,
		Secret:      secret,
		BackupCodes: nil,
		Verified:    false,
	}
	if !p.swapTwoFAState(w, r, userID, existing, state) {
		return
	}

	email := userID
	if us := p.mgr.UserStore(); us != nil {
		if user, err := us.FindByID(r.Context(), userID); err == nil && user != nil {
			email = user.GetEmail()
		}
	}
	otpauthURL := buildOTPAuthURL(p.config.Issuer, email, secret, p.config.Period)

	writeCredentialHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"secret": secret,
		"url":    otpauthURL,
	})
}

func (p *TwoFAPlugin) verifyHandler(w http.ResponseWriter, r *http.Request) {
	if p.challengeLimit != nil && !p.challengeLimit.guard(w, r) {
		return
	}
	sess, ok := p.requireStepUpSession(w, r)
	if !ok {
		return
	}
	userID := sess.UserID

	var body struct {
		Code string `json:"code"`
	}
	if !decodeJSONLimited(w, r, &body) {
		return
	}
	if body.Code == "" {
		writeAuthError(w, r, http.StatusBadRequest, reasonCodeRequired)
		return
	}

	state, err := p.store.GetTwoFA(r.Context(), userID)
	if err != nil || state == nil {
		writeAuthError(w, r, http.StatusBadRequest, reason2faNotEnrolled)
		return
	}
	if state.Enabled {
		writeAuthError(w, r, http.StatusBadRequest, reason2faEnabled)
		return
	}

	if !ValidateTOTP(state.Secret, body.Code, p.config.Period, p.config.Skew) {
		writeAuthError(w, r, http.StatusUnauthorized, reasonInvalidCode)
		return
	}

	plainCodes, hashedCodes, err := generateBackupCodes(p.config.BackupCodeCount)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonFailedToGenerateBackupCodes)
		return
	}

	enabled := cloneTwoFAState(state)
	enabled.Enabled = true
	enabled.Verified = true
	enabled.BackupCodes = hashedCodes
	if !p.swapTwoFAState(w, r, userID, state, enabled) {
		return
	}

	if err := p.markSessionTwoFA(r.Context(), sess.Token); err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonFailedToRecordVerification)
		return
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

func (p *TwoFAPlugin) challengeHandler(w http.ResponseWriter, r *http.Request) {
	if rejectCrossSiteForm(w, r) {
		return
	}
	if !guardAuthLimit(p.challengeLimit, w, r) {
		return
	}
	isForm := isFormRequest(r)
	fail := func(status int, reason i18n.Phrase) {
		if isForm {
			writeFormAuthError(w, r, status, reason)
			return
		}
		writeAuthError(w, r, status, reason)
	}
	verified := func(backupCode bool) {
		if isForm {
			http.Redirect(w, r, successRedirect(w, r, "/"), http.StatusSeeOther)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if backupCode {
			json.NewEncoder(w).Encode(map[string]any{"verified": true, "backup_code": true})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"verified": true})
	}
	sess, err := p.mgr.requestSession(r, true)
	if err != nil {
		fail(http.StatusUnauthorized, reasonNotAuthenticated)
		return
	}
	userID := sess.UserID

	var body struct {
		Code string `json:"code"`
	}
	if isForm {
		v, ok := decodeFormFields(w, r, "code")
		if !ok {
			return
		}
		body.Code = v["code"]
	} else if !decodeJSONLimited(w, r, &body) {
		return
	}
	if body.Code == "" {
		fail(http.StatusBadRequest, reasonCodeRequired)
		return
	}

	state, err := p.store.GetTwoFA(r.Context(), userID)
	if err != nil || state == nil || !state.Enabled {
		fail(http.StatusBadRequest, reason2faNotEnabled)
		return
	}

	if p.sessionGuesses != nil {
		allowed, retry, err := p.sessionGuesses.Admit(r.Context(), sha256hex(sess.Token))
		if err != nil {
			slog.Default().Warn("auth: 2fa session budget store error: failing closed", "err", err)
			w.Header().Set("Retry-After", ratelimit.RetryAfterSeconds(retry))
			fail(http.StatusServiceUnavailable, reasonTryAgainShortly)
			return
		}
		if !allowed {
			if err := p.mgr.SessionStore().Delete(r.Context(), sess.Token); err != nil {
				fail(http.StatusInternalServerError, reasonCouldNotEndTheSignIn)
				return
			}
			p.mgr.emitSecurity(r.Context(), SecurityEvent{
				Kind:   "2fa.challenge_locked",
				UserID: userID,
				Remote: remoteHost(r),
			})
			fail(http.StatusUnauthorized, reasonTooManyAttempts)
			return
		}
	}

	if step, ok := ValidateTOTPStep(state.Secret, body.Code, p.config.Period, p.config.Skew); ok && step > state.LastUsedStep {
		consumed := cloneTwoFAState(state)
		consumed.LastUsedStep = step
		if !p.swapTwoFAState(w, r, userID, state, consumed) {
			return
		}
		if !p.completeStepUp(w, r, sess) {
			return
		}
		p.mgr.emitSecurity(r.Context(), SecurityEvent{
			Kind:   "2fa.challenge_succeeded",
			UserID: userID,
			Remote: remoteHost(r),
			Meta:   map[string]string{"method": "totp"},
		})
		verified(false)
		return
	}

	consumed, err := p.store.ConsumeBackupCode(r.Context(), userID, body.Code)
	if err != nil {
		fail(http.StatusInternalServerError, reasonBackupCodeCheckFailed)
		return
	}
	if consumed {
		if !p.completeStepUp(w, r, sess) {
			return
		}
		p.mgr.emitSecurity(r.Context(), SecurityEvent{
			Kind:   "2fa.challenge_succeeded",
			UserID: userID,
			Remote: remoteHost(r),
			Meta:   map[string]string{"method": "backup_code"},
		})
		verified(true)
		return
	}

	p.mgr.emitSecurity(r.Context(), SecurityEvent{
		Kind:   "2fa.challenge_failed",
		UserID: userID,
		Remote: remoteHost(r),
	})
	fail(http.StatusUnauthorized, reasonInvalidCode)
}

// HasTwoFactorEnabled reports whether userID has an enabled factor. Implements TwoFactorChecker.
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

func (p *TwoFAPlugin) completeStepUp(w http.ResponseWriter, r *http.Request, sess *Session) bool {
	ctx := r.Context()
	store := p.mgr.SessionStore()
	fail := func(status int, reason i18n.Phrase, fresh *Session) bool {
		if fresh != nil {
			undo, cancel := undoContext(ctx)
			_ = store.Delete(undo, fresh.Token)
			cancel()
		}
		writeAuthError(w, r, status, reason)
		return false
	}
	//gofastr:allow(siblingresult) the step-up has just verified the factor: the fresh session is pending by construction and markSessionTwoFA clears it below
	fresh, _, err := p.mgr.MintSession(ctx, sess.UserID, p.mgr.Config().SessionTTL)
	if err != nil {
		return fail(http.StatusInternalServerError, reasonCouldNotCompleteTheStepUp, nil)
	}
	if err := p.markSessionTwoFA(ctx, fresh.Token); err != nil {
		return fail(http.StatusInternalServerError, reasonCouldNotCompleteTheStepUp, fresh)
	}
	if still, err := store.Get(ctx, sess.Token); err != nil || still == nil || still.UserID != sess.UserID {
		return fail(http.StatusUnauthorized, reasonNotAuthenticated, fresh)
	}
	if err := store.Delete(ctx, sess.Token); err != nil {
		return fail(http.StatusInternalServerError, reasonCouldNotCompleteTheStepUp, fresh)
	}
	writeCredentialHeaders(w)
	mintSessionCookie(w, p.mgr.Config(), fresh.Token, fresh.ExpiresAt)
	return true
}

func (p *TwoFAPlugin) markSessionTwoFA(ctx context.Context, token string) error {
	if marker, ok := p.mgr.SessionStore().(SessionTwoFAMarker); ok {
		return marker.MarkTwoFactorVerified(ctx, token)
	}
	return nil
}

// RequireTwoFA refuses a principal with an enabled factor unless one of their own sessions on the request passed the challenge.
func (p *TwoFAPlugin) RequireTwoFA() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := GetCurrentUser(r.Context())
			verified := false
			if framework.IsInProcessRequest(r) {
				assurance, ok := r.Context().Value(sessionAssuranceKey{}).(sessionAssurance)
				if !ok || u == nil || assurance.userID != u.GetID() || assurance.manager != p.mgr || assurance.tenantBound && assurance.tenantID != handler.GetTenantID(r.Context()) {
					writeAuthError(w, r, http.StatusUnauthorized, reasonNoSession)
					return
				}
				verified = assurance.verified
			} else {
				if u == nil {
					writeAuthError(w, r, http.StatusUnauthorized, reasonNotAuthenticated)
					return
				}
				mine := false
				for _, session := range p.mgr.requestSessions(r) {
					if session.UserID == u.GetID() && !session.PendingTwoFactor {
						mine = true
						verified = verified || session.TwoFactorVerified
					}
				}
				if !mine {
					writeAuthError(w, r, http.StatusUnauthorized, reasonNoSession)
					return
				}
			}
			userID := u.GetID()
			state, err := p.store.GetTwoFA(r.Context(), userID)
			if err != nil {
				writeAuthError(w, r, http.StatusInternalServerError, reason2faLookupFailed)
				return
			}
			if state == nil || !state.Enabled {
				next.ServeHTTP(w, r)
				return
			}
			if verified {
				next.ServeHTTP(w, r)
				return
			}
			writeAuthError(w, r, http.StatusForbidden, reason2faRequired)
		})
	}
}

func (p *TwoFAPlugin) disableHandler(w http.ResponseWriter, r *http.Request) {
	if rejectCrossSiteForm(w, r) {
		return
	}
	userID, ok := p.requireStepUpUser(w, r)
	if !ok {
		return
	}

	if err := p.store.DeleteTwoFA(r.Context(), userID); err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonFailedToDisable2fa)
		return
	}
	if purger, ok := p.mgr.SessionStore().(SessionPendingPurger); ok {
		if _, err := purger.DeletePendingByUser(r.Context(), userID); err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reason2faRevokeFailed)
			return
		}
	}

	p.mgr.emitSecurity(r.Context(), SecurityEvent{
		Kind:   "2fa.disabled",
		UserID: userID,
		Remote: remoteHost(r),
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"disabled": true})
}

func (p *TwoFAPlugin) backupCodesHandler(w http.ResponseWriter, r *http.Request) {
	if rejectCrossSiteForm(w, r) {
		return
	}
	userID, ok := p.requireStepUpUser(w, r)
	if !ok {
		return
	}

	state, err := p.store.GetTwoFA(r.Context(), userID)
	if err != nil || state == nil || !state.Enabled {
		writeAuthError(w, r, http.StatusBadRequest, reason2faNotEnabled)
		return
	}

	plainCodes, hashedCodes, err := generateBackupCodes(p.config.BackupCodeCount)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonFailedToGenerateBackupCodes)
		return
	}

	if !p.replaceBackupCodes(w, r, userID, state.Secret, hashedCodes) {
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

const regenSwapAttempts = 4

func (p *TwoFAPlugin) replaceBackupCodes(w http.ResponseWriter, r *http.Request, userID, secret string, codes []string) bool {
	swapper, canSwap := p.store.(TwoFAStateSwapper)
	for attempt := 0; attempt < regenSwapAttempts; attempt++ {
		cur, err := p.store.GetTwoFA(r.Context(), userID)
		if err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonFailedToSaveBackupCodes)
			return false
		}
		if cur == nil || !cur.Enabled || subtle.ConstantTimeCompare([]byte(cur.Secret), []byte(secret)) != 1 {
			writeAuthError(w, r, http.StatusConflict, reason2faGone)
			return false
		}
		next := cloneTwoFAState(cur)
		next.BackupCodes = codes
		if !canSwap {
			if err := p.store.SetTwoFA(r.Context(), userID, next); err != nil {
				writeAuthError(w, r, http.StatusInternalServerError, reasonFailedToSaveBackupCodes)
				return false
			}
			return true
		}
		wrote, err := swapper.CompareAndSwapTwoFA(r.Context(), userID, cur, next)
		if err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonFailedToSaveBackupCodes)
			return false
		}
		if wrote {
			return true
		}
	}
	writeAuthError(w, r, http.StatusConflict, reason2faConflict)
	return false
}

// GenerateSecret returns a random 20-byte base32 TOTP secret.
func GenerateSecret() string {
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		panic(fmt.Sprintf("auth: crypto/rand failed: %v", err))
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
}

// GenerateTOTP returns the six-digit RFC 6238 code for secret at timeStep.
func GenerateTOTP(secret string, timeStep uint64) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return ""
	}

	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, timeStep)

	mac := hmac.New(sha1.New, key)
	mac.Write(buf)
	hmacResult := mac.Sum(nil)

	offset := hmacResult[len(hmacResult)-1] & 0x0F
	code := binary.BigEndian.Uint32(hmacResult[offset:]) & 0x7FFFFFFF

	code = code % 1000000
	return fmt.Sprintf("%06d", code)
}

// ValidateTOTP reports whether code is valid within skew steps of now.
func ValidateTOTP(secret, code string, period, skew uint) bool {
	_, ok := ValidateTOTPStep(secret, code, period, skew)
	return ok
}

// ValidateTOTPStep is ValidateTOTP returning the matched step, so a caller can refuse a reused one.
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
		expected := []byte(GenerateTOTP(secret, uint64(step)))
		if subtle.ConstantTimeCompare(expected, codeBytes) == 1 {
			return uint64(step), true
		}
	}
	return 0, false
}

func buildOTPAuthURL(issuer, accountName, secret string, period uint) string {
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
	q.Set("period", strconv.FormatUint(uint64(period), 10))
	u.RawQuery = q.Encode()
	return u.String()
}

func generateBackupCodes(n int) (plain []string, hashed []string, err error) {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	plain = make([]string, n)
	hashed = make([]string, n)
	for i := range n {
		code, err := randomString(8, charset)
		if err != nil {
			return nil, nil, err
		}
		h, err := bcrypt.GenerateFromPassword([]byte(code), bcryptCost)
		if err != nil {
			return nil, nil, err
		}
		plain[i] = code
		hashed[i] = string(h)
	}
	return plain, hashed, nil
}

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
