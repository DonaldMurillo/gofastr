package admin

// F17 Authorization at derived surfaces — pinned 2026-09-05 round 4.
// Fix (this suite's green state): queryAudit and the overview COUNT(*) both filter
// with WHERE tenant_id = $1 when tenant.GetTenantID is non-empty. NULL-tenant system
// rows are NOT visible to a tenant-scoped admin (strict equality); an admin context
// with no tenant sees everything (platform-operator posture).
// Property: the admin battery's audit surfaces must apply the same tenant scope the entity screens apply — a tenant-scoped admin sees only their own tenant's audit rows and counts, per audit-log.md ("scope the read to the caller's tenant so one tenant can't see another's audit trail") and multi-tenant.md ("scope your audit queries with WHERE tenant_id = $1").
// Surfaces: battery/admin/admin.go::queryAudit (GET <prefix>/audit listing), battery/admin/admin.go::handleIndex (SELECT COUNT(*) FROM audit_log feeding the overview tile), battery/admin/admin.go::auditSummary (renders the count).
// Finding: queryAudit selects every tenant's rows (`FROM audit_log ORDER BY created_at DESC`, no tenant predicate) and handleIndex counts every row, so tenant-a's admin is served tenant-b's audit rows (entity, op, record_id, actor_id, created_at) on /admin/audit and an overview total inflated by tenant-b's writes.
// Severity: high — cross-tenant disclosure of write-activity metadata (who changed which record when) through the sanctioned back-office, contradicting the documented tenant-scoped audit-read contract the write side already honors (writeAuditRow stamps tenant_id).
import (
	"database/sql"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// seedTenantAuditRow inserts one audit row exactly the way writeAuditRow
// does (same columns, tenant stamped), bypassing the battery so the row set
// is fully controlled.
func seedTenantAuditRow(t *testing.T, db *sql.DB, id, tenantID, ent, op, recordID, actor string, created time.Time) {
	t.Helper()
	var tenantArg any
	if tenantID != "" {
		tenantArg = tenantID
	}
	if _, err := db.Exec(
		`INSERT INTO audit_log (id, entity, op, record_id, actor_id, tenant_id, created_at, diff)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NULL)`,
		id, ent, op, recordID, actor, tenantArg, created,
	); err != nil {
		t.Fatalf("seed audit row %s: %v", id, err)
	}
}

func TestAuditSurfacesStayInsideTenant(t *testing.T) {
	db := newDB(t)
	if err := framework.EnsureAuditTable(db, "audit_log"); err != nil {
		t.Fatalf("ensure audit table: %v", err)
	}
	app := newHostedApp(t, db, map[string]entity.EntityConfig{})
	handlerBase := mountAdminBattery(t, app, Config{DB: db})

	t0 := time.Now().UTC().Add(-time.Hour)
	// tenant-a: one row its admin may see.
	seedTenantAuditRow(t, db, "aud-a1", "tenant-a", "org_docs", "create", "rec-tenant-a", "admin-a", t0)
	// tenant-b: three rows that must be invisible to tenant-a's admin.
	for i := range 3 {
		seedTenantAuditRow(t, db,
			fmt.Sprintf("aud-b%d", i), "tenant-b", "billing", "update",
			fmt.Sprintf("rec-tenant-b-%d", i), "admin-b",
			t0.Add(time.Duration(i+1)*time.Minute))
	}

	a := asTenantUser(handlerBase, testUser{"admin-a"}, "tenant-a")

	// Surface 1: the audit listing.
	rr := get(a, "/admin/audit")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /admin/audit = %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "rec-tenant-a") {
		t.Fatalf("sanity: tenant-a's own audit row is missing from the listing — the page did not render the seeded rows: %s", body)
	}
	if strings.Contains(body, "rec-tenant-b") {
		t.Errorf("SECURITY: [admin-audit] /admin/audit listed tenant-b's audit rows (record ids rec-tenant-b-*) for tenant-a's admin — audit-log.md: \"scope the read to the caller's tenant so one tenant can't see another's audit trail\"; queryAudit has no tenant predicate")
	}
	if strings.Contains(body, "admin-b") {
		t.Errorf("SECURITY: [admin-audit] /admin/audit listed tenant-b's actor id (admin-b) for tenant-a's admin — same unscoped queryAudit read")
	}

	// Surface 2: the overview count tile. tenant-a owns 1 row; an unscoped
	// COUNT(*) returns 4.
	ov := get(a, "/admin")
	if ov.Code != http.StatusOK {
		t.Fatalf("GET /admin = %d body=%s", ov.Code, ov.Body.String())
	}
	m := regexp.MustCompile(`ui-stat-card__label">entries</p>\s*<p[^>]*ui-stat-card__value">(\d+)<`).FindStringSubmatch(ov.Body.String())
	if m == nil {
		t.Fatalf("sanity: overview audit entries tile not found in body=%s", ov.Body.String())
	}
	if m[1] != "1" {
		t.Errorf("SECURITY: [admin-audit] overview audit count = %s for tenant-a's admin, want 1 — handleIndex's SELECT COUNT(*) FROM audit_log counts every tenant's rows, a cross-tenant write-volume oracle (tenant-b seeded 3)", m[1])
	}
}
