package admin

import (
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// platformEnv is the RBAC env with a queue holding one failed job and
// one process module wired too: every page whose state carries no
// tenant.
func platformEnv(t *testing.T) (*rbacEnv, *fakeQueue, *fakeModules) {
	t.Helper()
	q := &fakeQueue{jobs: failedJobs("job-1")}
	f := &fakeModules{list: []framework.ProcessModuleInfo{moduleInfo("billing", framework.StateReady)}}
	return newRBACEnv(t, Config{Queue: q, ProcessModules: f}), q, f
}

// Queue jobs, auth accounts, role grants and process modules carry no
// tenant, so a tenant-scoped admin would read and change every tenant's.
// Their pages and posts answer 403 to a caller with a tenant, and change
// nothing; the platform operator, with none, still reaches them.
func TestTenantAdminRefusedPlatformPages(t *testing.T) {
	r, q, f := platformEnv(t)
	tenantAdmin := asTenant(r.h, theAdmin, "t1")
	pages := []string{"/admin/queue", "/admin/rbac/users", "/admin/rbac/roles", "/admin/modules", "/admin/_metric/jobs"}
	for _, p := range pages {
		if rr := get(tenantAdmin, p); rr.Code != http.StatusForbidden {
			t.Errorf("GET %s as a tenant admin = %d, want 403", p, rr.Code)
		}
	}
	posts := map[string]url.Values{
		"/admin/queue/_replay/job-1": {},
		"/admin/queue/_replay_all":   {},
		"/admin/rbac/_grant":         {"role": {"editor"}, "permission": {"posts:write"}},
		"/admin/rbac/_permissions":   {"role": {"editor"}, "permission": {"posts:write"}, "grant": {"editor|posts:write"}},
		"/admin/rbac/_assign":        {"user_id": {r.editor.GetID()}, "roles": {"admin"}},
		"/admin/modules/_enable":     {"name": {"billing"}},
		"/admin/modules/_disable":    {"name": {"billing"}},
		"/admin/modules/_bump":       {"name": {"billing"}},
		"/admin/modules/_revoke":     {"name": {"billing"}, "grant": {"posts:read"}},
	}
	for _, p := range slices.Sorted(maps.Keys(posts)) {
		if rr := post(tenantAdmin, p, posts[p]); rr.Code != http.StatusForbidden {
			t.Errorf("POST %s as a tenant admin = %d, want 403", p, rr.Code)
		}
	}
	if len(q.replayed) != 0 || len(f.calls) != 0 {
		t.Errorf("refused posts acted: replayed %v, module calls %v", q.replayed, f.calls)
	}
	if canAs(r.policy, "editor", "posts:write") {
		t.Error("a refused grant reached the policy")
	}
	if got := r.rolesOf(r.editor.GetID()); !slices.Equal(got, []string{"editor"}) {
		t.Errorf("a refused assign changed the editor's roles to %v", got)
	}
	for _, p := range pages {
		if rr := get(r.as(theAdmin), p); rr.Code != http.StatusOK {
			t.Errorf("GET %s as the platform admin = %d, want 200", p, rr.Code)
		}
	}
}

// A tenant admin's dashboard, sidebar and palette name none of the
// platform pages, its failed-jobs figure is gone, and with no watch of
// its own it draws no attention panel.
func TestTenantAdminNavOmitsPlatformPages(t *testing.T) {
	r, _, _ := platformEnv(t)
	body := get(asTenant(r.h, theAdmin, "t1"), "/admin").Body.String()
	for _, p := range []string{"/admin/queue", "/admin/rbac/", "/admin/modules", "/admin/_metric/jobs"} {
		if strings.Contains(body, p) {
			t.Errorf("a tenant admin's dashboard links %s", p)
		}
	}
	if strings.Contains(body, "Needs attention") {
		t.Error("a tenant admin's dashboard draws an attention panel with nothing it may show")
	}
	platform := get(r.as(theAdmin), "/admin").Body.String()
	for _, p := range []string{"/admin/queue", "/admin/rbac/users", "/admin/modules", "/admin/_metric/jobs"} {
		if !strings.Contains(platform, p) {
			t.Errorf("the platform admin's dashboard lacks %s", p)
		}
	}
}

// With an attention watch configured the panel draws for a tenant admin
// too, and it still leaves the queue's failed jobs out.
func TestTenantAttentionOmitsFailedJobs(t *testing.T) {
	x := ordersEnv(t, Config{Entities: []string{"orders"}, Queue: &fakeQueue{jobs: failedJobs("job-1")},
		Attention: []Watch{{Entity: "orders", View: "late", Columns: []string{"ref", "amount"}}}})
	if body := get(asTenant(x.h, theAdmin, "t1"), "/admin").Body.String(); strings.Contains(body, "/admin/queue") {
		t.Error("a tenant admin's attention panel links the queue")
	}
	if body := get(x.as(theAdmin), "/admin").Body.String(); !strings.Contains(body, "/admin/queue") {
		t.Error("the platform admin's attention panel lacks the failed jobs")
	}
}
