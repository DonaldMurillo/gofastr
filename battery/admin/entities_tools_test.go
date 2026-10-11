package admin

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// trashedPosts is lockedPosts with soft delete: the admin reaches its
// trash only through its elevation.
func trashedPosts() entity.EntityConfig {
	c := lockedPosts()
	c.Scope = &entity.ScopeConfig{SoftDelete: true}
	return c
}

// statefulPosts is lockedPosts whose status is a state machine.
func statefulPosts() entity.EntityConfig {
	c := lockedPosts()
	c.States = &entity.StatesConfig{
		Field:       "status",
		Transitions: []entity.Transition{{Key: "publish", From: []string{"draft"}, To: "published"}},
	}
	return c
}

// withGrant serves as user under a policy whose admin role holds perms.
func (x *env) withGrant(user roleUser, perms ...access.Permission) http.Handler {
	p := access.NewRolePolicy()
	p.Register(perms...)
	if err := p.Grant("admin", perms...); err != nil {
		x.t.Fatal(err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := handler.SetUser(r.Context(), user)
		ctx = access.WithPolicy(ctx, p)
		ctx = access.WithRoles(ctx, user.roles)
		x.h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (x *env) postStatus(id string) string {
	x.t.Helper()
	var s string
	if err := x.db.QueryRow(`SELECT status FROM posts WHERE id = ?`, id).Scan(&s); err != nil {
		x.t.Fatal(err)
	}
	return s
}

// The admin's list offers the trash of a soft-deleting entity, and its
// Restore and Delete permanently post to routes the admin mounts behind
// its gate and elevation.
func TestAdminTrashRestoresAndPurges(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": trashedPosts()}, Config{Entities: []string{"posts"}}, nil)
	x.insert("posts", map[string]any{"id": "p1", "title": "Binned one", "status": "draft", "deleted_at": "2026-01-01T00:00:00Z"})
	x.insert("posts", map[string]any{"id": "p2", "title": "Binned two", "status": "draft", "deleted_at": "2026-01-01T00:00:00Z"})
	h := withPolicy(x.as(theAdmin))

	if body := get(h, "/admin/entities/posts?view=deleted").Body.String(); !strings.Contains(body, "Binned one") {
		t.Fatalf("the admin's trash view does not list the trashed row:\n%s", body)
	}
	if rr := serve(withPolicy(x.as(aReader)), jsonReq(http.MethodPost, "/admin/api/posts/p1/_restore", `{}`)); rr.Code != http.StatusForbidden {
		t.Fatalf("SECURITY: a reader's admin restore = %d", rr.Code)
	}
	if rr := serve(h, jsonReq(http.MethodPost, "/admin/api/posts/p1/_restore", `{}`)); rr.Code != http.StatusOK {
		t.Fatalf("admin restore = %d %s", rr.Code, rr.Body.String())
	}
	body := get(h, "/admin/entities/posts").Body.String()
	if !strings.Contains(body, "Binned one") {
		t.Fatalf("the restored row is not live:\n%s", body)
	}
	// The admin's tabs count their rows: one live, one still trashed.
	if !regexp.MustCompile(`>All<span [^>]*class="fui-tab-nav__badge"[^>]*>1</span>`).MatchString(body) ||
		!regexp.MustCompile(`>Deleted<span [^>]*class="fui-tab-nav__badge"[^>]*>1</span>`).MatchString(body) {
		t.Fatalf("the admin's view tabs carry no counts:\n%s", body)
	}
	if rr := serve(h, jsonReq(http.MethodPost, "/admin/api/posts/p2/_purge", `{}`)); rr.Code != http.StatusOK {
		t.Fatalf("admin purge = %d %s", rr.Code, rr.Body.String())
	}
	var n int
	if err := x.db.QueryRow(`SELECT COUNT(*) FROM posts WHERE id = 'p2'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("the purged row is still stored")
	}
}

// The admin's elevation lifts the entity's update permission, never the
// override capability: an admin without <entity>:override_state is
// refused and sees no form; one holding it overrides, audited.
func TestAdminOverrideNeedsCapability(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": statefulPosts()}, Config{Entities: []string{"posts"}}, nil)
	x.insert("posts", map[string]any{"id": "p1", "title": "Stuck one", "status": "published"})
	body := `{"state":"draft","reason":"published by mistake"}`

	plain := withPolicy(x.as(theAdmin))
	if page := get(plain, "/admin/entities/posts/p1").Body.String(); strings.Contains(page, "published by mistake") || strings.Contains(page, "-override") {
		t.Fatal("SECURITY: the override form is drawn for an admin without the capability")
	}
	rr := serve(plain, jsonReq(http.MethodPost, "/admin/api/posts/p1/_override", body))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("SECURITY: an elevated admin without the capability overrode: %d", rr.Code)
	}
	if msg := rr.Body.String(); strings.Contains(msg, "{") && strings.Contains(msg, "{entity}") {
		t.Errorf("the refusal shows a raw placeholder: %s", msg)
	}
	if s := x.postStatus("p1"); s != "published" {
		t.Fatalf("a refused override changed the status to %q", s)
	}

	granted := x.withGrant(theAdmin, "posts:override_state")
	if page := get(granted, "/admin/entities/posts/p1").Body.String(); !strings.Contains(page, "-override") {
		t.Fatalf("the override form is missing for an admin holding the capability:\n%s", page)
	}
	if rr := serve(granted, jsonReq(http.MethodPost, "/admin/api/posts/p1/_override", body)); rr.Code != http.StatusOK {
		t.Fatalf("admin override = %d %s", rr.Code, rr.Body.String())
	}
	if s := x.postStatus("p1"); s != "draft" {
		t.Fatalf("status = %q after the override, want draft", s)
	}
	if ops := x.auditOps("posts"); len(ops) != 1 || ops[0] != "state_override" {
		t.Fatalf("audit ops = %v, want one state_override", ops)
	}
}

// Every tool on one entity mounts without a route conflict: a stateful,
// soft-deleting entity with saved views on. The saved-view delete route
// once overlapped the transitions route and panicked Init.
func TestAdminToolsMountTogether(t *testing.T) {
	c := statefulPosts()
	c.Scope = &entity.ScopeConfig{SoftDelete: true}
	x := setup(t, map[string]entity.EntityConfig{"posts": c}, Config{Entities: []string{"posts"}, SavedViews: true}, nil)
	h := x.as(theAdmin)
	rr := serve(h, newPost("/admin/api/posts/_views", url.Values{
		"name": {"Drafts"}, "filter": {`status = "draft"`}, "back": {"/admin/entities/posts"},
	}))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("save view = %d %s", rr.Code, rr.Body.String())
	}
	saved, err := url.Parse(rr.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	id := saved.Query().Get("saved")
	rr = serve(h, newPost("/admin/api/posts/_views/_delete/"+url.PathEscape(id), url.Values{"back": {"/admin/entities/posts"}}))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("delete view = %d %s", rr.Code, rr.Body.String())
	}
	if page := get(h, "/admin/entities/posts").Body.String(); strings.Contains(page, "Drafts") {
		t.Fatal("the deleted view is still listed")
	}
}

// With Config.SavedViews the admin's list saves a view per admin: the
// saving admin sees and opens it, another admin does not see it.
func TestAdminSavedViewsPerAdmin(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, Config{Entities: []string{"posts"}, SavedViews: true}, nil)
	x.insert("posts", map[string]any{"id": "p1", "title": "Draft one", "status": "draft"})
	x.insert("posts", map[string]any{"id": "p2", "title": "Live one", "status": "published"})
	h := x.as(theAdmin)

	rr := serve(h, newPost("/admin/api/posts/_views", url.Values{
		"name": {"Drafts only"}, "filter": {`status = "draft"`}, "back": {"/admin/entities/posts"},
	}))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("save view = %d %s", rr.Code, rr.Body.String())
	}
	loc := rr.Header().Get("Location")
	if !strings.HasPrefix(loc, "/admin/entities/posts?") || !strings.Contains(loc, "saved=") {
		t.Fatalf("save view redirected to %q, want the list with the view open", loc)
	}
	page := get(h, loc).Body.String()
	if !strings.Contains(page, "Drafts only") || !strings.Contains(page, "Draft one") || strings.Contains(page, "Live one") {
		t.Fatalf("the opened view does not narrow the list:\n%s", page)
	}

	other := x.as(roleUser{id: "admin-2", roles: []string{"admin"}})
	page = get(other, loc).Body.String()
	if strings.Contains(page, "Drafts only") {
		t.Fatal("SECURITY: another admin sees the first admin's saved view")
	}
	if !strings.Contains(page, "Live one") {
		t.Fatal("another admin opening a foreign view id got a narrowed list")
	}
	if vs, err := x.b.SavedViews().List(context.Background(), "posts"); err == nil && len(vs) != 0 {
		t.Fatal("SECURITY: the store listed views for a context with no user")
	}
}
