// Package decide provides the constructors for app.Decision values
// returned from Policy.Decide implementations. Lives in a subpackage
// (rather than under app directly) so call sites read like
// `decide.Block(403, "no")` without shadowing common variable names
// ("block", "redirect") that authors reach for.
//
//	import (
//	    "github.com/DonaldMurillo/gofastr/core-ui/app"
//	    "github.com/DonaldMurillo/gofastr/core-ui/app/decide"
//	)
//
//	pol := app.PolicyFunc(func(ctx context.Context) app.Decision {
//	    if authed(ctx) { return decide.Allow() }
//	    return decide.Redirect("/login")
//	})
package decide

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

// Allow returns the "let the request proceed" Decision.
func Allow() app.Decision { return app.Decision{Kind: app.DecisionAllow} }

// Redirect returns a Decision that redirects the request to url.
// Hosts typically use HTTP 303 See Other so POST→Redirect→GET works.
func Redirect(url string) app.Decision {
	return app.Decision{Kind: app.DecisionRedirect, URL: url}
}

// RenderAlt returns a Decision that renders an alt component in place
// of the screen's own. factory MUST return a FRESH instance per call
// — the framework invokes it once per request and the returned
// component is then mutated by SetParams / Inject / Load under the
// screen lock. Sharing a singleton is a cross-user data leak.
func RenderAlt(factory func() component.Component) app.Decision {
	return app.Decision{Kind: app.DecisionRenderAlt, AltFactory: factory}
}

// Block returns a Decision that aborts with status (e.g. 401, 403,
// 404). Message is optional human-readable text.
func Block(status int, message string) app.Decision {
	return app.Decision{Kind: app.DecisionBlock, Status: status, Message: message}
}
