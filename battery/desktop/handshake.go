package desktop

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// The boot handshake. The desktop host serves the app on loopback; any
// local process can connect to the port, so the page must present a
// credential no other process has. That credential is the session
// cookie minted by the single-use /enter token, and the Host pin
// closes DNS rebinding from a browser tab (Origin checks never stop
// rebinding; pinning Host does, 2026-07-24 audit).
//
// Neither the boot token nor the session value is ever logged or put
// into an error string.

const (
	// enterPath is the single-use token door.
	enterPath = "/__gofastr/desktop/enter"
	// sessionCookieName carries the session value.
	sessionCookieName = "__gofastr_desktop"
)

// enterHandler answers GET /__gofastr/desktop/enter?t=<boot token>.
// The token is compared in constant time and is single-use: a second
// request fails with 403 even with the right token. On success it sets
// the session cookie and 302s to /.
func (b *Battery) enterHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Match first (constant time), single-use second: a tokenless
		// or wrong-token probe must not burn the flag, and two
		// concurrent correct requests are decided by the CAS so exactly
		// one passes.
		if !tokenMatches(r.URL.Query().Get("t"), b.bootToken) ||
			!b.tokenUsed.CompareAndSwap(false, true) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		// Plain loopback: no Secure (it would stop the cookie dead on
		// http://127.0.0.1), session lifetime (no Max-Age/Expires).
		//gofastr:allow(security/insecure-cookie) plain loopback origin only, Secure would make the browser drop this cookie over http://127.0.0.1
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    b.sessionValue,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Path:     "/",
		})
		http.Redirect(w, r, b.bootPath(), http.StatusFound)
	})
}

// bootPath is the enter redirect's target: the main window's
// remembered path when window remembering is on and the stored path
// still passes the navigate grammar, else "/". The stored path is data
// at rest; it is validated again here, on the way out.
func (b *Battery) bootPath() string {
	if s := b.winStore.Load(); s != nil {
		if p := s.mainPath(); validNavigatePath(p) {
			return p
		}
	}
	return "/"
}

// tokenMatches compares a presented token against the minted one in
// constant time.
func tokenMatches(presented, minted string) bool {
	return subtle.ConstantTimeCompare([]byte(presented), []byte(minted)) == 1
}

// gateMiddleware refuses everything except /enter once Run has armed it,
// then requires the pinned Host and a valid session cookie. Installed
// with app.Use in Init, so it wraps every route including uihost pages
// and the SSE bus.
//
// Arming is what Run does: until then the gate is a pass-through. A
// battery that is registered but never Run (an app serving itself over
// plain HTTP behind a --serve flag, the host-independence mode) must not
// be locked out of its own routes by a handshake that never happens.
// Run arms the gate BEFORE the app's listener opens, so the desktop
// posture (every request needs the boot cookie from the moment the port
// answers) is unchanged.
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
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if !b.requestHasSession(r) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// armGate arms the boot gate and is called by Run before the app's
// listener opens. In-memory tests arm it directly instead of running
// the whole Run flow.
func (b *Battery) armGate() {
	b.gateArmed.Store(true)
}

// requestHasSession checks EVERY cookie of the session name, not only
// the first: a stale extra cookie of the same name in front of the
// real one must still authenticate (the battery/auth
// sessionCookieCandidates precedent, a proxy or a set-cookie race can
// duplicate the pair, and honoring only cookies[0] logs the user out).
func (b *Battery) requestHasSession(r *http.Request) bool {
	for _, c := range r.Cookies() {
		if c.Name != sessionCookieName || c.Value == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(c.Value), []byte(b.sessionValue)) == 1 {
			return true
		}
	}
	return false
}

// setHostPin pins the gate to the listener-reported address. Called by
// Run once the app is bound; in-memory tests call it directly.
func (b *Battery) setHostPin(addr string) {
	b.hostPin.Store(strings.TrimSpace(addr))
}
