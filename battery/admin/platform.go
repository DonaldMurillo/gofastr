package admin

import (
	"context"
	"net/http"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

func platformCaller(ctx context.Context) bool { return tenant.GetTenantID(ctx) == "" }

func platformPolicy() appui.Policy {
	return appui.PolicyFunc(func(ctx context.Context) appui.Decision {
		if platformCaller(ctx) {
			return decide.Allow()
		}
		return decide.Block(http.StatusForbidden)
	})
}

func platformOnly(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !platformCaller(r.Context()) {
			i18n.HTTPError(w, r, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
			return
		}
		h(w, r)
	}
}
