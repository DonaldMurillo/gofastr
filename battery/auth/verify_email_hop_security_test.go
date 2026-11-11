package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// A verification link clicked in webmail is a cross-site navigation, so
// the browser withholds the SameSite=Strict session cookie and every such
// click answered 401 (2026-10-27 A10). The cross-site, sessionless click
// now gets a page from this origin that loads the same link again; that
// navigation carries the cookie. The hop itself redeems nothing.
func TestVerifyLinkFromWebmailHops(t *testing.T) {
	c := newClaimRig(t)
	const email, pw = "owner@example.com", "owner-chosen-pw-1"
	c.json("/auth/register", map[string]string{"email": email, "password": pw}, "")
	sess := c.login(email, pw)
	uid := c.sessionUser(sess)
	if w := c.json("/auth/send-verification", map[string]string{}, sess); w.Code != http.StatusOK {
		t.Fatalf("send-verification: %d %s", w.Code, w.Body.String())
	}
	_, body := c.verifies.snapshot()
	path := "/auth/verify-email?token=" + url.QueryEscape(extractTokenFromBody(body))

	open := func(site, cookie string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Sec-Fetch-Site", site)
		if cookie != "" {
			req.Header.Set("Cookie", "session_id="+cookie)
		}
		w := httptest.NewRecorder()
		c.r.ServeHTTP(w, req)
		return w
	}

	w := open("cross-site", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `url=`+strings.ReplaceAll(path, "&", "&amp;")) {
		t.Fatalf("webmail click = %d %s, want the same-origin hop page", w.Code, w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("hop page Cache-Control = %q, want no-store (the URL carries the token)", cc)
	}
	if c.verified(uid) {
		t.Fatal("SECURITY: the sessionless hop marked the account verified")
	}
	// A sessionless same-origin load never hops again.
	if w := open("same-origin", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("sessionless same-origin load = %d, want 401", w.Code)
	}
	if w := open("same-origin", sess); w.Code != http.StatusOK || !c.verified(uid) {
		t.Fatalf("the hop's follow-up with the session = %d %s, want verified", w.Code, w.Body.String())
	}
}

// A browser sends no Fetch Metadata to a plain-HTTP LAN address, so a
// webmail click there looks like curl. It hops too, and the reload's
// marker ends the hop: sessionless it answers 401 instead of hopping
// again, and with the session it verifies.
func TestVerifyLinkWithoutFetchMetaHops(t *testing.T) {
	c := newClaimRig(t)
	const email, pw = "nofm@example.com", "owner-chosen-pw-1"
	c.json("/auth/register", map[string]string{"email": email, "password": pw}, "")
	sess := c.login(email, pw)
	uid := c.sessionUser(sess)
	if w := c.json("/auth/send-verification", map[string]string{}, sess); w.Code != http.StatusOK {
		t.Fatalf("send-verification: %d %s", w.Code, w.Body.String())
	}
	_, body := c.verifies.snapshot()
	path := "/auth/verify-email?token=" + url.QueryEscape(extractTokenFromBody(body))

	open := func(target, cookie string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if cookie != "" {
			req.Header.Set("Cookie", "session_id="+cookie)
		}
		w := httptest.NewRecorder()
		c.r.ServeHTTP(w, req)
		return w
	}
	reload := path + "&" + handler.SameSiteHopParam + "=1"
	w := open(path, "")
	if w.Code != http.StatusOK || w.Header().Get("Refresh") != "0;url="+reload {
		t.Fatalf("no-Fetch-Metadata click = %d Refresh %q, want the hop page loading %s", w.Code, w.Header().Get("Refresh"), reload)
	}
	if c.verified(uid) {
		t.Fatal("SECURITY: the sessionless hop marked the account verified")
	}
	if w := open(reload, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("sessionless reload = %d, want 401, not another hop", w.Code)
	}
	if w := open(reload, sess); w.Code != http.StatusOK || !c.verified(uid) {
		t.Fatalf("reload with the session = %d %s, want verified", w.Code, w.Body.String())
	}
}
