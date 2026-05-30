package print

import (
	"errors"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// ErrNotFound is the sentinel a Document.Build returns when the requested
// resource doesn't exist (or the caller may not see it). It renders a
// clean 404 instead of a 500.
var ErrNotFound = errors.New("print: document not found")

// ErrForbidden is the sentinel a Document.Build returns to render a clean
// 403. Prefer an Access policy for authz; this is for cases only Build
// can decide.
var ErrForbidden = errors.New("print: forbidden")

// AccessPolicy gates a print document. It runs with the request and
// returns the HTTP status to short-circuit with, plus a message. A zero
// status means "allow". Policies run BEFORE Document.Build, so an
// unauthorized caller never triggers a data load.
type AccessPolicy func(r *http.Request) (status int, msg string)

// RequireAuth allows the request only when an authenticated user is
// present in context (set by the framework's auth chain). It is the
// default policy, so per-user documents are never world-readable unless
// a Document explicitly opts into Public.
func RequireAuth(r *http.Request) (int, string) {
	if _, ok := handler.GetUser(r.Context()); !ok {
		return http.StatusUnauthorized, "unauthorized"
	}
	return 0, ""
}

// Public allows every request, authenticated or not. Use only for
// documents that contain no per-user data.
func Public(_ *http.Request) (int, string) { return 0, "" }

// RequireOwner allows the request only when a user is authenticated AND
// the owns callback returns true for that user. The user is passed as the
// opaque value the auth chain stored (handler.GetUser); the host asserts
// its own concrete user type. The framework cannot know an app's
// ownership model, so this delegates it.
func RequireOwner(owns func(r *http.Request, user any) bool) AccessPolicy {
	return func(r *http.Request) (int, string) {
		user, ok := handler.GetUser(r.Context())
		if !ok {
			return http.StatusUnauthorized, "unauthorized"
		}
		if !owns(r, user) {
			return http.StatusForbidden, "forbidden"
		}
		return 0, ""
	}
}
