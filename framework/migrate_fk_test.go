package framework

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/gofastr/gofastr/core/schema"
	"github.com/gofastr/gofastr/framework/entity"
)

// usersAndPostsRegistry returns a registry where posts.author_id is a
// BelongsTo to users, with both tables having a non-null requirement on the
// FK column so a bogus insert reliably trips the constraint.
func usersAndPostsRegistry() *Registry {
	reg := NewRegistry()
	reg.Register(entity.Define("users", entity.EntityConfig{
		Table: "users",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false)))
	reg.Register(entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "author_id", Type: schema.String, Required: true},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("author", "users", "author_id"),
		},
	}.WithTimestamps(false)))
	return reg
}

// isFKError detects a foreign-key violation in either dialect's error string.
// SQLite says "FOREIGN KEY constraint failed"; Postgres (lib/pq) says
// "violates foreign key constraint".
func isFKError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "foreign key") || strings.Contains(s, "constraint")
}

// ============================================================================
// Test: FK constraint enforces at runtime — inserting an orphan post fails.
// (Replaces the SQLite-only DDL scrape; the runtime semantic is what we care
// about and it's directly observable on both engines.)
// ============================================================================

func TestMigrate_FK_BelongsToEnforced(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		reg := usersAndPostsRegistry()
		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("automigrate: %v", err)
		}

		_, err := db.Exec("INSERT INTO posts(id, title, author_id) VALUES ($1, $2, $3)", "p1", "orphan", "no-such-user")
		if err == nil {
			t.Fatal("expected FK violation when inserting post with bogus author_id, got nil")
		}
		if !isFKError(err) {
			t.Fatalf("expected FK error, got %v", err)
		}
	})
}

// ============================================================================
// Test: AutoMigrate creates referenced tables before referencers, regardless
// of registration order.
// ============================================================================

func TestMigrate_FK_TopologicallySorted(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		reg := NewRegistry()
		// Register in reverse dependency order to prove the sort works.
		reg.Register(entity.Define("posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
				{Name: "author_id", Type: schema.String},
			},
			Relations: []entity.Relation{entity.BelongsTo("author", "users", "author_id")},
		}.WithTimestamps(false)))
		reg.Register(entity.Define("users", entity.EntityConfig{
			Table: "users",
			Fields: []schema.Field{
				{Name: "name", Type: schema.String, Required: true},
			},
		}.WithTimestamps(false)))

		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("automigrate: %v", err)
		}
	})
}

// ============================================================================
// Test: missing FK target → error before any DDL runs.
// ============================================================================

func TestMigrate_FK_MissingTarget_Errors(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		reg := NewRegistry()
		reg.Register(entity.Define("posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
				{Name: "author_id", Type: schema.String},
			},
			Relations: []entity.Relation{
				entity.BelongsTo("author", "users_does_not_exist", "author_id"),
			},
		}.WithTimestamps(false)))

		err := AutoMigrate(db, reg)
		if err == nil {
			t.Fatal("expected error for missing FK target, got nil")
		}
		if !strings.Contains(err.Error(), "users_does_not_exist") {
			t.Fatalf("expected error to name missing entity, got %v", err)
		}
	})
}

// ============================================================================
// Test: HasMany / HasOne don't add an FK on the source entity (they live on
// the target). We assert by attempting to insert an "orphan" user with a
// bogus identity that would only fail if users had an outbound FK; it must
// succeed. Then we confirm posts.author_id still enforces.
// ============================================================================

func TestMigrate_FK_HasManyDoesNotAddSourceFK(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		reg := NewRegistry()
		reg.Register(entity.Define("users", entity.EntityConfig{
			Table: "users",
			Fields: []schema.Field{
				{Name: "name", Type: schema.String, Required: true},
			},
			Relations: []entity.Relation{
				entity.HasMany("posts", "posts", "author_id"), // FK lives on posts, not users
			},
		}.WithTimestamps(false)))
		reg.Register(entity.Define("posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
				{Name: "author_id", Type: schema.String},
			},
			Relations: []entity.Relation{
				entity.BelongsTo("author", "users", "author_id"),
			},
		}.WithTimestamps(false)))

		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("automigrate: %v", err)
		}

		// users has no outbound FK — a plain insert with no related rows must succeed.
		if _, err := db.Exec("INSERT INTO users(id, name) VALUES ($1, $2)", "u1", "Alice"); err != nil {
			t.Fatalf("insert into users without FK should succeed, got: %v", err)
		}

		// posts has an FK on author_id — orphan insert must fail.
		_, err := db.Exec("INSERT INTO posts(id, title, author_id) VALUES ($1, $2, $3)", "p1", "orphan", "ghost-user")
		if err == nil {
			t.Fatal("expected FK violation on posts.author_id, got nil")
		}
		if !isFKError(err) {
			t.Fatalf("expected FK error, got %v", err)
		}

		// Insert with a real author succeeds.
		if _, err := db.Exec("INSERT INTO posts(id, title, author_id) VALUES ($1, $2, $3)", "p2", "valid", "u1"); err != nil {
			t.Fatalf("expected valid author insert to succeed, got: %v", err)
		}
	})
}
