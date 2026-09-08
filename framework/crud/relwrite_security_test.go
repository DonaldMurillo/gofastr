package crud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

// Pins: an owner-scoped row's belongs_to relation is a write-side trust
// boundary. The read side resolves include=/EagerLoad targets under
// eagerScopeFilters, so a foreign parent never surfaces; the write side
// (doCreate/doUpdate) resolves the SAME predicate before persisting a body
// FK, and a foreign parent answers 404 (errNotFound) exactly as reading
// that row does — no 403, so existence is not leaked.
//
// This is the framework half of the round-5 finding pinned end-to-end by
// examples/ecommerce/app/orderitem_relwrite (owned by the examples slice):
// a signed-in customer could attach an order_items row to another
// customer's order by POSTing that orderId; the migrate FK is
// existence-only, so the real foreign order satisfied it.

// relwriteWorld builds the two-entity shape: items (owner-scoped,
// BelongsTo orders) over orders (owner-scoped), seeded with one order per
// customer.
func relwriteWorld(t *testing.T) *CrudHandler {
	t.Helper()
	ddl := `
CREATE TABLE orders (
	id      TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	total   TEXT
);
CREATE TABLE items (
	id       TEXT PRIMARY KEY,
	user_id  TEXT NOT NULL,
	order_id TEXT,
	label    TEXT
);
`
	ordersCfg := makeEntityConfig("orders", "orders", "user_id",
		[]schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "total", Type: schema.String},
		})
	itemsCfg := makeEntityConfig("items", "items", "user_id",
		[]schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "order_id", Type: schema.String},
			{Name: "label", Type: schema.String},
		},
		func(c *entity.EntityConfig) {
			c.Relations = []entity.Relation{
				entity.BelongsTo("order", "orders", "order_id"),
			}
		},
	)
	ch, db := setupSecurityTestHandler(t, itemsCfg, ddl)
	ordersEnt := entity.Define(ordersCfg.Table, ordersCfg)
	ordersEnt.SetDB(db)
	reg := newTestRegistry(t)
	reg.add(t, ch.Entity)
	reg.add(t, ordersEnt)
	ch.Registry = reg
	seedRows(t, db, "orders", []map[string]any{
		{"id": "o-alice", "user_id": "alice", "total": "10.00"},
		{"id": "o-bob", "user_id": "bob", "total": "20.00"},
	})
	return ch
}

// TestRelWriteForeignBelongsToRefused: alice POSTing bob's order id must
// 404 (the foreign parent does not resolve for her), and PATCHing her own
// item at bob's order must 404 too. Positive controls: her own order
// target succeeds on both arms — a fix that refuses everything cannot
// pass.
func TestRelWriteForeignBelongsToRefused(t *testing.T) {
	ch := relwriteWorld(t)

	post := func(body, user string) *httptest.ResponseRecorder {
		req := withTestUser(httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(body)), user)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		ch.Create()(rr, req)
		return rr
	}

	// Attack: attach an item to bob's order.
	rr := post(`{"id":"i1","label":"mine","order_id":"o-bob"}`, "alice")
	if rr.Code != http.StatusNotFound {
		t.Errorf("SECURITY: [crud-relwrite] alice create with bob's order_id = %d (%s), want 404 — the belongs_to FK is a write-side trust boundary exactly as include= is a read-side one", rr.Code, rr.Body.String())
	}

	// Positive control: her own order.
	rr = post(`{"id":"i2","label":"mine","order_id":"o-alice"}`, "alice")
	if rr.Code != http.StatusCreated {
		t.Fatalf("setup broken: alice create against her own order = %d (%s)", rr.Code, rr.Body.String())
	}

	// Attack: retarget her item at bob's order.
	req := withTestUser(httptest.NewRequest(http.MethodPatch, "/items/i2", strings.NewReader(`{"order_id":"o-bob"}`)), "alice")
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "i2")
	rr = httptest.NewRecorder()
	ch.Update()(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("SECURITY: [crud-relwrite] alice PATCH retargeting her item at bob's order = %d (%s), want 404 — the update path shares the same body trust boundary", rr.Code, rr.Body.String())
	}

	// Positive control: retarget stays within her own orders.
	req = withTestUser(httptest.NewRequest(http.MethodPatch, "/items/i2", strings.NewReader(`{"order_id":"o-alice","label":"moved"}`)), "alice")
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "i2")
	rr = httptest.NewRecorder()
	ch.Update()(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("setup broken: alice PATCH within her own orders = %d (%s)", rr.Code, rr.Body.String())
	}
}

