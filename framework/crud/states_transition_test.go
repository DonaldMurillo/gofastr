package crud

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	fwdb "github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// statesRouter mounts the invoices handler's full route set, transition
// route included, so route-level tests run through the real ServeMux.
func statesRouter(ch *CrudHandler) *router.Router {
	r := router.New()
	RegisterCrudRoutes(r, ch, "/invoices")
	return r
}

func transitionRequest(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// statesScopedWorld builds the invoices world with one extra scope group
// and the extra column it needs.
func statesScopedWorld(t *testing.T, scope *entity.ScopeConfig, extraCol string) (*CrudHandler, *sql.DB) {
	t.Helper()
	ddl := `CREATE TABLE invoices (
	id TEXT PRIMARY KEY,
	number TEXT NOT NULL,
	status TEXT DEFAULT 'draft',
	amount INTEGER,
	paid_on TEXT,
	created_at TEXT,
	updated_at TEXT,
	` + extraCol + `
)`
	return statesWorldDDL(t, ddl, func(c *entity.EntityConfig) { c.Scope = scope })
}

// ============================================================================
// RunTransition happy path
// ============================================================================

func TestRunTransitionHappyPath(t *testing.T) {
	ch, db := statesWorld(t)
	seedStateInvoice(t, db, "i1", "open", nil)
	ch.Hooks = hook.NewHookRegistry()
	var hookMove string
	ch.Hooks.RegisterHook(hook.BeforeUpdate, func(ctx context.Context, _ any) error {
		hookMove = TransitionFromContext(ctx)
		return nil
	})
	ch.Events = event.NewEventBus()
	updates := make(chan map[string]any, 4)
	cancel := ch.Events.Subscribe(event.EntityUpdated, func(_ context.Context, ev event.Event) error {
		if m, ok := ev.Data.(map[string]any); ok {
			updates <- m
		}
		return nil
	})
	defer cancel()

	res, err := ch.RunTransition(context.Background(), "i1", "pay")
	if err != nil {
		t.Fatalf("RunTransition pay: %v", err)
	}
	if res["status"] != "paid" {
		t.Fatalf("result status = %v, want paid", res["status"])
	}
	if hookMove != "pay" {
		t.Fatalf("BeforeUpdate hook saw move %q, want pay", hookMove)
	}
	status, paidOn := readStateInvoice(t, db, "i1")
	if status != "paid" {
		t.Fatalf("stored status = %q, want paid", status)
	}
	if want := time.Now().UTC().Format(time.DateOnly); paidOn.String != want {
		t.Fatalf("paid_on = %q, want today's UTC date %q", paidOn.String, want)
	}
	var updatedAt string
	if err := db.QueryRow(`SELECT updated_at FROM invoices WHERE id = 'i1'`).Scan(&updatedAt); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(updatedAt, time.Now().UTC().Format(time.DateOnly)) {
		t.Fatalf("updated_at = %q, want it restamped to now", updatedAt)
	}
	select {
	case m := <-updates:
		rec, _ := m[eventKeyRecord].(map[string]any)
		if rec["status"] != "paid" {
			t.Fatalf("entity.updated carried status %v, want paid", rec["status"])
		}
	case <-time.After(time.Second):
		t.Fatal("entity.updated not emitted")
	}
	select {
	case m := <-updates:
		t.Fatalf("entity.updated emitted twice: %v", m)
	default:
	}
}

// A Timestamp stamp gets the server's current instant, bound the way
// updated_at is, not a date.
func TestRunTransitionTimestampStamp(t *testing.T) {
	ch, db := statesWorld(t, func(c *entity.EntityConfig) {
		for i := range c.Fields {
			if c.Fields[i].Name == "paid_on" {
				c.Fields[i].Type = schema.Timestamp
			}
		}
	})
	seedStateInvoice(t, db, "i1", "open", nil)
	before := time.Now().UTC().Add(-time.Second)
	if _, err := ch.RunTransition(context.Background(), "i1", "pay"); err != nil {
		t.Fatalf("RunTransition pay: %v", err)
	}
	after := time.Now().UTC().Add(time.Second)
	_, paidOn := readStateInvoice(t, db, "i1")
	at, err := time.Parse(time.RFC3339Nano, paidOn.String)
	if err != nil {
		t.Fatalf("paid_on = %q, want an RFC 3339 instant: %v", paidOn.String, err)
	}
	if at.Before(before) || at.After(after) {
		t.Fatalf("paid_on = %v, want between %v and %v", at, before, after)
	}
}

// ============================================================================
// Conflicts and unknown keys
// ============================================================================

func TestRunTransitionConflictNamesOpenMoves(t *testing.T) {
	ch, db := statesWorld(t)
	seedStateInvoice(t, db, "i1", "paid", nil)

	_, err := ch.RunTransition(context.Background(), "i1", "issue")
	tce, ok := errors.AsType[*TransitionConflictError](err)
	if !ok {
		t.Fatalf("err = %v (%T), want *TransitionConflictError", err, err)
	}
	if tce.Current != "paid" {
		t.Fatalf("conflict current = %q, want paid", tce.Current)
	}
	assertMovesEqual(t, tce.Moves, []string{"void"})
	if status, _ := readStateInvoice(t, db, "i1"); status != "paid" {
		t.Fatalf("conflicting move changed the row to %q", status)
	}

	// The route answers the same refusal as 409 with current and moves.
	r := statesRouter(ch)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, transitionRequest(http.MethodPost, "/invoices/i1/transitions/issue"))
	if rr.Code != http.StatusConflict {
		t.Fatalf("route conflict = %d (%s), want 409", rr.Code, rr.Body.String())
	}
	body := decodeStateBody(t, rr)
	if body["current"] != "paid" {
		t.Fatalf("409 current = %v, want paid", body["current"])
	}
	assertMovesEqual(t, movesList(t, body), []string{"void"})
}

