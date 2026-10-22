package framework

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// orderEntities: an order keyed on (store, no), and lines that belong to
// it through a two-column foreign key (order_store, order_no).
func orderEntities(orderScope *entity.ScopeConfig, orderNoType schema.FieldType) []*entity.Entity {
	orderFields := []schema.Field{
		{Name: "store", Type: schema.String, Required: true},
		{Name: "no", Type: schema.Int, Required: true},
		{Name: "total", Type: schema.Int},
	}
	if orderScope != nil && orderScope.OwnerField != "" {
		orderFields = append(orderFields, schema.Field{Name: orderScope.OwnerField, Type: schema.String})
	}
	return []*entity.Entity{
		entity.Define("order", entity.EntityConfig{
			Table:      "orders",
			PrimaryKey: []string{"store", "no"},
			Fields:     orderFields,
			Relations:  []entity.Relation{entity.HasMany("lines", "line", "order_store", "order_no")},
			Scope:      orderScope,
		}.WithTimestamps(false)),
		entity.Define("line", entity.EntityConfig{
			Table: "lines",
			Fields: []schema.Field{
				{Name: "order_store", Type: schema.String},
				{Name: "order_no", Type: orderNoType},
				{Name: "sku", Type: schema.String},
			},
			Relations: []entity.Relation{entity.BelongsTo("order", "order", "order_store", "order_no")},
		}.WithTimestamps(false)),
	}
}

// A foreign key of two columns to a two-column key joins on both: the
// database enforces it, includes and nested filters follow it from either
// end, and EagerLoad reads it.
func TestCompositeForeignKey(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ents := orderEntities(nil, schema.Int)
		app := relApp(t, db, ents...)
		relDo(t, app, "", "POST", "/orders", map[string]any{"store": "s1", "no": 7, "total": 10})
		relDo(t, app, "", "POST", "/orders", map[string]any{"store": "s1", "no": 8, "total": 20})
		relDo(t, app, "", "POST", "/orders", map[string]any{"store": "s2", "no": 7, "total": 30})
		for _, l := range []struct {
			store string
			no    int
			sku   string
		}{{"s1", 7, "a"}, {"s1", 7, "b"}, {"s1", 8, "c"}, {"s2", 7, "d"}} {
			relDo(t, app, "", "POST", "/lines", map[string]any{"order_store": l.store, "order_no": l.no, "sku": l.sku})
		}

		// (s2, 8) names no order, though each half names one.
		if rec := serveKeyedAs(t, app, "", "POST", "/lines", map[string]any{"order_store": "s2", "order_no": 8, "sku": "x"}); rec.Code < 400 {
			t.Errorf("a line referencing no order = %d %s; want the foreign key refused", rec.Code, rec.Body)
		}

		lines, _ := relDo(t, app, "", "GET", "/lines?include=order", nil).([]any)
		got := map[string]any{}
		for _, l := range lines {
			got[relField(l, "sku").(string)] = relField(l, "order", "total")
		}
		for sku, want := range map[string]float64{"a": 10, "b": 10, "c": 20, "d": 30} {
			if got[sku] != want {
				t.Errorf("line %s order total = %v, want %v", sku, got[sku], want)
			}
		}
		order := relDo(t, app, "", "GET", "/orders/s1/7?include=lines", nil)
		if got := relList(relField(order, "lines"), "sku"); !slices.Equal(got, []string{"a", "b"}) {
			t.Errorf("order s1/7 lines = %v, want [a b]", got)
		}

		for path, want := range map[string][]string{
			"/lines?order.total=10":  {"a", "b"},
			"/lines?order.total=30":  {"d"},
			"/orders?lines.sku=c":    {"8"},
			"/orders?lines.sku=d":    {"7"},
			"/orders?lines.sku=none": nil,
		} {
			field := "sku"
			if strings.HasPrefix(path, "/orders") {
				field = "no"
			}
			if got := relList(relDo(t, app, "", "GET", path, nil), field); !slices.Equal(got, want) {
				t.Errorf("GET %s = %v, want %v", path, got, want)
			}
		}

		reg := NewRegistry()
		for _, e := range ents {
			_ = reg.Register(e)
		}
		id := entity.JoinKey([]string{"s1", "7"})
		loaded, err := EagerLoad(context.Background(), db, ents[0], ents[0].Config.Relations, []string{id}, reg)
		if err != nil {
			t.Fatal(err)
		}
		if got := relList(loaded[id]["lines"], "sku"); !slices.Equal(got, []string{"a", "b"}) {
			t.Errorf("EagerLoad lines = %v, want [a b]", got)
		}
	})
}

