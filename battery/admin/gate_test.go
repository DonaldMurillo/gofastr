package admin

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/embed"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// adminPaths are the pages and routes one gate covers, with an exposed
// entity, a queue and a policy wired.
func gatedEnv(t *testing.T, cfg Config) *env {
	return setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, cfg, func(x *env, c *Config) {
		c.Entities = []string{"posts"}
		c.Queue = newDBQueue(t, x.db)
	})
}

var gatedPages = []string{
	"/admin", "/admin/search", "/admin/queue", "/admin/audit",
	"/admin/entities/posts", "/admin/entities/posts/create", "/admin/entities/posts/p1",
	"/admin/_count/posts",
}

func TestGateRefusesAnonymous(t *testing.T) {
	x := gatedEnv(t, Config{})
	for _, p := range gatedPages {
		if rr := get(x.as(nil), p); rr.Code != http.StatusUnauthorized {
			t.Errorf("anonymous GET %s = %d, want 401", p, rr.Code)
		}
	}
	if rr := post(x.as(nil), "/admin/_palette", url.Values{"q": {"a"}}); rr.Code != http.StatusUnauthorized {
		t.Errorf("anonymous palette = %d, want 401", rr.Code)
	}
}

func TestGateRefusesNonAdmin(t *testing.T) {
	x := gatedEnv(t, Config{})
	for _, user := range []any{aReader, struct{}{}} {
		for _, p := range gatedPages {
			if rr := get(x.as(user), p); rr.Code != http.StatusForbidden {
				t.Errorf("%T GET %s = %d, want 403", user, p, rr.Code)
			}
		}
		rr := post(x.as(user), "/admin/api/posts", url.Values{"title": {"x"}})
		if rr.Code != http.StatusForbidden {
			t.Errorf("%T create = %d, want 403", user, rr.Code)
		}
	}
	var n int
	_ = x.db.QueryRow(`SELECT COUNT(*) FROM posts`).Scan(&n)
	if n != 0 {
		t.Fatalf("a refused create wrote %d rows", n)
	}
}

func TestGateAdmitsAdmin(t *testing.T) {
	x := gatedEnv(t, Config{})
	for _, p := range []string{"/admin", "/admin/search", "/admin/queue", "/admin/audit", "/admin/entities/posts"} {
		if rr := get(x.as(theAdmin), p); rr.Code != http.StatusOK {
			t.Errorf("admin GET %s = %d, want 200", p, rr.Code)
		}
	}
}

func TestGateHonorsAdminRole(t *testing.T) {
	x := gatedEnv(t, Config{AdminRole: "ops"})
	if rr := get(x.as(theAdmin), "/admin"); rr.Code != http.StatusForbidden {
		t.Errorf("admin role under AdminRole ops = %d, want 403", rr.Code)
	}
	if rr := get(x.as(roleUser{id: "o", roles: []string{"ops"}}), "/admin"); rr.Code != http.StatusOK {
		t.Errorf("ops role = %d, want 200", rr.Code)
	}
}