// TestRelWriteCrossOwnerAndServerWritesExempt pins the two sanctioned
// exemptions: a caller holding the target's CrossOwnerRead permission (or
// an explicit AllowCrossOwner marker) sees the foreign parent on reads, so
// their writes may reference it; a WithServerWrites context is a
// host-driven privileged write.
func TestRelWriteCrossOwnerAndServerWritesExempt(t *testing.T) {
	ch := relwriteWorld(t)

	// AllowCrossOwner context (the in-process grant marker): allowed.
	req := withTestUser(httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(`{"id":"i3","label":"admin","order_id":"o-bob"}`)), "alice")
	req = req.WithContext(owner.AllowCrossOwner(req.Context()))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	ch.Create()(rr, req)
	if rr.Code != http.StatusCreated {
		t.Errorf("AllowCrossOwner create with foreign order_id = %d (%s), want 201 — the write gate must mirror the read-side cross-owner exemption", rr.Code, rr.Body.String())
	}

	// WithServerWrites context (host-driven write): allowed, the host
	// opted into privileged writes exactly as for ReadOnly columns.
	req2 := withTestUser(httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(`{"id":"i4","label":"import","order_id":"o-bob"}`)), "alice")
	req2 = req2.WithContext(WithServerWrites(req2.Context()))
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	ch.Create()(rr2, req2)
	if rr2.Code != http.StatusCreated {
		t.Errorf("WithServerWrites create with foreign order_id = %d (%s), want 201 — server-write contexts own their references", rr2.Code, rr2.Body.String())
	}

}

// TestRelWriteUnscopedTargetUntouched: a BelongsTo onto an UNSCOPED entity
// must keep working (the guard only fires where the read side scopes).
func TestRelWriteUnscopedTargetUntouched(t *testing.T) {
	ddl := `
CREATE TABLE catalogs (id TEXT PRIMARY KEY, name TEXT);
CREATE TABLE widgets (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, catalog_id TEXT, label TEXT);
`
	widgetsCfg := makeEntityConfig("widgets", "widgets", "user_id",
		[]schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "catalog_id", Type: schema.String},
			{Name: "label", Type: schema.String},
		},
		func(c *entity.EntityConfig) {
			c.Relations = []entity.Relation{
				entity.BelongsTo("catalog", "catalogs", "catalog_id"),
			}
		},
	)
	ch, db := setupSecurityTestHandler(t, widgetsCfg, ddl)
	catalogs := entity.Define("catalogs", makeEntityConfig("catalogs", "catalogs", "",
		[]schema.Field{{Name: "name", Type: schema.String}}))
	catalogs.SetDB(db)
	reg := newTestRegistry(t)
	reg.add(t, ch.Entity)
	reg.add(t, catalogs)
	ch.Registry = reg
	seedRows(t, db, "catalogs", []map[string]any{{"id": "c1", "name": "shared"}})

	req := withTestUser(httptest.NewRequest(http.MethodPost, "/widgets", strings.NewReader(`{"id":"w1","label":"x","catalog_id":"c1"}`)), "alice")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	ch.Create()(rr, req)
	if rr.Code != http.StatusCreated {
		t.Errorf("create referencing an unscoped target = %d (%s), want 201 — the guard must not widen beyond the read-side scope", rr.Code, rr.Body.String())
	}
}

// TestRelWriteMissingTargetStill404sViaGuard: a FK pointing at a row that
// does not exist at all is refused by the same resolver (not just foreign
// rows), keeping the failure shape uniform.
func TestRelWriteMissingTargetStill404sViaGuard(t *testing.T) {
	ch := relwriteWorld(t)
	req := withTestUser(httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(`{"id":"i5","label":"ghost","order_id":"o-ghost"}`)), "alice")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	ch.Create()(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("create with nonexistent order_id = %d (%s), want 404 from the resolver", rr.Code, rr.Body.String())
	}
}
