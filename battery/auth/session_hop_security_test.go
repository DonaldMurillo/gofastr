package auth

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// hopRig is a host app behind SessionMiddleware: a public page, pages
// guarded by RequireSession (redirect and 401), and redirects of its own.
type hopRig struct {
	h      http.Handler
	cookie string
	runs   map[string]int
}

func newHopRig(t *testing.T) *hopRig {
	t.Helper()
	mgr, users := newTestManager(t)
	u := seedUser(t, users, "ada@example.com", "a-long-password-1")
	sess, err := mgr.SessionStore().Create(context.Background(), u.GetID(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	g := &hopRig{cookie: sess.Token, runs: map[string]int{}}
	page := func(name string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			g.runs[name]++
			_, _ = io.WriteString(w, name+" page")
		})
	}
	mux := http.NewServeMux()
	mux.Handle("/public", page("public"))
	mux.Handle("/login", page("login"))
	mux.Handle("/app/", RequireSession(WithRedirectOnFail("/login"))(page("app")))
	mux.Handle("/data/", RequireSession()(page("data")))
	mux.HandleFunc("/moved", func(w http.ResponseWriter, r *http.Request) {
		g.runs["moved"]++
		http.Redirect(w, r, "/public", http.StatusFound)
	})
	mux.HandleFunc("/own-guard", func(w http.ResponseWriter, r *http.Request) {
		if GetCurrentUser(r.Context()) == nil {
			handler.NoteSessionRefusal(r.Context())
			http.Redirect(w, r, "/login?next=%2Fown-guard", http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, "own page")
	})
	g.h = SessionMiddleware(mgr, WithSessionLogger(nil))(mux)
	return g
}

// navigation is the Fetch Metadata of a top-level page load.
func navigation(site string) http.Header {
	return http.Header{
		"Sec-Fetch-Site": {site}, "Sec-Fetch-Mode": {"navigate"}, "Sec-Fetch-Dest": {"document"},
		"Accept": {"text/html"},
	}
}

func (g *hopRig) do(method, target string, h http.Header, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range h {
		req.Header[k] = v
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	}
	w := httptest.NewRecorder()
	g.h.ServeHTTP(w, req)
	return w
}

// A link followed on another site reaches a guarded page with no
// SameSite=Strict cookie (issue 696). The refusal is answered with the
// hop page, which reloads the same path and query from this origin.
func TestCrossSiteArrivalAtGuardedPageHops(t *testing.T) {
	g := newHopRig(t)
	for _, target := range []string{
		"/app/orders?status=open&q=two%20words&next=https%3A%2F%2Fevil.example%2F",
		"/data/report.json?page=2",
		"/own-guard",
	} {
		w := g.do(http.MethodGet, target, navigation("cross-site"), "")
		if w.Code != http.StatusOK || w.Header().Get("Refresh") != "0;url="+target {
			t.Fatalf("GET %s = %d Refresh %q, want the hop to the same path and query", target, w.Code, w.Header().Get("Refresh"))
		}
		if loc := w.Header().Get("Location"); loc != "" {
			t.Errorf("GET %s: the hop kept Location %q", target, loc)
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("GET %s: hop Cache-Control = %q, want no-store", target, cc)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("GET %s: hop Content-Type = %q, want the hop page's", target, ct)
		}
		if !strings.Contains(w.Body.String(), "Opening the page") || strings.Contains(w.Body.String(), "login_required") {
			t.Errorf("GET %s: body = %q, want only the hop page", target, w.Body.String())
		}
	}
	if w := g.do(http.MethodHead, "/app/orders", navigation("cross-site"), ""); w.Header().Get("Refresh") != "0;url=/app/orders" {
		t.Errorf("HEAD hop Refresh = %q", w.Header().Get("Refresh"))
	}
	if g.runs["app"]+g.runs["data"] != 0 {
		t.Fatalf("SECURITY: a hop ran a guarded page: %v", g.runs)
	}
}

// The reload is same-origin, so it is never hopped again: with the
// cookie it gets the page, and a signed-out visitor gets the refusal.
func TestCrossSiteArrivalHopDoesNotLoop(t *testing.T) {
	g := newHopRig(t)
	if w := g.do(http.MethodGet, "/app/orders?status=open", navigation("same-origin"), g.cookie); w.Code != http.StatusOK || w.Body.String() != "app page" {
		t.Fatalf("reload with the session = %d %q, want the page", w.Code, w.Body.String())
	}
	w := g.do(http.MethodGet, "/app/orders?status=open", navigation("same-origin"), "")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" || w.Header().Get("Refresh") != "" {
		t.Fatalf("signed-out reload = %d Location %q Refresh %q, want the login redirect", w.Code, w.Header().Get("Location"), w.Header().Get("Refresh"))
	}
	if w := g.do(http.MethodGet, "/data/report.json", navigation("same-origin"), ""); w.Code != http.StatusUnauthorized || w.Header().Get("Refresh") != "" {
		t.Fatalf("signed-out reload of a 401 page = %d Refresh %q, want 401", w.Code, w.Header().Get("Refresh"))
	}
}

