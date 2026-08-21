package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework"
)

// admin_restyle_test.go pins the "one styling surface" contract (CLAUDE.md
// Hard rule 7) for the standalone ops pages: the admin battery must ship ZERO
// bespoke CSS, no `const baseCSS` served outside the registry, no unprefixed
// generic selectors, and must compose framework/ui components (DataTable,
// StatCard, FilterToolbar, Tag, …) whose own registered styles do the work.
//
// These assertions are RED against the pre-restyle tree (which serves a
// ~70-line baseCSS string and hand-rolls <table>/<div class=card> markup) and
// GREEN once the pages are rebuilt on the design system.

// baseCSSTelltale are selector fragments that exist ONLY in the legacy const
// baseCSS. If any survive in /admin.css the registry-only contract is broken.
var baseCSSTelltale = []string{
	".card .value",      // the hand-rolled overview metric card
	".filters a.active", // the hand-rolled queue status filter
	"nav a.current",     // the hand-rolled nav active state
	".toolbar .muted",   // the hand-rolled queue toolbar
	".form-row label",   // the hand-rolled form layout
}

func adminGet(t *testing.T, h http.Handler, path string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"admin"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("%s: status %d body=%s", path, rr.Code, trunc(rr.Body.String(), 300))
	}
	return rr.Body.String()
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// TestAdmin_CSSServedFromRegistryNotBaseCSS is the contract keystone: the
// served stylesheet must come from registry.All() (so every composed ui.*
// component is styled) and must NOT carry any legacy baseCSS telltale.
func TestAdmin_CSSServedFromRegistryNotBaseCSS(t *testing.T) {
	h := mountAdminBare(t, Config{PathPrefix: "/admin"})
	req := httptest.NewRequest(http.MethodGet, "/admin/admin.css", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("admin.css: status %d", rr.Code)
	}
	css := rr.Body.String()

	// Positive: the registry-served sheet carries the components the pages
	// compose. DataTable + StatCard + ui-admin are registered styles, so
	// their scoped selectors appear once handleCSS emits registry.All().
	for _, want := range []string{"ui-data-table", "ui-stat-card", "ui-admin"} {
		if !strings.Contains(css, want) {
			t.Errorf("admin.css must serve registry CSS (missing %q) — baseCSS leak or registry not wired", want)
		}
	}
	// Negative: no legacy baseCSS telltale selector survives.
	for _, bad := range baseCSSTelltale {
		if strings.Contains(css, bad) {
			t.Errorf("admin.css must not carry legacy baseCSS selector %q — delete const baseCSS", bad)
		}
	}
}

// TestAdmin_OpsPagesRenderViaUIComponents asserts each standalone ops page
// emits the framework/ui component markers instead of hand-rolled markup.
func TestAdmin_OpsPagesRenderViaUIComponents(t *testing.T) {
	db := newDB(t)
	newAuditTable(t, db)
	q := newDBQueue(t, db)
	h := mountAdmin(t, Config{DB: db, Queue: q})

	// Overview → StatCards.
	if body := adminGet(t, h, "/admin"); !strings.Contains(body, `data-fui-comp="ui-stat-card"`) {
		t.Errorf("overview must render ui.StatCard; got %s", trunc(body, 300))
	}

	// Queue → FilterToolbar + DataTable.
	qbody := adminGet(t, h, "/admin/queue")
	if !strings.Contains(qbody, `data-fui-comp="ui-filter-toolbar"`) {
		t.Errorf("queue must render ui.FilterToolbar; got %s", trunc(qbody, 300))
	}
	if !strings.Contains(qbody, `data-fui-comp="ui-data-table"`) {
		t.Errorf("queue must render ui.DataTable; got %s", trunc(qbody, 300))
	}

	// Audit → DataTable.
	if body := adminGet(t, h, "/admin/audit"); !strings.Contains(body, `data-fui-comp="ui-data-table"`) {
		t.Errorf("audit must render ui.DataTable; got %s", trunc(body, 300))
	}
}

// TestAdmin_RolesRenderViaUIComponentsAndDropOrphanBadges asserts the RBAC
// roles page composes ui.DataTable + ui.Tag and drops the orphan .badge /
// .badge-remove classes (which had NO CSS anywhere).
func TestAdmin_RolesRenderViaUIComponentsAndDropOrphanBadges(t *testing.T) {
	_, h, _, _, _, _ := rbacTestEnv(t)
	body := adminGet(t, h, "/admin/rbac/roles")

	if !strings.Contains(body, `data-fui-comp="ui-data-table"`) {
		t.Errorf("roles must render ui.DataTable; got %s", trunc(body, 300))
	}
	// Orphan badge classes (never had CSS) must be gone, replaced by ui.Tag.
	if strings.Contains(body, `class="badge"`) || strings.Contains(body, "badge-remove") {
		t.Errorf("roles must drop orphan .badge/.badge-remove in favor of ui.Tag; got %s", trunc(body, 400))
	}
	if !strings.Contains(body, `ui-tag`) {
		t.Errorf("roles must render permission chips via ui.Tag; got %s", trunc(body, 300))
	}
}

// TestAdmin_ModulesRenderViaUIComponents asserts the process-module screen
// composes ui.DataTable.
func TestAdmin_ModulesRenderViaUIComponents(t *testing.T) {
	fake := &fakeModuleController{list: []framework.ProcessModuleInfo{
		{Name: "billing", State: framework.StateReady},
	}}
	_, r, _ := moduleTestEnv(t, fake)
	body := adminGet(t, r, "/admin/modules")
	if !strings.Contains(body, `data-fui-comp="ui-data-table"`) {
		t.Errorf("modules must render ui.DataTable; got %s", trunc(body, 300))
	}
}
