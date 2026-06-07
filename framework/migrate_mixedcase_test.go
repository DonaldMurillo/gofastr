package framework

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// mixedCaseRegistry declares two entities whose field names carry mixed case
// (UserName, AccountId). The child has a unique index on a mixed-case column
// and a BelongsTo to the parent. On Postgres, CREATE TABLE folds the unquoted
// column to lowercase, so any DDL that *quotes* those identifiers (index / FK)
// references a column that doesn't exist and fails.
func mixedCaseRegistry() *Registry {
	reg := NewRegistry()
	reg.Register(entity.Define("MixedAccount", entity.EntityConfig{
		Table: "MixedAccount",
		Fields: []schema.Field{
			{Name: "AccountName", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false)))
	reg.Register(entity.Define("MixedUser", entity.EntityConfig{
		Table: "MixedUser",
		Fields: []schema.Field{
			{Name: "UserName", Type: schema.String, Required: true},
			{Name: "AccountId", Type: schema.String, Required: true},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("Account", "MixedAccount", "AccountId"),
		},
		Indices: []entity.Index{
			{Name: "uq_mixeduser_username", Columns: []string{"UserName"}, Unique: true},
		},
	}.WithTimestamps(false)))
	return reg
}

// TestMigrate_MixedCaseIndexAndFK verifies that AutoMigrate emits index and FK
// DDL that agrees with the (case-folded) CREATE TABLE on Postgres, and that a
// CRUD round-trip through the migrated schema works. Invisible on SQLite
// (case-insensitive identifiers); the bug only surfaces on Postgres.
func TestMigrate_MixedCaseIndexAndFK(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		reg := mixedCaseRegistry()
		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("automigrate: %v", err)
		}

		// Parent insert.
		if _, err := db.Exec(
			"INSERT INTO MixedAccount(id, AccountName) VALUES ($1, $2)", "a1", "acme",
		); err != nil {
			t.Fatalf("insert account: %v", err)
		}
		// Child insert referencing the parent.
		if _, err := db.Exec(
			"INSERT INTO MixedUser(id, UserName, AccountId) VALUES ($1, $2, $3)", "u1", "alice", "a1",
		); err != nil {
			t.Fatalf("insert user: %v", err)
		}

		// Unique index is enforced — a duplicate UserName must be rejected.
		_, err := db.Exec(
			"INSERT INTO MixedUser(id, UserName, AccountId) VALUES ($1, $2, $3)", "u2", "alice", "a1",
		)
		if err == nil {
			t.Fatal("expected unique-index violation on duplicate UserName, got nil")
		}

		// Round-trip read.
		var name string
		if err := db.QueryRow(
			"SELECT UserName FROM MixedUser WHERE id = $1", "u1",
		).Scan(&name); err != nil {
			t.Fatalf("select user: %v", err)
		}
		if name != "alice" {
			t.Fatalf("expected alice, got %q", name)
		}
	})
}

// TestMigrate_MixedCaseTableExistsBulk covers mig-2: after a mixed-case table
// is created (folded to lowercase in pg_tables on Postgres), TableExistsBulk
// must still report it as existing when queried by its original mixed-case
// name. Otherwise AutoMigrate believes the table is missing and re-attempts
// CREATE/index DDL on every boot. Keyed by the requested name so the
// AutoMigrate caller's existing[ent.GetTable()] lookup hits.
func TestMigrate_MixedCaseTableExistsBulk(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, dialect Dialect) {
		reg := mixedCaseRegistry()
		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("automigrate: %v", err)
		}

		existing, err := migrate.TableExistsBulk(
			context.Background(), db,
			[]string{"MixedAccount", "MixedUser"}, dialect,
		)
		if err != nil {
			t.Fatalf("TableExistsBulk: %v", err)
		}
		if !existing["MixedAccount"] {
			t.Errorf("MixedAccount should be reported as existing, got %v", existing)
		}
		if !existing["MixedUser"] {
			t.Errorf("MixedUser should be reported as existing, got %v", existing)
		}
	})
}
