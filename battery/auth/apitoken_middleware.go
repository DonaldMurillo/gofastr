package auth

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/middleware"
)

// TokenMiddlewareOption tunes TokenMiddleware.
type TokenMiddlewareOption func(*tokenMiddlewareOpts)

type tokenMiddlewareOpts struct {
	sink   AuditSink
	logger *slog.Logger
	prefix string
}

// WithTokenPrefix sets the plaintext marker this middleware recognizes
// (default TokenPrefix, "gfsk_"). Pair it with the same TokenSpec.Prefix at
// issue time — hosts that brand their credentials must brand both sides or
// every authenticated request passes through unrecognized.
func WithTokenPrefix(prefix string) TokenMiddlewareOption {
	if !tokenPrefixPattern.MatchString(prefix) {
		panic("auth.WithTokenPrefix: invalid prefix " + prefix + " (want lowercase alnum starting with a letter, trailing underscore)")
	}
	return func(o *tokenMiddlewareOpts) { o.prefix = prefix }
}

// WithTokenAudit routes token auth events to sink. TokenMiddleware has no
// AuthManager, so it cannot use mgr.emitSecurity; this option is the seam.
// A nil sink (the default) disables auditing entirely and never panics.
func WithTokenAudit(sink AuditSink) TokenMiddlewareOption {
	return func(o *tokenMiddlewareOpts) { o.sink = sink }
}

// WithTokenLogger overrides the logger TokenMiddleware uses for store-error
// observability. When unset, slog.Default() is used; pass nil to silence.
func WithTokenLogger(l *slog.Logger) TokenMiddlewareOption {
	return func(o *tokenMiddlewareOpts) { o.logger = l }
}

// lastUsedTouchInterval is the minimum gap between two last_used_at stamps
// for the same token. The middleware writes at most one touch per token
// per this window, keeping the hot request path off the write side.
const lastUsedTouchInterval = 60 * time.Second

// TokenMiddleware authenticates `Authorization: Bearer gfsk_…` requests.
//
// Non-gfsk_ bearer tokens and requests without the header pass through
// UNTOUCHED (the session/JWT middleware handle those) — it does NOT clear
// an existing ctx user in that case. A gfsk_-prefixed credential that fails
// validation proceeds ANONYMOUSLY with the ctx user CLEARED (mirroring
// SessionMiddleware's anon semantics), never falling back to a prior
// identity.
//
// Validation order:
//  1. Extract bearer; if it doesn't start with gfsk_, pass through.
//  2. hash := sha256(credential); FindByHash (uniform timing — no string
//     comparison against stored plaintext anywhere).
//  3. Unknown hash / RevokedAt set / ExpiresAt passed → anonymous (cleared)
//     + audit token.auth_failed (reason ∈ unknown|revoked|expired; token
//     PREFIX only, never the credential).
//  4. Resolve owner (user → UserStore, service → ServiceAccountStore);
//     missing owner or disabled service account → anonymous + audit
//     (reason ∈ owner_missing|owner_disabled).
//  5. Success: handler.SetUser(ctx, principal), WithTokenScopes(ctx, scopes),
//     throttled best-effort TouchLastUsed.
func TokenMiddleware(users UserStore, accounts ServiceAccountStore, tokens APITokenStore, opts ...TokenMiddlewareOption) middleware.Middleware {
	o := tokenMiddlewareOpts{logger: slog.Default(), prefix: TokenPrefix}
	for _, fn := range opts {
		fn(&o)
	}
	log := o.logger
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			cred := extractBearerToken(r)

			// (1) A credential that doesn't carry our token prefix is none of
			// our business — leave ctx exactly as the outer middleware left it.
			if !strings.HasPrefix(cred, o.prefix) {
				next.ServeHTTP(w, r)
				return
			}

			// failClosed clears any prior identity and audits. Every gfsk_
			// failure funnels through here so the "never carry an outer
			// identity past a failed gfsk_ credential" invariant has one
			// enforcement point. The credential only reaches tokenDisplayPrefix,
			// which truncates to the safe 12-char prefix.
			failClosed := func(reason string) {
				emitTokenEvent(o.sink, ctx, r, "token.auth_failed", reason, cred)
				next.ServeHTTP(w, r.WithContext(handler.SetUser(ctx, nil)))
			}

			// (2) Hash-then-lookup.
			t, err := tokens.FindByHash(ctx, sha256hex(cred))
			if err != nil {
				// Store outage degrades to anonymous but isn't one of the
				// closed-vocabulary fail reasons, so it clears the user and
				// logs rather than emitting a fabricated reason.
				if log != nil {
					log.Warn("api-token: store lookup failed — request degraded to anonymous", "err", err.Error())
				}
				next.ServeHTTP(w, r.WithContext(handler.SetUser(ctx, nil)))
				return
			}
			if t == nil {
				failClosed("unknown")
				return
			}
			// (3) State checks.
			if t.RevokedAt != nil {
				failClosed("revoked")
				return
			}
			if t.ExpiresAt != nil && !t.ExpiresAt.After(time.Now()) {
				failClosed("expired")
				return
			}

			// (4) Resolve owner principal. A non-empty failReason means the
			// owner couldn't be established (missing or disabled) — audit it
			// with the precise reason and proceed anonymous+cleared.
			principal, failReason := resolveTokenOwner(ctx, t, users, accounts, log)
			if failReason != "" {
				emitTokenEvent(o.sink, ctx, r, "token.auth_failed", failReason, cred)
				next.ServeHTTP(w, r.WithContext(handler.SetUser(ctx, nil)))
				return
			}

			// (5) Success. Alongside the owner and scopes, expose the token's
			// own ID — per-token metering/quotas/audit need to attribute the
			// request to the SPECIFIC credential, not just its owner.
			newCtx := handler.SetUser(ctx, principal)
			newCtx = WithTokenScopes(newCtx, t.Scopes)
			newCtx = WithTokenID(newCtx, t.ID)
			if shouldTouchLastUsed(t.LastUsedAt) {
				// Synchronous + best-effort: the write is fast (indexed PK
				// UPDATE) and happens at most once per token per 60s. Doing
				// it synchronously (rather than fire-and-forget) makes the
				// throttle EFFECTIVE for rapid requests — the next lookup
				// sees the fresh last_used_at and skips. The error is
				// ignored: last_used is observability, never auth state.
				_ = tokens.TouchLastUsed(r.Context(), t.ID, time.Now().UTC())
			}
			next.ServeHTTP(w, r.WithContext(newCtx))
		})
	}
}

