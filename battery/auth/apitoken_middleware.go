package auth

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// TokenMiddlewareOption tunes TokenMiddleware.
type TokenMiddlewareOption func(*tokenMiddlewareOpts)

type tokenMiddlewareOpts struct {
	sink   AuditSink
	logger *slog.Logger
	prefix string
}

// WithTokenPrefix sets the brand TokenMiddleware recognizes; it must match TokenSpec.Prefix.
func WithTokenPrefix(prefix string) TokenMiddlewareOption {
	if !tokenPrefixPattern.MatchString(prefix) {
		panic("auth.WithTokenPrefix: invalid prefix " + prefix + " (want lowercase alnum starting with a letter, trailing underscore)")
	}
	return func(o *tokenMiddlewareOpts) { o.prefix = prefix }
}

// WithTokenAudit sends token auth events to sink; nil disables them.
func WithTokenAudit(sink AuditSink) TokenMiddlewareOption {
	return func(o *tokenMiddlewareOpts) { o.sink = sink }
}

// WithTokenLogger sets TokenMiddleware's logger; nil silences it.
func WithTokenLogger(l *slog.Logger) TokenMiddlewareOption {
	return func(o *tokenMiddlewareOpts) { o.logger = l }
}

const lastUsedTouchInterval = 60 * time.Second

// TokenMiddleware authenticates `Authorization: Bearer gfsk_…` requests.
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

			if !strings.HasPrefix(cred, o.prefix) {
				next.ServeHTTP(w, r)
				return
			}

			failClosed := func(reason string) {
				emitTokenEvent(o.sink, ctx, r, "token.auth_failed", reason, cred)
				next.ServeHTTP(w, r.WithContext(handler.SetUser(ctx, nil)))
			}

			t, err := tokens.FindByHash(ctx, sha256hex(cred))
			if err != nil {
				if log != nil {
					log.Warn("api-token: store lookup failed: request degraded to anonymous", "err", err.Error())
				}
				next.ServeHTTP(w, r.WithContext(handler.SetUser(ctx, nil)))
				return
			}
			if t == nil {
				failClosed("unknown")
				return
			}
			if t.RevokedAt != nil {
				failClosed("revoked")
				return
			}
			if t.ExpiresAt != nil && !t.ExpiresAt.After(time.Now()) {
				failClosed("expired")
				return
			}

			principal, failReason := resolveTokenOwner(ctx, t, users, accounts, log)
			if failReason != "" {
				emitTokenEvent(o.sink, ctx, r, "token.auth_failed", failReason, cred)
				next.ServeHTTP(w, r.WithContext(handler.SetUser(ctx, nil)))
				return
			}

			newCtx := handler.SetUser(ctx, principal)
			newCtx = WithTokenScopes(newCtx, t.Scopes)
			newCtx = WithTokenID(newCtx, t.ID)
			newCtx = handler.WithPrincipalCheck(newCtx, tokenRecheck(cred, users, accounts, tokens, log))
			if shouldTouchLastUsed(t.LastUsedAt) {
				_ = tokens.TouchLastUsed(r.Context(), t.ID, time.Now().UTC())
			}
			next.ServeHTTP(w, r.WithContext(newCtx))
		})
	}
}

func tokenRecheck(cred string, users UserStore, accounts ServiceAccountStore, tokens APITokenStore, log *slog.Logger) handler.PrincipalCheck {
	hash := sha256hex(cred)
	return func(ctx context.Context) (context.Context, bool) {
		t, err := tokens.FindByHash(ctx, hash)
		if err != nil || t == nil || t.RevokedAt != nil {
			return ctx, false
		}
		if t.ExpiresAt != nil && !t.ExpiresAt.After(time.Now()) {
			return ctx, false
		}
		principal, failReason := resolveTokenOwner(ctx, t, users, accounts, log)
		if failReason != "" {
			return ctx, false
		}
		ctx = handler.SetUser(ctx, principal)
		ctx = WithTokenScopes(ctx, t.Scopes)
		return WithTokenID(ctx, t.ID), true
	}
}

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

func shouldTouchLastUsed(last *time.Time) bool {
	if last == nil {
		return true
	}
	return time.Since(*last) > lastUsedTouchInterval
}

func emitTokenEvent(sink AuditSink, ctx context.Context, r *http.Request, kind, reason, cred string) {
	if sink == nil {
		return
	}
	deliverAudit(ctx, sink, SecurityEvent{
		At:     time.Now().UTC(),
		Kind:   kind,
		Remote: remoteHost(r),
		Meta: map[string]string{
			"reason": reason,
			"token":  tokenDisplayPrefix(cred),
		},
	})
}

// RequireAPIScopes refuses a token caller under apiPrefix lacking <resource>:read for GET and HEAD or <resource>:write otherwise.
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
			verb := "write"
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				verb = "read"
			}
			for _, resource := range apiScopeResources(r, prefix, rest) {
				if !scopeMatches(held, resource+":"+verb) {
					writeAuthError(w, r, http.StatusForbidden, reasonInsufficientTokenScope)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func apiScopeResources(r *http.Request, prefix, rest string) []string {
	if res, ok := router.RouteResource(r.Context()); ok {
		return []string{res}
	}
	if pat := patternPath(r.Pattern); pat != "" {
		if res, ok := patternResource(pat, r.URL.EscapedPath(), prefix); ok {
			if res == "" {
				return nil
			}
			return []string{res}
		}
	}
	var segs []string
	for seg := range strings.SplitSeq(rest, "/") {
		if seg != "" {
			segs = append(segs, seg)
		}
	}
	var out []string
	for i, seg := range segs {
		out = append(out, seg)
		if !isVersionSegment(seg) || i == len(segs)-1 {
			break
		}
	}
	return out
}

func patternPath(pattern string) string {
	if i := strings.IndexByte(pattern, '/'); i >= 0 {
		return pattern[i:]
	}
	return ""
}

func patternResource(pat, escPath, prefix string) (string, bool) {
	psegs := strings.Split(pat, "/")
	segs := strings.Split(escPath, "/")
	start := len(strings.Split(prefix, "/"))
	if prefix == "" {
		start = 1
	}
	if len(psegs) < start || len(segs) <= start {
		return "", false
	}
	literal := func(i int) bool {
		if i >= len(psegs) {
			return false // past the pattern: a trailing "/" or {x...} matched it
		}
		s := psegs[i]
		return s != "" && !strings.HasPrefix(s, "{")
	}
	for i := start; i < len(segs); i++ {
		seg, err := url.PathUnescape(segs[i])
		if err != nil {
			seg = segs[i]
		}
		if literal(i) && isVersionSegment(seg) && literal(i+1) {
			continue
		}
		return seg, true
	}
	return "", true
}

// RequireScope refuses a token caller lacking scope with 403; sessions and JWTs pass.
func RequireScope(scope string) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if HasScope(r.Context(), scope) {
				next.ServeHTTP(w, r)
				return
			}
			writeAuthError(w, r, http.StatusForbidden, reasonInsufficientTokenScope)
		})
	}
}

var versionSegmentRe = regexp.MustCompile(`^v\d+(\.\d+)?$`)

func isVersionSegment(seg string) bool {
	return versionSegmentRe.MatchString(seg)
}
