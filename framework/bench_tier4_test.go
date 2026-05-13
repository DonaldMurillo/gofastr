package framework

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// ============================================================================
// Tier 4 — startup & infra (mixed: AutoMigrate + SchemaDiff need DB)
// ============================================================================

// BenchmarkAutoMigrate_Idempotent measures the cost of re-running AutoMigrate
// against an unchanged database. This is the "safe to run on every boot"
// claim — if it costs significantly, apps will skip it and drift.
//
// Runs against both SQLite and Postgres. The first call creates the tables;
// the timed loop re-runs against an already-correct schema.
func BenchmarkAutoMigrate_Idempotent(b *testing.B) {
	forEachBenchDialect(b, func(b *testing.B, db *sql.DB, _ Dialect) {
		for _, n := range []int{1, 10, 50} {
			n := n
			b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
				registry := buildBenchRegistry(n)
				if err := AutoMigrate(db, registry); err != nil {
					b.Fatalf("initial migrate: %v", err)
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := AutoMigrate(db, registry); err != nil {
						b.Fatalf("re-migrate: %v", err)
					}
				}
			})
		}
	})
}

// BenchmarkSchemaDiff measures the cost of computing the diff between the
// live database schema and the registered entities, without applying it.
// This runs whenever someone calls `gofastr migrate diff` or invokes the
// programmatic API to inspect drift.
func BenchmarkSchemaDiff(b *testing.B) {
	forEachBenchDialect(b, func(b *testing.B, db *sql.DB, _ Dialect) {
		for _, n := range []int{1, 10, 50} {
			n := n
			b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
				registry := buildBenchRegistry(n)
				if err := AutoMigrate(db, registry); err != nil {
					b.Fatalf("migrate: %v", err)
				}
				ctx := context.Background()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := DiffSchema(ctx, db, registry); err != nil {
						b.Fatalf("diff: %v", err)
					}
				}
			})
		}
	})
}

// buildBenchRegistry returns a Registry populated with n synthetic entities
// of moderate field complexity. Used by AutoMigrate/SchemaDiff benchmarks
// to measure scaling with entity count.
func buildBenchRegistry(n int) *Registry {
	reg := NewRegistry()
	for i := 0; i < n; i++ {
		entity := Define(fmt.Sprintf("ent_%d", i), EntityConfig{
			Table: fmt.Sprintf("ent_%d", i),
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
				{Name: "body", Type: schema.Text},
				{Name: "status", Type: schema.String, Default: "draft"},
				{Name: "author_id", Type: schema.String},
				{Name: "view_count", Type: schema.Int, Default: 0},
				{Name: "is_archived", Type: schema.Bool, Default: false},
			},
		})
		reg.Register(entity)
	}
	return reg
}
