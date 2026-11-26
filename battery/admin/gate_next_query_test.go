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
}

func TestGateLoginRedirectBeforeFragment(t *testing.T) {
	cases := []struct {
		name      string
		loginPath string
		wantLoc   func(next string) string
	}{
		{
			name:      "fragment only",
			loginPath: "/auth#login",
			wantLoc:   func(next string) string { return "/auth?next=" + url.QueryEscape(next) + "#login" },
		},
		{
			name:      "existing query",
			loginPath: "/login?x=1",
			wantLoc:   func(next string) string { return "/login?next=" + url.QueryEscape(next) + "&x=1" },
		},
		{
			name:      "query and fragment",
			loginPath: "/auth?tab=1#login",
			wantLoc:   func(next string) string { return "/auth?next=" + url.QueryEscape(next) + "&tab=1#login" },
		},
		{
			name:      "question mark inside fragment",
			loginPath: "/auth#step?a=1",
			wantLoc:   func(next string) string { return "/auth?next=" + url.QueryEscape(next) + "#step?a=1" },
		},
	}
	targets := []struct {
		kind string
		path string
	}{
		{kind: "screen", path: "/admin/audit?actor=u1"},
		{kind: "gate", path: "/admin/_count/posts?view=mine"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x := gatedEnv(t, Config{LoginPath: tc.loginPath})
			for _, target := range targets {
				t.Run(target.kind, func(t *testing.T) {
					rr := get(x.as(nil), target.path)
					if rr.Code != http.StatusSeeOther && rr.Code != http.StatusFound {
						t.Fatalf("signed-out %s = %d, want redirect", target.path, rr.Code)
					}
					want := tc.wantLoc(target.path)
					if got := rr.Header().Get("Location"); got != want {
						t.Errorf("Location = %q, want %q", got, want)
					}
				})
			}
		})
	}
}
