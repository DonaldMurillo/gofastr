package print

import (
	"errors"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// ErrNotFound is the sentinel a Document.Build returns when the requested
// resource doesn't exist (or the caller may not see it). It renders a
// clean 404 instead of a 500.
var ErrNotFound = errors.New("print: document not found")

// ErrForbidden is the sentinel a Document.Build returns to render a clean
// 403. Prefer an Access policy for authz; this is for cases only Build
// can decide.
var ErrForbidden = errors.New("print: forbidden")

// Scope is the API-token scope that reaches print documents through the
// built-in gates ("print:*", "*:read" and "*:*" cover it). A token carries
// its owner's user, so without it a token an admin minted for
// "customers:read" would print every document its owner can. RequireAuth
// and RequireOwner refuse a scope-restricted caller (an API token or embed
// grant, access.WithHeldScopes) that does not hold it; sessions and JWTs
// carry no scopes and are unaffected. It is the scope
// auth.RequireAPIScopes derives for a GET under a root-mounted /print.
const Scope = "print:read"

// AccessPolicy gates a print document. It runs with the request and
// returns the HTTP status to short-circuit with, plus the refusal's
// phrase (zero means the status's generic one), translated per request.
// A zero status means "allow". Policies run BEFORE Document.Build, so
// an unauthorized caller never triggers a data load.
type AccessPolicy func(r *http.Request) (status int, reason i18n.Phrase)

// RequireAuth allows the request only when an authenticated user is
// present in context (set by the framework's auth chain). It is the
// default policy, so per-user documents are never world-readable unless
// a Document explicitly opts into Public.
//
// A scope-restricted caller (an API token or embed grant) must also hold
// Scope: 403 otherwise.
func RequireAuth(r *http.Request) (int, i18n.Phrase) {
	if _, ok := handler.GetUser(r.Context()); !ok {
		return http.StatusUnauthorized, i18n.Phrase{}
	}
	if !access.ScopeAllows(r.Context(), Scope) {
		return http.StatusForbidden, i18n.Phrase{}
	}
	return 0, i18n.Phrase{}
}

// Public allows every request, authenticated or not. Use only for
// documents that contain no per-user data.
func Public(_ *http.Request) (int, i18n.Phrase) { return 0, i18n.Phrase{} }

// RequireOwner allows the request only when a user is authenticated AND
// the owns callback returns true for that user. The user is passed as the
// opaque value the auth chain stored (handler.GetUser); the host asserts
// its own concrete user type. The framework cannot know an app's
// ownership model, so this delegates it. Like RequireAuth, a
// scope-restricted caller must also hold Scope; owns is not asked for one
// that does not.
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