func TestRunTransitionUnknownAndSystemKeys(t *testing.T) {
	ch, db := statesWorld(t)
	seedStateInvoice(t, db, "i1", "open", nil)

	if _, err := ch.RunTransition(context.Background(), "i1", "bogus"); !errors.Is(err, ErrUnknownTransition) {
		t.Fatalf("unknown key = %v, want ErrUnknownTransition", err)
	}
	// A System move has no route and no permission ask: only Go code runs
	// it, and a bare context is enough.
	res, err := ch.RunTransition(context.Background(), "i1", "mark_overdue")
	if err != nil {
		t.Fatalf("system move from Go: %v", err)
	}
	if res["status"] != "void" {
		t.Fatalf("system move wrote status %v, want void", res["status"])
	}

	// The route answers 404 for an unknown key and for a System key alike.
	seedStateInvoice(t, db, "i2", "open", nil)
	r := statesRouter(ch)
	for _, key := range []string{"bogus", "mark_overdue"} {
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, transitionRequest(http.MethodPost, "/invoices/i2/transitions/"+key))
		if rr.Code != http.StatusNotFound {
			t.Fatalf("route for %q = %d (%s), want 404", key, rr.Code, rr.Body.String())
		}
	}
	if status, _ := readStateInvoice(t, db, "i2"); status != "open" {
		t.Fatalf("refused route call moved the row to %q", status)
	}
}

// ============================================================================
// Permissions
// ============================================================================

// statesPermittedWorld adds an update permission plus a move permission, so
// a caller needs both to run pay.
func statesPermittedWorld(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	return statesWorld(t, func(c *entity.EntityConfig) {
		c.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{Update: "invoices:write"}}
		c.States.Transitions[1].Permission = "invoices:pay"
	})
}

// A move's Permission is held by name: a role granted the Wildcard passes
// the entity's update permission but not the move's own, while a Decider
// that allows the move about this record still does.
func TestRunTransitionWildcardNoMove(t *testing.T) {
	ch, db := statesPermittedWorld(t)
	seedStateInvoice(t, db, "i1", "open", nil)

	policy := access.NewRolePolicy()
	if err := policy.Grant("root", access.Wildcard); err != nil {
		t.Fatal(err)
	}
	root := access.WithRoles(access.WithPolicy(ctxWithUser("u1"), policy), []string{"root"})
	_, err := ch.RunTransition(root, "i1", "pay")
	if err == nil || !strings.Contains(err.Error(), "missing permission invoices:pay") {
		t.Fatalf("wildcard move: err = %v, want a denial naming invoices:pay", err)
	}
	if status, _ := readStateInvoice(t, db, "i1"); status != "open" {
		t.Fatalf("wildcard move changed the row to %q", status)
	}

	allow := access.WithDecider(root, func(_ context.Context, _ []string, p access.Permission, ref access.Ref) access.Decision {
		if p == "invoices:pay" && ref.ID == "i1" {
			return access.DecisionAllow
		}
		return access.DecisionAbstain
	})
	if _, err := ch.RunTransition(allow, "i1", "pay"); err != nil {
		t.Fatalf("decider-allowed move: %v", err)
	}
}

