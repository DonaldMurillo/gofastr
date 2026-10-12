package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// lockedPosts is posts behind permissions the admin's roles do not hold:
// the admin reaches it only through its own elevation.
func lockedPosts() entity.EntityConfig {
	c := postsConfig()
	c.Exposure = &entity.ExposureConfig{}
	c.Exposure.Access = entity.AccessControl{Read: "posts:read", Create: "posts:write", Update: "posts:write", Delete: "posts:write"}
	return c
}

func orgDocs() entity.EntityConfig {
	return entity.EntityConfig{
		Table:  "org_docs",
		Scope:  &entity.ScopeConfig{MultiTenant: true},
		Fields: []schema.Field{{Name: "title", Type: schema.String, Required: true}},
	}.WithTimestamps(false)
}

// withPolicy puts a policy granting nothing to the admin role on every
// request, the shape an app with RBAC serves.
func withPolicy(h http.Handler) http.Handler {
	p := access.NewRolePolicy()
	_ = p.Grant("admin", "unrelated:thing")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := access.WithPolicy(r.Context(), p)
		ctx = access.WithRoles(ctx, callerHeldRoles(ctx))
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

// The admin elevates only the entities it exposes: a relation, hook or
// stat that reaches an entity it does not show keeps that entity's
// Access check.
func TestElevationStopsAtExposedEntities(t *testing.T) {
	notes := lockedPosts()
	notes.Table = "notes"
	notes.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{Read: "notes:read"}}
	x := setup(t, map[string]entity.EntityConfig{"posts": lockedPosts(), "notes": notes}, Config{Entities: []string{"posts"}}, nil)
	p := access.NewRolePolicy()
	_ = p.Grant("admin", "unrelated:thing")
	ctx := x.b.elevate(access.WithRoles(access.WithPolicy(context.Background(), p), []string{"admin"}))
	for name, want := range map[string]bool{"posts": true, "notes": false} {
		ch, err := x.app.CrudHandler(name)
		if err != nil {
			t.Fatal(err)
		}
		if got := ch.CanReadScoped(ctx); got != want {
			t.Errorf("SECURITY: elevated read of %s = %v, want %v", name, got, want)
		}
	}
}

func jsonReq(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestEntitiesNoneExposedByDefault(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, Config{}, nil)
	if rr := get(x.as(theAdmin), "/admin/entities/posts"); rr.Code != http.StatusNotFound {
		t.Fatalf("an unnamed entity = %d, want 404", rr.Code)
	}
	if rr := serve(x.as(theAdmin), jsonReq(http.MethodPost, "/admin/api/posts", `{"title":"x"}`)); rr.Code == http.StatusCreated {
		t.Fatal("SECURITY: the admin API wrote an entity it does not expose")
	}
}

func TestEntitiesUnknownNameFailsBoot(t *testing.T) {
	_, _, err := trySetup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, Config{Entities: []string{"post"}}, nil)
	if err == nil || !strings.Contains(err.Error(), `"post"`) {
		t.Fatalf("Init = %v, want the unknown name refused", err)
	}
}

func TestEntitiesNeedTheUI(t *testing.T) {
	_, _, err := trySetup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, Config{Entities: []string{"posts"}},
		func(_ *env, c *Config) { c.UI = nil })
	if err == nil || !strings.Contains(err.Error(), "Config.UI") {
		t.Fatalf("Init = %v, want Config.UI asked for", err)
	}
}

func TestEntitiesOnlyNamedAreExposed(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig(), "notes": notesConfig()},
		Config{Entities: []string{"posts"}}, nil)
	if rr := get(x.as(theAdmin), "/admin/entities/notes"); rr.Code != http.StatusNotFound {
		t.Errorf("an unnamed entity = %d, want 404", rr.Code)
	}
	if rr := get(x.as(theAdmin), "/admin/_count/notes"); rr.Code != http.StatusNotFound {
		t.Errorf("an unnamed entity's count = %d, want 404", rr.Code)
	}
	if body := get(x.as(theAdmin), "/admin").Body.String(); strings.Contains(body, "/admin/entities/notes") {
		t.Error("the shell links an unnamed entity")
	}
}

