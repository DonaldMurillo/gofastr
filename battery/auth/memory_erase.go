package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/datexport"
)

var memoryEraseMu sync.Mutex

func unregisterMemoryStoreEraser(mgr *AuthManager) {
	memoryEraseMu.Lock()
	prev := mgr.unregisterMemoryErase
	mgr.unregisterMemoryErase, mgr.memoryEraseName = nil, ""
	memoryEraseMu.Unlock()
	if prev != nil {
		prev()
	}
}

func registerMemoryStoreEraser(mgr *AuthManager) {
	unregisterMemoryStoreEraser(mgr)
	var (
		sessions *MemorySessionStore
		twofa    *MemoryTwoFAStore
		passkeys *MemoryPasskeyStore
		tokens   []*MemoryMagicLinkTokenStore
	)
	sessions, _ = mgr.SessionStore().(*MemorySessionStore)
	addTokens := func(s MagicLinkTokenStore) {
		if ms, ok := s.(*MemoryMagicLinkTokenStore); ok && !slices.Contains(tokens, ms) {
			tokens = append(tokens, ms)
		}
	}
	for _, name := range mgr.order {
		switch p := mgr.plugins[name].(type) {
		case *TwoFAPlugin:
			twofa, _ = p.store.(*MemoryTwoFAStore)
		case *PasskeyPlugin:
			passkeys, _ = p.store.(*MemoryPasskeyStore)
			addTokens(p.challenges)
		case *PasswordResetPlugin:
			addTokens(p.store)
		case *EmailVerificationPlugin:
			addTokens(p.store)
		case *MagicLinkPlugin:
			addTokens(p.tokenStore)
		case *EmailCodePlugin:
			addTokens(p.tokenStore)
		}
	}
	if sessions == nil && twofa == nil && passkeys == nil && len(tokens) == 0 {
		return
	}
	name, unregister := datexport.RegisterStoreEraser(datexport.StoreEraser{
		Name:   "auth_memory",
		Source: "auth",
		EraseResolved: func(ctx context.Context, user datexport.ErasedUser, dryRun bool) (int, error) {
			userID := user.ID
			email, ok := user.Identity(datexport.IdentityEmail)
			if _, sqlUsers := mgr.UserStore().(*EntityUserStore); !ok && !sqlUsers && holdsEmailKeyed(tokens) {
				u, err := mgr.UserStore().FindByID(ctx, userID)
				switch {
				case errors.Is(err, ErrUserNotFound):
				case err != nil:
					return 0, fmt.Errorf("auth: reading the erased user's email: %w", err)
				case u != nil:
					email = u.GetEmail()
					if c, err := mgr.canonicalizeEmail(email); err == nil {
						email = c
					}
				}
			}
			n := 0
			if sessions != nil {
				n += sessions.eraseUser(userID, dryRun)
			}
			if twofa != nil {
				n += twofa.eraseUser(userID, dryRun)
			}
			if passkeys != nil {
				n += passkeys.eraseUser(userID, dryRun)
			}
			for _, ts := range tokens {
				n += ts.eraseUser(userID, email, dryRun)
			}
			return n, nil
		},
	})
	memoryEraseMu.Lock()
	mgr.memoryEraseName, mgr.unregisterMemoryErase = name, unregister
	memoryEraseMu.Unlock()
}

func holdsEmailKeyed(stores []*MemoryMagicLinkTokenStore) bool {
	for _, ts := range stores {
		ts.mu.RLock()
		n := countMatching(ts.tokens, func(e *magicLinkEntry) bool {
			_, link := peekPurposePayload(e.email, purposeMagicLink)
			_, code := peekPurposePayload(e.email, purposeEmailCode)
			return link || code
		})
		ts.mu.RUnlock()
		if n > 0 {
			return true
		}
	}
	return false
}

func (m *MemorySessionStore) eraseUser(userID string, dryRun bool) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	match := func(s *Session) bool { return s.UserID == userID }
	if dryRun {
		return countMatching(m.sessions, match)
	}
	return purgeMatching(m.sessions, match)
}

func (m *MemoryTwoFAStore) eraseUser(userID string, dryRun bool) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.states[userID]; !ok {
		return 0
	}
	if !dryRun {
		delete(m.states, userID)
	}
	return 1
}

func (m *MemoryPasskeyStore) eraseUser(userID string, dryRun bool) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	match := func(c PasskeyCredential) bool { return c.UserID == userID }
	if dryRun {
		return countMatching(m.creds, match)
	}
	return purgeMatching(m.creds, match)
}

func (m *MemoryMagicLinkTokenStore) eraseUser(userID, email string, dryRun bool) int {
	var payloads []string
	for _, p := range userPurposes {
		payloads = append(payloads, string(p)+":"+userID)
	}
	if email != "" {
		payloads = append(payloads, string(purposeMagicLink)+":"+email, string(purposeEmailCode)+":"+email)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	match := func(e *magicLinkEntry) bool { return slices.Contains(payloads, e.email) }
	if dryRun {
		return countMatching(m.tokens, match)
	}
	return purgeMatching(m.tokens, match)
}

func countMatching[K comparable, V any](m map[K]V, match func(V) bool) int {
	n := 0
	for _, v := range m {
		if match(v) {
			n++
		}
	}
	return n
}
