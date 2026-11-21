package print

import (
	"errors"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// ErrNotFound makes a Build answer 404.
var ErrNotFound = errors.New("print: document not found")

// ErrForbidden makes a Build answer 403.
var ErrForbidden = errors.New("print: forbidden")

// Scope is the API-token scope the built-in policies require of a scoped caller.
const Scope = "print:read"

// AccessPolicy returns the status to refuse with, or zero to allow.
type AccessPolicy func(r *http.Request) (status int, reason i18n.Phrase)

// RequireAuth allows an authenticated caller holding Scope.
func RequireAuth(r *http.Request) (int, i18n.Phrase) {
	if _, ok := handler.GetUser(r.Context()); !ok {
		return http.StatusUnauthorized, i18n.Phrase{}
	}
	if !access.ScopeAllows(r.Context(), Scope) {
		return http.StatusForbidden, i18n.Phrase{}
	}
	return 0, i18n.Phrase{}
}

// Public allows every request, authenticated or not.
func Public(_ *http.Request) (int, i18n.Phrase) { return 0, i18n.Phrase{} }

// RequireOwner allows an authenticated caller holding Scope for whom owns is true.
func RequireOwner(owns func(r *http.Request, user any) bool) AccessPolicy {
	return func(r *http.Request) (int, i18n.Phrase) {
		user, ok := handler.GetUser(r.Context())
		if !ok {
			return http.StatusUnauthorized, i18n.Phrase{}
		}
		if !access.ScopeAllows(r.Context(), Scope) || !owns(r, user) {
			return http.StatusForbidden, i18n.Phrase{}
		}
		return 0, i18n.Phrase{}
	}
}