// A write sets a composite reference whole or not at all: naming one of
// its columns, or nulling one and not the other, would leave the row
// pointing half at the old order and half at a new one.
func TestCompositeForeignKeyPartialWrite(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := relApp(t, db, orderEntities(nil, schema.Int)...)
		relDo(t, app, "", "POST", "/orders", map[string]any{"store": "s1", "no": 7})
		relDo(t, app, "", "POST", "/orders", map[string]any{"store": "s2", "no": 8})
		line := relID(t, app, "/lines", map[string]any{"order_store": "s1", "order_no": 7, "sku": "a"})

		for name, tc := range map[string]struct {
			method, path string
			body         map[string]any
		}{
			"create with one column": {"POST", "/lines", map[string]any{"order_store": "s1", "sku": "b"}},
			"patch one column":       {"PATCH", "/lines/" + line, map[string]any{"order_no": 8}},
			"patch one column null":  {"PATCH", "/lines/" + line, map[string]any{"order_store": nil, "order_no": 7}},
		} {
			rec := serveKeyedAs(t, app, "", tc.method, tc.path, tc.body)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "partial_foreign_key") {
				t.Errorf("%s = %d %s; want 400 partial_foreign_key", name, rec.Code, rec.Body)
			}
		}
		if got := relField(relDo(t, app, "", "GET", "/lines/"+line, nil), "orderStore"); got != "s1" {
			t.Errorf("a refused patch moved the line to store %v", got)
		}

		relDo(t, app, "", "PATCH", "/lines/"+line, map[string]any{"order_store": "s2", "order_no": 8})
		if got := relField(relDo(t, app, "", "GET", "/lines/"+line+"?include=order", nil), "order", "no"); got != float64(8) {
			t.Errorf("after a whole move the line's order is %v, want 8", got)
		}
		relDo(t, app, "", "PATCH", "/lines/"+line, map[string]any{"order_store": nil, "order_no": nil})
		row, _ := relDo(t, app, "", "GET", "/lines/"+line, nil).(map[string]any)
		if got, ok := row["orderNo"]; !ok || got != nil {
			t.Errorf("after clearing both columns orderNo = %v (present %v), want null", got, ok)
		}
	})
}

// A write may not point a line at an order its caller cannot read: the
// lookup checks the whole tuple under the order's owner scope.
func TestCompositeForeignKeyOwnerScope(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		useRelOwner(t)
		app := relApp(t, db, orderEntities(&entity.ScopeConfig{OwnerField: "owner_id"}, schema.Int)...)
		relDo(t, app, "alice", "POST", "/orders", map[string]any{"store": "s1", "no": 7})
		relDo(t, app, "bob", "POST", "/orders", map[string]any{"store": "s1", "no": 8})

		if rec := serveKeyedAs(t, app, "bob", "POST", "/lines", map[string]any{"order_store": "s1", "order_no": 7, "sku": "x"}); rec.Code != http.StatusNotFound {
			t.Errorf("bob pointing a line at alice's order = %d %s; want 404", rec.Code, rec.Body)
		}
		relDo(t, app, "bob", "POST", "/lines", map[string]any{"order_store": "s1", "order_no": 8, "sku": "y"})
		relDo(t, app, "alice", "POST", "/lines", map[string]any{"order_store": "s1", "order_no": 7, "sku": "z"})

		// A blank key column is a value, not an absent reference: the
		// lookup still runs on the whole tuple.
		if _, err := db.Exec(`INSERT INTO orders (store, no, owner_id) VALUES ('', 9, 'alice')`); err != nil {
			t.Fatal(err)
		}
		if rec := serveKeyedAs(t, app, "bob", "POST", "/lines", map[string]any{"order_store": "", "order_no": 9, "sku": "w"}); rec.Code != http.StatusNotFound {
			t.Errorf("bob pointing a line at alice's order ('', 9) = %d %s; want 404", rec.Code, rec.Body)
		}
	})
}

