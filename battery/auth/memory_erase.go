package auth

import (
	"context"
	"slices"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/datexport"
)

// The in-memory stores are allowed in production behind AllowInMemoryStores
// (one node, state lost on restart), and the SQL erasers never reach them:
// after App.EraseUserData the user's sessions, TOTP seed and backup-code
// hashes, passkeys and outstanding reset or verification links stayed in
// process memory until a restart. resolveEraseTables registers one
// datexport.StoreEraser over whichever of them the manager runs with.
//
// Magic-link sign-in tokens are keyed by email ("magiclink:<email>"), and a
// store eraser runs after the SQL erasure has deleted the user row the
// email would be read from, so those stay until they expire (15 minutes by
// default). Redeeming one creates a fresh account; it does not restore the
// erased one.

var memoryEraseMu sync.Mutex

// unregisterMemoryStoreEraser drops the manager's in-memory store eraser,
// if it registered one. AuthManager.OnStop calls it.
func unregisterMemoryStoreEraser(mgr *AuthManager) {
	memoryEraseMu.Lock()
	prev := mgr.unregisterMemoryErase
	mgr.unregisterMemoryErase, mgr.memoryEraseName = nil, ""
	memoryEraseMu.Unlock()
	if prev != nil {
		prev()
	}
}

// registerMemoryStoreEraser (re)registers the manager's in-memory store
// eraser. A later Init replaces the earlier registration. memoryEraseMu
// guards the manager's two fields; the unregister func runs outside it.
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
		}
	}
	if sessions == nil && twofa == nil && passkeys == nil && len(tokens) == 0 {
		return
	}
	name, unregister := datexport.RegisterStoreEraser(datexport.StoreEraser{
		Name:   "auth_memory",
		Source: "auth",
		Erase: func(_ context.Context, userID string, dryRun bool) (int, error) {
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
				n += ts.eraseUser(userID, dryRun)
			}
			return n, nil
		},
	})
	memoryEraseMu.Lock()
	mgr.memoryEraseName, mgr.unregisterMemoryErase = name, unregister
	memoryEraseMu.Unlock()
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

// eraseUser removes the tokens whose payload names userID: password reset,
// email verification and passkey registration.
func (m *MemoryMagicLinkTokenStore) eraseUser(userID string, dryRun bool) int {
	payloads := []string{
		string(purposeReset) + ":" + userID,
		string(purposeVerify) + ":" + userID,
		string(purposePasskeyRegister) + ":" + userID,
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
