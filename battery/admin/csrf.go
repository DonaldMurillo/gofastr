package admin

import (
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func rejectCrossSiteForm(w http.ResponseWriter, r *http.Request) bool {
	if !handler.IsForgeableRequest(r) {
		return false
	}
	if handler.IsCrossSiteRequest(r) {
		i18n.HTTPError(w, r, http.StatusForbidden, handler.ReasonCrossSiteRefused)
		return true
	}
	return false
}
