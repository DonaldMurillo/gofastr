package embed

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"reflect"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

type grantCtxKey struct{}

// GrantFromContext returns the embed grant that authenticated this request.
//
// A screen or handler uses it to read the scopes the app narrowed the embed to:
//
//	if g, ok := embed.GrantFromContext(ctx); ok && g.HasScope("comment") {
//	    return CommentForm()
//	}
//
// ok is false on an ordinary first-party request, which is what a surface
// rendered outside a frame should see.
func GrantFromContext(ctx context.Context) (Grant, bool) {
	g, ok := ctx.Value(grantCtxKey{}).(Grant)
	return g, ok
}

// WithGrant installs a verified grant on the context. Exported for the UI host,
// which verifies grants on its own routes before rendering.
func WithGrant(ctx context.Context, g Grant) context.Context {
	return context.WithValue(ctx, grantCtxKey{}, g)
}

// Middleware authenticates requests that carry an embed grant.
//
// # Why an app needs this
//
// The frame renders through the host's own embed routes, which verify the grant
// themselves. But everything the surface does AFTER first paint targets an
// ordinary app route: every island RPC, every form post, every poll. Those
// routes know nothing about embeds. Without this middleware, an embedded surface
// paints as its viewer and then acts as nobody:
//
//   - Cross-site framing: no cookie is sent and the grant is ignored, so the
//     handler runs anonymously and the island silently swaps authenticated
//     content for a logged-out render.
//   - Same-site framing (app.acme.com inside www.acme.com): the cookie IS sent
//     to the app route, so the surface's content renders as the grant's subject
//     while its islands mutate as the cookie's user. That identity confusion is
//     exactly what the embed routes strip cookies to prevent, one hop
//     downstream.
//
// Install it on the router (or on the group the embeddable surface's islands
// post to):
//
//	app.Use(embeds.Middleware())
//
// # What it does
//
// A request with no grant header passes through untouched. This is not an
// authenticator for ordinary traffic.
//
// A request carrying a grant header is an embed request, and is handled as one:
// the grant must verify (an invalid one is refused, never downgraded to
// anonymous), every ambient credential the request carries is discarded, and
// the grant plus its resolved subject are installed on the context.
//
// # Install it OUTERMOST
//
// This middleware must run BEFORE any authentication middleware, outside
// session auth, bearer auth, API-token auth, and anything that derives a tenant
// from a credential:
//
//	app.Use(embeds.Middleware())   // first
//	app.Use(auth.Session(...))     // then everything else
//
// It discards the credentials themselves (Cookie, Authorization, X-API-Key), so
// an authenticator running inside it finds nothing to authenticate and the
// grant's subject stands alone. Installed the other way round, an authenticator
// that already ran has written its own values onto the context, and this
// middleware cannot take them back: it does not know which keys another package
// used. The observable failures are a bearer token overwriting the grant's
// identity, and an API token's scopes surviving under the grant subject's name.
//
// # Scopes are not enforced here
//
// The grant carries the scopes the surface declared, and installing the subject
// gives the handler that subject's FULL authority, the same as a first-party
// request from that user. Nothing about holding a "reports:read" grant stops it
// reaching an admin route the subject happens to be allowed to use, and a grant
// lives in a third party's page where anyone with devtools can read it.
//
// Gate the routes an embed can reach with [Host.RequireScope]:
//
//	app.Use(embeds.Middleware())
//	reports := app.Group("/reports")
//	reports.Use(embeds.RequireScope("reports:read"))
func (h *Host) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get(GrantHeader)
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}
			if !h.Ready() {
				http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
				return
			}
			g, err := h.VerifyGrant(r.Context(), token)
			if err != nil {
				// Refuse rather than fall through anonymously. A caller that
				// presented a credential and had it rejected must not silently
				// become a public visitor. That turns an expired grant into a
				// wrong render instead of an error the frame can act on.
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}

			// What may this grant reach?
			//
			// Default-closed: the surface's own Path subtree, /__gofastr/*, and
			// whatever the surface declared in Reach. Everything else is 403,
			// deliberately NOT a fall-through to anonymous, which would turn a
			// refused embed into a silently logged-out render, the failure this
			// middleware's whole design exists to prevent.
			//
			// The refusal names the fix, because the alternative failure mode,
			// "my embed silently doesn't work", is the one that produced most
			// of the bugs this feature has had.
			surface, known := h.Lookup(g.Surface)
			if !known || surface == nil {
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			}
			// Decide on the path the ROUTER will dispatch on, not on the
			// decoded one. See RoutedPath: comparing a normalised string
			// against a router that does not normalise let a grant reach
			// /api/docs/ and an app's own "/{slug}".
			routed, routable := RoutedPath(r.URL)
			if !routable {
				http.Error(w, "embed: request path is not canonical", http.StatusBadRequest)
				return
			}
			if !surface.MayReach(routed) {
				http.Error(w, fmt.Sprintf(
					"embed surface %q may not reach %s: add it to that surface's Reach "+
						"to allow it, e.g. Reach: []string{%q}",
					g.Surface, routed, reachSuggestion(routed)), http.StatusForbidden)
				return
			}

			// The grant is the only identity an embed request may have.
			//
			// Cookies, because in a same-site framing the browser really does
			// send them and honouring one would let the frame act as the
			// cookie's user. The bearer and API-key headers for the same
			// reason: a frame never needs them, and leaving them in place lets
			// an authenticator running inside this middleware install a second,
			// unrelated identity over the grant's, or leave its own token
			// scopes on the context underneath the grant subject's name.
			r.Header.Del("Cookie")
			r.Header.Del("Authorization")
			r.Header.Del("X-API-Key")

			// "Install this OUTERMOST" was a contract enforced by nothing.
			// Installed inside an authenticator, the credentials are already
			// spent: the header deletions above hit an empty request while the
			// values the authenticator derived sit on the context, out of
			// reach. This package does not know the keys other packages use.
			// The observed failure was an embed running as the grant's subject
			// with the COOKIE user's tenant, which is tenant isolation off.
			//
			// A wrong order is a boot-time mistake, so make it fail on the
			// first request, loudly, instead of serving cross-tenant data
			// quietly for the life of the deployment.
			if u, ok := handler.GetUser(r.Context()); ok && u != nil {
				misordered(w, "a user")
				return
			}
			if t, ok := handler.GetTenant(r.Context()); ok && t != nil {
				misordered(w, "a tenant")
				return
			}
			if tenant.GetTenantID(r.Context()) != "" {
				misordered(w, "a tenant id")
				return
			}

			// A grant-authenticated response is per-subject and must never be
			// shared. There is no Set-Cookie and no Authorization header on
			// these requests, so to a CDN or a caching proxy two different
			// subjects are byte-identical requests. battery/cache was taught
			// about the grant header; nothing outside the process was.
			w.Header().Add("Vary", GrantHeader)
			w.Header().Set("Cache-Control", "private, no-store")

			ctx := WithGrant(r.Context(), g)
			// Belt and braces. The ordering guard above already refuses any
			// request that arrives with a user or a tenant, so these three
			// clears are unreachable while it holds. No test can pin them,
			// and one that claims to is asserting nothing. They stay because
			// the guard is the thing that would have to be wrong, and the
			// cost of being wrong here is a cross-tenant read.
			//
			// The reachable clearing is on the embed CONTENT route, which
			// builds its own context and has no such guard; see
			// framework/uihost/embed.go.
			ctx = handler.SetUser(ctx, nil)
			ctx = handler.SetTenant(ctx, nil)
			ctx = tenant.SetTenantID(ctx, "")

			// Bound the request by the grant's own expiry.
			//
			// Verification happens once, at entry. A handler that then holds
			// the request open, an SSE stream or a long poll, outlives the
			// credential that authorized it, and the deadline is the entire
			// answer to "this token lives in a page the app does not control".
			// A stream was observed still emitting three seconds after a fresh
			// request with the same grant had started answering 401.
			if !g.Expires.IsZero() {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, g.Expires)
				defer cancel()
			}

			// The tenant, if the app can name one for this subject. Installed
			// from a server-side lookup keyed on the grant's subject, never
			// from anything the request carried, which is what makes a stolen
			// grant unable to choose its own tenant.
			if h.resolveTenant != nil && g.Subject != "" {
				tid, err := h.resolveTenant(ctx, g.Subject)
				if err != nil {
					// Fail closed, for the same reason the subject resolver
					// does: continuing without a tenant would either error
					// deep in CRUD or, worse, run untenanted.
					http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
					return
				}
				if tid != "" {
					ctx = handler.SetTenant(ctx, tid)
					ctx = tenant.SetTenantID(ctx, tid)
				}
			}

			if h.resolve != nil && g.Subject != "" {
				user, err := h.resolve(ctx, g.Subject)
				if err != nil {
					// The subject no longer resolves. Fail closed: continuing
					// anonymously would downgrade an authenticated embed into a
					// public one without saying so.
					http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
					return
				}
				if !IsNilValue(user) {
					ctx = handler.SetUser(ctx, user)
				}
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireScope refuses any embed request whose grant does not carry scope.
//
// [Host.Middleware] authenticates an embed request; this decides what that
// request is allowed to reach. The two are separate because only the app knows
// which of its routes correspond to which declared scope: the framework has no
// route-to-scope map and inventing one would mean guessing.
//
//	app.Use(embeds.Middleware())
//
//	reports := app.Group("/reports")
//	reports.Use(embeds.RequireScope("reports:read"))
//
//	// No surface declares "admin", so no embed reaches these routes.
//	admin := app.Group("/admin")
//	admin.Use(embeds.RequireScope("admin"))
//
// # Ordinary traffic passes
//
// A request with no grant is not an embed request and is not this middleware's
// business, so it passes through. Gating first-party traffic is the app's
// existing auth middleware's job, and refusing here would break every ordinary
// visitor to the same route. The consequence is worth stating plainly: this
// narrows what an EMBED may do, and nothing else.
//
// # Why a grant needs narrowing at all
//
// A grant is minted for a surface, handed to a third party's page, and readable
// by anyone with devtools on that page. The subject behind it may be an admin.
// Without this, holding a grant minted for a read-only reporting surface is
// enough to act as that admin anywhere the admin is allowed to act, for as long
// as the grant refreshes.
func (h *Host) RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			g, ok := GrantFromContext(r.Context())
			if !ok {
				// Not an embed request. If a grant header is nevertheless
				// present, Middleware did not run on this route. Refuse rather
				// than let the caller past a gate that never looked at it.
				if r.Header.Get(GrantHeader) != "" {
					http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			if !g.HasScope(scope) {
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// misordered answers a request that proves Middleware is installed inside an
// authenticator instead of outside it. 500, not 403: the caller did nothing
// wrong and retrying will not help.
func misordered(w http.ResponseWriter, what string) {
	http.Error(w, "embed: Middleware() ran after something that already resolved "+
		what+": install it OUTERMOST, before session/bearer/API-token/tenant "+
		"middleware, so the grant's subject is the only identity on the request",
		http.StatusInternalServerError)
}

// IsNilValue reports whether v is nil, including a non-nil interface wrapping a
// nil pointer.
//
// Exported because framework/uihost's embed content route installs a resolved
// subject on its own. It builds a fresh context rather than going through
// Middleware, and needs the identical check. Two call sites disagreeing about
// what "no user" means is how the content route ended up installing typed nils
// after Middleware had stopped. A SubjectResolver written as `func(...) (*User, error)` and
// returning a nil *User produces exactly that: `user != nil` is true, the nil
// pointer is installed, and every "is a user present" gate downstream reports
// authenticated for a subject that does not exist.
func IsNilValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return rv.IsNil()
	}
	return false
}

// reachSuggestion proposes the narrowest Reach entry that would admit p: its
// first two segments, which is almost always the resource root rather than one
// record. Advice only; the author decides.
func reachSuggestion(p string) string {
	cleaned := path.Clean(p)
	parts := strings.SplitN(strings.TrimPrefix(cleaned, "/"), "/", 3)
	if len(parts) >= 2 && parts[1] != "" {
		return "/" + parts[0] + "/" + parts[1]
	}
	if len(parts) >= 1 && parts[0] != "" {
		return "/" + parts[0]
	}
	return cleaned
}
