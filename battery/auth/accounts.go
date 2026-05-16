package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// Account represents one linked OAuth provider for a user. The optional
// profile fields are populated when the store implements
// OAuthEnrichedLinker — useful for rendering "you're connected to
// Google as alice@example.com (Alice, 📸)" in a settings UI. Stores
// that only implement OAuthLinker continue to return the minimum
// (Provider + ProviderID), the rest stay empty.
type Account struct {
	Provider   string     `json:"provider"`
	ProviderID string     `json:"providerId"`
	Email      string     `json:"email,omitempty"`
	Name       string     `json:"name,omitempty"`
	AvatarURL  string     `json:"avatarUrl,omitempty"`
	LinkedAt   *time.Time `json:"linkedAt,omitempty"`
}

// OAuthAccountProfile is the optional profile payload an
// OAuthEnrichedLinker receives at link time. Mirrors the subset of
// OAuth2UserInfo that's worth surfacing in a UI later.
type OAuthAccountProfile struct {
	Email     string
	Name      string
	AvatarURL string
}

// OAuthEnrichedLinker is the optional UserStore extension that accepts
// profile metadata at link time so AccountLister can return it later.
// Stores that implement this interface get the OAuth handler's full
// FetchUserInfo payload; stores that only implement OAuthLinker get
// just (provider, providerID) and the rest is dropped.
type OAuthEnrichedLinker interface {
	LinkOAuthEnriched(ctx context.Context, userID, provider, providerID string, profile OAuthAccountProfile) error
}

// AccountLister is the optional UserStore extension that returns the
// linked OAuth accounts for a user. Required by AccountsPlugin.
type AccountLister interface {
	ListAccounts(ctx context.Context, userID string) ([]Account, error)
}

// AccountUnlinker is the optional UserStore extension that removes a
// link. Required by AccountsPlugin's unlink endpoint.
type AccountUnlinker interface {
	UnlinkOAuth(ctx context.Context, userID, provider string) error
}

// PasswordChecker is the optional UserStore extension that reports whether
// a user has a real password set (vs the placeholder hash used by OAuth /
// magic-link auto-created accounts). AccountsPlugin.unlinkHandler uses it
// to refuse an unlink that would leave the user with no way to log in.
// Stores that do not implement it fall back to the conservative
// links-only check.
type PasswordChecker interface {
	HasPassword(ctx context.Context, userID string) (bool, error)
}

// OAuthUserCreator is the optional UserStore extension that creates a
// user known to have NO password (OAuth or magic-link auto-create). The
// store is responsible for recording that fact so a subsequent
// HasPassword call returns false. Stores that don't implement it get the
// legacy "CreateUser with placeholder hash" path, which is functional
// but cannot distinguish password-set from placeholder-only users.
type OAuthUserCreator interface {
	CreateUserNoPassword(ctx context.Context, email string, roles []string) (User, error)
}

// ─── AccountsPlugin ─────────────────────────────────────────────────────────

// AccountsPlugin provides:
//   - GET    /auth/accounts          → list linked OAuth providers.
//   - DELETE /auth/unlink/{provider} → remove a link, refusing if it
//     would leave the user without any way to log in (last credential).
type AccountsPlugin struct {
	mgr *AuthManager
}

// NewAccountsPlugin constructs the plugin.
func NewAccountsPlugin() *AccountsPlugin { return &AccountsPlugin{} }

func (p *AccountsPlugin) Name() string { return "accounts" }

func (p *AccountsPlugin) Init(mgr *AuthManager) error {
	p.mgr = mgr
	return nil
}

func (p *AccountsPlugin) RegisterRoutes(r *router.Router, basePath string) {
	r.Get(basePath+"/accounts", http.HandlerFunc(p.listHandler))
	r.Delete(basePath+"/unlink/{provider}", http.HandlerFunc(p.unlinkHandler))
}

func (p *AccountsPlugin) requireUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	cfg := p.mgr.Config()
	cookie, err := r.Cookie(cfg.SessionCookie)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, "no session")
		return "", false
	}
	sess, err := p.mgr.SessionStore().Get(r.Context(), cookie.Value)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, "invalid session")
		return "", false
	}
	if sess.PendingTwoFactor {
		writeAuthError(w, http.StatusForbidden, "two-factor verification required")
		return "", false
	}
	return sess.UserID, true
}

func (p *AccountsPlugin) listHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := p.requireUserID(w, r)
	if !ok {
		return
	}
	lister, ok := p.mgr.UserStore().(AccountLister)
	if !ok {
		writeAuthError(w, http.StatusInternalServerError,
			"user store does not implement AccountLister")
		return
	}
	accts, err := lister.ListAccounts(r.Context(), userID)
	if err != nil {
		writeAuthError(w, http.StatusInternalServerError, "list accounts failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"accounts": accts})
}

func (p *AccountsPlugin) unlinkHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := p.requireUserID(w, r)
	if !ok {
		return
	}
	provider := router.Param(r, "provider")
	if provider == "" {
		writeAuthError(w, http.StatusBadRequest, "provider required")
		return
	}

	lister, listerOK := p.mgr.UserStore().(AccountLister)
	unlinker, unlinkerOK := p.mgr.UserStore().(AccountUnlinker)
	if !listerOK || !unlinkerOK {
		writeAuthError(w, http.StatusInternalServerError,
			"user store must implement AccountLister and AccountUnlinker")
		return
	}

	// Refuse to remove the user's last login method. Two signals feed
	// this: linked OAuth accounts AND whether the user has set a real
	// password. If the store implements PasswordChecker we use the
	// precise check; otherwise we fall back to the conservative
	// "at least one link must remain" rule.
	accts, err := lister.ListAccounts(r.Context(), userID)
	if err != nil {
		writeAuthError(w, http.StatusInternalServerError, "list accounts failed")
		return
	}
	count := 0
	for _, a := range accts {
		if a.Provider == provider {
			count++
		}
	}
	if count == 0 {
		writeAuthError(w, http.StatusNotFound, "account not linked")
		return
	}
	remainingLinks := len(accts) - count

	hasPassword := false // unknown — treated as false unless store opts in
	if checker, ok := p.mgr.UserStore().(PasswordChecker); ok {
		hp, herr := checker.HasPassword(r.Context(), userID)
		if herr != nil {
			writeAuthError(w, http.StatusInternalServerError, "password check failed")
			return
		}
		hasPassword = hp
	}

	if remainingLinks <= 0 && !hasPassword {
		writeAuthError(w, http.StatusConflict,
			"cannot unlink the last login method — set a password first or link another provider")
		return
	}

	if err := unlinker.UnlinkOAuth(r.Context(), userID, provider); err != nil {
		writeAuthError(w, http.StatusInternalServerError, "unlink failed")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"unlinked": provider})
}
