package entityui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// postBulk posts body to the invoices bulk handler as ctx's caller and
// returns the status and decoded JSON.
func postBulk(t *testing.T, x *testUI, ctx context.Context, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/invoices/_bulk", strings.NewReader(string(raw))).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	x.ui.BulkHandler("invoices").ServeHTTP(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("bulk answered non-JSON (%d): %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("bulk answer Cache-Control = %q, want no-store", got)
	}
	return rec.Code, out
}

// bulkCtx is a request context for u with the given roles under policy.
func bulkCtx(u string, policy *access.RolePolicy, roles ...string) context.Context {
	ctx := handler.SetUser(context.Background(), &testUser{id: u})
	if policy != nil {
		ctx = access.WithRoles(access.WithPolicy(ctx, policy), roles)
	}
	return ctx
}

// invoiceIDs reads the invoice ids left, sorted.
func invoiceIDs(t *testing.T, x *testUI) []string {
	t.Helper()
	rows, err := x.db.Query(`SELECT id FROM invoices ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func invoiceStatus(t *testing.T, x *testUI, id string) string {
	t.Helper()
	var s string
	if err := x.db.QueryRow(`SELECT status FROM invoices WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// ownedInvoices is the invoices fixture scoped to an owner, two rows each
// for u1 and u2.
func ownedInvoices(t *testing.T, ext Extensions, opts ...testUIOption) *testUI {
	t.Helper()
	installOwnerExtractor(t)
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Fields = append(inv.Fields, schema.Field{Name: "owner_id", Type: schema.String, Hidden: true})
	inv.Scope = &entity.ScopeConfig{OwnerField: "owner_id"}
	ents["invoices"] = inv
	rows := invoiceRows()
	rows["invoices"] = []map[string]any{
		{"id": "a1", "number": "A-1", "status": "draft", "owner_id": "u1"},
		{"id": "a2", "number": "A-2", "status": "draft", "owner_id": "u1"},
		{"id": "b1", "number": "B-1", "status": "draft", "owner_id": "u2"},
		{"id": "b2", "number": "B-2", "status": "draft", "owner_id": "u2"},
	}
	return newTestUIExt(t, ents, rows, ext, append([]testUIOption{withAPI(map[string]string{"invoices": "/api/invoices"})}, opts...)...)
}

// An id from another owner drops out at the re-read: u1's delete of
// [a1, b1] deletes a1 and never touches b1.
func TestBulkForeignOwnerIDDropsOut(t *testing.T) {
	x := ownedInvoices(t, Extensions{})
	code, out := postBulk(t, x, bulkCtx("u1", nil), map[string]any{
		"_csrf": "tok", "action": "delete", "scope": "selected", "ids": []string{"a1", "b1"},
	})
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, out)
	}
	if got := invoiceIDs(t, x); !slices.Equal(got, []string{"a2", "b1", "b2"}) {
		t.Fatalf("rows left = %v, want a2 b1 b2 (b1 is u2's)", got)
	}
	if out["done"] != float64(1) {
		t.Fatalf("done = %v, want 1 (the foreign id is not counted)", out["done"])
	}
}

// A single checked row arrives as a string, not an array.
func TestBulkSingleIDAsString(t *testing.T) {
	x := ownedInvoices(t, Extensions{})
	code, out := postBulk(t, x, bulkCtx("u1", nil), map[string]any{
		"action": "delete", "scope": "selected", "ids": "a1",
	})
	if code != http.StatusOK || out["done"] != float64(1) {
		t.Fatalf("status %d: %v", code, out)
	}
}

