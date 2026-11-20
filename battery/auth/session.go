package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Session is one cookie session; PendingTwoFactor means only the 2FA challenge accepts it.
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

// SessionStore stores cookie sessions.
type SessionStore interface {
	Create(ctx context.Context, userID string, ttl time.Duration) (*Session, error)
	Get(ctx context.Context, token string) (*Session, error)
	Delete(ctx context.Context, token string) error
	Cleanup(ctx context.Context) (int, error) // returns count removed
}

// SessionTwoFAMarker marks a session verified; it must set TwoFactorVerified and clear PendingTwoFactor.
type SessionTwoFAMarker interface {
	MarkTwoFactorVerified(ctx context.Context, token string) error
}

// SessionUserPurger deletes every session of a user and returns how many.
type SessionUserPurger interface {
	DeleteByUser(ctx context.Context, userID string) (int, error)
}

// SessionPendingPurger deletes a user's sessions still awaiting the 2FA challenge and returns how many.
type SessionPendingPurger interface {
	DeletePendingByUser(ctx context.Context, userID string) (int, error)
}

// SessionPendingMarker marks a new session as awaiting the 2FA challenge; 2FA needs it.
type SessionPendingMarker interface {
	MarkPendingTwoFactor(ctx context.Context, token string) error
}

// TwoFactorChecker is a plugin that reports whether a user has a second factor.
type TwoFactorChecker interface {
	HasTwoFactorEnabled(ctx context.Context, userID string) (bool, error)
}

// ErrSessionNotFound means the token is unknown or expired.
var ErrSessionNotFound = errors.New("auth: session not found")

// MemorySessionStore is an in-memory SessionStore for one process.
type MemorySessionStore struct {
	mu        sync.RWMutex
	sessions  map[string]*Session
	lastSweep time.Time
}

const sessionSweepInterval = 15 * time.Minute

// NewMemorySessionStore returns a fresh, empty MemorySessionStore.
func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{sessions: map[string]*Session{}}
}

// Create stores a new session; a zero ttl means seven days and a negative one is refused.
func (m *MemorySessionStore) Create(_ context.Context, userID string, ttl time.Duration) (*Session, error) {
	if ttl < 0 {
		return nil, fmt.Errorf("auth: MemorySessionStore.Create: ttl must be >= 0 (got %v)", ttl)
	}
	if ttl == 0 {
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
	if now.Sub(m.lastSweep) >= sessionSweepInterval {
		purgeMatching(m.sessions, func(s *Session) bool { return !now.Before(s.ExpiresAt) })
		m.lastSweep = now
	}
	cp := *sess
	m.mu.Unlock()
	return &cp, nil
}

// Get returns the session, or ErrSessionNotFound when unknown or expired.
func (m *MemorySessionStore) Get(_ context.Context, token string) (*Session, error) {
	m.mu.RLock()
	sess, ok := m.sessions[token]
	if !ok {
		m.mu.RUnlock()
		return nil, ErrSessionNotFound
	}
	cp := *sess
	m.mu.RUnlock()
	if cp.Expired() {
		m.mu.Lock()
		delete(m.sessions, token)
		m.mu.Unlock()
		return nil, ErrSessionNotFound
	}
	return &cp, nil
}

// Delete drops the session; an unknown token is not an error.
func (m *MemorySessionStore) Delete(_ context.Context, token string) error {
	m.mu.Lock()
	delete(m.sessions, token)
	m.mu.Unlock()
	return nil
}

func purgeMatching[K comparable, V any](m map[K]V, match func(V) bool) int {
	n := 0
	for k, v := range m {
		if match(v) {
			delete(m, k)
			n++
		}
	}
	return n
}

// DeleteByUser deletes every session of userID. Implements SessionUserPurger.
func (m *MemorySessionStore) DeleteByUser(_ context.Context, userID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return purgeMatching(m.sessions, func(s *Session) bool { return s.UserID == userID }), nil
}

// DeletePendingByUser deletes userID's pending sessions. Implements SessionPendingPurger.
func (m *MemorySessionStore) DeletePendingByUser(_ context.Context, userID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return purgeMatching(m.sessions, func(s *Session) bool { return s.UserID == userID && s.PendingTwoFactor }), nil
}

// MarkTwoFactorVerified sets TwoFactorVerified and clears PendingTwoFactor; an unknown token is a no-op.
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
func (m *MemorySessionStore) MarkPendingTwoFactor(_ context.Context, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sess, ok := m.sessions[token]; ok {
		sess.PendingTwoFactor = true
	}
	return nil
}

// Cleanup deletes expired sessions and returns how many.
func (m *MemorySessionStore) Cleanup(_ context.Context) (int, error) {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	return purgeMatching(m.sessions, func(s *Session) bool { return now.After(s.ExpiresAt) }), nil
}

func newSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
