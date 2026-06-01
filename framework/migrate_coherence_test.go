package framework

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// The coherence suite pins the contract that the entity schema, AutoMigrate
// (table creation), and DiffSchema (declarative diff) all agree. The
// load-bearing property is the round-trip: a table AutoMigrate just created
// must diff CLEAN — DiffSchema reports zero changes. Any drift between what
// Define injects, what AutoMigrate emits, and what DiffSchema expects shows
// up here as a spurious ADD/DROP.

// liveColumns is a small helper to read the live column set for an assertion.
func liveColumns(t *testing.T, db *sql.DB, table string) map[string]string {
	t.Helper()
	cols, err := migrate.ReadLiveColumns(context.Background(), db, table, migrate.DetectDialect(db))
	if err != nil {
		t.Fatalf("ReadLiveColumns(%s): %v", table, err)
	}
	return cols
}

// TestCoherence_AllFieldTypesRoundTrip migrates an entity carrying every
// schema field type and asserts DiffSchema then reports no changes. This is
// the canonical "the type map AutoMigrate emits is the type map DiffSchema
// reads back" guarantee, across both dialects.
func TestCoherence_AllFieldTypesRoundTrip(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		reg := NewRegistry()
		reg.Register(entity.Define("widget", entity.EntityConfig{
			Table: "widget",
			Fields: []schema.Field{
				{Name: "s", Type: schema.String},
				{Name: "smax", Type: schema.String, Max: f64(120)},
				{Name: "txt", Type: schema.Text},
				{Name: "i", Type: schema.Int},
				{Name: "fl", Type: schema.Float},
				{Name: "b", Type: schema.Bool},
				{Name: "dec", Type: schema.Decimal},
				{Name: "en", Type: schema.Enum, Values: []string{"a", "b"}},
				{Name: "uu", Type: schema.UUID},
				{Name: "ts", Type: schema.Timestamp},
				{Name: "dt", Type: schema.Date},
				{Name: "js", Type: schema.JSON},
				{Name: "img", Type: schema.Image},
				{Name: "fil", Type: schema.File},
			},
		}.WithTimestamps(false)))

		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		changes, err := DiffSchema(context.Background(), db, reg)
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		if len(changes) != 0 {
			t.Fatalf("AutoMigrate→DiffSchema not coherent: %d spurious change(s): %+v", len(changes), changes)
		}
	})
}

// TestCoherence_ManagedColumnsRoundTrip turns on every framework-managed
// column surface (timestamps, soft-delete, multi-tenant) and asserts the
// migrated table both HAS the columns and diffs clean. This is the
// regression guard for the multi-tenant gap: MultiTenant injected tenant_id
// on writes but nothing created the column, so the first INSERT blew up.
func TestCoherence_ManagedColumnsRoundTrip(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		reg := NewRegistry()
		reg.Register(entity.Define("account", entity.EntityConfig{
			Table:       "account",
			SoftDelete:  true,
			MultiTenant: true,
			Fields: []schema.Field{
				{Name: "name", Type: schema.String, Required: true},
			},
		})) // Timestamps default on.

		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}

		cols := liveColumns(t, db, "account")
		for _, want := range []string{"id", "name", "created_at", "updated_at", "deleted_at", "tenant_id"} {
			if _, ok := cols[want]; !ok {
				t.Errorf("managed column %q not created by AutoMigrate; live=%v", want, keysOf(cols))
			}
		}

		changes, err := DiffSchema(context.Background(), db, reg)
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		if len(changes) != 0 {
			t.Fatalf("managed-column entity not coherent: %d spurious change(s): %+v", len(changes), changes)
		}
	})
}

// TestCoherence_UniqueAndIndexRoundTrip pins that UNIQUE columns and declared
// indices survive the round-trip without DiffSchema wanting to re-add them.
func TestCoherence_UniqueAndIndexRoundTrip(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		reg := NewRegistry()
		reg.Register(entity.Define("member", entity.EntityConfig{
			Table: "member",
			Fields: []schema.Field{
				{Name: "email", Type: schema.String, Unique: true, Required: true},
				{Name: "age", Type: schema.Int},
			},
			Indices: []entity.Index{
				{Name: "idx_member_age", Columns: []string{"age"}},
			},
		}.WithTimestamps(false)))

		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		// Idempotency: a second AutoMigrate must be a no-op, not an error.
		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("second AutoMigrate (idempotency): %v", err)
		}
		changes, err := DiffSchema(context.Background(), db, reg)
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		if len(changes) != 0 {
			t.Fatalf("unique/index entity not coherent: %+v", changes)
		}
	})
}

// TestCoherence_RelationsRoundTrip pins that an entity graph with a
// BelongsTo relation (FK column + constraint) migrates and then diffs clean —
// the FK column is a declared field, so DiffSchema must not want to re-add or
// drop it, and the topo-sorted CREATE TABLEs leave nothing pending.
func TestCoherence_RelationsRoundTrip(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		reg := usersAndPostsRegistry()
		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		changes, err := DiffSchema(context.Background(), db, reg)
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		if len(changes) != 0 {
			t.Fatalf("relation graph not coherent: %d spurious change(s): %+v", len(changes), changes)
		}
	})
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func f64(v float64) *float64 { return &v }
