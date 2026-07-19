package auth

import (
	"context"
	"time"
)

// Linker + PasswordChecker + OAuthUserCreator extensions for the test-only
// memoryUserStore defined in manager_test.go. Production code never sees
// these methods — they exist so OAuth callback tests can use the same
// in-memory store the rest of the suite uses, without each test stubbing
// the linker interface from scratch.
//
// The extensions are deliberately conservative:
//   - HasPassword returns true for users created via CreateUser / seedUser
//     (the existing path) and false for users created via
//     CreateUserNoPassword. That matches EntityUserStore's contract.
//   - Link state is keyed (provider, providerID) → userID so the same
//     pair never binds to two users, mirroring the EntityUserStore PK.
//   - ListAccounts returns profile fields only when LinkOAuthEnriched was
//     used; plain LinkOAuth leaves them empty (parity with production).

// memoryLinkEntry is one row in the in-memory link table. The profile
// fields are populated by LinkOAuthEnriched and left empty by LinkOAuth.
type memoryLinkEntry struct {
	userID   string
	profile  OAuthAccountProfile
	linkedAt time.Time
}

// linksMap returns the (provider, providerID) → entry map, initializing it
// on first use so existing callers (which never touch the linker surface)
// pay no allocation cost. Callers MUST already hold s.mu (every method below
// locks it), so the lazy init is race-free.
func (s *memoryUserStore) linksMap() map[string]memoryLinkEntry {
	if s.links == nil {
		s.links = make(map[string]memoryLinkEntry)
	}
	return s.links
}

// linkKey is the composite (provider, providerID) map key.
func linkKey(provider, providerID string) string { return provider + "\x00" + providerID }

// FindByOAuth implements OAuthLinker. Returns ErrUserNotFound when no link
// exists, mirroring EntityUserStore's contract so resolveOAuthUser can
// treat both stores identically.
func (s *memoryUserStore) FindByOAuth(_ context.Context, provider, providerID string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.linksMap()[linkKey(provider, providerID)]; ok {
		if u, ok := s.byID[e.userID]; ok {
			return u.user, nil
		}
	}
	return nil, ErrUserNotFound
}

// LinkOAuth implements OAuthLinker. Idempotent: a second call for the same
// (provider, providerID) is a no-op (the first binding wins), matching
// EntityUserStore's PK behavior.
func (s *memoryUserStore) LinkOAuth(_ context.Context, userID, provider, providerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.linksMap()
	k := linkKey(provider, providerID)
	if _, exists := m[k]; !exists {
		m[k] = memoryLinkEntry{userID: userID, linkedAt: time.Now()}
	}
	return nil
}

// LinkOAuthEnriched implements OAuthEnrichedLinker. On a new binding it
// stores the profile; on an existing binding it refreshes the profile in
// place (the user_id stays immutable).
func (s *memoryUserStore) LinkOAuthEnriched(_ context.Context, userID, provider, providerID string, profile OAuthAccountProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.linksMap()
	k := linkKey(provider, providerID)
	prev := m[k]
	prev.userID = userID // immutable from this path's perspective; set once
	prev.profile = profile
	prev.linkedAt = time.Now()
	m[k] = prev
	return nil
}

// ListAccounts implements AccountLister. Ordered by provider for stable
// test output, matching EntityUserStore.
func (s *memoryUserStore) ListAccounts(_ context.Context, userID string) ([]Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Account, 0)
	for k, e := range s.linksMap() {
		if e.userID != userID {
			continue
		}
		provider, providerID := splitLinkKey(k)
		a := Account{Provider: provider, ProviderID: providerID,
			Email: e.profile.Email, Name: e.profile.Name, AvatarURL: e.profile.AvatarURL}
		t := e.linkedAt
		a.LinkedAt = &t
		out = append(out, a)
	}
	// Stable order: by provider then providerID. Keep it simple — tests
	// don't depend on the exact order, but a deterministic one makes
	// assertions easier.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Provider < out[i].Provider {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

// UnlinkOAuth implements AccountUnlinker. Deleting an absent link is a
// no-op.
func (s *memoryUserStore) UnlinkOAuth(_ context.Context, userID, provider string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.linksMap()
	for k, e := range m {
		if e.userID == userID {
			if p, _ := splitLinkKey(k); p == provider {
				delete(m, k)
			}
		}
	}
	return nil
}

// HasPassword implements PasswordChecker. Tracks a per-user flag set by
// CreateUser (true) and cleared by CreateUserNoPassword (false). Defaults
// to true for users seeded before this file existed — matches the
// production EntityUserStore contract.
func (s *memoryUserStore) HasPassword(_ context.Context, userID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.byID[userID]; ok {
		// storeEntry is the test shape; passwordSet defaults to true so
		// pre-extension seeded users (the common case in tests) report a
		// real password, matching the EntityUserStore CreateUser contract.
		if e.passwordSet {
			return true, nil
		}
		return false, nil
	}
	return false, ErrUserNotFound
}

// CreateUserNoPassword implements OAuthUserCreator. Marks the new user
// passwordless so HasPassword reports false — the same contract
// EntityUserStore.CreateUserNoPassword upholds.
func (s *memoryUserStore) CreateUserNoPassword(_ context.Context, email string, roles []string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.users[email]; exists {
		return nil, ErrEmailTaken
	}
	s.nextID++
	id := "user-" + itoa(s.nextID)
	user := &BasicUser{ID: id, Email: email, Roles: roles}
	entry := &storeEntry{user: user, hash: passwordPlaceholderHash, passwordSet: false}
	s.users[email] = entry
	s.byID[id] = entry
	return user, nil
}

// splitLinkKey reverses linkKey.
func splitLinkKey(k string) (provider, providerID string) {
	for i := 0; i < len(k); i++ {
		if k[i] == 0 {
			return k[:i], k[i+1:]
		}
	}
	return k, ""
}

// itoa is a tiny strconv.Itoa without the import, used only for
// synthesizing user IDs in the in-memory test store.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
