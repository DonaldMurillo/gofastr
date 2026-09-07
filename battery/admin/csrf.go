package admin

import (
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// rejectCrossSiteForm refuses a browser cross-site submission to a mutating
// admin route and reports whether it wrote a response. The battery needs its
// own posture because the CSRF middleware is an optional app-level add-on:
// under the battery's own default mounting (RegisterRoutes = SecurityHeaders +
// gate) a rendered-but-empty hidden _csrf input is the only token the screens
// can carry, so the battery itself must refuse the forgeable cross-site shape.
//
// The gate is handler.IsForgeableRequest, NOT the Content-Type alone: a form
// with enctype="text/plain" and a bodyless fetch() POST are CORS-simple,
// preflight-free CSRF vehicles exactly like a urlencoded form, and the
// queue-replay route mutates state with no body at all.
//
// handler.IsCrossSiteRequest is the repo's one cross-site predicate
// (Sec-Fetch-Site first; "same-site" falls through to the Origin-host
// compare — a sibling subdomain is same-site while its Origin names another
// host, and the SameSite session cookie still attaches). Non-browser clients
// (curl, tests, native apps) send neither header and pass.
func rejectCrossSiteForm(w http.ResponseWriter, r *http.Request) bool {
	if !handler.IsForgeableRequest(r) {
		return false
	}
	if handler.IsCrossSiteRequest(r) {
		http.Error(w, "forbidden: cross-site request", http.StatusForbidden)
		return true
	}
	return false
}