// Only a refusal hops. A public page, and a redirect no gate noted,
// reach a cross-site visitor as they are, run once.
func TestCrossSiteArrivalAtPublicPageIsServedDirectly(t *testing.T) {
	g := newHopRig(t)
	w := g.do(http.MethodGet, "/public?x=1", navigation("cross-site"), "")
	if w.Code != http.StatusOK || w.Body.String() != "public page" || w.Header().Get("Refresh") != "" {
		t.Fatalf("public page = %d %q Refresh %q, want it served directly", w.Code, w.Body.String(), w.Header().Get("Refresh"))
	}
	w = g.do(http.MethodGet, "/moved", navigation("cross-site"), "")
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/public" || w.Header().Get("Refresh") != "" {
		t.Fatalf("a redirect that is no refusal = %d Location %q Refresh %q, want it untouched", w.Code, w.Header().Get("Location"), w.Header().Get("Refresh"))
	}
	if g.runs["public"] != 1 || g.runs["moved"] != 1 {
		t.Fatalf("runs = %v, want each handler once", g.runs)
	}
}

// Anything but a cookieless top-level GET or HEAD from another site
// gets the gate's own answer.
// The hop asks handler.IsCrossSiteRequest, the one cross-site predicate:
// a sibling subdomain's navigation that cannot prove its origin counts.
func TestHopUsesTheSharedCrossSitePredicate(t *testing.T) {
	g := newHopRig(t)
	if w := g.do(http.MethodGet, "/app/orders", navigation("same-site"), ""); w.Code != http.StatusOK || w.Header().Get("Refresh") != "0;url=/app/orders" {
		t.Fatalf("same-site navigation = %d Refresh %q, want the hop", w.Code, w.Header().Get("Refresh"))
	}
	if w := g.do(http.MethodGet, "/app/orders", navigation("same-origin"), ""); w.Code != http.StatusSeeOther || w.Header().Get("Refresh") != "" {
		t.Fatalf("the hop's reload = %d Refresh %q, want the gate's 303", w.Code, w.Header().Get("Refresh"))
	}
}

func TestOnlyCookielessCrossSiteNavigationsHop(t *testing.T) {
	g := newHopRig(t)
	with := func(site, mode, dest string, more ...string) http.Header {
		h := http.Header{"Accept": {"text/html"}}
		for k, v := range map[string]string{"Sec-Fetch-Site": site, "Sec-Fetch-Mode": mode, "Sec-Fetch-Dest": dest} {
			if v != "" {
				h.Set(k, v)
			}
		}
		for i := 0; i+1 < len(more); i += 2 {
			h.Set(more[i], more[i+1])
		}
		return h
	}
	cases := []struct {
		name, method, cookie string
		h                    http.Header
	}{
		{"fetch", http.MethodGet, "", with("cross-site", "cors", "empty")},
		{"same-origin fetch (island, poll, RPC read)", http.MethodGet, "", with("same-origin", "cors", "empty", "X-Requested-With", "gofastr")},
		{"event stream", http.MethodGet, "", with("cross-site", "cors", "empty", "Accept", "text/event-stream, text/html")},
		{"frame", http.MethodGet, "", with("cross-site", "navigate", "iframe")},
		{"image", http.MethodGet, "", with("cross-site", "no-cors", "image")},
		{"prefetch with no mode", http.MethodGet, "", with("cross-site", "", "document")},
		{"typed address", http.MethodGet, "", with("none", "navigate", "document")},
		{"same-origin navigation", http.MethodGet, "", with("same-origin", "navigate", "document")},
		{"no Fetch Metadata", http.MethodGet, "", with("", "", "")},
		{"form post", http.MethodPost, "", with("cross-site", "navigate", "document")},
		{"put", http.MethodPut, "", with("cross-site", "navigate", "document")},
		{"a dead cookie was sent", http.MethodGet, "not-a-session", with("cross-site", "navigate", "document")},
	}
	for _, c := range cases {
		for target, want := range map[string]int{"/app/orders": http.StatusSeeOther, "/data/report.json": http.StatusUnauthorized, "/own-guard": http.StatusFound} {
			w := g.do(c.method, target, c.h, c.cookie)
			if w.Code != want || w.Header().Get("Refresh") != "" {
				t.Errorf("%s: %s %s = %d Refresh %q, want the gate's %d and no hop", c.name, c.method, target, w.Code, w.Header().Get("Refresh"), want)
			}
		}
	}
	// A live cookie on a cross-site request is a session like any other.
	if w := g.do(http.MethodGet, "/app/orders", navigation("cross-site"), g.cookie); w.Code != http.StatusOK || w.Body.String() != "app page" {
		t.Errorf("cross-site navigation with a live cookie = %d %q, want the page", w.Code, w.Body.String())
	}
}

