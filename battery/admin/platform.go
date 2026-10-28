package admin

import (
	"context"
	"net/http"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// platformCaller reports whether the caller is the platform operator: a
// caller with no tenant, the reading the audit page uses. Queue jobs,
// auth accounts, role grants and process modules carry no tenant, so a
// tenant-scoped admin on their pages would read and change every
// tenant's. Those pages and their posts are the platform operator's
// alone, and the sidebar, palette and dashboard leave them out for
// anyone else.
func platformCaller(ctx context.Context) bool { return tenant.GetTenantID(ctx) == "" }

// platformPolicy is a platform page's screen policy: 403 for a
// tenant-scoped caller, before the page reads anything.
func platformPolicy() appui.Policy {
	return appui.PolicyFunc(func(ctx context.Context) appui.Decision {
		if platformCaller(ctx) {
			return decide.Allow()
		}
		return decide.Block(http.StatusForbidden)
	})
}

// platformOnly answers a tenant-scoped caller of a platform post or
// fragment 403 before h reads the body.
func platformOnly(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !platformCaller(r.Context()) {
			i18n.HTTPError(w, r, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
			return
		}
		h(w, r)
	}
}