// useRelOwner reads the owner from the relUser the test request carries.
func useRelOwner(t *testing.T) {
	prev := owner.GetExtractor()
	owner.SetExtractor(func(ctx context.Context) (any, bool) {
		if u, ok := handler.GetUser(ctx); ok && u != nil {
			return u.(relUser).GetID(), true
		}
		return nil, false
	})
	t.Cleanup(func() { owner.SetExtractor(prev) })
}

// keptKeyEntities: orders keyed on (store, no), owned; lines keyed on
// (order_store, line_id), so the store half of their reference is a key
// column no update writes; slips, whose store half is ReadOnly.
func keptKeyEntities() []*entity.Entity {
	ref := func(name string, fields []schema.Field, key ...string) *entity.Entity {
		return entity.Define(name, entity.EntityConfig{
			Table:      name + "s",
			PrimaryKey: key,
			Fields:     fields,
			Relations:  []entity.Relation{entity.BelongsTo("order", "order", "order_store", "order_no")},
		}.WithTimestamps(false))
	}
	return []*entity.Entity{
		entity.Define("order", entity.EntityConfig{
			Table:      "orders",
			PrimaryKey: []string{"store", "no"},
			Fields: []schema.Field{
				{Name: "store", Type: schema.String, Required: true},
				{Name: "no", Type: schema.String, Required: true},
				{Name: "owner_id", Type: schema.String},
			},
			Scope: &entity.ScopeConfig{OwnerField: "owner_id"},
		}.WithTimestamps(false)),
		ref("line", []schema.Field{
			{Name: "order_store", Type: schema.String},
			{Name: "line_id", Type: schema.String, Required: true},
			{Name: "order_no", Type: schema.String},
			{Name: "sku", Type: schema.String},
		}, "order_store", "line_id"),
		ref("slip", []schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "order_store", Type: schema.String, ReadOnly: true},
			{Name: "order_no", Type: schema.String},
		}, "id"),
		ref("chit", []schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "order_store", Type: schema.String, ReadOnly: true, Default: "A"},
			{Name: "order_no", Type: schema.String},
		}, "id"),
		ref("gen", []schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "order_store", Type: schema.String, AutoGenerate: schema.AutoUUID},
			{Name: "order_no", Type: schema.String, AutoGenerate: schema.AutoUUID},
		}, "id"),
		ref("tag", []schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "order_store", Type: schema.String, AutoGenerate: schema.AutoUUID},
			{Name: "order_no", Type: schema.String},
		}, "id"),
		entity.Define("pick", entity.EntityConfig{
			Table:      "picks",
			PrimaryKey: []string{"order_store", "pick_id"},
			Fields: []schema.Field{
				{Name: "order_store", Type: schema.String},
				{Name: "pick_id", Type: schema.String, Required: true},
				{Name: "order_no", Type: schema.String},
				{Name: "alt_no", Type: schema.String},
			},
			Relations: []entity.Relation{
				entity.BelongsTo("order", "order", "order_store", "order_no"),
				entity.BelongsTo("alt", "order", "order_store", "alt_no"),
			},
		}.WithTimestamps(false)),
	}
}

