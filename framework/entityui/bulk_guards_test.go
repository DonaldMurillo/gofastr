package entityui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// guardedInvoices is the invoices fixture under ac with n extra draft rows,
// queued runs backed by a memBulk, and a policy holding every permission
// ac names (grant them per test). edit changes the entities before boot.
func guardedInvoices(t *testing.T, ac entity.AccessControl, n int, ext Extensions, edit func(map[string]entity.EntityConfig, map[string][]map[string]any)) (*testUI, *memBulk, *access.RolePolicy) {
	t.Helper()
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Exposure = &entity.ExposureConfig{Access: ac}
	ents["invoices"] = inv
	rows := invoiceRows()
	for i := range n {
		id := fmt.Sprintf("q%03d", i)
		rows["invoices"] = append(rows["invoices"], map[string]any{"id": id, "number": "Q-" + id, "status": "draft"})
	}
	if edit != nil {
		edit(ents, rows)
	}
	x := newTestHost(t, ents, rows, withAPI(map[string]string{"invoices": "/api/invoices"}))
	mb := newMemBulk()
	ext.Jobs = mb
	u, err := New(bulkTestHost{x.host, mb}, ext)
	if err != nil {
		t.Fatal(err)
	}
	x.ui = u
	policy := access.NewRolePolicy()
	for _, p := range []string{ac.Read, ac.Create, ac.Update, ac.Delete} {
		if p != "" {
			policy.Register(access.Permission(p))
		}
	}
	return x, mb, policy
}

func grant(t *testing.T, policy *access.RolePolicy, role string, perms ...access.Permission) {
	t.Helper()
	if err := policy.Grant(role, perms...); err != nil {
		t.Fatal(err)
	}
}

// lastAudit is the newest bulk summary row's detail.
func lastAudit(t *testing.T, mb *memBulk) map[string]any {
	t.Helper()
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.audits) == 0 {
		t.Fatal("no bulk audit row")
	}
	return mb.audits[len(mb.audits)-1]["detail"].(map[string]any)
}

// A run that died after one chunk and was resumed reports the whole
// job in its summary row, not only the chunks the last call ran.
func TestBulkResumedRunTalliesAll(t *testing.T) {
	x, mb, policy := guardedInvoices(t, entity.AccessControl{Delete: "invoices:delete"}, 150, Extensions{}, nil)
	grant(t, policy, "clerk", "invoices:delete")
	mb.principal = func(BulkJob) (context.Context, error) { return bulkCtx("u1", policy, "clerk"), nil }
	if code, out := postBulk(t, x, bulkCtx("u1", policy, "clerk"), map[string]any{"action": "delete", "scope": "every", "count": "151"}); code != http.StatusAccepted {
		t.Fatalf("status %d: %v", code, out)
	}
	mb.failPending = func(call int) bool { return call == 2 }
	if err := x.ui.RunBulkJob(context.Background(), mb.queued[0].ID); err == nil {
		t.Fatal("the dying worker's run reported no error")
	}
	mb.failPending = nil
	if err := x.ui.RunBulkJob(context.Background(), mb.queued[0].ID); err != nil {
		t.Fatal(err)
	}
	if d := lastAudit(t, mb); d["done"] != 151 || d["status"] != BulkDone {
		t.Fatalf("summary = %v, want 151 done", d)
	}
}

// A caller who may write but not read the entity is refused by both
// routes before any selection is resolved: no tallies to learn from.
func TestBulkRefusesWriterWithoutRead(t *testing.T) {
	x, _, policy := guardedInvoices(t, entity.AccessControl{Read: "invoices:read", Update: "invoices:update", Delete: "invoices:delete"}, 0, Extensions{}, nil)
	grant(t, policy, "writer", "invoices:update", "invoices:delete")
	ctx := bulkCtx("u1", policy, "writer")
	if code, out := postBulk(t, x, ctx, map[string]any{"action": "delete", "scope": "selected", "ids": "inv-1"}); code != http.StatusForbidden {
		t.Fatalf("bulk: status %d: %v, want 403", code, out)
	}
	if rec, _ := getExport(t, x, ctx, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("export: status %d, want 403", rec.Code)
	}
	if got := invoiceIDs(t, x); len(got) != 1 {
		t.Fatalf("rows left = %v", got)
	}
}

// A creator who lost read access between enqueue and run stops the run
// before any write, even while still holding the delete permission.
func TestBulkJobStopsWhenReadRevoked(t *testing.T) {
	x, mb, policy := guardedInvoices(t, entity.AccessControl{Read: "invoices:read", Delete: "invoices:delete"}, 150, Extensions{}, nil)
	grant(t, policy, "clerk", "invoices:read", "invoices:delete")
	grant(t, policy, "deleter", "invoices:delete")
	mb.principal = func(BulkJob) (context.Context, error) { return bulkCtx("u1", policy, "deleter"), nil }
	if code, out := postBulk(t, x, bulkCtx("u1", policy, "clerk"), map[string]any{"action": "delete", "scope": "every", "count": "151"}); code != http.StatusAccepted {
		t.Fatalf("status %d: %v", code, out)
	}
	if err := x.ui.RunBulkJob(context.Background(), mb.queued[0].ID); err != nil {
		t.Fatal(err)
	}
	if got := len(invoiceIDs(t, x)); got != 151 {
		t.Fatalf("%d rows left, want all 151", got)
	}
	if j, _ := mb.Job(context.Background(), mb.queued[0].ID); j.Status != BulkStopped {
		t.Fatalf("job status %q, want stopped", j.Status)
	}
}

