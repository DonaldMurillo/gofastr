//go:build red

package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Open finding from the 2026-11-10 bug hunt, kept as a red probe: it
// asserts the correct behaviour and fails until the fix lands.
//
// battery/auth mints every session cookie SameSite=Strict, and a browser
// withholds a Strict cookie from a navigation that starts on another
// site. So an admin who is signed in and follows a link into the back
// office from a mail client, a chat or an issue tracker arrives with no
// cookie: the gate reads them as signed out and answers the login
// redirect (or a 401 with no LoginPath), and the login page is part of
// the same navigation, so it renders signed out too. They sign in again
// for every link that comes from outside the app.
//
// battery/auth answers the same arrival on its own routes with a page
// from this origin that loads the URL again (email verification, the
// OAuth link and sign-in callbacks; battery/setup does it for the setup
// link). That reload starts on this site and carries the cookie.
//
// The decision this needs: the admin gate cannot tell a signed-out
// visitor from a signed-in one whose cookie was withheld, so the hop
// would cost every cross-site signed-out arrival one extra load. The
// hop page itself lives unexported in battery/auth (writeSameSiteHop,
// AuthConfig.Pages.Hop); the gate needs it exported, or the hop belongs
// in SessionMiddleware for document navigations, which covers every
// guarded app page and not only the back office.
func TestRedCrossSiteArrivalIsNotSentToLogin(t *testing.T) {
	x := gatedEnv(t, Config{LoginPath: "/login"})
	req := httptest.NewRequest(http.MethodGet, "/admin/audit?actor=u1", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Dest", "document")
	rr := httptest.NewRecorder()
	x.as(nil).ServeHTTP(rr, req)
	if loc := rr.Header().Get("Location"); loc != "" {
		t.Fatalf("a cross-site arrival was sent to %q: a signed-in admin's Strict cookie is withheld on it, so they are asked to sign in again", loc)
	}
	if got := rr.Header().Get("Refresh"); got != "0;url=/admin/audit?actor=u1" {
		t.Fatalf("Refresh = %q (status %d), want a reload of the same page from this origin", got, rr.Code)
	}
}
