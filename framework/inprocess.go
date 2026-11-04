package framework

import (
	"context"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

type inProcessCallKey struct{}

// InProcessHandler dispatches trusted server-side calls through the app's
// complete middleware and router. Identity, tenant and roles come only from
// the caller's context. Requests carrying credentials (cookies, Authorization,
// Proxy-Authorization, X-API-Key, the X-Gofastr-Embed grant, URL userinfo or
// a TLS peer certificate) are refused with 403.
// Pass this handler to generated Local repositories, not to a network listener.
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

// isInProcessCredentialHeader reports whether a header carries a credential
// InProcessHandler refuses: the framework's credential canon
// (delegationCredentialHeaders, which includes the embed grant) plus
// Proxy-Authorization. Header map keys are compared case-insensitively
// because a caller can set a non-canonical key directly.
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

// IsInProcessRequest reports whether InProcessHandler admitted this request.
// It is a read-only middleware seam; network headers cannot set the marker.
// Custom CSRF middleware may use it as its skip predicate.
func IsInProcessRequest(request *http.Request) bool {
	marked, _ := request.Context().Value(inProcessCallKey{}).(bool)
	return marked
}