// Each reference reads as the row will hold it, one at a time. A create
// that drops a ReadOnly column holds its default; an upsert cannot know a
// generated column, which its two arms write differently; a PATCH moving
// one reference leaves another that shares its kept column alone; and a
// reference with a null column names no row.
func TestCompositeForeignKeyHeldColumn(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		useRelOwner(t)
		app := relApp(t, db, keptKeyEntities()...)
		relDo(t, app, "alice", "POST", "/orders", map[string]any{"store": "A", "no": "7"})
		for _, o := range [][2]string{{"A", "8"}, {"A", "9"}, {"B", "7"}} {
			relDo(t, app, "bob", "POST", "/orders", map[string]any{"store": o[0], "no": o[1]})
		}

		// (B, 7) is bob's, but the INSERT drops order_store and the row
		// would hold (A, 7), alice's.
		if rec := serveKeyedAs(t, app, "bob", "POST", "/chits", map[string]any{"id": "c1", "order_store": "B", "order_no": "7"}); rec.Code != http.StatusNotFound {
			t.Errorf("bob creating a chit on alice's order = %d %s; want 404", rec.Code, rec.Body)
		}
		relDo(t, app, "bob", "POST", "/chits", map[string]any{"id": "c2", "order_no": "8"})

		ctx := handler.SetUser(context.Background(), relUser("bob"))
		tags, err := app.Registry.Get("tag")
		if err != nil {
			t.Fatal(err)
		}
		ch, err := app.CrudHandlerForEntity(tags)
		if err != nil {
			t.Fatal(err)
		}
		var ve *crud.ValidationError
		if _, err := ch.UpsertOne(ctx, map[string]any{"id": "t1", "order_store": "B", "order_no": "7"}); !errors.As(err, &ve) {
			t.Errorf("upsert writing a generated store: err = %v; want a validation error", err)
		}
		// Every column generated: the conflict arm moves nothing, and the
		// insert arm writes (A, 7), alice's.
		gens, err := app.Registry.Get("gen")
		if err != nil {
			t.Fatal(err)
		}
		gch, err := app.CrudHandlerForEntity(gens)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := gch.UpsertOne(ctx, map[string]any{"id": "g1", "order_store": "A", "order_no": "7"}); err == nil {
			t.Error("bob upserting a gen onto alice's order succeeded; want it refused")
		}

		relDo(t, app, "bob", "POST", "/picks", map[string]any{"order_store": "A", "pick_id": "p1", "order_no": "8", "alt_no": "8"})
		// alt holds (A, 8) too; moving order alone leaves it whole.
		relDo(t, app, "bob", "PATCH", "/picks/A/p1", map[string]any{"order_no": "9"})
		relDo(t, app, "bob", "PATCH", "/picks/A/p1", map[string]any{"alt_no": nil})
		row, _ := relDo(t, app, "bob", "GET", "/picks/A/p1", nil).(map[string]any)
		if row["orderNo"] != "9" || row["altNo"] != nil {
			t.Errorf("after moving order and clearing alt the pick holds %v / %v, want 9 / null", row["orderNo"], row["altNo"])
		}
	})
}

// A reference through the tenant column holds the caller's tenant: the
// create drops the column from the body and the lookup reads the tenant
// the INSERT writes.
func TestCompositeForeignKeyTenantColumn(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		useRelOwner(t)
		scope := &entity.ScopeConfig{MultiTenant: true, OwnerField: "owner_id"}
		app := relApp(t, db,
			entity.Define("torder", entity.EntityConfig{
				Table:      "torders",
				PrimaryKey: []string{"tenant_id", "no"},
				Fields: []schema.Field{
					{Name: "tenant_id", Type: schema.String, Required: true},
					{Name: "no", Type: schema.String, Required: true},
					{Name: "owner_id", Type: schema.String},
				},
				Scope: scope,
			}.WithTimestamps(false)),
			entity.Define("bin", entity.EntityConfig{
				Table: "bins",
				Fields: []schema.Field{
					{Name: "tenant_id", Type: schema.String},
					{Name: "order_no", Type: schema.String},
				},
				Relations: []entity.Relation{entity.BelongsTo("order", "torder", "tenant_id", "order_no")},
				Scope:     &entity.ScopeConfig{MultiTenant: true},
			}.WithTimestamps(false)),
		)
		if _, err := db.Exec(`INSERT INTO torders (tenant_id, no, owner_id) VALUES ('ta', '1', 'alice'), ('ta', '2', 'bob')`); err != nil {
			t.Fatal(err)
		}
		bins, err := app.Registry.Get("bin")
		if err != nil {
			t.Fatal(err)
		}
		ch, err := app.CrudHandlerForEntity(bins)
		if err != nil {
			t.Fatal(err)
		}
		bob := tenant.SetTenantID(handler.SetUser(context.Background(), relUser("bob")), "ta")
		if _, err := ch.CreateOne(bob, map[string]any{"order_no": "1"}); err == nil {
			t.Error("bob creating a bin on alice's order (ta, 1) succeeded; want it refused")
		}
		if _, err := ch.CreateOne(bob, map[string]any{"order_no": "2"}); err != nil {
			t.Errorf("bob creating a bin on his order (ta, 2): %v", err)
		}
	})
}

