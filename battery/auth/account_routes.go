package auth

import (
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework"
)

func rejectInProcessAccountOperation(w http.ResponseWriter, r *http.Request) bool {
	if !framework.IsInProcessRequest(r) {
		return false
	}
	writeAuthError(w, r, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
	return true
}

func browserAccountOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rejectInProcessAccountOperation(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}