// An unknown body key is refused, the strict decode every form RPC gets.
func TestBulkRefusesUnknownKey(t *testing.T) {
	x := ownedInvoices(t, Extensions{})
	code, _ := postBulk(t, x, bulkCtx("u1", nil), map[string]any{
		"action": "delete", "scope": "selected", "ids": "a1", "owner_id": "u2",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", code)
	}
	if got := invoiceIDs(t, x); len(got) != 4 {
		t.Fatalf("a refused body deleted rows: %v", got)
	}
}

// A Locked field and the state field are never offered for "set a
// field": posting either key is refused as an unknown action.
func TestBulkLockedAndStateNeverSettable(t *testing.T) {
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Fields = append(inv.Fields, schema.Field{Name: "priority", Type: schema.Enum, Values: []string{"low", "high"}})
	inv.Fields = append(inv.Fields, schema.Field{Name: "tier", Type: schema.Enum, Values: []string{"a", "b"}})
	inv.Display.Fields["tier"] = entity.FieldDisplay{Locked: true}
	ents["invoices"] = inv
	x := newTestUI(t, ents, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	for _, key := range []string{"set:status:paid", "set:tier:b", "set:number:X"} {
		code, out := postBulk(t, x, bulkCtx("u1", nil), map[string]any{"action": key, "scope": "selected", "ids": "inv-1"})
		if code != http.StatusForbidden {
			t.Errorf("%s: status %d (%v), want 403", key, code, out)
		}
	}
	if got := invoiceStatus(t, x, "inv-1"); got != "draft" {
		t.Fatalf("status = %q after refused sets, want draft", got)
	}
	code, out := postBulk(t, x, bulkCtx("u1", nil), map[string]any{"action": "set:priority:high", "scope": "selected", "ids": "inv-1"})
	if code != http.StatusOK || out["done"] != float64(1) {
		t.Fatalf("set:priority:high: status %d: %v", code, out)
	}
}

// A move runs per record: one whose From holds moves, one that does not
// is skipped, not failed.
func TestBulkMoveSkipsWrongState(t *testing.T) {
	rows := invoiceRows()
	rows["invoices"] = append(rows["invoices"], map[string]any{"id": "inv-2", "number": "INV-2", "status": "paid", "customer_id": "cus-1"})
	x := newTestUI(t, invoiceEntities(), rows, withAPI(map[string]string{"invoices": "/api/invoices"}))
	code, out := postBulk(t, x, bulkCtx("u1", nil), map[string]any{"action": "move:send", "scope": "selected", "ids": []string{"inv-1", "inv-2"}})
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, out)
	}
	if out["done"] != float64(1) || out["skipped"] != float64(1) || out["failed"] != float64(0) {
		t.Fatalf("tally = %v, want 1 done 1 skipped", out)
	}
	if invoiceStatus(t, x, "inv-1") != "open" || invoiceStatus(t, x, "inv-2") != "paid" {
		t.Fatal("the move did not land on the draft only")
	}
}

// An app action's Permission is held by name: a Wildcard role is
// offered nothing, and a named grant runs it.
func TestBulkActionPermissionExact(t *testing.T) {
	var ran []string
	ext := Extensions{Entities: map[string]Extension{"invoices": {Actions: []Action{{
		Key: "remind", Permission: "invoices:remind", Bulk: true,
		Run: func(_ context.Context, ac ActionContext) error { ran = append(ran, ac.IDs...); return nil },
	}}}}}
	policy := access.NewRolePolicy()
	if err := policy.Grant("root", access.Wildcard); err != nil {
		t.Fatal(err)
	}
	if err := policy.Grant("clerk", "invoices:remind"); err != nil {
		t.Fatal(err)
	}
	x := newTestUIExt(t, invoiceEntities(), invoiceRows(), ext, withAPI(map[string]string{"invoices": "/api/invoices"}))
	code, out := postBulk(t, x, bulkCtx("u1", policy, "root"), map[string]any{"action": "run:remind", "scope": "selected", "ids": "inv-1"})
	if code != http.StatusForbidden || len(ran) != 0 {
		t.Fatalf("Wildcard: status %d %v ran=%v, want the action refused", code, out, ran)
	}
	code, out = postBulk(t, x, bulkCtx("u1", policy, "clerk"), map[string]any{"action": "run:remind", "scope": "selected", "ids": "inv-1"})
	if code != http.StatusOK || out["done"] != float64(1) || !slices.Equal(ran, []string{"inv-1"}) {
		t.Fatalf("named grant: status %d %v ran=%v", code, out, ran)
	}
}

// An app action receives only the ids its caller can read: another
// owner's id never reaches Run.
func TestBulkRunSeesOnlyReadableIDs(t *testing.T) {
	var ran []string
	ext := Extensions{Entities: map[string]Extension{"invoices": {Actions: []Action{{
		Key: "remind", Bulk: true,
		Run: func(_ context.Context, ac ActionContext) error { ran = append(ran, ac.IDs...); return nil },
	}}}}}
	x := ownedInvoices(t, ext)
	code, out := postBulk(t, x, bulkCtx("u1", nil), map[string]any{"action": "run:remind", "scope": "selected", "ids": []string{"a1", "b1", "b2"}})
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, out)
	}
	if !slices.Equal(ran, []string{"a1"}) {
		t.Fatalf("Run received %v, want only a1", ran)
	}
}

