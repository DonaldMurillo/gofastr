package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/embed"
)

// TokensPlugin is the AuthPlugin + AuthPluginRoutes implementation that
// exposes self-service API-token management to logged-in users:
//
//	POST   {base}/tokens       : create a token for the caller (plaintext shown once)
//	GET    {base}/tokens       : list the caller's tokens (prefix only, no plaintext)
//	DELETE {base}/tokens/{id}  : revoke one of the caller's tokens
//
// Every endpoint resolves the owner from the authenticated session user,
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

// ReservedEmbedPrefixes reports the prefix this plugin's routes sit under:
// the manager's BasePath. These routes mint permanent API tokens, so no embed
// surface may sit over them or reach them. It used to return the token
// brand set by WithPrefix ("acme_"), which is not a path and which the embed
// host dropped. The framework reads the battery, so the AuthManager reports
// the same prefix; see framework.EmbedReserving.
func (p *TokensPlugin) ReservedEmbedPrefixes() []string {
	if p.mgr == nil {
		return []string{"/auth"}
	}
	return p.mgr.ReservedEmbedPrefixes()
}

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
// empty-scoped) token could POST here to mint a `*:*` token for its owner,
// escaping its own scope leash, and list/revoke the owner's other tokens.
// Token scopes in ctx are the discriminator (only TokenMiddleware sets them).
func (p *TokensPlugin) requireSessionUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	u := GetCurrentUser(r.Context())
	if u == nil {
		writeAuthError(w, r, http.StatusUnauthorized, handler.ReasonAuthenticationRequired)
		return "", false
	}
	if _, tokenAuth := TokenScopes(r.Context()); tokenAuth {
		writeAuthError(w, r, http.StatusUnauthorized, reasonTokensNeedSession)
		return "", false
	}
	// An embed grant is the third credential with that property, and the worst
	// of the three to accept here. It resolves to the same ctx user a session
	// does, but it lives in a third party's page where any script, or anyone
	// with devtools, can read it, and it is deliberately scoped and
	// time-bounded. Minting from it converts a 15-minute, one-surface,
	// one-origin credential into a permanent `*:*` API token that outlives the
	// grant's deadline, ignores its scopes and is invisible to the embed system.
	if _, embedded := embed.GrantFromContext(r.Context()); embedded {
		writeAuthError(w, r, http.StatusUnauthorized, reasonTokensNeedSessionEmbed)
		return "", false
	}
	// A token is a permanent way in, so on an account with a second factor
	// only a session that passed it may manage tokens, the bar passkey add
	// and password change already hold. A session that predates enrollment
	// (a stolen cookie the owner never knew about) is never stepped up, and
	// a bearer JWT carries no step-up at all: either minted a permanent
	// `*:*` token that outlived the session and ignored the new factor.
	if p.mgr != nil {
		on, err := p.mgr.twoFactorEnabled(r.Context(), u.GetID())
		if err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reason2faLookupFailed)
			return "", false
		}
		if on {
			// Any of the principal's own session cookies that passed the
			// factor will do, as RequireTwoFA judges it: judging only the
			// first refused a browser whose stale pre-enrollment cookie
			// (a parent-domain scope) came before its verified one.
			mine, verified := false, false
			for _, sess := range p.mgr.requestSessions(r) {
				if sess.UserID != u.GetID() {
					continue
				}
				mine = true
				if !sess.PendingTwoFactor && sess.TwoFactorVerified {
					verified = true
					break
				}
			}
			if !mine {
				writeAuthError(w, r, http.StatusUnauthorized, reasonTokensNeedSession)
				return "", false
			}
			if !verified {
				writeAuthError(w, r, http.StatusForbidden, reason2faRequired)
				return "", false
			}
		}
	}
	return u.GetID(), true
}

// sessionStillHolds re-runs the request's principal check (the session
// middleware's re-resolve of its cookie). A request with no check
// installed passes, as handler.RecheckPrincipal does.
func sessionStillHolds(ctx context.Context) bool {
	_, ok := handler.RecheckPrincipal(ctx)
	return ok
}

// createTokenRequest is the POST /tokens body. OwnerKind/OwnerID are decoded
// intentionally so we can prove they are IGNORED, the owner is always the
// session user. They never reach IssueToken.
type createTokenRequest struct {
	Name       string   `json:"name"`
	Scopes     []string `json:"scopes"`
	TTLSeconds int64    `json:"ttl_seconds"`
	OwnerKind  string   `json:"owner_kind"` // ignored: forced to "user"
	OwnerID    string   `json:"owner_id"`   // ignored: forced to session user
}