// A write checks the reference the row will hold. A PATCH that moves the
// store of a line keyed on it keeps the stored store, so the check reads
// that; the upsert and bulk update, which cannot read one stored tuple,
// refuse moving part of a reference they keep the rest of; and a blank
// tuple is a reference like any other.
func TestCompositeForeignKeyKeptColumn(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		useRelOwner(t)
		ents := keptKeyEntities()
		app := relApp(t, db, ents...)
		relDo(t, app, "alice", "POST", "/orders", map[string]any{"store": "A", "no": "7"})
		for _, o := range [][2]string{{"A", "8"}, {"A", "9"}, {"B", "7"}} {
			relDo(t, app, "bob", "POST", "/orders", map[string]any{"store": o[0], "no": o[1]})
		}
		relDo(t, app, "bob", "POST", "/lines", map[string]any{"order_store": "A", "line_id": "l1", "order_no": "8"})

		// (B, 7) is bob's, but the row would hold (A, 7), alice's.
		if rec := serveKeyedAs(t, app, "bob", "PATCH", "/lines/A/l1", map[string]any{"order_store": "B", "order_no": "7"}); rec.Code != http.StatusNotFound {
			t.Errorf("bob moving his line onto alice's order = %d %s; want 404", rec.Code, rec.Body)
		}
		if got := relField(relDo(t, app, "bob", "GET", "/lines/A/l1", nil), "orderNo"); got != "8" {
			t.Errorf("a refused patch moved the line to order %v", got)
		}
		// The written half alone moves within the stored store.
		relDo(t, app, "bob", "PATCH", "/lines/A/l1", map[string]any{"order_no": "9"})
		// The kept half alone moves nothing.
		relDo(t, app, "bob", "PATCH", "/lines/A/l1", map[string]any{"order_store": "B", "sku": "y"})
		if got := relField(relDo(t, app, "bob", "GET", "/lines/A/l1", nil), "orderNo"); got != "9" {
			t.Errorf("after moving order_no the line's order is %v, want 9", got)
		}

		if _, err := db.Exec(`INSERT INTO orders (store, no, owner_id) VALUES ('', '', 'alice')`); err != nil {
			t.Fatal(err)
		}
		if rec := serveKeyedAs(t, app, "bob", "POST", "/lines", map[string]any{"order_store": "", "line_id": "l2", "order_no": ""}); rec.Code != http.StatusNotFound {
			t.Errorf("bob pointing a line at alice's order ('', '') = %d %s; want 404", rec.Code, rec.Body)
		}

		ctx := handler.SetUser(context.Background(), relUser("bob"))
		var ve *crud.ValidationError
		handlerOf := func(name string) *crud.CrudHandler {
			e, err := app.Registry.Get(name)
			if err != nil {
				t.Fatal(err)
			}
			ch, err := app.CrudHandlerForEntity(e)
			if err != nil {
				t.Fatal(err)
			}
			return ch
		}
		lines := handlerOf("line")
		if _, err := NewTypedQuery[struct{}](lines).UpdateAll(ctx, map[string]any{"order_store": "B", "order_no": "7"}); !errors.As(err, &ve) {
			// (B, 7) is bob's; the rows would hold (A, 7), alice's.
			t.Errorf("bulk update moving order_no under a kept store: err = %v; want a validation error", err)
		}
		if _, err := handlerOf("slip").UpsertOne(ctx, map[string]any{"id": "s1", "order_store": "A", "order_no": "8"}); !errors.As(err, &ve) {
			t.Errorf("upsert writing order_no under a ReadOnly store: err = %v; want a validation error", err)
		}
	})
}