func TestRunTransitionPermissionDenied(t *testing.T) {
	ch, db := statesPermittedWorld(t)
	seedStateInvoice(t, db, "i1", "open", nil)
	seedStateInvoice(t, db, "i2", "open", nil)

	policy := access.NewRolePolicy()
	policy.Grant("member", "invoices:write")
	updOnly := access.WithRoles(access.WithPolicy(ctxWithUser("u1"), policy), []string{"member"})

	denials := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{"no permission at all", context.Background(), "invoices:write"},
		{"entity update permission only", updOnly, "invoices:pay"},
		{"server writes do not bypass", WithServerWrites(context.Background()), "invoices:write"},
	}
	for _, d := range denials {
		_, err := ch.RunTransition(d.ctx, "i1", "pay")
		if err == nil || !strings.Contains(err.Error(), "missing permission "+d.want) {
			t.Fatalf("%s: err = %v, want denial naming %q", d.name, err, d.want)
		}
		if status, _ := readStateInvoice(t, db, "i1"); status != "open" {
			t.Fatalf("%s: denied move still changed the row to %q", d.name, status)
		}
	}

	// Both permissions granted: the move runs.
	policy.Grant("member", "invoices:pay")
	if _, err := ch.RunTransition(updOnly, "i1", "pay"); err != nil {
		t.Fatalf("granted move: %v", err)
	}
	if status, _ := readStateInvoice(t, db, "i1"); status != "paid" {
		t.Fatalf("granted move stored status %q", status)
	}

	// A Decider refusing exactly this record denies it; another id moves.
	denied := access.WithDecider(updOnly, func(_ context.Context, _ []string, _ access.Permission, ref access.Ref) access.Decision {
		if ref.Type == "invoices" && ref.ID == "i2" {
			return access.DecisionDeny
		}
		return access.DecisionAbstain
	})
	if _, err := ch.RunTransition(denied, "i2", "pay"); err == nil {
		t.Fatal("Decider-denied move went through")
	}
	if status, _ := readStateInvoice(t, db, "i2"); status != "open" {
		t.Fatalf("denied move changed the row to %q", status)
	}
	abstain := access.WithDecider(updOnly, func(context.Context, []string, access.Permission, access.Ref) access.Decision {
		return access.DecisionAbstain
	})
	if _, err := ch.RunTransition(abstain, "i2", "pay"); err != nil {
		t.Fatalf("abstaining Decider blocked a granted move: %v", err)
	}
}

// ============================================================================
// Owner / tenant / soft-delete scope
// ============================================================================

func TestRunTransitionOwnerIsolation(t *testing.T) {
	ch, db := statesScopedWorld(t, &entity.ScopeConfig{OwnerField: "user_id"}, "user_id TEXT")
	seedStateInvoice(t, db, "i1", "open", nil)
	if _, err := db.Exec(`UPDATE invoices SET user_id = 'alice' WHERE id = 'i1'`); err != nil {
		t.Fatal(err)
	}

	if _, err := ch.RunTransition(ctxWithUser("bob"), "i1", "pay"); !errors.Is(err, errNotFound) {
		t.Fatalf("SECURITY: bob moving alice's row = %v, want errNotFound", err)
	}
	if status, _ := readStateInvoice(t, db, "i1"); status != "open" {
		t.Fatalf("cross-owner move changed the row to %q", status)
	}
	if _, err := ch.RunTransition(ctxWithUser("alice"), "i1", "pay"); err != nil {
		t.Fatalf("owner's own move: %v", err)
	}

	// On the route the same attempt answers 404: no existence leak.
	seedStateInvoice(t, db, "i2", "open", nil)
	if _, err := db.Exec(`UPDATE invoices SET user_id = 'alice' WHERE id = 'i2'`); err != nil {
		t.Fatal(err)
	}
	r := statesRouter(ch)
	req := withTestUser(transitionRequest(http.MethodPost, "/invoices/i2/transitions/pay"), "bob")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("cross-owner route move = %d (%s), want 404", rr.Code, rr.Body.String())
	}
	if status, _ := readStateInvoice(t, db, "i2"); status != "open" {
		t.Fatalf("cross-owner route move changed the row to %q", status)
	}
}