// AllEntities skips an entity with CRUD off, the credential tables'
// posture; naming one exposes it.
func TestAllEntitiesSkipsCRUDOff(t *testing.T) {
	off := notesConfig()
	no := false
	off.Exposure = &entity.ExposureConfig{CRUD: &no}
	ents := map[string]entity.EntityConfig{"posts": postsConfig(), "notes": off}
	x := setup(t, ents, Config{AllEntities: true}, nil)
	if rr := get(x.as(theAdmin), "/admin/entities/posts"); rr.Code != http.StatusOK {
		t.Errorf("a CRUD entity under AllEntities = %d", rr.Code)
	}
	if rr := get(x.as(theAdmin), "/admin/entities/notes"); rr.Code != http.StatusNotFound {
		t.Errorf("SECURITY: a CRUD-off entity under AllEntities = %d, want 404", rr.Code)
	}
}

func TestEntityListEscapesValues(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, Config{Entities: []string{"posts"}}, nil)
	x.insert("posts", map[string]any{"id": "p1", "title": "<script>alert(1)</script>", "status": "draft"})
	x.insert("posts", map[string]any{"id": "p2", "title": "Second", "status": "published"})
	body := get(x.as(theAdmin), "/admin/entities/posts").Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatal("SECURITY: the list rendered a stored script unescaped")
	}
	if !strings.Contains(body, "&lt;script&gt;") || !strings.Contains(body, "Second") {
		t.Fatalf("the list lacks its rows:\n%s", body)
	}
}

// The elevation lifts the entity's Access permissions for the admin's
// own reads and writes; the app's API keeps refusing the same caller.
func TestEntityElevationLiftsAccess(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": lockedPosts()}, Config{Entities: []string{"posts"}}, nil)
	x.insert("posts", map[string]any{"id": "p1", "title": "Locked one", "status": "draft"})
	h := withPolicy(x.as(theAdmin))
	if rr := get(h, "/posts"); rr.Code != http.StatusForbidden {
		t.Fatalf("setup: the app API let the admin read posts (%d); the test proves nothing", rr.Code)
	}
	for _, p := range []string{"/admin/entities/posts", "/admin/entities/posts/p1", "/admin/_count/posts"} {
		if body := get(h, p).Body.String(); !strings.Contains(body, "Locked one") && !strings.Contains(body, ">1<") {
			t.Errorf("the admin cannot read %s through its elevation:\n%s", p, body)
		}
	}
	if rr := serve(h, jsonReq(http.MethodPost, "/admin/api/posts", `{"title":"Made here","status":"draft"}`)); rr.Code != http.StatusCreated {
		t.Fatalf("admin create = %d %s", rr.Code, rr.Body.String())
	}
	if rr := serve(h, jsonReq(http.MethodPatch, "/admin/api/posts/p1", `{"title":"Renamed"}`)); rr.Code != http.StatusOK {
		t.Fatalf("admin update = %d %s", rr.Code, rr.Body.String())
	}
	if ops := x.auditOps("posts"); len(ops) != 2 {
		t.Fatalf("audit ops = %v, want the create and the update", ops)
	}
	// A reader who passes no gate gets nothing from the admin API.
	if rr := serve(withPolicy(x.as(aReader)), jsonReq(http.MethodDelete, "/admin/api/posts/p1", "")); rr.Code != http.StatusForbidden {
		t.Fatalf("SECURITY: a reader's admin delete = %d", rr.Code)
	}
}

// The elevation never widens tenant scope: a tenant's admin reads and
// opens only its tenant's rows, and with no tenant reads none.
func TestEntityElevationKeepsTenantScope(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"org_docs": orgDocs()}, Config{Entities: []string{"org_docs"}}, nil)
	x.insert("org_docs", map[string]any{"id": "da", "title": "doc for tenant-a", "tenant_id": "tenant-a"})
	x.insert("org_docs", map[string]any{"id": "db", "title": "doc for tenant-b", "tenant_id": "tenant-b"})
	a := asTenant(x.h, theAdmin, "tenant-a")
	list := get(a, "/admin/entities/org_docs").Body.String()
	if !strings.Contains(list, "doc for tenant-a") {
		t.Fatalf("tenant-a's admin cannot see its own doc:\n%s", list)
	}
	if strings.Contains(list, "doc for tenant-b") {
		t.Error("SECURITY: the list showed tenant-b's row to tenant-a's admin")
	}
	if body := get(a, "/admin/entities/org_docs/db").Body.String(); strings.Contains(body, "doc for tenant-b") {
		t.Error("SECURITY: tenant-a's admin opened tenant-b's record (or its title in the crumbs)")
	}
	if rr := serve(a, jsonReq(http.MethodPatch, "/admin/api/org_docs/db", `{"title":"hijacked"}`)); rr.Code == http.StatusOK {
		t.Error("SECURITY: tenant-a's admin updated tenant-b's row")
	}
	if body := get(a, "/admin/_count/org_docs").Body.String(); !strings.Contains(body, ">1<") {
		t.Errorf("tenant-a's count is not 1:\n%s", body)
	}
	if body := get(x.as(theAdmin), "/admin/entities/org_docs").Body.String(); strings.Contains(body, "doc for tenant") {
		t.Error("SECURITY: an admin with no tenant read multi-tenant rows")
	}
}