// A panicking app action fails its records and the handler survives.
func TestBulkActionPanicContained(t *testing.T) {
	ext := Extensions{Entities: map[string]Extension{"invoices": {Actions: []Action{{
		Key: "boom", Bulk: true,
		Run: func(context.Context, ActionContext) error { panic("secret row text") },
	}}}}}
	x := newTestUIExt(t, invoiceEntities(), invoiceRows(), ext, withAPI(map[string]string{"invoices": "/api/invoices"}))
	code, out := postBulk(t, x, bulkCtx("u1", nil), map[string]any{"action": "run:boom", "scope": "selected", "ids": "inv-1"})
	if code != http.StatusOK || out["failed"] != float64(1) {
		t.Fatalf("status %d: %v, want the record failed", code, out)
	}
}

// Over InRequestCap with no JobRunner is refused naming the cap, and
// nothing runs.
func TestBulkOverCapRefusedWithoutJobs(t *testing.T) {
	rows := invoiceRows()
	ids := make([]string, 0, InRequestCap+1)
	for i := range InRequestCap + 1 {
		id := fmt.Sprintf("n%03d", i)
		ids = append(ids, id)
		rows["invoices"] = append(rows["invoices"], map[string]any{"id": id, "number": "N-" + id, "status": "draft"})
	}
	x := newTestUI(t, invoiceEntities(), rows, withAPI(map[string]string{"invoices": "/api/invoices"}))
	code, out := postBulk(t, x, bulkCtx("u1", nil), map[string]any{"action": "delete", "scope": "selected", "ids": ids})
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d: %v, want 422", code, out)
	}
	if msg, _ := out["error"].(string); !strings.Contains(msg, "100") {
		t.Fatalf("refusal %q does not name the cap", msg)
	}
	if got := invoiceIDs(t, x); len(got) != InRequestCap+2 {
		t.Fatalf("%d rows left, want all %d", len(got), InRequestCap+2)
	}
}

// Every match follows the list's own narrowing: a view-less filter on
// status deletes the drafts only.
func TestBulkEveryMatchFollowsQuery(t *testing.T) {
	rows := invoiceRows()
	rows["invoices"] = append(rows["invoices"], map[string]any{"id": "inv-2", "number": "INV-2", "status": "paid"})
	x := newTestUI(t, invoiceEntities(), rows, withAPI(map[string]string{"invoices": "/api/invoices"}))
	code, out := postBulk(t, x, bulkCtx("u1", nil), map[string]any{"action": "delete", "scope": "every", "query": `filter=status+%3D+%22draft%22`, "count": "1"})
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, out)
	}
	if got := invoiceIDs(t, x); !slices.Equal(got, []string{"inv-2"}) {
		t.Fatalf("rows left = %v, want inv-2", got)
	}
}

// A filter the list refuses is refused for every match too, rather than
// widening to every row.
func TestBulkEveryMatchBadFilterRefused(t *testing.T) {
	x := newTestUI(t, invoiceEntities(), invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	code, _ := postBulk(t, x, bulkCtx("u1", nil), map[string]any{"action": "delete", "scope": "every", "query": `filter=token+%3D+%22x%22`, "count": "1"})
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422 for a filter on a NoQuery field", code)
	}
	if got := invoiceIDs(t, x); len(got) != 1 {
		t.Fatalf("rows left = %v", got)
	}
}

// Display.NoBulk turns the route off.
func TestBulkNoBulkIsNotFound(t *testing.T) {
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Display.NoBulk = true
	ents["invoices"] = inv
	x := newTestUI(t, ents, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	if code, _ := postBulk(t, x, bulkCtx("u1", nil), map[string]any{"action": "delete", "scope": "selected", "ids": "inv-1"}); code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", code)
	}
}

// memBulk is an in-memory BulkHost + JobRunner for the queued-run tests.
type memBulk struct {
	mu        sync.Mutex
	jobs      map[string]BulkJob
	ids       map[string][]string
	settled   map[string]map[string]string
	queued    []BulkJob
	audits    []map[string]any
	principal func(BulkJob) (context.Context, error)
	// failPending, when set, fails the Pending call it answers true for
	// (counted from 1): a worker that died mid-run.
	failPending  func(call int) bool
	pendingCalls int
}

func newMemBulk() *memBulk {
	return &memBulk{jobs: map[string]BulkJob{}, ids: map[string][]string{}, settled: map[string]map[string]string{}}
}

func (b *memBulk) BulkStore() BulkStore { return b }

func (b *memBulk) AuditEvent(_ context.Context, _, op, id string, detail map[string]any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.audits = append(b.audits, map[string]any{"op": op, "id": id, "detail": detail})
	return nil
}

func (b *memBulk) Create(_ context.Context, job BulkJob, ids []string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.jobs[job.ID] = job
	b.ids[job.ID] = slices.Clone(ids)
	b.settled[job.ID] = map[string]string{}
	return nil
}