func TestRunTransitionTenantIsolation(t *testing.T) {
	ch, db := statesScopedWorld(t, &entity.ScopeConfig{MultiTenant: true}, "tenant_id TEXT")
	seedStateInvoice(t, db, "i1", "open", nil)
	if _, err := db.Exec(`UPDATE invoices SET tenant_id = 't2' WHERE id = 'i1'`); err != nil {
		t.Fatal(err)
	}

	ctx := tenant.SetTenantID(context.Background(), "t1")
	if _, err := ch.RunTransition(ctx, "i1", "pay"); !errors.Is(err, errNotFound) {
		t.Fatalf("SECURITY: t1 moving t2's row = %v, want errNotFound", err)
	}
	if status, _ := readStateInvoice(t, db, "i1"); status != "open" {
		t.Fatalf("cross-tenant move changed the row to %q", status)
	}
	own := tenant.SetTenantID(context.Background(), "t2")
	if _, err := ch.RunTransition(own, "i1", "pay"); err != nil {
		t.Fatalf("same-tenant move: %v", err)
	}
}

func TestRunTransitionSoftDeletedHidden(t *testing.T) {
	ch, db := statesScopedWorld(t, &entity.ScopeConfig{SoftDelete: true}, "deleted_at TEXT")
	seedStateInvoice(t, db, "i1", "open", nil)
	if _, err := db.Exec(`UPDATE invoices SET deleted_at = '2026-01-01T00:00:00Z' WHERE id = 'i1'`); err != nil {
		t.Fatal(err)
	}

	if _, err := ch.RunTransition(context.Background(), "i1", "pay"); !errors.Is(err, errNotFound) {
		t.Fatalf("moving a soft-deleted row = %v, want errNotFound", err)
	}
	if status, _ := readStateInvoice(t, db, "i1"); status != "open" {
		t.Fatalf("move of a soft-deleted row changed it to %q", status)
	}
}

// ============================================================================
// The conditional UPDATE is pinned to From
// ============================================================================

// A concurrent write that moves the row between the read and the UPDATE
// must surface as a conflict, and the row must hold the concurrent value.
func TestRunTransitionRaceConflict(t *testing.T) {
	ch, db := statesWorld(t)
	seedStateInvoice(t, db, "i1", "open", nil)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeUpdate, func(ctx context.Context, _ any) error {
		if TransitionFromContext(ctx) != "pay" {
			return nil
		}
		tx, ok := fwdb.TxFromContext(ctx)
		if !ok {
			return errors.New("no transaction on the hook context")
		}
		_, err := tx.Exec(`UPDATE invoices SET status = 'void' WHERE id = 'i1'`)
		return err
	})

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	_, err = ch.RunTransition(fwdb.WithTx(context.Background(), tx), "i1", "pay")
	tce, ok := errors.AsType[*TransitionConflictError](err)
	if !ok {
		t.Fatalf("raced move = %v (%T), want *TransitionConflictError", err, err)
	}
	if tce.Current != "void" {
		t.Fatalf("conflict current = %q, want void (the hook's value)", tce.Current)
	}
	// The caller owns the ambient transaction: commit keeps the hook's
	// write, proving the move did not clobber it.
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	status, paidOn := readStateInvoice(t, db, "i1")
	if status != "void" {
		t.Fatalf("row status = %q, want the concurrent writer's void", status)
	}
	if paidOn.Valid {
		t.Fatalf("raced move still stamped paid_on = %q", paidOn.String)
	}
}

// ============================================================================
// Route contract
// ============================================================================

func TestTransitionRouteContract(t *testing.T) {
	ch, db := statesWorld(t)
	seedStateInvoice(t, db, "i1", "open", nil)
	r := statesRouter(ch)

	// No JSON content type: a cross-site form cannot run a move.
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/invoices/i1/transitions/pay", nil))
	if rr.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("transition without JSON content type = %d, want 415", rr.Code)
	}

	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, transitionRequest(http.MethodPost, "/invoices/i1/transitions/pay"))
	if rr.Code != http.StatusOK {
		t.Fatalf("transition = %d (%s), want 200", rr.Code, rr.Body.String())
	}
	if cc := rr.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
	data, _ := decodeStateBody(t, rr)["data"].(map[string]any)
	if data == nil || data["status"] != "paid" {
		t.Fatalf("200 body is not the moved record in the single envelope: %v", data)
	}
	if status, _ := readStateInvoice(t, db, "i1"); status != "paid" {
		t.Fatalf("route move stored status %q", status)
	}
}

