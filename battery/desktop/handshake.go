package desktop

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/internal/gatepath"
)

const (
	enterPath         = "/__gofastr/desktop/enter"
	sessionCookieName = "__gofastr_desktop"
)

func (b *Battery) cookieName() string {
	if b.sessionCookieKey != "" {
		return b.sessionCookieKey
	}
	return sessionCookieName
}

func (b *Battery) enterHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			i18n.HTTPError(w, r, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
			return
		}
		if !tokenMatches(r.URL.Query().Get("t"), b.bootToken) ||
			!b.tokenUsed.CompareAndSwap(false, true) {
			i18n.HTTPError(w, r, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
			return
		}
		//gofastr:allow(security/insecure-cookie) plain loopback origin only, Secure would make the browser drop this cookie over http://127.0.0.1
		http.SetCookie(w, &http.Cookie{
			Name:     b.cookieName(),
			Value:    b.sessionValue,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Path:     "/",
		})
		http.Redirect(w, r, b.bootPath(), http.StatusFound)
	})
}

func (b *Battery) bootPath() string {
	if s := b.winStore.Load(); s != nil {
		if p := s.mainPath(); validNavigatePath(p) {
			return p
		}
	}
	return "/"
}

func tokenMatches(presented, minted string) bool {
	return subtle.ConstantTimeCompare([]byte(presented), []byte(minted)) == 1
}

func (b *Battery) gateMiddleware() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !b.gateArmed.Load() {
				next.ServeHTTP(w, r)
				return
			}
			if r.URL.Path == enterPath {
				next.ServeHTTP(w, r)
				return
			}
			pin := b.hostPin.Load()
			if pin == "" || r.Host != pin {
				i18n.HTTPError(w, r, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
				return
			}
			if b.openPath(r) {
				if handler.IsForgedCrossSiteWrite(r) {
					i18n.HTTPError(w, r, http.StatusForbidden, handler.ReasonCrossSiteRefused)
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			if !b.requestHasSession(r) {
				i18n.HTTPError(w, r, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (b *Battery) armGate() {
	b.gateArmed.Store(true)
	if b.app != nil && !b.authOwnsIdentity {
		b.app.NoteAuthenticator()
	}
}

func (b *Battery) requestHasSession(r *http.Request) bool {
	for _, c := range r.Cookies() {
		if c.Name != b.cookieName() || c.Value == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(c.Value), []byte(b.sessionValue)) == 1 {
			return true
		}
	}
	return false
}

func (b *Battery) openPath(r *http.Request) bool {
	return gatepath.Open(r, b.cfg.OpenPrefixes)
}

func (b *Battery) setHostPin(addr string) {
	b.hostPin.Store(strings.TrimSpace(addr))
}