func (b *memBulk) Job(_ context.Context, id string) (BulkJob, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	j, ok := b.jobs[id]
	if !ok {
		return BulkJob{}, errors.New("no such job")
	}
	return j, nil
}

func (b *memBulk) Pending(_ context.Context, id string, limit int) ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pendingCalls++
	if b.failPending != nil && b.failPending(b.pendingCalls) {
		return nil, errors.New("worker died")
	}
	var out []string
	for _, rid := range b.ids[id] {
		if _, done := b.settled[id][rid]; !done {
			out = append(out, rid)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

func (b *memBulk) Settle(_ context.Context, id string, outcomes map[string]string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for rid, o := range outcomes {
		if _, done := b.settled[id][rid]; !done {
			b.settled[id][rid] = o
		}
	}
	return nil
}

func (b *memBulk) Tally(_ context.Context, id string) (map[string]int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := map[string]int{}
	for _, o := range b.settled[id] {
		out[o]++
	}
	return out, nil
}

func (b *memBulk) Finish(_ context.Context, id, status string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	j := b.jobs[id]
	j.Status = status
	b.jobs[id] = j
	return nil
}

func (b *memBulk) Enqueue(_ context.Context, job BulkJob) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.queued = append(b.queued, job)
	return nil
}

func (b *memBulk) Principal(_ context.Context, job BulkJob) (context.Context, error) {
	return b.principal(job)
}

// bulkTestHost is the test host with bulk backing.
type bulkTestHost struct {
	*testHost
	*memBulk
}

// newQueuedUI is the invoices fixture with n extra drafts, a delete
// permission, and an in-memory snapshot store and runner.
func newQueuedUI(t *testing.T, n int) (*testUI, *memBulk, *access.RolePolicy) {
	t.Helper()
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{Delete: "invoices:delete"}}
	ents["invoices"] = inv
	rows := invoiceRows()
	for i := range n {
		id := fmt.Sprintf("q%03d", i)
		rows["invoices"] = append(rows["invoices"], map[string]any{"id": id, "number": "Q-" + id, "status": "draft"})
	}
	x := newTestHost(t, ents, rows, withAPI(map[string]string{"invoices": "/api/invoices"}))
	mb := newMemBulk()
	ui, err := New(bulkTestHost{x.host, mb}, Extensions{Jobs: mb})
	if err != nil {
		t.Fatal(err)
	}
	x.ui = ui
	policy := access.NewRolePolicy()
	policy.Register("invoices:delete")
	if err := policy.Grant("clerk", "invoices:delete"); err != nil {
		t.Fatal(err)
	}
	return x, mb, policy
}

// A selection over InRequestCap is snapshotted and queued, not run, and
// RunBulkJob runs it under the creator's rebuilt context.
func TestBulkQueuedRunsUnderCreator(t *testing.T) {
	x, mb, policy := newQueuedUI(t, 150)
	mb.principal = func(job BulkJob) (context.Context, error) {
		if job.Creator != "u1" {
			return nil, errors.New("wrong creator")
		}
		return bulkCtx("u1", policy, "clerk"), nil
	}
	code, out := postBulk(t, x, bulkCtx("u1", policy, "clerk"), map[string]any{"action": "delete", "scope": "every", "count": "151"})
	if code != http.StatusAccepted {
		t.Fatalf("status %d: %v, want 202", code, out)
	}
	if got := len(invoiceIDs(t, x)); got != 151 {
		t.Fatalf("a queued run deleted rows in the request: %d left", got)
	}
	if len(mb.queued) != 1 || mb.queued[0].Count != 151 {
		t.Fatalf("queued = %+v", mb.queued)
	}
	if err := x.ui.RunBulkJob(context.Background(), mb.queued[0].ID); err != nil {
		t.Fatal(err)
	}
	if got := invoiceIDs(t, x); len(got) != 0 {
		t.Fatalf("%d rows left after the run", len(got))
	}
	if j, _ := mb.Job(context.Background(), mb.queued[0].ID); j.Status != BulkDone {
		t.Fatalf("job status %q, want done", j.Status)
	}
}

