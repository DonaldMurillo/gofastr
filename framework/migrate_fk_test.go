package framework

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
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
// Test: FK constraint enforces at runtime, inserting an orphan post fails.
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

		// users has no outbound FK, a plain insert with no related rows must succeed.
		if _, err := db.Exec("INSERT INTO users(id, name) VALUES ($1, $2)", "u1", "Alice"); err != nil {
			t.Fatalf("insert into users without FK should succeed, got: %v", err)
		}

		// posts has an FK on author_id, orphan insert must fail.
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

// ============================================================================
// Test: OnDelete CASCADE deletes child rows when the parent is deleted.
// ============================================================================

func TestMigrate_FK_OnDeleteCascade(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
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
				entity.BelongsTo("author", "users", "author_id").OnDeleteAction(entity.OnDeleteCascade),
			},
		}.WithTimestamps(false)))

		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("automigrate: %v", err)
		}

		if _, err := db.Exec("INSERT INTO users(id, name) VALUES ($1, $2)", "u1", "Alice"); err != nil {
			t.Fatalf("insert user: %v", err)
		}
		if _, err := db.Exec("INSERT INTO posts(id, title, author_id) VALUES ($1, $2, $3)", "p1", "Post 1", "u1"); err != nil {
			t.Fatalf("insert post: %v", err)
		}

		// Delete user u1: post p1 must be cascade-deleted.
		if _, err := db.Exec("DELETE FROM users WHERE id = $1", "u1"); err != nil {
			t.Fatalf("delete user: %v", err)
		}

		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM posts WHERE id = 'p1'").Scan(&count); err != nil {
			t.Fatalf("count posts: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected 0 posts after cascade delete, got %d", count)
		}
	})
}

// ============================================================================
// Test: Foreign keys on BelongsTo are auto-indexed.
// ============================================================================

func TestMigrate_FK_AutoIndex(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, dialect Dialect) {
		reg := usersAndPostsRegistry()
		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("automigrate: %v", err)
		}

		// Verify index exists on posts(author_id)
		var indexFound bool
		if dialect == DialectSQLite {
			var count int
			err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_posts_author_id'").Scan(&count)
			if err != nil {
				t.Fatalf("query sqlite_master: %v", err)
			}
			indexFound = count > 0
		} else {
			var count int
			err := db.QueryRow("SELECT COUNT(*) FROM pg_indexes WHERE indexname = 'idx_posts_author_id'").Scan(&count)
			if err != nil {
				t.Fatalf("query pg_indexes: %v", err)
			}
			indexFound = count > 0
		}

		if !indexFound {
			t.Errorf("expected auto-created index idx_posts_author_id to exist")
		}
	})
}

// ============================================================================
// Test: ManyToMany pivot tables are auto-created with cascading foreign keys.
// ============================================================================

func TestMigrate_ManyToMany_PivotTable(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		reg := NewRegistry()
		reg.Register(entity.Define("posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
			},
			Relations: []entity.Relation{
				entity.ManyToMany("tags", "tags", "post_tags", "post_id", "tag_id"),
			},
		}.WithTimestamps(false)))
		reg.Register(entity.Define("tags", entity.EntityConfig{
			Table: "tags",
			Fields: []schema.Field{
				{Name: "name", Type: schema.String, Required: true},
			},
		}.WithTimestamps(false)))

		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("automigrate: %v", err)
		}

		// Insert post and tag
		if _, err := db.Exec("INSERT INTO posts(id, title) VALUES ($1, $2)", "p1", "Go"); err != nil {
			t.Fatalf("insert post: %v", err)
		}
		if _, err := db.Exec("INSERT INTO tags(id, name) VALUES ($1, $2)", "t1", "tech"); err != nil {
			t.Fatalf("insert tag: %v", err)
		}

		// Insert into auto-created pivot table post_tags
		if _, err := db.Exec("INSERT INTO post_tags(post_id, tag_id) VALUES ($1, $2)", "p1", "t1"); err != nil {
			t.Fatalf("insert into post_tags: %v", err)
		}

		// Delete post p1: pivot row must cascade-delete
		if _, err := db.Exec("DELETE FROM posts WHERE id = $1", "p1"); err != nil {
			t.Fatalf("delete post: %v", err)
		}

		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM post_tags WHERE post_id = 'p1'").Scan(&count); err != nil {
			t.Fatalf("count post_tags: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected 0 pivot rows after post delete, got %d", count)
		}
	})
}