// The columns of a two-column foreign key are plain fields, typed by their
// own declarations; one whose type differs from the key column it
// references is refused before any DDL runs.
func TestCompositeForeignKeyTypes(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		reg := NewRegistry()
		for _, e := range orderEntities(nil, schema.String) {
			if err := reg.Register(e); err != nil {
				t.Fatal(err)
			}
		}
		err := AutoMigrate(db, reg)
		if !errors.Is(err, entity.ErrForeignKeyShape) || !strings.Contains(err.Error(), "order_no") {
			t.Fatalf("AutoMigrate = %v; want ErrForeignKeyShape naming order_no", err)
		}
	})
}

// A raw migrate.Table references a two-column key through Columns.
func TestTableCompositeForeignKey(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		reg := NewRegistry()
		for _, tb := range []migrate.Table{
			{Name: "stores_orders", Columns: []migrate.Column{
				{Name: "store", Type: schema.String, PrimaryKey: true},
				{Name: "no", Type: schema.Int, PrimaryKey: true},
			}},
			{Name: "order_notes", Columns: []migrate.Column{
				{Name: "id", Type: schema.String, PrimaryKey: true},
				{Name: "order_store", Type: schema.String},
				{Name: "order_no", Type: schema.Int},
			}, ForeignKeys: []migrate.ForeignKey{{Columns: []string{"order_store", "order_no"}, RefTable: "stores_orders"}}},
		} {
			if err := reg.Register(tb.ToEntity()); err != nil {
				t.Fatal(err)
			}
		}
		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO stores_orders (store, no) VALUES ('s1', 7)`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO order_notes (id, order_store, order_no) VALUES ('n1', 's1', 7)`); err != nil {
			t.Fatalf("a note on an existing order: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO order_notes (id, order_store, order_no) VALUES ('n2', 's1', 8)`); err == nil {
			t.Error("a note on a missing order was accepted; want the foreign key to refuse it")
		}
	})
}

// A many_to_many cascade inserts its pivot row through the pivot's own
// belongs_to checks: a tagging references the grant keyed on its
// (post_id, tag_id), so linking a tag needs the caller's grant, however
// the many_to_many spells the link columns.
func TestCascadePivotForeignKey(t *testing.T) {
	for _, link := range [][2]string{{"post_id", "tag_id"}, {"POST_ID", "TAG_ID"}} {
		t.Run(link[0], func(t *testing.T) { cascadePivotForeignKey(t, link) })
	}
}