// The hop names only this request's own path and query, so no arrival
// can make it send the browser to another origin.
func TestCrossSiteArrivalHopIsNoOpenRedirect(t *testing.T) {
	g := newHopRig(t)
	guard := SessionMiddleware(mustManager(t), WithSessionLogger(nil))(RequireSession()(http.NotFoundHandler()))
	for _, path := range []string{"//evil.example/x", "//evil.example", "/\\evil.example/x", "/data/x"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.URL = &url.URL{Path: path, RawQuery: "next=//evil.example&u=https://evil.example/"}
		req.Header = navigation("cross-site")
		w := httptest.NewRecorder()
		guard.ServeHTTP(w, req)
		refresh := w.Header().Get("Refresh")
		if refresh == "" {
			if w.Code != http.StatusUnauthorized {
				t.Errorf("path %q = %d, want the plain 401 when no hop is safe", path, w.Code)
			}
			continue
		}
		target := strings.TrimPrefix(refresh, "0;url=")
		u, err := url.Parse(target)
		if err != nil || u.Host != "" || u.Scheme != "" || !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") || strings.HasPrefix(target, "/\\") {
			t.Errorf("SECURITY: path %q hops to %q, which leaves this origin", path, target)
		}
		if u != nil && u.RawQuery != req.URL.RawQuery {
			t.Errorf("path %q: hop query = %q, want it kept exactly", path, u.RawQuery)
		}
	}
	// A query that names another origin stays a query of this page.
	w := g.do(http.MethodGet, "/app/x?next=https://evil.example/&r=//evil.example", navigation("cross-site"), "")
	if got := w.Header().Get("Refresh"); got != "0;url=/app/x?next=https://evil.example/&r=//evil.example" {
		t.Errorf("Refresh = %q, want the request's own path and query", got)
	}
}

// The hop replaces the refusal whole: the gate's body and the headers
// that described it are gone, cookies it set stay.
func TestCrossSiteArrivalHopReplacesTheRefusal(t *testing.T) {
	refuse := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Length", "9")
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.SetCookie(w, &http.Cookie{Name: "flash", Value: "1"})
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "refused!!")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	})
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header = navigation("cross-site")
	w := httptest.NewRecorder()
	SessionMiddleware(mustManager(t), WithSessionLogger(nil))(refuse).ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Header().Get("Refresh") != "0;url=/x" || strings.Contains(w.Body.String(), "refused") {
		t.Fatalf("= %d Refresh %q body %q, want only the hop page", w.Code, w.Header().Get("Refresh"), w.Body.String())
	}
	for _, k := range []string{"Content-Encoding", "Content-Length", "WWW-Authenticate"} {
		if v := w.Header().Get(k); v != "" {
			t.Errorf("the hop kept the refusal's %s: %q", k, v)
		}
	}
	if !strings.Contains(w.Header().Get("Set-Cookie"), "flash=1") {
		t.Errorf("the hop dropped a cookie the gate set")
	}
}

func mustManager(t *testing.T) *AuthManager {
	t.Helper()
	mgr, _ := newTestManager(t)
	return mgr
}
