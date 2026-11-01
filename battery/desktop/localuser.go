package desktop

import (
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/internal/localuser"
)

// The local identity. A desktop app ships without battery/auth, but
// owner-scoped CRUD still needs an owner id. The LocalUser middleware
// sets a stable per-installation identity (internal/localuser, shared
// with battery/service) into the request context, and the owner
// extractor (installed only when nothing else installed one) reads it
// back. An app that also imports battery/auth keeps auth's extractor
// AND auth's identity: the local identity is a fallback for an app
// with no auth battery, never an override of one that has already
// decided, including a decision of "anonymous". See
// localUserMiddleware for why that distinction needed a field.

// localUserMiddleware installs the identity into the request context.
// It applies ONLY while the boot gate is armed, which Run does before
// the listener opens: a request that reached the handler then came
// from the desktop window and carries the boot cookie, so it is the
// local user. An app serving itself over plain HTTP (the --serve
// mode; the gate stays unarmed) gets no identity from this battery,
// because every anonymous browser would otherwise become the local
// admin. Such an app brings battery/auth or stays anonymous.
//
// Within desktop mode it is a FALLBACK, and the decision about whether
// it applies at all is made at INIT, not per request.
//
// The per-request test used to be `if _, ok := handler.GetUser(ctx);
// !ok`, which looks like "nobody has decided yet" and is not. When
// battery/auth is mounted, its session middleware marks an anonymous
// request with handler.SetUser(ctx, nil), and handler.GetUser type-
// asserts, so a stored nil reads back ok=FALSE. Auth's explicit "nobody
// is signed in" was byte-identical to "no middleware ran", and this
// fallback overrode it, handing a signed-out desktop window the local
// identity, whose GetRoles() is ["admin"], which is exactly the
// structural interface battery/admin authorizes through. Worse, which
// way it went depended on battery REGISTRATION ORDER: with desktop's
// Use installed first, auth's anon path overwrote the local user with
// nil instead.
//
// So: battery/auth installs the framework/owner extractor from its
// package init(), which makes owner.GetExtractor() != nil a reliable
// "an auth battery is linked into this binary" signal, the same test
// installOwnerExtractor already makes. When it is set, this battery
// installs NO identity and auth owns identity end to end. An app that
// wants an identity in --serve mode brings battery/auth or stays
// anonymous.
func (b *Battery) localUserMiddleware() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !b.gateArmed.Load() || b.authOwnsIdentity {
				next.ServeHTTP(w, r)
				return
			}
			// An open path without the window's cookie is a client the
			// app authenticates itself; it is not the local user.
			if b.openPath(r) && !b.requestHasSession(r) {
				next.ServeHTTP(w, r)
				return
			}
			if _, ok := handler.GetUser(r.Context()); !ok {
				r = r.WithContext(handler.SetUser(r.Context(), b.user))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// installOwnerExtractor sets the process-wide owner extractor only when
// none exists, and records whether a foreign one (battery/auth's, from
// its package init) already did. Never overwrite it, and never speak
// for a request it has already decided about (see localUserMiddleware).
func (b *Battery) installOwnerExtractor() {
	b.authOwnsIdentity = localuser.Install()
}