func TestEntityRecordTitleInCrumbs(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, Config{Entities: []string{"posts"}}, nil)
	x.insert("posts", map[string]any{"id": "p1", "title": "Hello world", "status": "draft"})
	body := get(x.as(theAdmin), "/admin/entities/posts/p1").Body.String()
	if !strings.Contains(body, "<title>Hello world") {
		t.Errorf("the record page's <title> is not the record's title")
	}
	if !strings.Contains(body, `aria-current="page"`) || !strings.Contains(body, "Hello world") {
		t.Errorf("the crumbs lack the record")
	}
	if body := get(x.as(theAdmin), "/admin/entities/posts/missing").Body.String(); strings.Contains(body, "Hello world") {
		t.Error("a missing record borrowed another title")
	}
}

func TestEntityCountReadsAndFormats(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig().WithTimestamps(true)}, Config{Entities: []string{"posts"}}, nil)
	for _, id := range []string{"p1", "p2", "p3"} {
		x.insert("posts", map[string]any{"id": id, "title": id, "status": "draft"})
	}
	rr := get(x.as(theAdmin), "/admin/_count/posts")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), ">3<") || !strings.Contains(rr.Body.String(), `href="/admin/entities/posts"`) {
		t.Fatalf("count = %d:\n%s", rr.Code, rr.Body.String())
	}
	dash := get(x.as(theAdmin), "/admin").Body.String()
	if !strings.Contains(dash, `data-cui-poll-src="/admin/_count/posts"`) || !strings.Contains(dash, `href="/admin/entities/posts/create"`) {
		t.Fatal("the dashboard card does not poll its count or offer New")
	}
	if strings.Contains(rr.Body.String(), "Updated") {
		t.Fatalf("rows with no updated_at drew a date:\n%s", rr.Body.String())
	}
	two := time.Now().UTC().Add(-2*time.Hour - time.Minute).Format("2006-01-02 15:04:05")
	x.insert("posts", map[string]any{"id": "p4", "title": "p4", "status": "draft", "updated_at": two})
	if body := get(x.as(theAdmin), "/admin/_count/posts").Body.String(); !strings.Contains(body, "Updated 2h ago") {
		t.Fatalf("the card lacks its newest write's age:\n%s", body)
	}
	ctx := context.Background()
	if got := countText(ctx, "12,345"); got != "10k+" {
		t.Errorf("countText(12,345) = %q", got)
	}
	if got := countText(ctx, "9,999"); got != "9,999" {
		t.Errorf("countText(9,999) = %q", got)
	}
	if got := countText(ctx, "—"); got != "—" {
		t.Errorf("countText(—) = %q", got)
	}
}

// A count that misses its deadline is not "—" when a bounded read of the
// rows answers: the card shows that many, or "10k+" past the cap, still
// in the caller's scope.
func TestCountFallsBackToBoundedRead(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig().WithTimestamps(true)}, Config{Entities: []string{"posts"}}, nil)
	for _, id := range []string{"p1", "p2", "p3"} {
		x.insert("posts", map[string]any{"id": id, "title": id, "status": "draft"})
	}
	prev := countDeadline
	countDeadline = 0 // the exact count cannot finish
	t.Cleanup(func() { countDeadline = prev })
	body := get(x.as(theAdmin), "/admin/_count/posts").Body.String()
	if !strings.Contains(body, ">3<") {
		t.Fatalf("a late count did not fall back to the bounded read:\n%s", body)
	}
}