// ============================================================================
// Test: DiffSchema emits pivot table creation and secondary index.
// ============================================================================

func TestMigrate_SchemaDiff_PivotSecondaryIndex(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		reg := NewRegistry()
		reg.Register(entity.Define("posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
			},
			Relations: []entity.Relation{
				entity.ManyToMany("tags", "tags", "post_tags", "post_id", "tag_id"),
			},
		}.WithTimestamps(false)))
		reg.Register(entity.Define("tags", entity.EntityConfig{
			Table: "tags",
			Fields: []schema.Field{
				{Name: "name", Type: schema.String, Required: true},
			},
		}.WithTimestamps(false)))

		changes, err := DiffSchema(context.Background(), db, reg)
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}

		var foundPivotTable, foundPivotIndex bool
		for _, ch := range changes {
			if strings.Contains(ch.Summary, "post_tags: create pivot table") {
				foundPivotTable = true
				if ch.Down != "DROP TABLE IF EXISTS post_tags" {
					t.Errorf("expected DROP TABLE IF EXISTS post_tags, got %q", ch.Down)
				}
			}
			if strings.Contains(ch.Summary, "post_tags: index tag_id") {
				foundPivotIndex = true
				if ch.Down != "DROP INDEX IF EXISTS idx_post_tags_tag_id" {
					t.Errorf("expected DROP INDEX IF EXISTS idx_post_tags_tag_id, got %q", ch.Down)
				}
			}
		}

		if !foundPivotTable {
			t.Errorf("expected pivot table change in DiffSchema")
		}
		if !foundPivotIndex {
			t.Errorf("expected pivot index change in DiffSchema")
		}
	})
}

// TestMigrate_Snapshot_ManyToMany_PivotTable asserts that SnapshotFromPlan and GeneratePlan
// include ManyToMany pivot tables and their secondary indexes with symmetric rollback.
func TestMigrate_Snapshot_ManyToMany_PivotTable(t *testing.T) {
	reg := NewRegistry()
	reg.Register(entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
		Relations: []entity.Relation{
			entity.ManyToMany("tags", "tags", "post_tags", "post_id", "tag_id"),
		},
	}.WithTimestamps(false)))

	reg.Register(entity.Define("tags", entity.EntityConfig{
		Table: "tags",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false)))

	snap := SnapshotFromPlan(migrate.Plan{Registry: reg}, DialectSQLite)
	if snap.Tables["post_tags"] == nil {
		t.Fatalf("expected post_tags in SnapshotFromPlan tables")
	}
	if snap.TableDDL["post_tags"] == "" {
		t.Fatalf("expected post_tags DDL in SnapshotFromPlan TableDDL")
	}

	up, down, next, err := GenerateMigration(reg, migrate.SchemaSnapshot{}, DialectSQLite)
	if err != nil {
		t.Fatalf("GenerateMigration: %v", err)
	}
	if !strings.Contains(up, "CREATE TABLE IF NOT EXISTS post_tags") {
		t.Fatalf("expected CREATE TABLE post_tags in up migration, got:\n%s", up)
	}
	if !strings.Contains(up, "CREATE INDEX IF NOT EXISTS idx_post_tags_tag_id") {
		t.Fatalf("expected CREATE INDEX idx_post_tags_tag_id in up migration, got:\n%s", up)
	}
	if !strings.Contains(down, "DROP TABLE IF EXISTS post_tags") {
		t.Fatalf("expected DROP TABLE post_tags in down migration, got:\n%s", down)
	}
	if !strings.Contains(down, "DROP INDEX IF EXISTS idx_post_tags_tag_id") {
		t.Fatalf("expected DROP INDEX idx_post_tags_tag_id in down migration, got:\n%s", down)
	}
	if next.Tables["post_tags"] == nil {
		t.Fatalf("expected post_tags in next snapshot")
	}
}
