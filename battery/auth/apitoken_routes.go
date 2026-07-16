package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// TokensPlugin is the AuthPlugin + AuthPluginRoutes implementation that
// exposes self-service API-token management to logged-in users:
//
//	POST   {base}/tokens       — create a token for the caller (plaintext shown once)
//	GET    {base}/tokens       — list the caller's tokens (prefix only, no plaintext)
//	DELETE {base}/tokens/{id}  — revoke one of the caller's tokens
//
// Every endpoint resolves the owner from the authenticated session user —
// OwnerKind is forced to "user" and OwnerID to the current user's ID, so a
// caller can never mint for or revoke another user's token. Service-account
// management is programmatic-only in v1 (no HTTP surface).
type TokensPlugin struct {
	mgr    *AuthManager
	tokens APITokenStore
	prefix string
}

// NewTokensPlugin constructs the plugin. The token store is supplied here;
// the manager arrives via Init (so emitSecurity is available after wiring).
func NewTokensPlugin(tokens APITokenStore) *TokensPlugin {
	return &TokensPlugin{tokens: tokens}
}

// WithPrefix brands tokens this plugin mints (default TokenPrefix, "gfsk_")
// so a host's credentials are greppable as ITS credentials. Pair with
// TokenMiddleware's WithTokenPrefix so the branded tokens authenticate.
func (p *TokensPlugin) WithPrefix(prefix string) *TokensPlugin {
	if !tokenPrefixPattern.MatchString(prefix) {
		panic("auth: TokensPlugin.WithPrefix: invalid prefix " + prefix)
	}
	p.prefix = prefix
	return p
}

func (p *TokensPlugin) Name() string { return "api-tokens" }

func (p *TokensPlugin) Init(mgr *AuthManager) error {
	p.mgr = mgr
	return nil
}

func (p *TokensPlugin) RegisterRoutes(r *router.Router, basePath string) {
	r.Post(basePath+"/tokens", p.createTokenHandler())
	r.Get(basePath+"/tokens", p.listTokensHandler())
	r.Delete(basePath+"/tokens/{id}", p.revokeTokenHandler())
}

// requireSessionUserID resolves the caller from the request context (set by
// SessionMiddleware). It answers 401 when no user is present OR when the
// request is authenticated by an API token rather than an interactive
// session. The session-only gate is load-bearing: TokenMiddleware sets the
// same ctx user a session does, so without it a leaked scoped (or even
// empty-scoped) token could POST here to mint a `*:*` token for its owner —
// escaping its own scope leash — and list/revoke the owner's other tokens.
// Token scopes in ctx are the discriminator (only TokenMiddleware sets them).
func (p *TokensPlugin) requireSessionUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	u := GetCurrentUser(r.Context())
	if u == nil {
		writeAuthError(w, http.StatusUnauthorized, "authentication required")
		return "", false
	}
	if _, tokenAuth := TokenScopes(r.Context()); tokenAuth {
		writeAuthError(w, http.StatusUnauthorized, "token management requires an interactive session, not an API token")
		return "", false
	}
	return u.GetID(), true
}

// createTokenRequest is the POST /tokens body. OwnerKind/OwnerID are decoded
// intentionally so we can prove they are IGNORED — the owner is always the
// session user. They never reach IssueToken.
type createTokenRequest struct {
	Name       string   `json:"name"`
	Scopes     []string `json:"scopes"`
	TTLSeconds int64    `json:"ttl_seconds"`
	OwnerKind  string   `json:"owner_kind"` // ignored — forced to "user"
	OwnerID    string   `json:"owner_id"`   // ignored — forced to session user
}

func (p *TokensPlugin) createTokenHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := p.requireSessionUserID(w, r)
		if !ok {
			return
		}
		var body createTokenRequest
		if !decodeJSONLimited(w, r, &body) {
			return
		}
		// Owner is ALWAYS the session user — body owner_kind/owner_id are
		// discarded by construction here.
		plaintext, rec, err := IssueToken(r.Context(), p.tokens, TokenSpec{
			Name:      body.Name,
			OwnerKind: OwnerKindUser,
			OwnerID:   userID,
			Scopes:    body.Scopes,
			TTL:       time.Duration(body.TTLSeconds) * time.Second,
			Prefix:    p.prefix,
		})
		if err != nil {
			writeAuthError(w, http.StatusBadRequest, err.Error())
			return
		}
		p.mgr.emitSecurity(r.Context(), SecurityEvent{
			Kind:   "token.created",
			UserID: userID,
			Remote: remoteHost(r),
			Meta: map[string]string{
				"token": rec.Prefix,
				"name":  rec.Name,
			},
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":     plaintext, // shown exactly ONCE; never retrievable again
			"id":        rec.ID,
			"name":      rec.Name,
			"prefix":    rec.Prefix,
			"scopes":    rec.Scopes,
			"expiresAt": rec.ExpiresAt,
			"createdAt": rec.CreatedAt,
		})
	}
}

func (p *TokensPlugin) listTokensHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := p.requireSessionUserID(w, r)
		if !ok {
			return
		}
		toks, err := p.tokens.List(r.Context(), OwnerKindUser, userID)
		if err != nil {
			writeAuthError(w, http.StatusInternalServerError, "list tokens failed")
			return
		}
		out := make([]map[string]any, 0, len(toks))
		for _, t := range toks {
			out = append(out, map[string]any{
				"id":         t.ID,
				"name":       t.Name,
				"prefix":     t.Prefix, // display prefix only — never the plaintext
				"scopes":     t.Scopes,
				"expiresAt":  t.ExpiresAt,
				"lastUsedAt": t.LastUsedAt,
				"revokedAt":  t.RevokedAt,
				"createdAt":  t.CreatedAt,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"tokens": out})
	}
}

func (p *TokensPlugin) revokeTokenHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := p.requireSessionUserID(w, r)
		if !ok {
			return
		}
		id := router.Param(r, "id")
		if id == "" {
			writeAuthError(w, http.StatusBadRequest, "token id required")
			return
		}
		// Revoke is owner-scoped: a foreign id returns ErrTokenNotFound → 404.
		// The same id revoking twice is a no-op success (idempotent).
		if err := p.tokens.Revoke(r.Context(), id, OwnerKindUser, userID); err != nil {
			if errors.Is(err, ErrTokenNotFound) {
				writeAuthError(w, http.StatusNotFound, "token not found")
				return
			}
			writeAuthError(w, http.StatusInternalServerError, "revoke failed")
			return
		}
		p.mgr.emitSecurity(r.Context(), SecurityEvent{
			Kind:   "token.revoked",
			UserID: userID,
			Remote: remoteHost(r),
			Meta:   map[string]string{"token_id": id},
		})
		w.WriteHeader(http.StatusNoContent)
	}
}

// Compile-time interface checks.
var (
	_ AuthPlugin       = (*TokensPlugin)(nil)
	_ AuthPluginRoutes = (*TokensPlugin)(nil)
)
