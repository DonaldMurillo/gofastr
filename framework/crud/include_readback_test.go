package crud

import (
	"context"
	"database/sql"
	"reflect"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// An included row is the same row a direct GET of its entity reads, so
// every value a client gets through ?include= must be the value GET
// returns and the value the next write accepts back. Each test below
// migrates an orders/lines pair the way AutoMigrate does and compares the
// two reads, on SQLite and on Postgres.

// includedLines is the fixture: an order with one line carrying a JSON
// document and a date, the line handler, and the order handler that
// includes lines. dueName and metaName are the line's declared field
// names, so a test can declare them mixed-case.
type includedLines struct {
	orders, lines *CrudHandler
	conn          *sql.DB
	orderID       string
	lineID        string
}

func forIncludedLines(t *testing.T, dueName, metaName string, meta any, fn func(t *testing.T, fx includedLines)) {
	run := func(t *testing.T, conn *sql.DB) {
		ctx := context.Background()
		lines := entity.Define("lines", entity.EntityConfig{
			Fields: []schema.Field{
				{Name: "order_id", Type: schema.String},
				{Name: dueName, Type: schema.Date},
				{Name: metaName, Type: schema.JSON},
				{Name: "price", Type: schema.Decimal},
			},
		}.WithTimestamps(false))
		orders := entity.Define("orders", entity.EntityConfig{
			Fields:    []schema.Field{{Name: "title", Type: schema.String}},
			Relations: []entity.Relation{entity.HasMany("lines", "lines", "order_id")},
		}.WithTimestamps(false))
		for _, ent := range []*entity.Entity{orders, lines} {
			if err := migrate.MigrateEntity(conn, ent); err != nil {
				t.Fatalf("MigrateEntity: %v", err)
			}
			ent.SetDB(conn)
		}
		reg := stubRegistry{byName: map[string]*entity.Entity{"orders": orders, "lines": lines}}
		fx := includedLines{orders: NewCrudHandler(orders, conn), lines: NewCrudHandler(lines, conn), conn: conn}
		fx.orders.Registry, fx.lines.Registry = reg, reg
		order, err := fx.orders.CreateOne(ctx, map[string]any{"title": "o"})
		if err != nil {
			t.Fatalf("create order: %v", err)
		}
		fx.orderID, _ = order["id"].(string)
		line, err := fx.lines.CreateOne(ctx, map[string]any{
			"order_id": fx.orderID, dueName: "2026-10-27", metaName: meta, "price": "12.5",
		})
		if err != nil {
			t.Fatalf("create line: %v", err)
		}
		fx.lineID, _ = line["id"].(string)
		fn(t, fx)
	}
	t.Run("sqlite", func(t *testing.T) { run(t, setupDB(t)) })
	t.Run("postgres", func(t *testing.T) { run(t, pgtest.DB(t)) })
}

// includedLine reads the order with ?include=lines and returns its one line.
func (fx includedLines) includedLine(t *testing.T) map[string]any {
	t.Helper()
	got, err := fx.orders.GetOne(context.Background(), fx.orderID, []string{"lines"})
	if err != nil {
		t.Fatalf("GetOne with include: %v", err)
	}
	rows, _ := got["lines"].([]map[string]any)
	if len(rows) != 1 {
		t.Fatalf("included lines = %#v, want one", got["lines"])
	}
	return rows[0]
}

func (fx includedLines) directLine(t *testing.T) map[string]any {
	t.Helper()
	got, err := fx.lines.GetOne(context.Background(), fx.lineID, nil)
	if err != nil {
		t.Fatalf("GetOne line: %v", err)
	}
	return got
}

func TestIncludedJSONKeepsStoredKeys(t *testing.T) {
	meta := map[string]any{"key_name": "v", "Outer": map[string]any{"inner_key": []any{map[string]any{"deep_key": true}}}}
	forIncludedLines(t, "due", "meta", meta, func(t *testing.T, fx includedLines) {
		key := fx.lines.convertKey("meta")
		direct := fx.directLine(t)[key]
		if !reflect.DeepEqual(direct, meta) {
			t.Fatalf("direct meta = %#v, want %#v", direct, meta)
		}
		included := fx.includedLine(t)[key]
		if !reflect.DeepEqual(included, direct) {
			t.Fatalf("included meta = %#v, want the direct read %#v", included, direct)
		}
		// Saving what the include returned must leave the document as it was.
		if _, err := fx.lines.UpdateOne(context.Background(), fx.lineID, map[string]any{"meta": included}); err != nil {
			t.Fatalf("resave included meta: %v", err)
		}
		if after := fx.directLine(t)[key]; !reflect.DeepEqual(after, meta) {
			t.Fatalf("meta after resave = %#v, want %#v", after, meta)
		}
	})
}

func TestIncludedMixedCaseFieldsDecode(t *testing.T) {
	meta := map[string]any{"a": float64(1)}
	forIncludedLines(t, "Due", "Meta", meta, func(t *testing.T, fx includedLines) {
		direct := fx.directLine(t)
		included := fx.includedLine(t)
		for _, name := range []string{"Due", "Meta"} {
			key := fx.lines.convertKey(name)
			if !reflect.DeepEqual(included[key], direct[key]) {
				t.Errorf("included %s = %#v (%T), want the direct read %#v (row %v)", key, included[key], included[key], direct[key], included)
			}
		}
		if direct[fx.lines.convertKey("Due")] != "2026-10-27" {
			t.Errorf("direct Due = %#v, want 2026-10-27", direct[fx.lines.convertKey("Due")])
		}
	})
}

func TestEagerLoadDecodesLikeInclude(t *testing.T) {
	meta := map[string]any{"a": float64(1)}
	forIncludedLines(t, "due", "meta", meta, func(t *testing.T, fx includedLines) {
		direct := fx.directLine(t)
		loaded, err := EagerLoad(context.Background(), fx.conn, fx.orders.Entity, fx.orders.Entity.Config.Relations, []string{fx.orderID}, fx.orders.Registry)
		if err != nil {
			t.Fatalf("EagerLoad: %v", err)
		}
		rows, _ := loaded[fx.orderID]["lines"].([]map[string]any)
		if len(rows) != 1 {
			t.Fatalf("eager lines = %#v, want one", loaded[fx.orderID])
		}
		for _, col := range []string{"due", "meta", "price"} {
			if got, want := rows[0][col], direct[fx.lines.convertKey(col)]; !reflect.DeepEqual(got, want) {
				t.Errorf("eager %s = %#v (%T), want the direct read %#v", col, got, got, want)
			}
		}
	})
}
