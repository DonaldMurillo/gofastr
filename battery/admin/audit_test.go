package admin

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// A field the entity's AfterGet hook masks, here only when it holds a
// value, is left out of the change lists on the Audit log page and the
// dashboard's activity, as it is on the record's own Activity tab: the
// admin's reads of the record show the mask, so its audit diffs do not
// print the stored values either.
func TestAuditChangesHideHookMaskedField(t *testing.T) {
	cases := entity.EntityConfig{
		Table: "cases",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "secret", Type: schema.String},
			{Name: "status", Type: schema.Enum, Values: []string{"open", "closed"}, Default: "open"},
		},
	}.WithTimestamps(false)
	mask := func(x *env, _ *Config) {
		framework.OnAfterGet(x.app, "cases", func(_ context.Context, p *hook.GetPayload) error {
			if s, _ := p.Result["secret"].(string); s != "" {
				p.Result["secret"] = "****"
			}
			return nil
		})
	}
	x := setup(t, map[string]entity.EntityConfig{"cases": cases}, Config{Entities: []string{"cases"}}, mask)
	x.insert("cases", map[string]any{"id": "c-1", "title": "Case one", "status": "closed"})
	x.seedAuditDiff("a1", "", "cases", "update", "c-1", "admin-1", time.Now().UTC(),
		`{"old":{"title":"Case one","secret":"old-secret-value","status":"open"},"new":{"title":"Case one","secret":"new-secret-value","status":"closed"}}`)
	for _, p := range []string{"/admin/audit", "/admin"} {
		body := get(x.as(theAdmin), p).Body.String()
		if strings.Contains(body, "secret-value") {
			t.Errorf("SECURITY: %s drew a hook-masked field's stored values", p)
		}
		if !strings.Contains(body, `>Closed</span></ins>`) {
			t.Errorf("%s lost the unmasked change", p)
		}
	}
}

func capturingLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelError}))
}

func (x *env) seedAudit(id, tenantID, ent, op, record, actor string, at time.Time) {
	x.t.Helper()
	x.seedAuditDiff(id, tenantID, ent, op, record, actor, at, "")
}

// seedAuditDiff seeds a row with a stored diff, NULL when diff is "".
func (x *env) seedAuditDiff(id, tenantID, ent, op, record, actor string, at time.Time, diff string) {
	x.t.Helper()
	var tid, d any
	if tenantID != "" {
		tid = tenantID
	}
	if diff != "" {
		d = diff
	}
	if _, err := x.db.Exec(`INSERT INTO audit_log (id, entity, op, record_id, actor_id, tenant_id, created_at, diff)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, ent, op, record, actor, tid, at, d); err != nil {
		x.t.Fatalf("seed audit %s: %v", id, err)
	}
}

func TestAuditPageListsRows(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, Config{Entities: []string{"posts"}}, nil)
	x.insert("posts", map[string]any{"id": "p-42", "title": "Launch notes", "status": "published"})
	now := time.Now().UTC()
	x.seedAuditDiff("a1", "", "posts", "update", "p-42", "admin-1", now,
		`{"old":{"title":"Launch notes","status":"draft"},"new":{"title":"Launch notes","status":"published"}}`)
	x.seedAuditDiff("a2", "", "posts", "delete", "p-43", "", now.Add(time.Second),
		`{"old":{"title":"Old & gone","status":"draft"}}`)
	x.seedAudit("a3", "", "billing", "create", "<b>raw</b>", "admin-1", now.Add(2*time.Second))
	body := get(x.as(theAdmin), "/admin/audit").Body.String()
	if !strings.Contains(body, `href="/admin/entities/posts/p-42"`) || !strings.Contains(body, ">Launch notes</a>") {
		t.Error("an exposed entity's row does not link its record by title")
	}
	if strings.Contains(body, `href="/admin/entities/posts/p-43"`) {
		t.Error("a deleted record is linked")
	}
	if !strings.Contains(body, "Old &amp; gone") {
		t.Error("a deleted record is not titled from its stored copy")
	}
	if !strings.Contains(body, `>Draft</span></del>`) || !strings.Contains(body, `>Published</span></ins>`) {
		t.Errorf("the row does not list what the update changed:\n%s", body)
	}
	if strings.Contains(body, `fui-change-list__label">Title`) {
		t.Error("an unchanged field is listed as a change")
	}
	if !strings.Contains(body, "System") {
		t.Error("a row with no actor does not read as the system")
	}
	if strings.Contains(body, "<b>raw</b>") || !strings.Contains(body, "billing") {
		t.Error("an unexposed entity's row is missing or unescaped")
	}
}

// An integer past 2^53 in a stored diff reads on the audit page with its
// own digits: decoding the diff into map[string]any read it as a float64
// and showed 9007199254740993 as 9007199254740992.
func TestAuditChangesKeepBigIntegers(t *testing.T) {
	ledger := entity.EntityConfig{
		Table: "ledger",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "ref", Type: schema.Int},
		},
	}.WithTimestamps(false)
	x := setup(t, map[string]entity.EntityConfig{"ledger": ledger}, Config{Entities: []string{"ledger"}}, nil)
	x.insert("ledger", map[string]any{"id": "l-1", "title": "Wire", "ref": 7})
	x.seedAuditDiff("a1", "", "ledger", "update", "l-1", "admin-1", time.Now().UTC(),
		`{"old":{"title":"Wire","ref":7},"new":{"title":"Wire","ref":9007199254740993}}`)
	// The markup only: an inlined head's sheets name the marker too.
	_, body, _ := strings.Cut(get(x.as(theAdmin), "/admin/audit").Body.String(), "<body")
	_, list, _ := strings.Cut(body, `data-cui-comp="ui-change-list"`)
	if !strings.Contains(list, "9007199254740993") {
		t.Errorf("the change list lost the integer's digits:\n%.600s", list)
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

// A move's operation carries its key ("transition:send"); it badges and
// draws like any move, not as an unknown operation.
func TestAuditOpKindFoldsKeyedOps(t *testing.T) {
	for _, op := range []string{"transition:send", "transition:mark_paid", "state_override"} {
		if got := opVariant(op); got != ui.StatusInfo {
			t.Errorf("opVariant(%q) = %v, want info", op, got)
		}
		if got := opIcon(op); got != "repeat" {
			t.Errorf("opIcon(%q) = %q, want repeat", op, got)
		}
	}
	if got := opKind("transitional"); got != "transitional" {
		t.Errorf("opKind folded an operation that only starts like a move: %q", got)
	}
}

// A sensitive key's audit rows carry its digest, which names no record.
// Looking it up would title and link a record whose own id is that
// digest string.
func TestAuditDigestIDLinksNothing(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, Config{Entities: []string{"posts"}}, nil)
	digest := "sha256:" + strings.Repeat("ab", 32)
	x.insert("posts", map[string]any{"id": digest, "title": "Planted", "status": "draft"})
	x.seedAudit("a1", "", "posts", "update", digest, "admin-1", time.Now().UTC())
	body := get(x.as(theAdmin), "/admin/audit").Body.String()
	if strings.Contains(body, "Planted") || strings.Contains(body, `href="/admin/entities/posts/sha256`) {
		t.Fatal("a digest record_id was looked up as a live record")
	}
}