// resolveTokenOwner loads the principal for a valid token. It returns
// (principal, "") on success. On failure it returns (nil, reason) where
// reason is the closed-vocabulary token.auth_failed code
// (owner_missing|owner_disabled) the caller emits — keeping the reason
// taxonomy in one place. A transport error during lookup is treated as
// owner_missing (fail closed) and logged at debug.
func resolveTokenOwner(ctx context.Context, t *APIToken, users UserStore, accounts ServiceAccountStore, log *slog.Logger) (User, string) {
	switch t.OwnerKind {
	case OwnerKindUser:
		if users == nil {
			return nil, "owner_missing"
		}
		u, err := users.FindByID(ctx, t.OwnerID)
		if err != nil {
			if log != nil {
				log.Debug("api-token: owner user lookup failed", "err", err.Error())
			}
			return nil, "owner_missing"
		}
		if u == nil {
			return nil, "owner_missing"
		}
		return u, ""
	case OwnerKindService:
		if accounts == nil {
			return nil, "owner_missing"
		}
		sa, err := accounts.Get(ctx, t.OwnerID)
		if err != nil {
			if log != nil {
				log.Debug("api-token: service account lookup failed", "err", err.Error())
			}
			return nil, "owner_missing"
		}
		if sa == nil {
			return nil, "owner_missing"
		}
		if sa.Disabled {
			return nil, "owner_disabled"
		}
		return &serviceAccountUser{sa: sa}, ""
	default:
		return nil, "owner_missing"
	}
}

// shouldTouchLastUsed reports whether the middleware should stamp
// last_used_at: never set, or older than the touch interval.
func shouldTouchLastUsed(last *time.Time) bool {
	if last == nil {
		return true
	}
	return time.Since(*last) > lastUsedTouchInterval
}

// (The last-used touch is performed synchronously in the success path
// above; there is no async helper.)

// emitTokenEvent forwards a token security event to sink (no-op when sink
// is nil). The credential reaches only tokenDisplayPrefix (12 chars), so
// no plaintext is ever placed in the audit trail.
func emitTokenEvent(sink AuditSink, ctx context.Context, r *http.Request, kind, reason, cred string) {
	if sink == nil {
		return
	}
	sink.SecurityEvent(ctx, SecurityEvent{
		Kind:   kind,
		Remote: remoteHost(r),
		Meta: map[string]string{
			"reason": reason,
			"token":  tokenDisplayPrefix(cred),
		},
	})
}

// RequireAPIScopes returns middleware that scope-gates a whole auto-CRUD
// API tree for token-authenticated callers, so a token minted with
// ["customers:*"] really is limited to customers. The required scope is
// derived from the route: the first path segment after apiPrefix is the
// resource (the entity table), and the HTTP method maps to the verb —
// GET/HEAD need "<resource>:read", everything else "<resource>:write".
// Subroutes (/{id}, /_batch, /_events, /llm.md) inherit the resource.
//
// Session/JWT callers (no token on the request) pass through unscoped, as
// do paths outside apiPrefix — this only *narrows* what a token may do,
// mirroring HasScope semantics. Mount once, after TokenMiddleware:
//
//	app.Use(auth.TokenMiddleware(users, accounts, tokens))
//	app.Use(auth.RequireAPIScopes("/api"))
//
// Per-route RequireScope remains the tool for custom endpoints and
// non-CRUD scopes.
func RequireAPIScopes(apiPrefix string) middleware.Middleware {
	prefix := "/" + strings.Trim(apiPrefix, "/")
	if prefix == "/" {
		prefix = ""
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			held, tokenAuthed := TokenScopes(r.Context())
			if !tokenAuthed {
				next.ServeHTTP(w, r)
				return
			}
			rest, ok := strings.CutPrefix(r.URL.Path, prefix+"/")
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			resource, _, _ := strings.Cut(rest, "/")
			if resource == "" {
				next.ServeHTTP(w, r)
				return
			}
			verb := "write"
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				verb = "read"
			}
			if !scopeMatches(held, resource+":"+verb) {
				writeAuthError(w, http.StatusForbidden, "insufficient token scope")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireScope returns middleware that 403s token-authenticated requests
// lacking the scope. Non-token requests (sessions/JWT) pass through
// unscoped — sessions carry full user capability; scopes are an additional
// token-level restriction only.
func RequireScope(scope string) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if HasScope(r.Context(), scope) {
				next.ServeHTTP(w, r)
				return
			}
			writeAuthError(w, http.StatusForbidden, "insufficient token scope")
		})
	}
}