func TestTransitionRouteNotMountedWithoutMoves(t *testing.T) {
	// All moves System: no route.
	ch, _ := statesWorld(t, func(c *entity.EntityConfig) {
		c.States.Transitions = []entity.Transition{c.States.Transitions[3]}
	})
	r := statesRouter(ch)
	for _, rt := range r.Routes() {
		if strings.Contains(rt.Pattern, "transitions") {
			t.Fatalf("all-System states mounted a transition route: %s", rt.Pattern)
		}
	}

	// No states at all: no route.
	ch2, _ := statesWorld(t, func(c *entity.EntityConfig) { c.States = nil })
	r2 := statesRouter(ch2)
	for _, rt := range r2.Routes() {
		if strings.Contains(rt.Pattern, "transitions") {
			t.Fatalf("states-free entity mounted a transition route: %s", rt.Pattern)
		}
	}
	rr := httptest.NewRecorder()
	r2.ServeHTTP(rr, transitionRequest(http.MethodPost, "/invoices/i1/transitions/pay"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("transition on a states-free entity = %d, want 404", rr.Code)
	}
}

// ============================================================================
// MCP tools
// ============================================================================

func TestMCPTransitionTools(t *testing.T) {
	ch, db := statesWorld(t)
	seedStateInvoice(t, db, "i1", "open", nil)
	r := statesRouter(ch)
	srv := mcp.NewServer()
	if err := RegisterEntityMCPTools(srv, ch, r); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"invoices_issue", "invoices_pay", "invoices_void"} {
		if !srv.HasTool(name) {
			t.Fatalf("non-system move %q has no MCP tool", name)
		}
	}
	if srv.HasTool("invoices_mark_overdue") {
		t.Fatal("System move mark_overdue exposed an MCP tool")
	}

	if _, err := srv.CallTool(ctxWithUser("u1"), "invoices_pay", map[string]any{"id": "i1"}); err != nil {
		t.Fatalf("invoices_pay tool: %v", err)
	}
	status, paidOn := readStateInvoice(t, db, "i1")
	if status != "paid" {
		t.Fatalf("tool move stored status %q", status)
	}
	if !paidOn.Valid {
		t.Fatal("tool move did not stamp paid_on")
	}
}

// ============================================================================
// Re-entrant moves
// ============================================================================

// A hook cannot move the record whose write is running it: the nested move
// would either break the outer statement's From pin (rolling everything
// back) or leave the outer write answering a state it no longer holds. A
// move of another record from the same hook still runs.
func TestRunTransitionRefusesReentry(t *testing.T) {
	ch, db := statesWorld(t)
	seedStateInvoice(t, db, "i1", "open", nil)
	seedStateInvoice(t, db, "i2", "draft", nil)
	var same, other, fromEdit error
	editing := false
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterUpdate, func(ctx context.Context, _ any) error {
		if TransitionFromContext(ctx) == "pay" {
			_, same = ch.RunTransition(ctx, "i1", "void")
			_, other = ch.RunTransition(ctx, "i2", "issue")
		}
		return nil
	})
	ch.Hooks.RegisterHook(hook.BeforeUpdate, func(ctx context.Context, _ any) error {
		if editing {
			editing = false
			_, fromEdit = ch.RunTransition(ctx, "i1", "void")
		}
		return nil
	})

	row, err := ch.RunTransition(context.Background(), "i1", "pay")
	if err != nil {
		t.Fatalf("outer move: %v", err)
	}
	if !errors.Is(same, ErrReentrantMove) {
		t.Fatalf("nested move of the same record = %v, want ErrReentrantMove", same)
	}
	if other != nil {
		t.Fatalf("nested move of another record = %v, want nil", other)
	}
	if row["status"] != "paid" {
		t.Fatalf("outer move answered %v, want paid", row["status"])
	}
	if s, _ := readStateInvoice(t, db, "i1"); s != "paid" {
		t.Fatalf("i1 stored %q, want paid", s)
	}
	if s, _ := readStateInvoice(t, db, "i2"); s != "open" {
		t.Fatalf("i2 stored %q, want open", s)
	}

	editing = true
	if _, err := ch.UpdateOne(context.Background(), "i1", map[string]any{"amount": 9}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if !errors.Is(fromEdit, ErrReentrantMove) {
		t.Fatalf("move from an edit's hook of the same record = %v, want ErrReentrantMove", fromEdit)
	}
	if s, _ := readStateInvoice(t, db, "i1"); s != "paid" {
		t.Fatalf("i1 stored %q after the edit, want paid", s)
	}
}

