package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// Account is one OAuth identity linked to a user; the profile fields are set when the store keeps them.
type Account struct {
	Provider   string     `json:"provider"`
	ProviderID string     `json:"providerId"`
	Email      string     `json:"email,omitempty"`
	Name       string     `json:"name,omitempty"`
	AvatarURL  string     `json:"avatarUrl,omitempty"`
	LinkedAt   *time.Time `json:"linkedAt,omitempty"`
}

// OAuthAccountProfile is the profile an OAuthEnrichedLinker stores with a link.
type OAuthAccountProfile struct {
	Email     string
	Name      string
	AvatarURL string
}

// OAuthEnrichedLinker is the UserStore extension that stores a profile with each link.
type OAuthEnrichedLinker interface {
	LinkOAuthEnriched(ctx context.Context, userID, provider, providerID string, profile OAuthAccountProfile) error
}

// OAuthProfileRefresher updates the profile of an existing link to userID and never creates one.
type OAuthProfileRefresher interface {
	RefreshOAuthProfile(ctx context.Context, userID, provider, providerID string, profile OAuthAccountProfile) error
}

// AccountLister returns a user's linked identities; AccountsPlugin requires it.
type AccountLister interface {
	ListAccounts(ctx context.Context, userID string) ([]Account, error)
}

// AccountUnlinker removes a link; AccountsPlugin's unlink requires it.
type AccountUnlinker interface {
	UnlinkOAuth(ctx context.Context, userID, provider string) error
}

// UnlinkOutcome reports what one guarded unlink decided.
type UnlinkOutcome int

const (
	// UnlinkNotLinked: the user has no link for the provider.
	UnlinkNotLinked UnlinkOutcome = iota
	// UnlinkRefusedLast: the link is the user's last login method.
	UnlinkRefusedLast
	// UnlinkRemoved: the link was deleted.
	UnlinkRemoved
)

// AtomicUnlinker applies the last-login-method guard and the delete in one store operation.
type AtomicUnlinker interface {
	// UnlinkOAuthGuarded removes the link unless it is userID's last login method, reporting the outcome.
	UnlinkOAuthGuarded(ctx context.Context, userID, provider string) (UnlinkOutcome, error)
}

// PasswordChecker reports whether a user has a real password, not the placeholder.
type PasswordChecker interface {
	HasPassword(ctx context.Context, userID string) (bool, error)
}

// OAuthUserCreator creates a user recorded as having no password.
type OAuthUserCreator interface {
	CreateUserNoPassword(ctx context.Context, email string, roles []string) (User, error)
}

// AccountsPlugin provides:
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
	r = r.Group("", browserAccountOnly)
	r.Get(basePath+"/accounts", http.HandlerFunc(p.listHandler))
	r.Delete(basePath+"/unlink/{provider}", http.HandlerFunc(p.unlinkHandler))
}

func (p *AccountsPlugin) requireUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	if rejectInProcessAccountOperation(w, r) {
		return "", false
	}
	sess, err := p.mgr.requestSession(r, false)
	if err != nil {
		writeAuthError(w, r, http.StatusUnauthorized, reasonNoSession)
		return "", false
	}
	if sess.PendingTwoFactor {
		writeAuthError(w, r, http.StatusForbidden, reason2faRequired)
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
		writeAuthError(w, r, http.StatusInternalServerError, reasonStoreNoAccountLister)
		return
	}
	accts, err := lister.ListAccounts(r.Context(), userID)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonListAccountsFailed)
		return
	}
	writeCredentialHeaders(w)
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
		writeAuthError(w, r, http.StatusBadRequest, reasonProviderRequired)
		return
	}
	if !textsafe.Storable(provider) {
		writeAuthError(w, r, http.StatusNotFound, reasonAccountNotLinked)
		return
	}

	lister, listerOK := p.mgr.UserStore().(AccountLister)
	unlinker, unlinkerOK := p.mgr.UserStore().(AccountUnlinker)
	if !listerOK || !unlinkerOK {
		writeAuthError(w, r, http.StatusInternalServerError, reasonStoreNoUnlinker)
		return
	}

	if atomicUnlinker, ok := p.mgr.UserStore().(AtomicUnlinker); ok {
		outcome, err := atomicUnlinker.UnlinkOAuthGuarded(r.Context(), userID, provider)
		if err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonUnlinkFailed)
			return
		}
		switch outcome {
		case UnlinkNotLinked:
			p.writeNotLinked(w, r, userID, provider)
			return
		case UnlinkRefusedLast:
			writeAuthError(w, r, http.StatusConflict, reasonLastLoginMethod)
			return
		}
		if err := p.revokeProviderTokens(r.Context(), userID, provider); err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonUnlinkRevokeFailed)
			return
		}
		p.mgr.emitSecurity(r.Context(), SecurityEvent{
			Kind:   "oauth.unlinked",
			UserID: userID,
			Remote: remoteHost(r),
			Meta:   map[string]string{"provider": provider},
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"unlinked": provider})
		return
	}

	accts, err := lister.ListAccounts(r.Context(), userID)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonListAccountsFailed)
		return
	}
	count := 0
	for _, a := range accts {
		if a.Provider == provider {
			count++
		}
	}
	if count == 0 {
		p.writeNotLinked(w, r, userID, provider)
		return
	}
	remainingLinks := len(accts) - count

	hasPassword := false // unknown, treated as false unless store opts in
	if checker, ok := p.mgr.UserStore().(PasswordChecker); ok {
		hp, herr := checker.HasPassword(r.Context(), userID)
		if herr != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonPasswordCheckFailed)
			return
		}
		hasPassword = hp
	}

	if remainingLinks <= 0 && !hasPassword {
		writeAuthError(w, r, http.StatusConflict, reasonLastLoginMethod)
		return
	}

	if err := unlinker.UnlinkOAuth(r.Context(), userID, provider); err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonUnlinkFailed)
		return
	}
	if err := p.revokeProviderTokens(r.Context(), userID, provider); err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonUnlinkRevokeFailed)
		return
	}
	p.mgr.emitSecurity(r.Context(), SecurityEvent{
		Kind:   "oauth.unlinked",
		UserID: userID,
		Remote: remoteHost(r),
		Meta:   map[string]string{"provider": provider},
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"unlinked": provider})
}

func (p *AccountsPlugin) writeNotLinked(w http.ResponseWriter, r *http.Request, userID, provider string) {
	if err := p.revokeProviderTokens(r.Context(), userID, provider); err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonUnlinkRevokeFailed)
		return
	}
	writeAuthError(w, r, http.StatusNotFound, reasonAccountNotLinked)
}

func (p *AccountsPlugin) revokeProviderTokens(ctx context.Context, userID, provider string) error {
	op, err := PluginAs[*OAuth2Plugin](p.mgr, "oauth2")
	if err != nil {
		return nil
	}
	ts := op.TokenStore()
	if ts == nil {
		return nil
	}
	return ts.Delete(ctx, userID, provider)
}
