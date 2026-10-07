package admin

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func capturingLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelError}))
}

func (x *env) seedAudit(id, tenantID, ent, op, record, actor string, at time.Time) {
	x.t.Helper()
	var tid any
	if tenantID != "" {
		tid = tenantID
	}
	if _, err := x.db.Exec(`INSERT INTO audit_log (id, entity, op, record_id, actor_id, tenant_id, created_at, diff)
		VALUES (?, ?, ?, ?, ?, ?, ?, NULL)`, id, ent, op, record, actor, tid, at); err != nil {
		x.t.Fatalf("seed audit %s: %v", id, err)
	}
}

func TestAuditPageListsRows(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, Config{Entities: []string{"posts"}}, nil)
	now := time.Now().UTC()
	x.seedAudit("a1", "", "posts", "update", "p-42", "admin-1", now)
	x.seedAudit("a2", "", "posts", "delete", "p-43", "", now.Add(time.Second))
	x.seedAudit("a3", "", "billing", "create", "<b>raw</b>", "admin-1", now.Add(2*time.Second))
	body := get(x.as(theAdmin), "/admin/audit").Body.String()
	if !strings.Contains(body, `href="/admin/entities/posts/p-42"`) {
		t.Error("an exposed entity's row does not link its record")
	}
	if strings.Contains(body, `href="/admin/entities/posts/p-43"`) {
		t.Error("a deleted record is linked")
	}
	if !strings.Contains(body, "System") {
		t.Error("a row with no actor does not read as the system")
	}
	if strings.Contains(body, "<b>raw</b>") || !strings.Contains(body, "billing") {
		t.Error("an unexposed entity's row is missing or unescaped")
	}
}

// A tenant-scoped admin reads only its tenant's rows, on the page and on
// the dashboard; an admin with no tenant reads every row.
func TestAuditStaysInsideTheTenant(t *testing.T) {
	x := setup(t, nil, Config{}, nil)
	t0 := time.Now().UTC().Add(-time.Hour)
	x.seedAudit("a-1", "tenant-a", "docs", "create", "rec-tenant-a", "admin-a", t0)
	for i := range 3 {
		x.seedAudit(fmt.Sprintf("b-%d", i), "tenant-b", "billing", "update",
			fmt.Sprintf("rec-tenant-b-%d", i), "admin-b", t0.Add(time.Duration(i+1)*time.Minute))
	}
	a := asTenant(x.h, roleUser{id: "admin-a", roles: []string{"admin"}}, "tenant-a")
	for _, p := range []string{"/admin/audit", "/admin"} {
		body := get(a, p).Body.String()
		if !strings.Contains(body, "rec-tenant-a") {
			t.Fatalf("%s lacks tenant-a's own row", p)
		}
		if strings.Contains(body, "rec-tenant-b") || strings.Contains(body, "admin-b") {
			t.Errorf("SECURITY: %s showed tenant-b's audit rows to tenant-a's admin", p)
		}
	}
	if body := get(x.as(theAdmin), "/admin/audit").Body.String(); !strings.Contains(body, "rec-tenant-b-2") {
		t.Error("the platform admin does not see every tenant's rows")
	}
}

func TestAuditLoadFailureIsLogged(t *testing.T) {
	var buf bytes.Buffer
	x := setup(t, nil, Config{AuditTable: "no_such_table", Logger: capturingLogger(&buf)}, nil)
	body := get(x.as(theAdmin), "/admin/audit").Body.String()
	if strings.Contains(body, "no such table") || !strings.Contains(body, "fui-callout") {
		t.Fatal("the audit page leaked the driver error or drew no notice")
	}
	if !strings.Contains(buf.String(), "no_such_table") {
		t.Fatalf("the load failure was not logged: %q", buf.String())
	}
}

// A mutation commits before its audit row; a failed row is logged with
// what it would have recorded.
func TestAuditWriteFailureIsLogged(t *testing.T) {
	var buf bytes.Buffer
	r := newRBACEnv(t, Config{AuditTable: "no_such_table", Logger: capturingLogger(&buf)})
	if got := resultOf(t, post(r.as(theAdmin), "/admin/rbac/_grant", url.Values{"role": {"editor"}, "permission": {"posts:write"}})); got != "granted" {
		t.Fatalf("result = %q", got)
	}
	if !canAs(r.policy, "editor", "posts:write") {
		t.Fatal("the grant did not apply")
	}
	if got := buf.String(); !strings.Contains(got, "audit write failed") || !strings.Contains(got, "editor") {
		t.Fatalf("the audit failure was not logged: %q", got)
	}
}
