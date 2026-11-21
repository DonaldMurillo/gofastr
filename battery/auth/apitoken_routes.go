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
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/embed"
)

// TokensPlugin serves self-service API-token create, list and revoke for a signed-in session.
type TokensPlugin struct {
	mgr    *AuthManager
	tokens APITokenStore
	prefix string
}

// NewTokensPlugin builds the plugin over tokens.
func NewTokensPlugin(tokens APITokenStore) *TokensPlugin {
	return &TokensPlugin{tokens: tokens}
}

// WithPrefix brands the tokens this plugin mints; pair it with WithTokenPrefix.
func (p *TokensPlugin) WithPrefix(prefix string) *TokensPlugin {
	if !tokenPrefixPattern.MatchString(prefix) {
		panic("auth: TokensPlugin.WithPrefix: invalid prefix " + prefix)
	}
	p.prefix = prefix
	return p
}

func (p *TokensPlugin) Name() string { return "api-tokens" }

// ReservedEmbedPrefixes reports the prefix this plugin's routes sit under:
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
	r = r.Group("", browserAccountOnly)
	r.Post(basePath+"/tokens", p.createTokenHandler())
	r.Get(basePath+"/tokens", p.listTokensHandler())
	r.Delete(basePath+"/tokens/{id}", p.revokeTokenHandler())
}

func (p *TokensPlugin) requireSessionUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	if rejectInProcessAccountOperation(w, r) {
		return "", false
	}
	u := GetCurrentUser(r.Context())
	if u == nil {
		writeAuthError(w, r, http.StatusUnauthorized, handler.ReasonAuthenticationRequired)
		return "", false
	}
	if _, tokenAuth := TokenScopes(r.Context()); tokenAuth {
		writeAuthError(w, r, http.StatusUnauthorized, reasonTokensNeedSession)
		return "", false
	}
	if _, embedded := embed.GrantFromContext(r.Context()); embedded {
		writeAuthError(w, r, http.StatusUnauthorized, reasonTokensNeedSessionEmbed)
		return "", false
	}
	if p.mgr != nil {
		on, err := p.mgr.twoFactorEnabled(r.Context(), u.GetID())
		if err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reason2faLookupFailed)
			return "", false
		}
		if on {
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

func sessionStillHolds(ctx context.Context) bool {
	_, ok := handler.RecheckPrincipal(ctx)
	return ok
}

type createTokenRequest struct {
	Name       string   `json:"name"`
	Scopes     []string `json:"scopes"`
	TTLSeconds int64    `json:"ttl_seconds"`
	OwnerKind  string   `json:"owner_kind"` // ignored: forced to "user"
	OwnerID    string   `json:"owner_id"`   // ignored: forced to session user
}

const maxTokenTTLSeconds = int64(math.MaxInt64 / int64(time.Second))

func (p *TokensPlugin) createTokenHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := p.requireSessionUserID(w, r)
		if !ok {
			return
		}
		snap, err := p.mgr.snapshotCredential(r.Context(), userID)
		if err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonUserLookupFailed)
			return
		}
		var body createTokenRequest
		if !decodeJSONLimited(w, r, &body) {
			return
		}
		if body.TTLSeconds < 0 || body.TTLSeconds > maxTokenTTLSeconds {
			writeAuthError(w, r, http.StatusBadRequest, reasonTokenTTLOutOfRange,
				map[string]any{"ttl": body.TTLSeconds, "max": maxTokenTTLSeconds})
			return
		}
		plaintext, rec, err := IssueToken(r.Context(), p.tokens, TokenSpec{
			Name:      body.Name,
			OwnerKind: OwnerKindUser,
			OwnerID:   userID,
			Scopes:    body.Scopes,
			TTL:       time.Duration(body.TTLSeconds) * time.Second,
			Prefix:    p.prefix,
		})
		if err != nil {
			if errors.Is(err, ErrInvalidTokenSpec) {
				writeAuthErrorFor(w, r, http.StatusBadRequest, err, i18n.StatusPhrase(http.StatusBadRequest))
				return
			}
			log.Printf("auth: issue token: %v", err)
			writeAuthError(w, r, http.StatusInternalServerError, reasonCouldNotCreateToken)
			return
		}
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
		writeAuthJSON(w, r, http.StatusOK, map[string]any{"tokens": out})
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
		if !textsafe.Storable(id) {
			writeAuthError(w, r, http.StatusNotFound, reasonTokenNotFound)
			return
		}
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

var (
	_ AuthPlugin       = (*TokensPlugin)(nil)
	_ AuthPluginRoutes = (*TokensPlugin)(nil)
)