// A refusal names the stored state and the open moves only to a caller
// whose ReadScope admits the record: one the scope hides answers the same
// 409/422 with neither, so a write-capable caller cannot read a hidden
// record's state through a refused move or edit.
func TestStateRefusalHidesFromReadScope(t *testing.T) {
	ch, db := statesWorld(t, func(c *entity.EntityConfig) {
		c.Exposure.ReadScope = &entity.ReadScopeConfig{
			Filter: []entity.RowPredicate{{Field: "status", Op: "in", Values: []string{"draft", "open"}}},
		}
	})
	seedStateInvoice(t, db, "hidden", "paid", "2026-01-02")
	seedStateInvoice(t, db, "shown", "open", nil)
	ctx := context.Background()

	_, err := ch.RunTransition(ctx, "hidden", "issue")
	tce, ok := errors.AsType[*TransitionConflictError](err)
	if !ok {
		t.Fatalf("move on a hidden record = %v, want TransitionConflictError", err)
	}
	if tce.Current != "" || tce.Moves != nil || strings.Contains(tce.Error(), "from") {
		t.Fatalf("hidden record's conflict names its state: %+v %q", tce, tce.Error())
	}
	_, err = ch.UpdateOne(ctx, "hidden", map[string]any{"status": "draft"})
	se := stateErr(t, err)
	if se.Current != "" || se.Moves != nil || strings.Contains(se.Error(), "from") {
		t.Fatalf("hidden record's state error names its state: %+v %q", se, se.Error())
	}

	_, err = ch.RunTransition(ctx, "shown", "issue")
	if tce, ok := errors.AsType[*TransitionConflictError](err); !ok || tce.Current != "open" || len(tce.Moves) == 0 {
		t.Fatalf("visible record's conflict = %v, want current open with moves", err)
	}
}

// Two moves of one record on two SQLite connections: the loser's deferred
// transaction read the old state, and the winner committed before it
// wrote, so SQLite refuses the loser's write with SQLITE_BUSY rather than
// matching zero rows. RunTransition restarts a move it began itself on
// BUSY, and the restart reads the winner's state: a typed conflict (409),
// never a database error (500).
func TestRunTransitionSQLiteBusyConflict(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "race.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(2)
	for _, stmt := range []string{"PRAGMA journal_mode = WAL", "PRAGMA busy_timeout = 2000", statesInvoiceDDL} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	cfg := entity.EntityConfig{
		Name: "invoices", Table: "invoices",
		Fields: statesInvoiceFields(), States: statesInvoiceStates(),
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(true)
	ent := entity.Define(cfg.Table, cfg)
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	seedStateInvoice(t, db, "i1", "open", nil)

	raced := false
	var winner error
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeUpdate, func(ctx context.Context, _ any) error {
		if TransitionFromContext(ctx) != "pay" || raced {
			return nil
		}
		raced = true
		// The winner runs to commit on the other connection while the
		// loser's transaction holds its read of "open".
		_, winner = ch.RunTransition(context.Background(), "i1", "void")
		return nil
	})

	_, err = ch.RunTransition(context.Background(), "i1", "pay")
	if winner != nil {
		t.Fatalf("winning move: %v", winner)
	}
	tce, ok := errors.AsType[*TransitionConflictError](err)
	if !ok {
		t.Fatalf("losing move = %v (%T), want *TransitionConflictError", err, err)
	}
	if tce.Current != "void" {
		t.Fatalf("conflict current = %q, want the winner's void", tce.Current)
	}
	if s, paidOn := readStateInvoice(t, db, "i1"); s != "void" || paidOn.Valid {
		t.Fatalf("stored %q paid_on %v, want void and no stamp", s, paidOn)
	}
}
