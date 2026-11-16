package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/queue"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// A columned App.View is mounted read-only; the admin serves no write
// for it and its screens draw none.
func TestReadOnlyViewGetsNoAdminWrites(t *testing.T) {
	db := newDB(t)
	if err := framework.EnsureAuditTable(db, ""); err != nil {
		t.Fatal(err)
	}
	site := appui.NewApp("admin-test")
	app := framework.NewUIHostApp(uihost.New(site), framework.WithDB(db), framework.WithoutDefaultMiddleware())
	app.Entity("posts", postsConfig())
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE VIEW open_posts AS SELECT id, title FROM posts`,
		`INSERT INTO posts (id, title, status) VALUES ('p1', 'hello', 'draft')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	app.View(migrate.View{
		Name: "open_posts", Select: "SELECT id, title FROM posts", DependsOn: []string{"posts"},
		Columns: []migrate.Column{{Name: "id", Type: schema.String, PrimaryKey: true}, {Name: "title", Type: schema.String}},
	})
	app.WithAuditLog(framework.AuditConfig{})
	b := New(Config{AllEntities: true, UI: app.EntityUI(entityui.Extensions{})})
	if err := b.Init(app); err != nil {
		t.Fatal(err)
	}
	x := &env{t: t, db: db, app: app, site: site, h: app.Router(), b: b}

	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/admin/api/open_posts"},
		{http.MethodPut, "/admin/api/open_posts/p1"},
		{http.MethodPatch, "/admin/api/open_posts/p1"},
		{http.MethodDelete, "/admin/api/open_posts/p1"},
		{http.MethodPost, "/admin/api/open_posts/_bulk"},
		{http.MethodPost, "/admin/api/open_posts/_pick"},
	} {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader(`{"title":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		if rr := serve(x.as(theAdmin), req); rr.Code != http.StatusNotFound && rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d, want no such route", c.method, c.path, rr.Code)
		}
	}
	for _, p := range []string{"/admin", "/admin/entities/open_posts", "/admin/entities/open_posts/p1"} {
		rr := get(x.as(theAdmin), p)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s = %d", p, rr.Code)
		}
		for _, write := range []string{"/admin/entities/open_posts/create", "/admin/api/open_posts"} {
			if strings.Contains(rr.Body.String(), write) {
				t.Errorf("%s offers %s for a read-only view", p, write)
			}
		}
	}
	if !strings.Contains(get(x.as(theAdmin), "/admin/entities/open_posts").Body.String(), "hello") {
		t.Error("the view's list lost its rows")
	}
	// The writable entity beside it keeps its routes.
	if body := get(x.as(theAdmin), "/admin/entities/posts").Body.String(); !strings.Contains(body, "/admin/entities/posts/create") {
		t.Error("posts lost its New link")
	}
}

// Every New link asks one gate: the dashboard, its count fragment and
// the palette agree whichever of read or create the caller is refused.
func TestNewLinkNeedsReadAndCreateEverywhere(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": lockedPosts()}, Config{Entities: []string{"posts"}, DashboardNew: "posts"}, nil)
	const link = "/admin/entities/posts/create"
	for _, denied := range []string{"", "posts:read", "posts:write"} {
		with := func(req *http.Request) *http.Request {
			if denied == "" {
				return req
			}
			return req.WithContext(access.WithDecider(req.Context(), func(_ context.Context, _ []string, p access.Permission, _ access.Ref) access.Decision {
				if p == access.Permission(denied) {
					return access.DecisionDeny
				}
				return access.DecisionAbstain
			}))
		}
		for name, req := range map[string]*http.Request{
			"dashboard": httptest.NewRequest(http.MethodGet, "/admin", nil),
			"count":     httptest.NewRequest(http.MethodGet, "/admin/_count/posts", nil),
			"palette":   newPost("/admin/_palette", url.Values{"q": {""}}),
		} {
			rr := serve(x.as(theAdmin), with(req))
			if got := strings.Contains(rr.Body.String(), link); rr.Code != http.StatusOK || got != (denied == "") {
				t.Errorf("deny %q: %s = %d, offers New=%v", denied, name, rr.Code, got)
			}
		}
	}
}

// Replay answers nil for a job that is not failed; the admin neither
// audits nor reports that as a replay.
func TestReplayOfAJobThatIsNotFailedDoesNothing(t *testing.T) {
	q := newDBQueue(t, newDB(t))
	if err := q.Enqueue(context.Background(), queue.Job{Type: "send.email"}); err != nil {
		t.Fatal(err)
	}
	pending, err := q.ListJobs(context.Background(), "pending", 1, 0)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending jobs: %v %v", pending, err)
	}
	x := queueEnv(t, q)
	for _, id := range []string{"no-such-job", pending[0].ID} {
		rr := rpc(x.as(theAdmin), "/admin/queue/_replay/"+id, map[string]any{})
		if rr.Code != http.StatusConflict || strings.Contains(rr.Header().Get("X-Gofastr-Toast"), "success") {
			t.Errorf("replay %s = %d, toast %q", id, rr.Code, rr.Header().Get("X-Gofastr-Toast"))
		}
		if got := resultOf(t, post(x.as(theAdmin), "/admin/queue/_replay/"+id, nil)); got != "replay-nothing" {
			t.Errorf("replay %s result = %q", id, got)
		}
	}
	if ops := x.auditOps("queue"); len(ops) != 0 {
		t.Errorf("audit rows for replays that replayed nothing: %v", ops)
	}
	if body := get(x.as(theAdmin), "/admin/queue?result=replay-nothing").Body.String(); !strings.Contains(body, "no longer failed") {
		t.Error("the jobs page draws no notice for replay-nothing")
	}
}

// A ?status= that names no status lists All, and says so, as the audit
// page does for a bad ?op=.
func TestUnknownFilterValueWarns(t *testing.T) {
	const warning = "Filter not applied"
	qx := queueEnv(t, &fakeQueue{})
	for path, want := range map[string]bool{
		"/admin/queue?status=Failed": true,
		"/admin/queue?status=failed": false,
		"/admin/queue?status=":       false,
		"/admin/queue":               false,
	} {
		if got := strings.Contains(get(qx.as(theAdmin), path).Body.String(), warning); got != want {
			t.Errorf("%s warns=%v, want %v", path, got, want)
		}
	}
}

// Every op the admin's own pages write on success is one the audit
// filter takes, and a module lever is tinted and drawn by its verb.
func TestAuditOpsCoverWhatTheAdminWrites(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, Config{Entities: []string{"posts"}}, nil)
	for _, op := range []string{"set-name", "replay", "grant", "revoke", "assign-roles", opModuleEnable, opModuleDisable, opModuleBump, opModuleRevoke} {
		if _, warned := x.b.parseAuditFilter(url.Values{"op": {op}}); len(warned) != 0 {
			t.Errorf("?op=%s is refused", op)
		}
	}
	for op, want := range map[string]ui.StatusVariant{
		opModuleEnable: ui.StatusSuccess, opModuleDisable: ui.StatusDanger,
		opModuleRevoke: ui.StatusDanger, opModuleBump: ui.StatusInfo,
	} {
		if got := opVariant(op); got != want {
			t.Errorf("opVariant(%s) = %v, want %v", op, got, want)
		}
	}
	if opIcon(opModuleRevoke) != "shield" || opIcon(opModuleBump) != "repeat" {
		t.Errorf("module icons: revoke %q, bump %q", opIcon(opModuleRevoke), opIcon(opModuleBump))
	}
}