// maxTokenTTLSeconds is the largest ttl_seconds whose Duration does not
// wrap an int64 (about 292 years).
const maxTokenTTLSeconds = int64(math.MaxInt64 / int64(time.Second))

func (p *TokensPlugin) createTokenHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := p.requireSessionUserID(w, r)
		if !ok {
			return
		}
		// Taken before the body is read: a request held open with its body
		// withheld until a reset finished inserted its token after the
		// reset's one listing of the user's tokens, and kept it.
		snap, err := p.mgr.snapshotCredential(r.Context(), userID)
		if err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonUserLookupFailed)
			return
		}
		var body createTokenRequest
		if !decodeJSONLimited(w, r, &body) {
			return
		}
		// Seconds become a Duration by multiplying by 1e9, which wraps an
		// int64: -2^55 wrapped to 0 (a permanent token past the negative
		// refusal) and 584 years wrapped to 290ms. Refuse both before the
		// multiply.
		if body.TTLSeconds < 0 || body.TTLSeconds > maxTokenTTLSeconds {
			writeAuthError(w, r, http.StatusBadRequest, reasonTokenTTLOutOfRange,
				map[string]any{"ttl": body.TTLSeconds, "max": maxTokenTTLSeconds})
			return
		}
		// Owner is ALWAYS the session user, body owner_kind/owner_id are
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
			// A rejected spec is the caller's problem and its message is
			// written for them. Anything else came from the store, where
			// the text carries DSNs and driver internals — and answering
			// a persistence fault with 400 tells the caller to fix a body
			// that was fine.
			if errors.Is(err, ErrInvalidTokenSpec) {
				writeAuthErrorFor(w, r, http.StatusBadRequest, err, i18n.StatusPhrase(http.StatusBadRequest))
				return
			}
			log.Printf("auth: issue token: %v", err)
			writeAuthError(w, r, http.StatusInternalServerError, reasonCouldNotCreateToken)
			return
		}
		// The snapshot catches a reset that lands after it. One that
		// finished between the session check and the snapshot left the
		// snapshot reading the new hash, so the session this request came
		// in on is re-checked too: the reset deleted it.
		if snap.moved(r.Context(), p.mgr) || !sessionStillHolds(r.Context()) {
			undo, cancel := undoContext(r.Context())
			err := p.tokens.Revoke(undo, rec.ID, OwnerKindUser, userID)
			cancel()
			if err != nil {
				log.Printf("auth: revoking a token minted across a credential change: %v", err)
				writeAuthError(w, r, http.StatusInternalServerError, reasonCouldNotCreateToken)
				return
			}
			writeAuthError(w, r, http.StatusUnauthorized, reasonInvalidSession)
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
		writeCredentialHeaders(w)
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
			writeAuthError(w, r, http.StatusInternalServerError, reasonListTokensFailed)
			return
		}
		out := make([]map[string]any, 0, len(toks))
		for _, t := range toks {
			out = append(out, map[string]any{
				"id":         t.ID,
				"name":       t.Name,
				"prefix":     t.Prefix, // display prefix only, never the plaintext
				"scopes":     t.Scopes,
				"expiresAt":  t.ExpiresAt,
				"lastUsedAt": t.LastUsedAt,
				"revokedAt":  t.RevokedAt,
				"createdAt":  t.CreatedAt,
			})
		}
		// Per-caller token inventory (names, prefixes, scopes, last-used):
		// replayable recon from a shared machine's cache, so the body is
		// uncacheable, the same posture /auth/me pins.
		writeCredentialHeaders(w)
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
			writeAuthError(w, r, http.StatusBadRequest, reasonTokenIdRequired)
			return
		}
		// Revoke is owner-scoped: a foreign id returns ErrTokenNotFound → 404.
		// The same id revoking twice is a no-op success (idempotent).
		if err := p.tokens.Revoke(r.Context(), id, OwnerKindUser, userID); err != nil {
			if errors.Is(err, ErrTokenNotFound) {
				writeAuthError(w, r, http.StatusNotFound, reasonTokenNotFound)
				return
			}
			writeAuthError(w, r, http.StatusInternalServerError, reasonRevokeFailed)
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
