package admin

import (
	"net/http"
	"net/url"
	"testing"
)

// A list's filter, view, sort and page live in its query string, so a
// link to a filtered page is the path and the query. The login redirect
// kept only the path: a signed-out admin who followed a link to
// /admin/audit?actor=u1 signed in and landed on the unfiltered log.
func TestGateLoginRedirectKeepsQuery(t *testing.T) {
	x := gatedEnv(t, Config{LoginPath: "/login"})
	for _, target := range []string{
		"/admin/audit?actor=u1&op=delete&p=2",   // a screen, through the page policy
		"/admin/entities/posts?view=mine&p=3",   // an entity list
		"/admin/_count/posts?view=mine",         // a gate route
		"/admin/entities/posts/a%2Fb?tab=notes", // an escaped key keeps its escaping
	} {
		rr := get(x.as(nil), target)
		if rr.Code != http.StatusSeeOther && rr.Code != http.StatusFound {
			t.Errorf("signed-out %s = %d, want a redirect", target, rr.Code)
			continue
		}
		loc, err := url.Parse(rr.Header().Get("Location"))
		if err != nil || loc.Path != "/login" {
			t.Errorf("signed-out %s: Location %q, want the login page", target, rr.Header().Get("Location"))
			continue
		}
		if next := loc.Query().Get("next"); next != target {
			t.Errorf("signed-out %s: next = %q, want the page with its query", target, next)
		}
	}
	// A page with no query is its path, as before.
	if loc := get(x.as(nil), "/admin/queue").Header().Get("Location"); loc != "/login?next=%2Fadmin%2Fqueue" {
		t.Errorf("no query: Location %q", loc)
	}
}