// Each chunk of a queued run is re-read under the creator: rows that
// moved to another owner after enqueue never reach an app action.
func TestBulkJobRereadsEachChunk(t *testing.T) {
	installOwnerExtractor(t)
	var mu sync.Mutex
	var ran []string
	ext := Extensions{Entities: map[string]Extension{"invoices": {Actions: []Action{{
		Key: "remind", Bulk: true,
		Run: func(_ context.Context, ac ActionContext) error {
			mu.Lock()
			defer mu.Unlock()
			ran = append(ran, ac.IDs...)
			return nil
		},
	}}}}}
	x, mb, _ := guardedInvoices(t, entity.AccessControl{}, 150, ext, func(ents map[string]entity.EntityConfig, rows map[string][]map[string]any) {
		inv := ents["invoices"]
		inv.Fields = append(inv.Fields, schema.Field{Name: "owner_id", Type: schema.String, Hidden: true})
		inv.Scope = &entity.ScopeConfig{OwnerField: "owner_id"}
		ents["invoices"] = inv
		for _, r := range rows["invoices"] {
			r["owner_id"] = "u1"
		}
	})
	mb.principal = func(BulkJob) (context.Context, error) { return bulkCtx("u1", nil), nil }
	if code, out := postBulk(t, x, bulkCtx("u1", nil), map[string]any{"action": "run:remind", "scope": "every", "count": "151"}); code != http.StatusAccepted {
		t.Fatalf("status %d: %v", code, out)
	}
	if _, err := x.db.Exec(`UPDATE invoices SET owner_id = 'u2' WHERE id IN ('q000', 'q149')`); err != nil {
		t.Fatal(err)
	}
	if err := x.ui.RunBulkJob(context.Background(), mb.queued[0].ID); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(ran, "q000") || slices.Contains(ran, "q149") {
		t.Fatalf("SECURITY: an app action received rows the creator no longer owns")
	}
	if len(ran) != 149 {
		t.Fatalf("Run received %d ids, want 149", len(ran))
	}
}

// Set and move are offered only to a caller who may update the entity:
// one who may not is refused the action, not run over every row and
// skipped.
func TestBulkSetAndMoveNeedUpdate(t *testing.T) {
	x, _, policy := guardedInvoices(t, entity.AccessControl{Update: "invoices:update", Delete: "invoices:delete"}, 0, Extensions{}, func(ents map[string]entity.EntityConfig, _ map[string][]map[string]any) {
		inv := ents["invoices"]
		inv.Fields = append(inv.Fields, schema.Field{Name: "priority", Type: schema.Enum, Values: []string{"low", "high"}})
		ents["invoices"] = inv
	})
	grant(t, policy, "deleter", "invoices:delete")
	for _, action := range []string{"set:priority:high", "move:send"} {
		if code, out := postBulk(t, x, bulkCtx("u1", policy, "deleter"), map[string]any{"action": action, "scope": "selected", "ids": "inv-1"}); code != http.StatusForbidden {
			t.Errorf("%s: status %d: %v, want 403", action, code, out)
		}
	}
	if got := invoiceStatus(t, x, "inv-1"); got != "draft" {
		t.Fatalf("status = %q, want draft", got)
	}
}

// A queued run needs a creator to rebuild: a caller with no user id is
// refused over the cap and nothing is queued.
func TestBulkQueueRefusesAnonymousCreator(t *testing.T) {
	x, mb, policy := guardedInvoices(t, entity.AccessControl{Delete: "invoices:delete"}, 150, Extensions{}, nil)
	grant(t, policy, "clerk", "invoices:delete")
	code, out := postBulk(t, x, bulkCtx("", policy, "clerk"), map[string]any{"action": "delete", "scope": "every", "count": "151"})
	if code != http.StatusUnprocessableEntity || !strings.Contains(fmt.Sprint(out["error"]), "Sign in") {
		t.Fatalf("status %d: %v, want 422 naming sign-in", code, out)
	}
	if len(mb.queued) != 0 {
		t.Fatalf("queued %d jobs for an anonymous creator", len(mb.queued))
	}
}

// The create screen is a read of the entity: a caller who may not read
// it gets the notice, never a form.
func TestCreateScreenNeedsRead(t *testing.T) {
	x, _, policy := guardedInvoices(t, entity.AccessControl{Read: "invoices:read", Create: "invoices:create"}, 0, Extensions{}, nil)
	grant(t, policy, "maker", "invoices:create")
	ctx := bulkCtx("u1", policy, "maker")
	body := string(x.ui.Create("invoices").Base("/rec/invoices").RenderCtx(app.WithRequest(ctx, httptest.NewRequest(http.MethodGet, "/rec/invoices/create", nil))))
	if strings.Contains(body, "<form") || !strings.Contains(body, AccessDeniedTitle) {
		t.Fatalf("a caller who may not read drew the create form:\n%s", body)
	}
}

// A field Display omits never reaches the export.
func TestExportDropsOmittedField(t *testing.T) {
	x, _, _ := guardedInvoices(t, entity.AccessControl{}, 0, Extensions{}, func(ents map[string]entity.EntityConfig, rows map[string][]map[string]any) {
		ents["invoices"].Display.Fields["memo"] = entity.FieldDisplay{Omit: true}
		rows["invoices"][0]["memo"] = "omit-me"
	})
	rec, got := getExport(t, x, bulkCtx("u1", nil), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "omit-me") || slices.Contains(got[0], "Memo") {
		t.Fatalf("the omitted memo reached the export:\n%s", rec.Body.String())
	}
}
