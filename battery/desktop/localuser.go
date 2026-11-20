package desktop

import (
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
)

func (b *Battery) localUserMiddleware() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !b.gateArmed.Load() || b.authOwnsIdentity {
				next.ServeHTTP(w, r)
				return
			}
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

func (b *Battery) installOwnerExtractor() {
	b.authOwnsIdentity = localapp.Install()
}
