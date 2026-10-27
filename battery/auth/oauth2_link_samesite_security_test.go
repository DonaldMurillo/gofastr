package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// hopTarget pulls the meta-refresh target out of a hop page.
var hopTarget = regexp.MustCompile(`http-equiv="refresh" content="0;url=([^"]+)"`)

// A browser following the IdP's redirect back to the callback is on a
// cross-site navigation, so it withholds the SameSite=Strict session
// cookie. The link callback required that cookie and answered 401, so
// linking a provider never worked from a real browser; the unit tests
// passed only because they attached the cookie by hand.
func TestOAuthLinkSurvivesStrictCookie(t *testing.T) {
	type flow struct {
		r           http.Handler
		store       *linkingUserStore
		session     string
		state       string
		stateCookie *http.Cookie
	}
	start := func(t *testing.T) flow {
		_, r, store, mgr := linkFixture(t, &OAuth2UserInfo{
			ID: "prov-strict", Email: "owner@example.com", Provider: "stub", EmailVerified: true,
		})
		owner := store.preExistingUser("owner@example.com")
		sess, err := mgr.SessionStore().Create(context.Background(), owner.GetID(), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		// The link starts on our own site, where the Strict cookie rides.
		req := httptest.NewRequest(http.MethodGet, "/auth/oauth/stub/link", nil)
		req.AddCookie(&http.Cookie{Name: "session_id", Value: sess.Token})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusFound {
			t.Fatalf("link start: %d %s", w.Code, w.Body.String())
		}
		f := flow{r: r, store: store, session: sess.Token}
		loc, _ := url.Parse(w.Header().Get("Location"))
		f.state = loc.Query().Get("state")
		for _, c := range w.Result().Cookies() {
			if c.Name == oauthStateCookie {
				f.stateCookie = c
			}
		}
		if f.state == "" || f.stateCookie == nil {
			t.Fatal("link start set no state")
		}
		return f
	}
	// idpReturn is the IdP's redirect back: cross-site, so the state
	// cookie (Lax) rides and the session cookie (Strict) does not.
	idpReturn := func(f flow) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/auth/oauth/stub/callback?state="+url.QueryEscape(f.state)+"&code=c", nil)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.AddCookie(f.stateCookie)
		w := httptest.NewRecorder()
		f.r.ServeHTTP(w, req)
		return w
	}
	// follow loads the hop page's target as the browser would: a
	// navigation our own page started, so both cookies ride.
	follow := func(t *testing.T, f flow, hop *httptest.ResponseRecorder, withSession bool) *httptest.ResponseRecorder {
		m := hopTarget.FindStringSubmatch(hop.Body.String())
		if m == nil {
			t.Fatalf("first hop = %d, not a same-site hop page: %s", hop.Code, hop.Body.String())
		}
		target := strings.ReplaceAll(m[1], "&amp;", "&")
		if !strings.HasPrefix(target, "/auth/oauth/stub/callback?") {
			t.Fatalf("hop target %q is not the same-origin callback", target)
		}
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.AddCookie(f.stateCookie)
		if withSession {
			req.AddCookie(&http.Cookie{Name: "session_id", Value: f.session})
		}
		w := httptest.NewRecorder()
		f.r.ServeHTTP(w, req)
		return w
	}

	t.Run("cookie withheld, then the same-site hop", func(t *testing.T) {
		f := start(t)
		hop := idpReturn(f)
		if hop.Code != http.StatusOK {
			t.Fatalf("IdP return without the Strict cookie = %d %s, want the 200 hop page", hop.Code, hop.Body.String())
		}
		if f.store.linkCalls != 0 {
			t.Fatal("the cross-site hop linked before the session was checked")
		}
		for _, c := range hop.Result().Cookies() {
			if c.Name == oauthStateCookie && c.MaxAge < 0 {
				t.Fatal("the hop cleared the state cookie the follow-up needs")
			}
		}
		if cc := hop.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
			t.Fatalf("hop page Cache-Control = %q, want no-store: its URL carries the code", cc)
		}
		w := follow(t, f, hop, true)
		if w.Code != http.StatusFound || f.store.linkCalls != 1 {
			t.Fatalf("same-site follow-up = %d (links %d) %s, want 302 and one link", w.Code, f.store.linkCalls, w.Body.String())
		}
	})

	t.Run("hop without a session still refuses", func(t *testing.T) {
		f := start(t)
		w := follow(t, f, idpReturn(f), false)
		if w.Code != http.StatusUnauthorized || f.store.linkCalls != 0 {
			t.Fatalf("follow-up with no session = %d (links %d), want 401 and none", w.Code, f.store.linkCalls)
		}
	})

	t.Run("no hop for a callback from another browser", func(t *testing.T) {
		f := start(t)
		req := httptest.NewRequest(http.MethodGet, "/auth/oauth/stub/callback?state="+url.QueryEscape(f.state)+"&code=c", nil)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		w := httptest.NewRecorder()
		f.r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest || hopTarget.MatchString(w.Body.String()) {
			t.Fatalf("callback without the state cookie = %d %s, want the 400 refusal", w.Code, w.Body.String())
		}
	})

	t.Run("no hop when the session rides", func(t *testing.T) {
		// A Lax session cookie, or an IdP on a sibling subdomain: the
		// session arrives on the first request and the link completes.
		f := start(t)
		req := httptest.NewRequest(http.MethodGet, "/auth/oauth/stub/callback?state="+url.QueryEscape(f.state)+"&code=c", nil)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.AddCookie(f.stateCookie)
		req.AddCookie(&http.Cookie{Name: "session_id", Value: f.session})
		w := httptest.NewRecorder()
		f.r.ServeHTTP(w, req)
		if w.Code != http.StatusFound || f.store.linkCalls != 1 {
			t.Fatalf("cross-site return with the session = %d (links %d), want 302 and one link", w.Code, f.store.linkCalls)
		}
	})

	t.Run("no hop for a spent state", func(t *testing.T) {
		f := start(t)
		if w := follow(t, f, idpReturn(f), true); w.Code != http.StatusFound {
			t.Fatalf("link: %d %s", w.Code, w.Body.String())
		}
		if w := idpReturn(f); w.Code != http.StatusBadRequest || hopTarget.MatchString(w.Body.String()) {
			t.Fatalf("replayed IdP return = %d %s, want the 400 refusal", w.Code, w.Body.String())
		}
	})

	t.Run("no hop off this origin", func(t *testing.T) {
		// The router does not route these paths today; the guard is for
		// a host that mounts the callback some other way.
		plugin, _, store, _ := linkFixture(t, &OAuth2UserInfo{ID: "p", Email: "o@example.com", EmailVerified: true})
		owner := store.preExistingUser("o@example.com")
		// A backslash needs no refusal: EscapedPath writes it as %5C.
		for _, path := range []string{"//evil.example/cb", "evil.example/cb"} {
			state, err := plugin.generateState("stub", owner.GetID())
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodGet, "/cb?state="+url.QueryEscape(state)+"&code=c", nil)
			req.URL.Path = path
			req.Header.Set("Sec-Fetch-Site", "cross-site")
			req.AddCookie(&http.Cookie{Name: oauthStateCookie, Value: state})
			w := httptest.NewRecorder()
			if plugin.sameSiteHop(w, req, state, "stub") {
				t.Fatalf("hop page for path %q: %s", path, w.Body.String())
			}
		}
	})

	t.Run("no hop for a login flow", func(t *testing.T) {
		// Only the link branch needs the session; a login return mints
		// one, so it completes on the first request as before.
		_, r, _, _ := linkFixture(t, &OAuth2UserInfo{ID: "p-new", Email: "new@example.com", EmailVerified: true})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/auth/oauth/stub", nil))
		loc, _ := url.Parse(w.Header().Get("Location"))
		state := loc.Query().Get("state")
		req := httptest.NewRequest(http.MethodGet, "/auth/oauth/stub/callback?state="+url.QueryEscape(state)+"&code=c", nil)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.AddCookie(&http.Cookie{Name: oauthStateCookie, Value: state})
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusFound || sessionCookie(w) == "" {
			t.Fatalf("cross-site login return = %d %s, want 302 with a session", w.Code, w.Body.String())
		}
	})

	t.Run("no hop for a forged state", func(t *testing.T) {
		f := start(t)
		f.state += "x"
		f.stateCookie.Value = f.state
		if w := idpReturn(f); w.Code != http.StatusBadRequest || hopTarget.MatchString(w.Body.String()) {
			t.Fatalf("forged state = %d %s, want the 400 refusal", w.Code, w.Body.String())
		}
	})
}
