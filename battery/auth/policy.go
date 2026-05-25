package auth

import (
	"context"
	"net/url"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/handler"
)

// SessionFrom returns the user loaded into ctx by SessionMiddleware (or
// RequireAuth). ok is false when the request is anonymous — no session,
// expired session, store outage, etc. Safe to call from any Render or
// RenderCtx path; never panics, never touches the DB.
//
// Use this inside ContextComponent.RenderCtx for in-page gating:
//
//	func (s *Dashboard) RenderCtx(ctx context.Context) render.HTML {
//	    if sess, ok := auth.SessionFrom(ctx); ok {
//	        return UserSidebar(sess)
//	    }
//	    return AnonSidebar()
//	}
func SessionFrom(ctx context.Context) (User, bool) {
	raw, ok := handler.GetUser(ctx)
	if !ok || raw == nil {
		return nil, false
	}
	u, ok := raw.(User)
	if !ok || u == nil {
		return nil, false
	}
	return u, true
}

// PolicyOption tunes a policy constructor (SessionPolicy, RolePolicy).
// Options control what happens on policy failure: redirect, render an
// alternate component, or block with an HTTP status. The default for
// SessionPolicy is Redirect("/login?next=…"); the default for
// RolePolicy is Block(403).
type PolicyOption func(*policyOpts)

type policyOpts struct {
	redirect   string
	altFactory func() component.Component
	block      int
	blockMsg   string
	nextPath   bool // when true, append ?next=<request-path> to redirect
}

// WithRedirect sets the failure outcome to a 303 redirect to url.
// By default, the policy appends ?next=<encoded-request-path> so the
// post-login flow can return the user where they were going. Pass
// auth.NoNext() to suppress the next-path append for fixed redirects
// (e.g. "/" → marketing).
//
//	auth.WithRedirect("/login")              // next-path appended
//	auth.WithRedirect("/", auth.NoNext())    // bare redirect, no next
func WithRedirect(url string, opts ...RedirectOpt) PolicyOption {
	cfg := redirectCfg{nextPath: true}
	for _, fn := range opts {
		fn(&cfg)
	}
	return func(o *policyOpts) {
		o.redirect = url
		o.altFactory = nil
		o.block = 0
		o.nextPath = cfg.nextPath
	}
}

// RedirectOpt tunes WithRedirect behaviour.
type RedirectOpt func(*redirectCfg)

type redirectCfg struct {
	nextPath bool
}

// NoNext suppresses the auto-appended ?next=<request-path> on a
// WithRedirect option. Use for fixed redirects where preserving the
// origin path would be incorrect (e.g. anon "/" → "/marketing").
func NoNext() RedirectOpt {
	return func(c *redirectCfg) { c.nextPath = false }
}

// WithRenderAlt sets the failure outcome to rendering an alt component
// instead of the screen's own. factory MUST return a FRESH instance
// every call — the framework invokes it once per request and the
// returned component is then mutated by SetParams / Inject / Load
// under the screen lock. A shared singleton is a cross-user data leak.
//
// For a stateless anon-landing page:
//
//	auth.WithRenderAlt(func() component.Component { return &Landing{} })
func WithRenderAlt(factory func() component.Component) PolicyOption {
	return func(o *policyOpts) {
		o.altFactory = factory
		o.redirect = ""
		o.block = 0
	}
}

// WithBlock sets the failure outcome to an HTTP status (e.g. 401, 403,
// 404). msg is optional human-readable text the host may include in
// the body. Default for SessionPolicy when no option is supplied is
// Redirect("/login"); for RolePolicy it is Block(403).
func WithBlock(status int, msg string) PolicyOption {
	return func(o *policyOpts) {
		o.block = status
		o.blockMsg = msg
		o.redirect = ""
		o.altFactory = nil
	}
}

// SessionPolicy returns an app.Policy that allows requests with a
// valid session and otherwise applies the configured failure outcome.
// Default failure outcome is Redirect("/login?next=<request-path>").
//
// Attach via NewScreenGroup, ScreenGroup.WithPolicy, or
// Screen.WithPolicy. Pair with SessionMiddleware upstream so the
// policy can see the loaded user.
func SessionPolicy(opts ...PolicyOption) app.Policy {
	o := policyOpts{redirect: "/login", nextPath: true}
	for _, fn := range opts {
		fn(&o)
	}
	return app.PolicyFunc(func(ctx context.Context) app.Decision {
		if _, ok := SessionFrom(ctx); ok {
			return decide.Allow()
		}
		return failureDecision(ctx, o)
	})
}

// Roles is a tiny ergonomic helper so RolePolicy callers can write
//
//	auth.RolePolicy(auth.Roles("admin", "owner"), auth.WithRedirect("/login"))
//
// instead of the literal []string{...}. The asymmetry with the
// sibling middleware auth.RequireRole(roles ...string) is forced by
// Go's no-double-variadic rule — RolePolicy needs to accept option
// values after the role list, so the roles can't themselves be
// variadic.
func Roles(roles ...string) []string {
	out := make([]string, len(roles))
	copy(out, roles)
	return out
}

// RolePolicy returns an app.Policy that allows requests where the
// loaded user has at least one of the given roles. Default failure
// outcome is Block(403). Implies SessionPolicy — an anonymous request
// fails the role check and produces the configured failure outcome.
//
// Pair with the Roles helper for ergonomic literal lists:
//
//	auth.RolePolicy(auth.Roles("admin"), auth.WithRedirect("/login"))
//
// Note that the sibling HTTP middleware auth.RequireRole takes its
// roles as variadic strings; RolePolicy can't because it accepts
// PolicyOptions after the role list.
func RolePolicy(roles []string, opts ...PolicyOption) app.Policy {
	o := policyOpts{block: 403, blockMsg: "forbidden"}
	for _, fn := range opts {
		fn(&o)
	}
	return app.PolicyFunc(func(ctx context.Context) app.Decision {
		u, ok := SessionFrom(ctx)
		if !ok {
			return failureDecision(ctx, o)
		}
		if !hasAnyRole(u, roles) {
			return failureDecision(ctx, o)
		}
		return decide.Allow()
	})
}

// failureDecision builds the app.Decision for a failed policy check
// based on configured policyOpts. Precedence: alt > redirect > block.
func failureDecision(ctx context.Context, o policyOpts) app.Decision {
	if o.altFactory != nil {
		return decide.RenderAlt(o.altFactory)
	}
	if o.redirect != "" {
		dest := o.redirect
		if o.nextPath {
			if path := requestPathFromCtx(ctx); path != "" && path != "/" {
				sep := "?"
				if containsQuery(dest) {
					sep = "&"
				}
				dest = dest + sep + "next=" + url.QueryEscape(path)
			}
		}
		return decide.Redirect(dest)
	}
	status := o.block
	if status == 0 {
		status = 401
	}
	msg := o.blockMsg
	if msg == "" {
		msg = "unauthorized"
	}
	return decide.Block(status, msg)
}

func containsQuery(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '?' {
			return true
		}
	}
	return false
}

// requestPathFromCtx pulls the live request path out of ctx if
// app.WithRequest was called upstream (uihost does this). Returns
// empty string when no request is attached.
func requestPathFromCtx(ctx context.Context) string {
	r := app.RequestFromContext(ctx)
	if r == nil || r.URL == nil {
		return ""
	}
	if r.URL.RawQuery != "" {
		return r.URL.Path + "?" + r.URL.RawQuery
	}
	return r.URL.Path
}