func cascadePivotForeignKey(t *testing.T, link [2]string) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		useRelOwner(t)
		pair := []schema.Field{{Name: "post_id", Type: schema.String}, {Name: "tag_id", Type: schema.String}}
		app := relApp(t, db,
			entity.Define("grant", entity.EntityConfig{
				Table:      "grants",
				PrimaryKey: []string{"post_id", "tag_id"},
				Fields:     append(slices.Clone(pair), schema.Field{Name: "owner_id", Type: schema.String}),
				Scope:      &entity.ScopeConfig{OwnerField: "owner_id"},
			}.WithTimestamps(false)),
			entity.Define("tagging", entity.EntityConfig{
				Table:      "taggings",
				PrimaryKey: []string{"post_id", "tag_id"},
				Fields:     pair,
				Relations:  []entity.Relation{entity.BelongsTo("grant", "grant", "post_id", "tag_id")},
			}.WithTimestamps(false)),
			entity.Define("tag", entity.EntityConfig{
				Table:  "tags",
				Fields: []schema.Field{{Name: "name", Type: schema.String}},
			}.WithTimestamps(false)),
			entity.Define("post", entity.EntityConfig{
				Table:     "posts",
				Fields:    []schema.Field{{Name: "title", Type: schema.String}},
				Relations: []entity.Relation{entity.ManyToMany("tags", "tag", "taggings", link[0], link[1]).Cascade()},
			}.WithTimestamps(false)),
		)
		id := func(data any) string { return fmt.Sprint(data.(map[string]any)["id"]) }
		post := id(relDo(t, app, "bob", "POST", "/posts", map[string]any{"title": "p"}))
		alices := id(relDo(t, app, "bob", "POST", "/tags", map[string]any{"name": "a"}))
		bobs := id(relDo(t, app, "bob", "POST", "/tags", map[string]any{"name": "b"}))
		if _, err := db.Exec(`INSERT INTO grants (post_id, tag_id, owner_id) VALUES ($1, $2, 'alice'), ($1, $3, 'bob')`, post, alices, bobs); err != nil {
			t.Fatal(err)
		}
		if rec := serveKeyedAs(t, app, "bob", "PATCH", "/posts/"+post, map[string]any{"tags": []any{alices}}); rec.Code < 400 {
			t.Errorf("bob linking a tag through alice's grant = %d; want it refused", rec.Code)
		}
		relDo(t, app, "bob", "PATCH", "/posts/"+post, map[string]any{"tags": []any{bobs}})
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM taggings WHERE post_id = $1`, post).Scan(&n); err != nil || n != 1 {
			t.Errorf("taggings = %d, %v; want bob's one link", n, err)
		}
	})
}

// A transition may not write a column of a multi-column belongs_to on
// its table: not the entity's own, another version's, or another name's
// over the same table. A move writes its column with no reference check.
// Registration refuses it, so an app that never migrates is covered.
func TestTransitionForeignKeyTable(t *testing.T) {
	fields := []schema.Field{
		{Name: "store", Type: schema.String},
		{Name: "status", Type: schema.Enum, Default: "a", Values: []string{"a", "b"}},
		{Name: "note", Type: schema.Enum, Default: "a", Values: []string{"a", "b"}},
		{Name: "day", Type: schema.Date},
	}
	// decl declares name at version v over table lines: rel, when set,
	// and states on field whose one move stamps stamp, when field is set.
	decl := func(name, v string, rel *entity.Relation, field, stamp string) *entity.Entity {
		cfg := entity.EntityConfig{Table: "lines", Fields: fields}
		if rel != nil {
			cfg.Relations = []entity.Relation{*rel}
		}
		if field != "" {
			cfg.States = &entity.StatesConfig{Field: field, Transitions: []entity.Transition{{Key: "go", From: []string{"a"}, To: "b", Stamp: stamp}}}
		}
		e := entity.Define(name, cfg)
		e.Version = v
		return e
	}
	belongs := func(col string) *entity.Relation {
		r := entity.BelongsTo("order", "orders", "store", col)
		return &r
	}
	hasMany := entity.HasMany("items", "items", "store", "status")
	for _, c := range []struct {
		name    string
		decls   []*entity.Entity
		refused string
	}{
		{"own states", []*entity.Entity{decl("lines", "", belongs("status"), "status", "")}, "status"},
		{"relation first", []*entity.Entity{decl("lines", "/api/v1", belongs("status"), "", ""), decl("lines", "/api/v2", nil, "status", "")}, "status"},
		{"states first", []*entity.Entity{decl("lines", "/api/v1", nil, "status", ""), decl("lines", "/api/v2", belongs("status"), "", "")}, "status"},
		{"stamp", []*entity.Entity{decl("lines", "/api/v1", belongs("day"), "", ""), decl("lines", "/api/v2", nil, "note", "day")}, "day"},
		{"other name", []*entity.Entity{decl("lines", "", belongs("status"), "", ""), decl("workflow_lines", "", nil, "status", "")}, "status"},
		{"states elsewhere", []*entity.Entity{decl("lines", "/api/v1", belongs("status"), "", ""), decl("lines", "/api/v2", nil, "note", "day")}, ""},
		{"has_many", []*entity.Entity{decl("lines", "/api/v1", &hasMany, "", ""), decl("lines", "/api/v2", nil, "status", "")}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			reg := NewRegistry()
			last := len(c.decls) - 1
			for _, e := range c.decls[:last] {
				if err := reg.Register(e); err != nil {
					t.Fatal(err)
				}
			}
			err := reg.Register(c.decls[last])
			if c.refused != "" && (err == nil || !strings.Contains(err.Error(), fmt.Sprintf("write %q", c.refused))) {
				t.Fatalf("Register = %v; want %q refused", err, c.refused)
			}
			if c.refused == "" && err != nil {
				t.Fatalf("Register = %v; want it accepted", err)
			}
		})
	}
}
