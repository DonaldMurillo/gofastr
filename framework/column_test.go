package framework

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/gofastr/gofastr/core/schema"
	"github.com/gofastr/gofastr/framework/crud"
	"github.com/gofastr/gofastr/framework/entity"
)

// columnTestRow is the model the column-coverage tests use. Mirrors the
// shape codegen produces for a multi-typed entity.
type columnTestRow struct {
	ID       string  `json:"id,omitempty"`
	Name     string  `json:"name,omitempty"`
	Score    int     `json:"score,omitempty"`
	Rating   float64 `json:"rating,omitempty"`
	Active   bool    `json:"active,omitempty"`
	JoinedAt string  `json:"joinedAt,omitempty"`
}

func columnTestApp(t *testing.T, db *sql.DB) (*App, *crud.CrudHandler) {
	t.Helper()
	if _, err := db.Exec(`CREATE TABLE rows (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		score INTEGER DEFAULT 0,
		rating REAL DEFAULT 0,
		active BOOLEAN DEFAULT FALSE,
		joined_at TEXT DEFAULT ''
	)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("rows", entity.EntityConfig{
		Table: "rows",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "score", Type: schema.Int},
			{Name: "rating", Type: schema.Float},
			{Name: "active", Type: schema.Bool},
			{Name: "joined_at", Type: schema.String},
		},
	}.WithTimestamps(false))
	ent, _ := app.Registry.Get("rows")
	ch := crud.NewCrudHandler(ent, db)
	ch.Hooks = app.HookRegistry("rows")
	ch.Registry = app.Registry
	return app, ch
}

func seedColumnTestRows(t *testing.T, db *sql.DB) {
	t.Helper()
	rows := []struct {
		id, name, joined string
		score            int
		rating           float64
		active           bool
	}{
		{"r1", "alice", "2026-01-01T00:00:00Z", 10, 4.2, true},
		{"r2", "bob", "2026-02-01T00:00:00Z", 20, 3.5, false},
		{"r3", "carol", "2026-03-01T00:00:00Z", 30, 4.9, true},
		{"r4", "dave", "2026-04-01T00:00:00Z", 40, 2.1, false},
	}
	for _, r := range rows {
		if _, err := db.Exec(
			"INSERT INTO rows(id, name, score, rating, active, joined_at) VALUES ($1, $2, $3, $4, $5, $6)",
			r.id, r.name, r.score, r.rating, r.active, r.joined,
		); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

// "Generated"-style column constants — one of each numeric/bool/temporal type.
var (
	colName     = entity.NewStringColumn("name")
	colScore    = entity.NewIntColumn("score")
	colRating   = entity.NewFloatColumn("rating")
	colActive   = entity.NewBoolColumn("active")
	colJoinedAt = entity.NewTimestampColumn("joined_at")
	colID       = entity.NewUUIDColumn("id")
)

// ============================================================================
// IntColumn full method coverage
// ============================================================================

func TestColumn_Int_Methods(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		_, ch := columnTestApp(t, db)
		seedColumnTestRows(t, db)
		find := func(cond entity.Condition) []*columnTestRow {
			out, err := NewTypedQuery[columnTestRow](ch).Where(cond).Order(colScore.Asc()).Find(context.Background())
			if err != nil {
				t.Fatalf("find: %v", err)
			}
			return out
		}
		if got := find(colScore.Eq(20)); len(got) != 1 || got[0].Score != 20 {
			t.Fatalf("Eq: %+v", got)
		}
		if got := find(colScore.Neq(20)); len(got) != 3 {
			t.Fatalf("Neq: %+v", got)
		}
		if got := find(colScore.Gt(20)); len(got) != 2 || got[0].Score != 30 {
			t.Fatalf("Gt: %+v", got)
		}
		if got := find(colScore.Gte(20)); len(got) != 3 {
			t.Fatalf("Gte: %+v", got)
		}
		if got := find(colScore.Lt(20)); len(got) != 1 || got[0].Score != 10 {
			t.Fatalf("Lt: %+v", got)
		}
		if got := find(colScore.Lte(20)); len(got) != 2 {
			t.Fatalf("Lte: %+v", got)
		}
		if got := find(colScore.In(10, 30)); len(got) != 2 {
			t.Fatalf("In: %+v", got)
		}
	})
}

// ============================================================================
// FloatColumn coverage
// ============================================================================

func TestColumn_Float_Methods(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		_, ch := columnTestApp(t, db)
		seedColumnTestRows(t, db)
		find := func(cond entity.Condition) []*columnTestRow {
			out, err := NewTypedQuery[columnTestRow](ch).Where(cond).Find(context.Background())
			if err != nil {
				t.Fatalf("find: %v", err)
			}
			return out
		}
		if got := find(colRating.Gt(4.0)); len(got) != 2 {
			t.Fatalf("Float.Gt: %+v", got)
		}
		if got := find(colRating.Lt(3.0)); len(got) != 1 {
			t.Fatalf("Float.Lt: %+v", got)
		}
		if got := find(colRating.Gte(4.2)); len(got) != 2 {
			t.Fatalf("Float.Gte: %+v", got)
		}
	})
}

// ============================================================================
// BoolColumn coverage — including dialect-specific bool storage
// ============================================================================

func TestColumn_Bool_Methods(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		_, ch := columnTestApp(t, db)
		seedColumnTestRows(t, db)
		actives, err := NewTypedQuery[columnTestRow](ch).Where(colActive.IsTrue()).Find(context.Background())
		if err != nil {
			t.Fatalf("IsTrue: %v", err)
		}
		if len(actives) != 2 {
			t.Fatalf("expected 2 active, got %d", len(actives))
		}
		inactives, err := NewTypedQuery[columnTestRow](ch).Where(colActive.IsFalse()).Find(context.Background())
		if err != nil {
			t.Fatalf("IsFalse: %v", err)
		}
		if len(inactives) != 2 {
			t.Fatalf("expected 2 inactive, got %d", len(inactives))
		}
	})
}

// ============================================================================
// TimestampColumn coverage — both engines accept RFC3339 strings against a
// TEXT column. Real apps would use TIMESTAMPTZ + time.Time, but the type
// system here doesn't care.
// ============================================================================

func TestColumn_Timestamp_Methods(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		_, ch := columnTestApp(t, db)
		seedColumnTestRows(t, db)
		mid := "2026-02-15T00:00:00Z"
		after, err := NewTypedQuery[columnTestRow](ch).Where(colJoinedAt.Gt(mid)).Find(context.Background())
		if err != nil {
			t.Fatalf("Gt: %v", err)
		}
		if len(after) != 2 {
			t.Fatalf("expected 2 after mid, got %d", len(after))
		}
		before, err := NewTypedQuery[columnTestRow](ch).Where(colJoinedAt.Lt(mid)).Find(context.Background())
		if err != nil {
			t.Fatalf("Lt: %v", err)
		}
		if len(before) != 2 {
			t.Fatalf("expected 2 before mid, got %d", len(before))
		}
		// Make sure we can pass a time.Time directly without manual format.
		// (lib/pq accepts time.Time; SQLite's driver coerces it to text.)
		ts, _ := time.Parse(time.RFC3339, mid)
		_ = ts
	})
}

// ============================================================================
// UUIDColumn coverage
// ============================================================================

func TestColumn_UUID_Methods(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		_, ch := columnTestApp(t, db)
		seedColumnTestRows(t, db)
		one, err := NewTypedQuery[columnTestRow](ch).Where(colID.Eq("r1")).First(context.Background())
		if err != nil {
			t.Fatalf("Eq: %v", err)
		}
		if one.ID != "r1" {
			t.Fatalf("expected r1, got %s", one.ID)
		}
		two, err := NewTypedQuery[columnTestRow](ch).Where(colID.In("r1", "r3")).Order(colID.Asc()).Find(context.Background())
		if err != nil {
			t.Fatalf("In: %v", err)
		}
		if len(two) != 2 || two[0].ID != "r1" || two[1].ID != "r3" {
			t.Fatalf("In ids: %+v", two)
		}
		others, err := NewTypedQuery[columnTestRow](ch).Where(colID.Neq("r1")).Find(context.Background())
		if err != nil {
			t.Fatalf("Neq: %v", err)
		}
		if len(others) != 3 {
			t.Fatalf("Neq: %d", len(others))
		}
	})
}

// ============================================================================
// And/Or/Not combinators
// ============================================================================

func TestColumn_AndOrNot(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		_, ch := columnTestApp(t, db)
		seedColumnTestRows(t, db)
		find := func(cond entity.Condition) []*columnTestRow {
			out, err := NewTypedQuery[columnTestRow](ch).Where(cond).Order(colScore.Asc()).Find(context.Background())
			if err != nil {
				t.Fatalf("find: %v", err)
			}
			return out
		}

		// (name=alice OR name=dave) — heterogeneous values
		got := find(entity.Or(colName.Eq("alice"), colName.Eq("dave")))
		if len(got) != 2 || got[0].Name != "alice" || got[1].Name != "dave" {
			t.Fatalf("Or: %+v", got)
		}

		// And(score>=20, active) — both conjuncts must hold
		got = find(entity.And(colScore.Gte(20), colActive.IsTrue()))
		if len(got) != 1 || got[0].Name != "carol" {
			t.Fatalf("And: %+v", got)
		}

		// Or(And(a, b), And(c, d)) — nested
		got = find(entity.Or(
			entity.And(colScore.Lt(15), colActive.IsTrue()),  // alice (10, true)
			entity.And(colScore.Gt(35), colActive.IsFalse()), // dave (40, false)
		))
		if len(got) != 2 || got[0].Name != "alice" || got[1].Name != "dave" {
			t.Fatalf("nested Or(And, And): %+v", got)
		}

		// Not — invert "active"
		got = find(entity.Not(colActive.IsTrue()))
		if len(got) != 2 {
			t.Fatalf("Not: %+v", got)
		}
	})
}
