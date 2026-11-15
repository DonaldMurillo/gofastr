package crud

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	fwdb "github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// A Postgres NOT NULL refusal that names another relation's column is not
// about this table's field, even when the column names match.
func TestNotNullColumnOtherRelationIsNotOurs(t *testing.T) {
	msg := `ERROR: null value in column "title" of relation "other" violates not-null constraint (SQLSTATE 23502)`
	if got := notNullColumn("inv", msg); got != "" {
		t.Fatalf("notNullColumn named %q from another relation's refusal", got)
	}
}

// A hook that writes the record a restore is restoring records its own
// operation: the "restore" override names the restore's write only, and
// inTx drops it before a nested write starts.
func TestHookWriteDuringRestoreRecordsItsOwnOperation(t *testing.T) {
	ch, db := setupSoftDeleteWorld(t)
	seedInvoice(t, db, "a1", "alice", "2026-01-01T00:00:00Z")

	var ops []string
	nested := false
	ch.Hooks.RegisterHook(hook.AfterUpdate, func(ctx context.Context, _ any) error {
		ops = append(ops, AuditOperationFor(ctx, "invoices", "a1"))
		if nested {
			return nil
		}
		nested = true
		_, err := ch.UpdateOne(ctx, "a1", map[string]any{"title": "touched by a hook"})
		return err
	})

	if err := ch.RestoreOne(ctxWithUser("alice"), "a1"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if len(ops) != 2 {
		t.Fatalf("AfterUpdate ran %d times, want 2 (restore, then the hook's update)", len(ops))
	}
	if ops[1] != "" {
		t.Fatalf("the hook's own update recorded operation %q, want none", ops[1])
	}
	if ops[0] != "restore" {
		t.Fatalf("the restore recorded operation %q, want restore", ops[0])
	}
}

// The restore's UPDATE matches only a row that is still soft-deleted: a
// row restored between the read and the write answers not found rather
// than a second, silent restore.
func TestRestoreUpdatePinnedToTrashedRow(t *testing.T) {
	ch, db := setupSoftDeleteWorld(t)
	seedInvoice(t, db, "a1", "alice", "2026-01-01T00:00:00Z")
	ch.Hooks.RegisterHook(hook.BeforeUpdate, func(ctx context.Context, _ any) error {
		tx, ok := fwdb.TxFromContext(ctx)
		if !ok {
			t.Fatal("restore hook ran outside its transaction")
		}
		_, err := tx.ExecContext(ctx, `UPDATE invoices SET deleted_at = NULL WHERE id = 'a1'`)
		return err
	})
	if err := ch.RestoreOne(ctxWithUser("alice"), "a1"); !errors.Is(err, errNotFound) {
		t.Fatalf("restore of a row restored mid-write = %v, want errNotFound", err)
	}
}

// A BeforeUpdate hook vetoes a restore: the error comes back and the row
// stays soft-deleted.
func TestRestoreBeforeUpdateHookVetoes(t *testing.T) {
	ch, db := setupSoftDeleteWorld(t)
	seedInvoice(t, db, "a1", "alice", "2026-01-01T00:00:00Z")
	veto := errors.New("retention policy")
	ch.Hooks.RegisterHook(hook.BeforeUpdate, func(context.Context, any) error { return veto })
	if err := ch.RestoreOne(ctxWithUser("alice"), "a1"); err == nil {
		t.Fatal("a vetoed restore succeeded")
	}
	if rowDeletedAt(t, db, "invoices", "a1") {
		t.Fatal("a vetoed restore cleared deleted_at")
	}
}

// The restore override answers only for its own entity and record.
func TestAuditOperationNamesOnlyItsRecord(t *testing.T) {
	ch, db := setupSoftDeleteWorld(t)
	seedInvoice(t, db, "a1", "alice", "2026-01-01T00:00:00Z")
	var otherID, otherEntity string
	ch.Hooks.RegisterHook(hook.AfterUpdate, func(ctx context.Context, _ any) error {
		otherID = AuditOperationFor(ctx, "invoices", "a2")
		otherEntity = AuditOperationFor(ctx, "notes", "a1")
		return nil
	})
	if err := ch.RestoreOne(ctxWithUser("alice"), "a1"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if otherID != "" || otherEntity != "" {
		t.Fatalf("override leaked: other id %q, other entity %q", otherID, otherEntity)
	}
}

// WithServerWrites skips the permission question on restore and purge, as
// on every other trusted write; owner scoping still applies.
func TestServerWritesRestoreAndPurgeSkipPermission(t *testing.T) {
	ch, db := setupSoftDeleteWorld(t)
	ch.Entity.Config.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{
		Update: "invoices:write",
		Delete: "invoices:delete",
	}}
	seedInvoice(t, db, "r1", "alice", "2026-01-01T00:00:00Z")
	seedInvoice(t, db, "p1", "alice", "2026-01-01T00:00:00Z")
	ctx := access.WithRoles(access.WithPolicy(ctxWithUser("alice"), access.NewRolePolicy()), []string{"member"})
	ctx = WithServerWrites(ctx)
	if err := ch.RestoreOne(ctx, "r1"); err != nil {
		t.Fatalf("server-write restore: %v", err)
	}
	if err := ch.PurgeOne(ctx, "p1"); err != nil {
		t.Fatalf("server-write purge: %v", err)
	}
}

// Storage-key provenance is checked on the caller's body: a key a
// BeforeCreate hook sets is the server's own value.
func TestHookSetStorageKeySkipsProvenance(t *testing.T) {
	ch, db := provWorld(t)
	if ch.Hooks == nil {
		ch.Hooks = hook.NewHookRegistry()
	}
	key := "uploads/prov_media/photo/hook_set.png"
	ch.Hooks.RegisterHook(hook.BeforeCreate, func(_ context.Context, data any) error {
		data.(map[string]any)["photo"] = key
		return nil
	})
	if _, err := ch.CreateOne(ctxWithUser("u1"), map[string]any{"id": "h1", "caption": "c"}); err != nil {
		t.Fatalf("create with a hook-set key: %v", err)
	}
	if got := provCol(t, db, "h1", "photo"); got != key {
		t.Fatalf("photo = %q, want the hook's key", got)
	}
}

// A Before hook sees a form's number as a number, on create and update:
// a typed hook decodes the body into its struct before validation runs.
func TestBeforeHooksSeeCoercedNumbers(t *testing.T) {
	db := setupDB(t, `CREATE TABLE stock (id TEXT PRIMARY KEY, qty INTEGER)`)
	ent := entity.Define("stock", entity.EntityConfig{Name: "stock", Table: "stock",
		Fields: []schema.Field{{Name: "qty", Type: schema.Int}}}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Hooks = hook.NewHookRegistry()
	var seen []any
	record := func(_ context.Context, data any) error {
		seen = append(seen, data.(map[string]any)["qty"])
		return nil
	}
	ch.Hooks.RegisterHook(hook.BeforeCreate, record)
	ch.Hooks.RegisterHook(hook.BeforeUpdate, record)
	row, err := ch.CreateOne(context.Background(), map[string]any{"qty": "5"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id, _ := ch.rowID(row)
	if _, err := ch.UpdateOne(context.Background(), id, map[string]any{"qty": "7"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("hooks ran %d times, want 2", len(seen))
	}
	for i, v := range seen {
		if _, isText := v.(string); isText {
			t.Fatalf("hook %d saw qty as text %q", i, v)
		}
	}
}

// doCreate refuses a row on a multi-tenant entity when the context names no
// tenant, whichever path reached it.
func TestCreateWithoutTenantIsRefused(t *testing.T) {
	db := setupDB(t, `CREATE TABLE mt (id TEXT PRIMARY KEY, tenant_id TEXT, body TEXT)`)
	ent := entity.Define("mt", entity.EntityConfig{Name: "mt", Table: "mt", Scope: &entity.ScopeConfig{MultiTenant: true},
		Fields: []schema.Field{{Name: "body", Type: schema.String}}}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ctx := context.Background()
	err := ch.inTx(ctx, func(ctx context.Context, ch *CrudHandler) error {
		_, err := ch.doCreate(ctx, syntheticRequest(ctx, "POST", "/"), map[string]any{"id": "o1", "body": "orphan"})
		return err
	})
	if _, ok := errors.AsType[*tenantMissingError](err); !ok {
		t.Fatalf("create without a tenant = %v, want tenantMissingError", err)
	}
}

// fkWorld is items (owner-scoped) referencing orders (owner-scoped, soft
// delete) and ghosts, an entity the registry does not hold.
func fkWorld(t *testing.T) func(body string) *httptest.ResponseRecorder {
	t.Helper()
	ddl := `
CREATE TABLE orders (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, deleted_at TEXT);
CREATE TABLE items (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, order_id TEXT, ghost_id TEXT, label TEXT);
`
	ordersCfg := makeEntityConfig("orders", "orders", "user_id",
		[]schema.Field{{Name: "user_id", Type: schema.String, Required: true}},
		func(c *entity.EntityConfig) { c.Scope.SoftDelete = true })
	itemsCfg := makeEntityConfig("items", "items", "user_id",
		[]schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "order_id", Type: schema.String},
			{Name: "ghost_id", Type: schema.String},
			{Name: "label", Type: schema.String},
		},
		func(c *entity.EntityConfig) {
			c.Relations = []entity.Relation{
				entity.BelongsTo("order", "orders", "order_id"),
				entity.BelongsTo("ghost", "ghosts", "ghost_id"),
			}
		})
	ch, db := setupSecurityTestHandler(t, itemsCfg, ddl)
	ordersEnt := entity.Define(ordersCfg.Table, ordersCfg)
	ordersEnt.SetDB(db)
	reg := newTestRegistry(t)
	reg.add(t, ch.Entity)
	reg.add(t, ordersEnt)
	ch.Registry = reg
	seedRows(t, db, "orders", []map[string]any{
		{"id": "o-live", "user_id": "alice"},
		{"id": "o-gone", "user_id": "alice", "deleted_at": "2026-01-01T00:00:00Z"},
	})
	return func(body string) *httptest.ResponseRecorder {
		req := withTestUser(httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(body)), "alice")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		ch.Create()(rr, req)
		return rr
	}
}

// A null foreign key names no row, so the scope check does not look one up.
func TestBelongsToNullKeyIsNotLookedUp(t *testing.T) {
	post := fkWorld(t)
	if rr := post(`{"id":"i1","label":"x","order_id":null}`); rr.Code != http.StatusCreated {
		t.Fatalf("create with a null order_id = %d (%s), want 201", rr.Code, rr.Body.String())
	}
}

// A reference onto an entity the registry does not hold is not checked:
// there is no scope to check it against.
func TestBelongsToUnregisteredTargetIsSkipped(t *testing.T) {
	post := fkWorld(t)
	if rr := post(`{"id":"i1","label":"x","order_id":"o-live","ghost_id":"g1"}`); rr.Code != http.StatusCreated {
		t.Fatalf("create naming an unregistered target = %d (%s), want 201", rr.Code, rr.Body.String())
	}
}

// A soft-deleted target is a missing one.
func TestBelongsToSoftDeletedTargetIsRefused(t *testing.T) {
	post := fkWorld(t)
	if rr := post(`{"id":"i1","label":"x","order_id":"o-gone"}`); rr.Code != http.StatusNotFound {
		t.Fatalf("create onto a soft-deleted order = %d (%s), want 404", rr.Code, rr.Body.String())
	}
	if rr := post(`{"id":"i2","label":"x","order_id":"o-live"}`); rr.Code != http.StatusCreated {
		t.Fatalf("control: create onto a live order = %d (%s), want 201", rr.Code, rr.Body.String())
	}
}

// An update never moves a row to another tenant: the tenant column is
// not in its SET, whatever the body says.
func TestUpdateKeepsTenantColumn(t *testing.T) {
	db := setupDB(t, `CREATE TABLE mt (id TEXT PRIMARY KEY, tenant_id TEXT, body TEXT)`)
	ent := entity.Define("mt", entity.EntityConfig{Name: "mt", Table: "mt", Scope: &entity.ScopeConfig{MultiTenant: true},
		Fields: []schema.Field{{Name: "body", Type: schema.String}}}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	seedRows(t, db, "mt", []map[string]any{{"id": "r1", "tenant_id": "t-a", "body": "b"}})
	ctx := WithServerWrites(tenant.SetTenantID(context.Background(), "t-a"))
	if _, err := ch.UpdateOne(ctx, "r1", map[string]any{"body": "c", "tenant_id": "t-b"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	var got string
	if err := db.QueryRow(`SELECT tenant_id FROM mt WHERE id = 'r1'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "t-a" {
		t.Fatalf("tenant_id = %q after an update that sent t-b, want t-a", got)
	}
}

// A cascade-only update finds its parent under the owner write scope: a
// CrossOwnerRead grant reads another owner's row but cannot attach
// children to it.
func TestCrossOwnerReadCannotCascadeIntoForeignRow(t *testing.T) {
	db := setupDB(t,
		`CREATE TABLE posts (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT)`,
		`CREATE TABLE notes (id TEXT PRIMARY KEY, post_id TEXT, body TEXT)`,
	)
	notes := entity.Define("notes", entity.EntityConfig{Table: "notes",
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID, ReadOnly: true},
			{Name: "post_id", Type: schema.String},
			{Name: "body", Type: schema.String},
		}}.WithTimestamps(false))
	posts := entity.Define("posts", entity.EntityConfig{Table: "posts",
		Scope: &entity.ScopeConfig{OwnerField: "user_id", CrossOwnerRead: "posts:read:all"},
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "title", Type: schema.String},
		},
		Relations: []entity.Relation{entity.HasMany("notes", "notes", "post_id").WithCascadeWrite(true)},
	}.WithTimestamps(false))
	notes.SetDB(db)
	posts.SetDB(db)
	installOwnerExtractor(t)
	ch := NewCrudHandler(posts, db).WithJSONCase(CaseSnake)
	ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"notes": notes, "posts": posts}}
	seedRows(t, db, "posts", []map[string]any{{"id": "p-alice", "user_id": "alice", "title": "hers"}})

	policy := access.NewRolePolicy()
	policy.Grant("auditor", "posts:read:all")
	ctx := access.WithRoles(access.WithPolicy(ctxWithUser("bob"), policy), []string{"auditor"})
	if _, err := ch.GetOne(ctx, "p-alice", nil); err != nil {
		t.Fatalf("control: the grant does not read alice's post: %v", err)
	}
	_, err := ch.UpdateOne(ctx, "p-alice", map[string]any{"notes": []any{map[string]any{"body": "planted"}}})
	if !errors.Is(err, errNotFound) {
		t.Fatalf("cascade-only update of a foreign post = %v, want errNotFound", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notes`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d notes attached to another owner's post", n)
	}
}

// A move's UPDATE skips a row soft-deleted between its read and its write.
func TestRunTransitionUpdateSkipsRowDeletedMidMove(t *testing.T) {
	ch, db := statesScopedWorld(t, &entity.ScopeConfig{SoftDelete: true}, "deleted_at TEXT")
	seedStateInvoice(t, db, "i1", "open", nil)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeUpdate, func(ctx context.Context, _ any) error {
		tx, ok := fwdb.TxFromContext(ctx)
		if !ok {
			t.Fatal("move hook ran outside its transaction")
		}
		_, err := tx.ExecContext(ctx, `UPDATE invoices SET deleted_at = '2026-01-01T00:00:00Z' WHERE id = 'i1'`)
		return err
	})
	if _, err := ch.RunTransition(context.Background(), "i1", "pay"); err == nil {
		t.Fatal("a move of a row deleted mid-move succeeded")
	}
	if status, _ := readStateInvoice(t, db, "i1"); status != "open" {
		t.Fatalf("a row deleted mid-move was moved to %q", status)
	}
}

// A move finds its row under the owner write scope: a CrossOwnerRead grant
// reads another owner's record but cannot move it.
func TestCrossOwnerReadCannotMoveForeignRow(t *testing.T) {
	ddl := `CREATE TABLE invoices (id TEXT PRIMARY KEY, number TEXT NOT NULL, status TEXT DEFAULT 'draft',
	amount INTEGER, paid_on TEXT, created_at TEXT, updated_at TEXT, user_id TEXT)`
	ch, db := statesWorldDDL(t, ddl, func(c *entity.EntityConfig) {
		c.Scope = &entity.ScopeConfig{OwnerField: "user_id", CrossOwnerRead: "invoices:read:all"}
		c.Fields = append(c.Fields, schema.Field{Name: "user_id", Type: schema.String})
	})
	seedStateInvoice(t, db, "i1", "open", nil)
	if _, err := db.Exec(`UPDATE invoices SET user_id = 'alice' WHERE id = 'i1'`); err != nil {
		t.Fatal(err)
	}
	policy := access.NewRolePolicy()
	policy.Grant("auditor", "invoices:read:all")
	policy.Grant("auditor", "invoices:update")
	ctx := access.WithRoles(access.WithPolicy(ctxWithUser("bob"), policy), []string{"auditor"})
	if _, err := ch.GetOne(ctx, "i1", nil); err != nil {
		t.Fatalf("control: the grant does not read alice's invoice: %v", err)
	}
	if _, err := ch.RunTransition(ctx, "i1", "pay"); !errors.Is(err, errNotFound) {
		t.Fatalf("moving another owner's invoice = %v, want errNotFound", err)
	}
	if status, _ := readStateInvoice(t, db, "i1"); status != "open" {
		t.Fatalf("another owner's invoice moved to %q", status)
	}
}

// The transition route answers the id only when the move takes the row out
// of the caller's read scope.
func TestTransitionRouteHidesRowLeavingReadScope(t *testing.T) {
	ch, db := statesWorld(t, func(c *entity.EntityConfig) {
		c.Exposure = &entity.ExposureConfig{Public: true, ReadScope: &entity.ReadScopeConfig{
			Filter: []entity.RowPredicate{{Field: "status", Value: "open"}},
		}}
	})
	seedStateInvoice(t, db, "i1", "open", nil)
	req := httptest.NewRequest(http.MethodPost, "/invoices/i1/transitions/pay", nil)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "i1")
	req.SetPathValue("key", "pay")
	rr := httptest.NewRecorder()
	ch.Transition()(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("move = %d (%s), want 200", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "row i1") {
		t.Fatalf("a row the move hid came back whole: %s", rr.Body.String())
	}
}

// An update that changes the state field under WithStateOverride is
// audited as "state_override" for that record.
func TestStateOverrideUpdateAuditsItsOperation(t *testing.T) {
	ch, db := statesWorld(t)
	ch.Entity.MarkAudited()
	seedStateInvoice(t, db, "i1", "open", nil)
	ch.Hooks = hook.NewHookRegistry()
	var op string
	ch.Hooks.RegisterHook(hook.AfterUpdate, func(ctx context.Context, _ any) error {
		op = AuditOperationFor(ctx, "invoices", "i1")
		return nil
	})
	if _, err := ch.UpdateOne(WithStateOverride(context.Background(), "repair"), "i1", map[string]any{"status": "paid"}); err != nil {
		t.Fatalf("override update: %v", err)
	}
	if op != "state_override" {
		t.Fatalf("override update audited as %q, want state_override", op)
	}
}

// UpsertOne runs the media-URL allow-list as create and update do: a
// javascript: value in an Image field is refused, even from server code
// that skips storage-key provenance.
func TestUpsertRefusesScriptMediaURL(t *testing.T) {
	ch, db := provWorld(t)
	ctx := WithServerWrites(ctxWithUser("u1"))
	_, err := ch.UpsertOne(ctx, map[string]any{"id": "x1", "photo": "javascript:alert(1)"})
	if err == nil {
		t.Fatal("upsert stored a javascript: URL in an Image field")
	}
	if got := provCol(t, db, "x1", "photo"); got != "<no row>" {
		t.Fatalf("refused upsert left photo %q", got)
	}
}

// A caller-supplied AutoIncrement key past exact float precision is a
// validation error, not a rounded conflict target.
func TestUpsertRefusesInexactIncrementKey(t *testing.T) {
	cfg := makeEntityConfig("counter_items", "counter_items", "", []schema.Field{
		{Name: "id", Type: schema.Int, AutoGenerate: schema.AutoIncrement},
		{Name: "title", Type: schema.String},
	})
	ch, _ := setupSecurityTestHandler(t, cfg,
		`CREATE TABLE counter_items (id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT)`)
	_, err := ch.UpsertOne(context.Background(), map[string]any{"id": float64(1 << 60), "title": "t"})
	if _, ok := errors.AsType[*ValidationError](err); !ok {
		t.Fatalf("upsert with a key past float precision = %v, want a ValidationError", err)
	}
}

// A blank form value a BeforeCreate hook then fills is a value: the
// conflict arm writes the hook's value, not a cleared column.
func TestUpsertHookFilledBlankIsKept(t *testing.T) {
	db := setupDB(t, `CREATE TABLE stock (id TEXT PRIMARY KEY, qty INTEGER)`)
	ent := entity.Define("stock", entity.EntityConfig{Name: "stock", Table: "stock",
		Fields: []schema.Field{{Name: "id", Type: schema.String}, {Name: "qty", Type: schema.Int}}}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	seedRows(t, db, "stock", []map[string]any{{"id": "s1", "qty": 1}})
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeCreate, func(_ context.Context, data any) error {
		data.(map[string]any)["qty"] = 5
		return nil
	})
	if _, err := ch.UpsertOne(context.Background(), map[string]any{"id": "s1", "qty": ""}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	var qty sql.NullInt64
	if err := db.QueryRow(`SELECT qty FROM stock WHERE id = 's1'`).Scan(&qty); err != nil {
		t.Fatal(err)
	}
	if !qty.Valid || qty.Int64 != 5 {
		t.Fatalf("qty = %v, want the hook's 5", qty)
	}
}

// An upsert that lands on an existing row and writes nothing runs no
// After hook and announces nothing.
func TestUpsertThatWritesNothingIsSilent(t *testing.T) {
	db := setupDB(t, `CREATE TABLE stock (id TEXT PRIMARY KEY, qty INTEGER)`)
	ent := entity.Define("stock", entity.EntityConfig{Name: "stock", Table: "stock",
		Fields: []schema.Field{{Name: "id", Type: schema.String}, {Name: "qty", Type: schema.Int}}}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	seedRows(t, db, "stock", []map[string]any{{"id": "s1", "qty": 1}})
	ch.Hooks = hook.NewHookRegistry()
	ran := 0
	count := func(context.Context, any) error { ran++; return nil }
	ch.Hooks.RegisterHook(hook.AfterCreate, count)
	ch.Hooks.RegisterHook(hook.AfterUpdate, count)
	bus := event.NewEventBus()
	ch.Events = bus
	got := make(chan event.Event, 4)
	for _, typ := range []string{event.EntityCreated, event.EntityUpdated} {
		cancel := bus.Subscribe(typ, func(_ context.Context, ev event.Event) error {
			got <- ev
			return nil
		})
		defer cancel()
	}
	if _, err := ch.UpsertOne(context.Background(), map[string]any{"id": "s1"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if ran != 0 {
		t.Fatalf("a no-op upsert ran %d After hooks", ran)
	}
	select {
	case ev := <-got:
		t.Fatalf("a no-op upsert announced %v", ev.Data)
	case <-time.After(300 * time.Millisecond):
	}
}

// A many-to-many link onto an entity whose CRUD is off answers the
// not-found an unreadable row does, so a link is no existence oracle.
func TestCascadeLinkOntoUnreachableTargetIsNotFound(t *testing.T) {
	ch, db := cascadeNotesHandler(t, CaseSnake, "")
	seedRows(t, db, "notes", []map[string]any{{"id": "n1", "status": "published", "body": "PUB"}})
	off := false
	ch.Registry.(stubRegistry).byName["notes"].Config.Exposure.CRUD = &off
	rec := sendPosts(ch, http.MethodPost, "/posts", "", `{"title":"t","tagged":["n1"]}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("link onto a CRUD-off note = %d (%s), want 404", rec.Code, rec.Body.String())
	}
}

// A many-to-many link id onto an Int key is refused when it is a float
// that is not an exact integer.
func TestCascadeLinkRefusesInexactIntID(t *testing.T) {
	target := entity.Define("counters", entity.EntityConfig{Table: "counters",
		Fields: []schema.Field{{Name: "id", Type: schema.Int}}}.WithTimestamps(false))
	if _, err := coerceLinkPK(nil, target, "id", "tagged.0", float64(1<<60)); err == nil {
		t.Fatal("a float past exact integer precision was accepted as a link id")
	}
	if got, err := coerceLinkPK(nil, target, "id", "tagged.0", float64(7)); err != nil || got != int64(7) {
		t.Fatalf("control: 7.0 = %v, %v; want int64 7", got, err)
	}
}