// A creator who lost the role between chunks stops the run: the first
// chunk lands, the rest never runs.
func TestBulkQueuedStopsWhenRoleRevoked(t *testing.T) {
	x, mb, policy := newQueuedUI(t, 150)
	calls := 0
	mb.principal = func(BulkJob) (context.Context, error) {
		calls++
		if calls == 1 {
			return bulkCtx("u1", policy, "clerk"), nil
		}
		return bulkCtx("u1", policy), nil
	}
	code, out := postBulk(t, x, bulkCtx("u1", policy, "clerk"), map[string]any{"action": "delete", "scope": "every", "count": "151"})
	if code != http.StatusAccepted {
		t.Fatalf("status %d: %v", code, out)
	}
	if err := x.ui.RunBulkJob(context.Background(), mb.queued[0].ID); err != nil {
		t.Fatal(err)
	}
	if got := len(invoiceIDs(t, x)); got != 151-InRequestCap {
		t.Fatalf("%d rows left, want %d (one chunk ran)", got, 151-InRequestCap)
	}
	if j, _ := mb.Job(context.Background(), mb.queued[0].ID); j.Status != BulkStopped {
		t.Fatalf("job status %q, want stopped", j.Status)
	}
	// A retried call on a stopped job does nothing.
	if err := x.ui.RunBulkJob(context.Background(), mb.queued[0].ID); err != nil {
		t.Fatal(err)
	}
	if got := len(invoiceIDs(t, x)); got != 151-InRequestCap {
		t.Fatalf("a stopped job ran again: %d left", got)
	}
}

// A creator context that cannot be rebuilt stops the run before any write.
func TestBulkQueuedStopsWithoutPrincipal(t *testing.T) {
	x, mb, policy := newQueuedUI(t, 150)
	mb.principal = func(BulkJob) (context.Context, error) { return nil, errors.New("user deleted") }
	if code, out := postBulk(t, x, bulkCtx("u1", policy, "clerk"), map[string]any{"action": "delete", "scope": "every", "count": "151"}); code != http.StatusAccepted {
		t.Fatalf("status %d: %v", code, out)
	}
	if err := x.ui.RunBulkJob(context.Background(), mb.queued[0].ID); err != nil {
		t.Fatal(err)
	}
	if got := len(invoiceIDs(t, x)); got != 151 {
		t.Fatalf("%d rows left, want all 151", got)
	}
}

// New refuses Jobs on a Host that keeps no snapshots.
func TestNewRefusesJobsWithoutBulkHost(t *testing.T) {
	x := newTestHost(t, invoiceEntities(), invoiceRows())
	if _, err := New(x.host, Extensions{Jobs: newMemBulk()}); err == nil {
		t.Fatal("New accepted Jobs on a Host with no BulkStore")
	}
}

// Each record is asked its own write gate: a Decider that denies one
// record skips it while the rest of the selection runs.
func TestBulkDeciderDeniesOneRecord(t *testing.T) {
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{Delete: "invoices:delete", Update: "invoices:update"}}
	ents["invoices"] = inv
	rows := invoiceRows()
	rows["invoices"] = append(rows["invoices"],
		map[string]any{"id": "inv-2", "number": "INV-2", "status": "draft"},
		map[string]any{"id": "inv-3", "number": "INV-3", "status": "draft"},
		map[string]any{"id": "inv-4", "number": "INV-4", "status": "draft"})
	x := newTestUI(t, ents, rows, withAPI(map[string]string{"invoices": "/api/invoices"}))
	policy := access.NewRolePolicy()
	policy.Register("invoices:delete", "invoices:update")
	if err := policy.Grant("clerk", "invoices:delete", "invoices:update"); err != nil {
		t.Fatal(err)
	}
	deny := func(_ context.Context, _ []string, _ access.Permission, r access.Ref) access.Decision {
		if r.ID == "inv-1" || r.ID == "inv-3" {
			return access.DecisionDeny
		}
		return access.DecisionAbstain
	}
	ctx := access.WithDecider(bulkCtx("u1", policy, "clerk"), deny)
	for _, c := range []struct{ action, a, b string }{{"move:send", "inv-3", "inv-4"}, {"delete", "inv-1", "inv-2"}} {
		code, out := postBulk(t, x, ctx, map[string]any{"action": c.action, "scope": "selected", "ids": []string{c.a, c.b}})
		if code != http.StatusOK || out["done"] != float64(1) || out["skipped"] != float64(1) {
			t.Fatalf("%s: status %d %v, want 1 done 1 skipped", c.action, code, out)
		}
	}
	if got := invoiceIDs(t, x); !slices.Equal(got, []string{"inv-1", "inv-3", "inv-4"}) {
		t.Fatalf("rows left = %v, want inv-1 inv-3 inv-4", got)
	}
	if invoiceStatus(t, x, "inv-3") != "draft" || invoiceStatus(t, x, "inv-4") != "open" {
		t.Fatal("the denied record moved, or the allowed one did not")
	}
}
