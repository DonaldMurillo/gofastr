package framework

import (
	"context"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

type inProcessCallKey struct{}

// InProcessHandler serves trusted server-side calls through the app's router,
// taking identity only from the caller's context. A request carrying any
// credential is refused with 403. Never serve it on a listener.
func (a *App) InProcessHandler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		for header := range request.Header {
			if isInProcessCredentialHeader(header) {
				i18n.HTTPError(writer, request, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
				return
			}
		}
		if request.URL.User != nil || (request.TLS != nil && len(request.TLS.PeerCertificates) != 0) {
			i18n.HTTPError(writer, request, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
			return
		}
		ctx := context.WithValue(request.Context(), inProcessCallKey{}, true)
		a.router.ServeHTTP(writer, request.WithContext(ctx))
	})
}

func isInProcessCredentialHeader(header string) bool {
	if strings.EqualFold(header, "Proxy-Authorization") {
		return true
	}
	for _, name := range delegationCredentialHeaders {
		if strings.EqualFold(header, name) {
			return true
		}
	}
	return false
}

// IsInProcessRequest reports whether InProcessHandler admitted this request;
// no header can set it.
func IsInProcessRequest(request *http.Request) bool {
	marked, _ := request.Context().Value(inProcessCallKey{}).(bool)
	return marked
}
