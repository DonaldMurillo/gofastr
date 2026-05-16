package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

// Session represents an authenticated cookie-bound session. UserID is the
// stable identifier the application's User repo uses to look up a row;
// Token is the opaque cookie value the browser sends back.
//
// TwoFactorVerified is set by the 2FA challenge handler after a successful
// TOTP / backup-code verification. The RequireTwoFA middleware uses it to
// gate access for users who have enrolled in 2FA.
//
// PendingTwoFactor is set when the login flow detects an enrolled user
// and means the session is ONLY valid for /auth/2fa/challenge. Every
// other handler refuses it. The successful challenge clears Pending and
// sets Verified.
type Session struct {
	Token             string
	UserID            string
	CreatedAt         time.Time
	ExpiresAt         time.Time
	TwoFactorVerified bool
	PendingTwoFactor  bool
}

// Expired reports whether s has passed its ExpiresAt boundary.
func (s *Session) Expired() bool { return time.Now().After(s.ExpiresAt) }

// SessionStore is the storage backend for cookie sessions. Production
// deployments swap in Redis / Postgres / etc. behind the same interface.
//
// All methods take a Context for cancellation; in-memory implementations
// can ignore it.
type SessionStore interface {
	Create(ctx context.Context, userID string, ttl time.Duration) (*Session, error)
	Get(ctx context.Context, token string) (*Session, error)
	Delete(ctx context.Context, token string) error
	Cleanup(ctx context.Context) (int, error) // returns count removed
}

// SessionTwoFAMarker is the optional SessionStore extension that lets the
// 2FA challenge handler mark a session as having completed the second
// factor. The implementation MUST set TwoFactorVerified=true AND clear
// PendingTwoFactor — the two are inverses in the post-login state model.
// RequireTwoFA refuses access if a 2FA-enabled user's session is not
// marked. Stores that don't implement this method effectively cannot
// participate in 2FA enforcement (RequireTwoFA will fail-closed).
type SessionTwoFAMarker interface {
	MarkTwoFactorVerified(ctx context.Context, token string) error
}

// SessionPendingMarker is the optional SessionStore extension that lets
// CorePlugin's login handler mark a freshly-minted session as awaiting
// a 2FA challenge. Without this, login of a 2FA-enrolled user produces a
// fully-authenticated session and 2FA enforcement is opt-in per route.
type SessionPendingMarker interface {
	MarkPendingTwoFactor(ctx context.Context, token string) error
}

// TwoFactorChecker is the optional plugin extension that lets CorePlugin
// know whether to mint a PendingTwoFactor session at login time. The
// 2FA plugin implements this. Other plugins (WebAuthn, SMS) can
// implement it too to participate in the same gating.
type TwoFactorChecker interface {
	HasTwoFactorEnabled(ctx context.Context, userID string) (bool, error)
}

// ErrSessionNotFound is returned by SessionStore.Get when the token is
// unknown or already expired.
var ErrSessionNotFound = errors.New("auth: session not found")

// MemorySessionStore is a goroutine-safe in-memory SessionStore suitable
// for single-instance deployments and tests. Tokens are 32 cryptographically
// random bytes base64'd, mirroring what most cookie auth systems use.
type MemorySessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewMemorySessionStore returns a fresh, empty MemorySessionStore.
func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{sessions: map[string]*Session{}}
}

// Create generates a random token, persists a new Session keyed on it, and
// returns the result. ttl is the session lifetime; consumers passing 0 get
// a sensible default (one week).
func (m *MemorySessionStore) Create(_ context.Context, userID string, ttl time.Duration) (*Session, error) {
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	tok, err := newSessionToken()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	sess := &Session{
		Token:     tok,
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	m.mu.Lock()
	m.sessions[tok] = sess
	m.mu.Unlock()
	return sess, nil
}

// Get returns the session for the given token, or ErrSessionNotFound if
// unknown / expired.
func (m *MemorySessionStore) Get(_ context.Context, token string) (*Session, error) {
	m.mu.RLock()
	sess, ok := m.sessions[token]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrSessionNotFound
	}
	if sess.Expired() {
		m.mu.Lock()
		delete(m.sessions, token)
		m.mu.Unlock()
		return nil, ErrSessionNotFound
	}
	return sess, nil
}

// Delete drops the session for token. Returns nil even if the token is
// unknown — idempotent logout.
func (m *MemorySessionStore) Delete(_ context.Context, token string) error {
	m.mu.Lock()
	delete(m.sessions, token)
	m.mu.Unlock()
	return nil
}

// MarkTwoFactorVerified flips TwoFactorVerified=true and clears
// PendingTwoFactor on the session. No-op (and no error) if the session
// is unknown.
func (m *MemorySessionStore) MarkTwoFactorVerified(_ context.Context, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sess, ok := m.sessions[token]; ok {
		sess.TwoFactorVerified = true
		sess.PendingTwoFactor = false
	}
	return nil
}

// MarkPendingTwoFactor flips PendingTwoFactor=true on the session.
// No-op if the session is unknown.
func (m *MemorySessionStore) MarkPendingTwoFactor(_ context.Context, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sess, ok := m.sessions[token]; ok {
		sess.PendingTwoFactor = true
	}
	return nil
}

// Cleanup removes all expired sessions and returns the count purged. Safe
// to call on a timer; concurrent with Get/Create/Delete.
func (m *MemorySessionStore) Cleanup(_ context.Context) (int, error) {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for tok, sess := range m.sessions {
		if now.After(sess.ExpiresAt) {
			delete(m.sessions, tok)
			n++
		}
	}
	return n, nil
}

// newSessionToken generates a 256-bit random token, base64-URL encoded.
func newSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
