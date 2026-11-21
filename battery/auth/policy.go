package auth

import (
	"context"
	"net/url"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// SessionFrom returns the user SessionMiddleware or RequireAuth put in ctx, or false.
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
type PolicyOption func(*policyOpts)

type policyOpts struct {
	redirect   string
	altFactory func() component.Component
	block      int
	blockWhy   i18n.Phrase
	nextPath   bool // when true, append ?next=<request-path> to redirect
}

// WithRedirect sets the failure outcome to a 303 redirect to url.
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

// NoNext stops WithRedirect appending ?next=<request-path>.
func NoNext() RedirectOpt {
	return func(c *redirectCfg) { c.nextPath = false }
}

// WithRenderAlt renders factory's component on failure; factory must return a fresh instance each call.
func WithRenderAlt(factory func() component.Component) PolicyOption {
	return func(o *policyOpts) {
		o.altFactory = factory
		o.redirect = ""
		o.block = 0
	}
}

// WithBlock answers failure with status and its generic phrase.
func WithBlock(status int) PolicyOption {
	return WithBlockReason(status, i18n.Phrase{})
}

// WithBlockReason is WithBlock with reason as the body.
func WithBlockReason(status int, reason i18n.Phrase) PolicyOption {
	return func(o *policyOpts) {
		o.block = status
		o.blockWhy = reason
		o.redirect = ""
		o.altFactory = nil
	}
}

// SessionPolicy allows a request with a session user (no API-token caller) and otherwise fails, by default to /login?next=<path>.
func SessionPolicy(opts ...PolicyOption) app.Policy {
	o := policyOpts{redirect: "/login", nextPath: true}
	for _, fn := range opts {
		fn(&o)
	}
	return app.PolicyFunc(func(ctx context.Context) app.Decision {
		if _, tokenAuth := TokenScopes(ctx); tokenAuth {
			return failureDecision(ctx, o)
		}
		if _, ok := SessionFrom(ctx); ok {
			return decide.Allow()
		}
		return failureDecision(ctx, o)
	})
}

// Roles returns its arguments as a slice, for RolePolicy.
func Roles(roles ...string) []string {
	out := make([]string, len(roles))
	copy(out, roles)
	return out
}

// RolePolicy allows a session user holding one of roles, as RequireRole judges it, and otherwise fails, by default with 403.
func RolePolicy(roles []string, opts ...PolicyOption) app.Policy {
	o := policyOpts{block: 403}
	for _, fn := range opts {
		fn(&o)
	}
	return app.PolicyFunc(func(ctx context.Context) app.Decision {
		u, ok := SessionFrom(ctx)
		if !ok {
			return failureDecision(ctx, o)
		}
		if _, tokenAuth := TokenScopes(ctx); tokenAuth {
			return failureDecision(ctx, o)
		}
		if roleGateDenied(ctx, u) {
			return failureDecision(ctx, o)
		}
		if !hasAnyRole(ctx, u, roles) {
			return failureDecision(ctx, o)
		}
		return decide.Allow()
	})
}

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
		handler.NoteSessionRefusal(ctx)
		return decide.Redirect(dest)
	}
	status := o.block
	if status == 0 {
		status = 401
	}
	return decide.BlockWith(status, o.blockWhy)
}

func containsQuery(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '?' {
			return true
		}
	}
	return false
}

func requestPathFromCtx(ctx context.Context) string {
	r := app.RequestFromContext(ctx)
	if r == nil || r.URL == nil {
		return ""
	}
	if r.URL.RawQuery != "" {
		return r.URL.EscapedPath() + "?" + r.URL.RawQuery
	}
	return r.URL.EscapedPath()
}