func TestGateSendsSignedOutToLogin(t *testing.T) {
	x := gatedEnv(t, Config{LoginPath: "/login"})
	rr := get(x.as(nil), "/admin/queue")
	if rr.Code != http.StatusSeeOther && rr.Code != http.StatusFound {
		t.Fatalf("signed-out page = %d, want a redirect", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/login?next=%2Fadmin%2Fqueue" {
		t.Errorf("Location = %q", loc)
	}
	// The gate's own routes send a signed-out GET to log in too; a
	// signed-out post is refused, since a redirect would drop its body.
	rr = get(x.as(nil), "/admin/_count/posts")
	if loc := rr.Header().Get("Location"); rr.Code != http.StatusSeeOther || loc != "/login?next=%2Fadmin%2F_count%2Fposts" {
		t.Errorf("signed-out count = %d, Location %q", rr.Code, loc)
	}
	if rr := post(x.as(nil), "/admin/queue/_replay/j1", nil); rr.Code != http.StatusUnauthorized {
		t.Errorf("signed-out post = %d, want 401", rr.Code)
	}
	// A signed-in caller without the role is refused, not sent to log in.
	if rr := get(x.as(aReader), "/admin/queue"); rr.Code != http.StatusForbidden {
		t.Errorf("reader = %d, want 403", rr.Code)
	}
}

func TestGateRefusesAnEmbedGrant(t *testing.T) {
	grant := func(ctx context.Context) context.Context {
		return embed.WithGrant(ctx, embed.Grant{Surface: "reports", Subject: "root", Scopes: []string{"reports:read"}, Origin: "https://acme.example"})
	}
	ctx := grant(handler.SetUser(context.Background(), theAdmin))
	if (&Battery{}).authorized(ctx) {
		t.Error("an embed grant reached the back office")
	}
	open := &Battery{cfg: Config{Authorize: func(context.Context) bool { return true }}}
	if open.authorized(ctx) {
		t.Error("Authorize lifted the embed refusal")
	}
	if !(&Battery{}).authorized(handler.SetUser(context.Background(), theAdmin)) {
		t.Error("an admin session was refused")
	}
}

func TestGateHonorsADeciderDeny(t *testing.T) {
	deny := func(context.Context, []string, access.Permission, access.Ref) access.Decision {
		return access.DecisionDeny
	}
	ctx := access.WithDecider(handler.SetUser(context.Background(), theAdmin), deny)
	if (&Battery{}).authorized(ctx) {
		t.Error("a Decider's deny admitted an admin")
	}
	open := &Battery{cfg: Config{Authorize: func(context.Context) bool { return true }}}
	if open.authorized(ctx) {
		t.Error("Authorize lifted a Decider's deny")
	}
	abstain := func(context.Context, []string, access.Permission, access.Ref) access.Decision {
		return access.DecisionAbstain
	}
	if !(&Battery{}).authorized(access.WithDecider(handler.SetUser(context.Background(), theAdmin), abstain)) {
		t.Error("an abstaining Decider refused an admin")
	}
}

func TestGateHeadersOnEveryAnswer(t *testing.T) {
	x := gatedEnv(t, Config{})
	for _, p := range []string{"/admin", "/admin/entities/posts", "/admin/_count/posts"} {
		for _, user := range []any{nil, theAdmin} {
			rr := get(x.as(user), p)
			if got := rr.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
				t.Errorf("%s (user %v) Cache-Control = %q, want no-store", p, user, got)
			}
			if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Errorf("%s (user %v) has no nosniff", p, user)
			}
		}
	}
	rr := post(x.as(theAdmin), "/admin/_palette", url.Values{"q": {"po"}})
	if rr.Code != http.StatusOK || !strings.Contains(rr.Header().Get("Cache-Control"), "no-store") {
		t.Errorf("palette answered %d with Cache-Control %q, want 200 no-store", rr.Code, rr.Header().Get("Cache-Control"))
	}
}

func TestGateRefusesCrossSitePosts(t *testing.T) {
	x := gatedEnv(t, Config{})
	for _, p := range []string{"/admin/api/posts", "/admin/queue/_replay/j1", "/admin/_palette"} {
		req := newPost(p, url.Values{"title": {"x"}})
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.Header.Set("Origin", "https://evil.example")
		rr := serve(x.as(theAdmin), req)
		if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "cross-site") {
			t.Errorf("cross-site POST %s = %d %q, want the cross-site 403", p, rr.Code, rr.Body.String())
		}
	}
	var n int
	_ = x.db.QueryRow(`SELECT COUNT(*) FROM posts`).Scan(&n)
	if n != 0 {
		t.Fatalf("a cross-site create wrote %d rows", n)
	}
	// Same-origin still works.
	req := newPost("/admin/_palette", url.Values{"q": {"a"}})
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if rr := serve(x.as(theAdmin), req); rr.Code != http.StatusOK {
		t.Errorf("same-origin palette = %d, want 200", rr.Code)
	}
}
