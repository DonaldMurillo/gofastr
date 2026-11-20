// Package decide constructs the app.Decision values a Policy returns.
package decide

import (
	"net/http"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// Allow returns the "let the request proceed" Decision.
func Allow() app.Decision { return app.Decision{Kind: app.DecisionAllow} }

// Redirect returns a Decision that redirects the request to url.
func Redirect(url string) app.Decision {
	return app.Decision{Kind: app.DecisionRedirect, URL: url}
}

// RenderAlt returns a Decision that renders an alt component in place of the screen's own.
func RenderAlt(factory func() component.Component) app.Decision {
	return app.Decision{Kind: app.DecisionRenderAlt, AltFactory: factory}
}

// Block returns a Decision that answers status with its generic phrase.
func Block(status int) app.Decision {
	return app.Decision{Kind: app.DecisionBlock, Status: status}
}

// NotFound returns a Decision that answers exactly what a URL no route matches gets.
func NotFound() app.Decision {
	return app.Decision{Kind: app.DecisionBlock, Status: http.StatusNotFound, NotFound: true}
}

// BlockWith is Block with the body naming reason, translated per request with params.
func BlockWith(status int, reason i18n.Phrase, params ...map[string]any) app.Decision {
	d := app.Decision{Kind: app.DecisionBlock, Status: status, Reason: reason}
	if len(params) > 0 {
		d.Params = params[0]
	}
	return d
}
