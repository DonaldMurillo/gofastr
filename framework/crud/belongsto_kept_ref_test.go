package crud

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// keptRefHandler is invoices belonging to customers through customer_id,
// and to orders through (order_store, order_no). Customers and orders
// admit only status=active through their ReadScope. On Postgres the
// stored values come back through another driver's types.
func keptRefHandler(t *testing.T, pg bool) (*CrudHandler, *sql.DB) {
	t.Helper()
	ddl := []string{
		`CREATE TABLE customers (id TEXT PRIMARY KEY, status TEXT)`,
		`CREATE TABLE orders (store TEXT, no INTEGER, status TEXT, PRIMARY KEY (store, no))`,
		`CREATE TABLE invoices (id TEXT PRIMARY KEY, amount TEXT, customer_id TEXT, order_store TEXT, order_no INTEGER)`,
	}
	var db *sql.DB
	seed := seedRows
	if pg {
		db = pgtest.DB(t)
		for _, stmt := range ddl {
			if _, err := db.Exec(stmt); err != nil {
				t.Fatal(err)
			}
		}
		seed = pgSeed
	} else {
		db = setupDB(t, ddl...)
	}
	activeOnly := &entity.ExposureConfig{Public: true, ReadScope: &entity.ReadScopeConfig{
		Filter: []entity.RowPredicate{{Field: "status", Value: "active"}},
	}}
	customers := entity.Define("customers", entity.EntityConfig{
		Table:    "customers",
		Fields:   []schema.Field{{Name: "id", Type: schema.String}, {Name: "status", Type: schema.String}},
		Exposure: activeOnly,
	}.WithTimestamps(false))
	orders := entity.Define("orders", entity.EntityConfig{
		Table:      "orders",
		PrimaryKey: []string{"store", "no"},
		Fields: []schema.Field{
			{Name: "store", Type: schema.String},
			{Name: "no", Type: schema.Int},
			{Name: "status", Type: schema.String},
		},
		Exposure: activeOnly,
	}.WithTimestamps(false))
	invoices := entity.Define("invoices", entity.EntityConfig{
		Table: "invoices",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "amount", Type: schema.String},
			{Name: "customer_id", Type: schema.String},
			{Name: "order_store", Type: schema.String},
			{Name: "order_no", Type: schema.Int},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("customer", "customers", "customer_id"),
			entity.BelongsTo("order", "orders", "order_store", "order_no"),
		},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	for _, e := range []*entity.Entity{customers, orders, invoices} {
		e.SetDB(db)
	}
	seed(t, db, "customers", []map[string]any{
		{"id": "c1", "status": "archived"},
		{"id": "c2", "status": "archived"},
		{"id": "c3", "status": "active"},
		{"id": "<nil>", "status": "archived"},
	})
	seed(t, db, "orders", []map[string]any{
		{"store": "s1", "no": 1, "status": "archived"},
		{"store": "s1", "no": 2, "status": "archived"},
		{"store": "s2", "no": 1, "status": "archived"},
	})
	seed(t, db, "invoices", []map[string]any{
		{"id": "i1", "amount": "1", "customer_id": "c1", "order_store": "s1", "order_no": 1},
		{"id": "i2", "amount": "1", "customer_id": nil, "order_store": nil, "order_no": nil},
	})
	ch := NewCrudHandler(invoices, db).WithJSONCase(CaseSnake)
	ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"customers": customers, "orders": orders, "invoices": invoices}}
	return ch, db
}

// An edit that leaves a reference as it is stored crosses no trust
// boundary, so it is not refused because the caller can no longer read
// the target. The record form keeps a target it cannot show by design,
// and once an invoice's customer was archived nobody could change its
// amount without re-picking the customer (2026-10-27 D42). A reference
// that moves, in any column, is still checked, and so is one leaving a
// stored null, whatever the body spells.
func TestUpdateKeepsUnreadableReference(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) { testUpdateKeepsUnreadableReference(t, false) })
	t.Run("postgres", func(t *testing.T) { testUpdateKeepsUnreadableReference(t, true) })
}

func testUpdateKeepsUnreadableReference(t *testing.T, pg bool) {
	for _, tc := range []struct {
		name, id, body string
		want           int
	}{
		{"unchanged single", "i1", `{"amount":"2","customer_id":"c1"}`, http.StatusOK},
		{"unchanged composite", "i1", `{"amount":"2","order_store":"s1","order_no":1}`, http.StatusOK},
		{"unchanged composite as text", "i1", `{"amount":"2","order_store":"s1","order_no":"1"}`, http.StatusOK},
		{"unchanged both", "i1", `{"amount":"2","customer_id":"c1","order_store":"s1","order_no":1}`, http.StatusOK},
		{"moved to unreadable", "i1", `{"amount":"2","customer_id":"c2"}`, http.StatusNotFound},
		{"composite moved in its second column", "i1", `{"amount":"2","order_store":"s1","order_no":2}`, http.StatusNotFound},
		{"composite moved in its first column", "i1", `{"amount":"2","order_store":"s2","order_no":1}`, http.StatusNotFound},
		{"one kept, one moved", "i1", `{"amount":"2","customer_id":"c1","order_store":"s1","order_no":2}`, http.StatusNotFound},
		{"moved to readable", "i1", `{"amount":"2","customer_id":"c3"}`, http.StatusOK},
		{"null moved to unreadable", "i2", `{"amount":"2","customer_id":"c1"}`, http.StatusNotFound},
		{"null moved to its own spelling", "i2", `{"amount":"2","customer_id":"<nil>"}`, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ch, db := keptRefHandler(t, pg)
			r := httptest.NewRequest(http.MethodPatch, "/invoices/"+tc.id, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			r.SetPathValue("id", tc.id)
			rec := httptest.NewRecorder()
			ch.Update()(rec, r)
			if rec.Code != tc.want {
				t.Fatalf("PATCH %s = %d %s, want %d", tc.body, rec.Code, rec.Body.String(), tc.want)
			}
			var amount string
			if err := db.QueryRow(`SELECT amount FROM invoices WHERE id = $1`, tc.id).Scan(&amount); err != nil {
				t.Fatal(err)
			}
			if wantAmount := map[bool]string{true: "2", false: "1"}[tc.want == http.StatusOK]; amount != wantAmount {
				t.Fatalf("amount = %q after PATCH %s, want %q", amount, tc.body, wantAmount)
			}
		})
	}
}
