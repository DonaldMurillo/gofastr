package crud

import (
	"context"
	"database/sql"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func openSqliteMem(t *testing.T) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { db.Close() })
	return db, nil
}

func buildSoftDeleteOwnerEntity() *entity.Entity {
	return entity.Define("softlogs", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "notes", Type: schema.String},
		},
		OwnerField: "user_id",
		SoftDelete: true,
	}.WithTimestamps(false))
}

// typedLog mirrors the row shape of the owner-scoped "logs" table the
// shared owner_test.go fixture creates.
type typedLog struct {
	ID     string `json:"id"`
	UserID string `json:"userId"`
	Notes  string `json:"notes"`
}

func ctxAs(uid string) context.Context {
	return handler.SetUser(context.Background(), &testUser{id: uid})
}

func TestTypedQuery_FindScopesByOwner(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	// Register the entity in a Registry so TypedQuery has the relations
	// metadata it may consult later (not strictly required for this
	// test, but matches the production wiring shape).
	seedRow(t, db, "log-a1", "alice", "alice row")
	seedRow(t, db, "log-b1", "bob", "bob row")

	q := NewTypedQuery[typedLog](ch)
	out, err := q.Find(ctxAs("alice"))
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("Find len = %d (want 1, alice should not see bob's row). got: %+v", len(out), out)
	}
	if out[0].UserID != "alice" {
		t.Errorf("leaked cross-user row via typed query: %+v", out[0])
	}
}

func TestTypedQuery_CountScopesByOwner(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-a1", "alice", "x")
	seedRow(t, db, "log-a2", "alice", "y")
	seedRow(t, db, "log-b1", "bob", "z")

	q := NewTypedQuery[typedLog](ch)
	n, err := q.Count(ctxAs("alice"))
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 2 {
		t.Errorf("Count = %d, want 2 (alice's rows only)", n)
	}
}

func TestTypedQuery_UpdateAllScopesByOwner(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-a1", "alice", "alice")
	seedRow(t, db, "log-b1", "bob", "bob_original")

	q := NewTypedQuery[typedLog](ch)
	n, err := q.UpdateAll(ctxAs("bob"), map[string]any{"notes": "bob_updated"})
	if err != nil {
		t.Fatalf("UpdateAll: %v", err)
	}
	if n != 1 {
		t.Errorf("UpdateAll touched %d rows (want 1, bob's only)", n)
	}
	// Verify alice's row was not touched.
	var notes string
	if err := db.QueryRow(`SELECT notes FROM logs WHERE id = ?`, "log-a1").Scan(&notes); err != nil {
		t.Fatal(err)
	}
	if notes != "alice" {
		t.Errorf("cross-user UpdateAll mutated alice's row: %q", notes)
	}
}

func TestTypedQuery_DeleteAllScopesByOwner(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-a1", "alice", "alice")
	seedRow(t, db, "log-b1", "bob", "bob")

	q := NewTypedQuery[typedLog](ch)
	n, err := q.DeleteAll(ctxAs("bob"))
	if err != nil {
		t.Fatalf("DeleteAll: %v", err)
	}
	if n != 1 {
		t.Errorf("DeleteAll removed %d rows (want 1)", n)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM logs WHERE id = ?`, "log-a1").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Error("alice's row deleted by bob's DeleteAll — cross-user leak")
	}
}

func TestTypedQuery_AdminCallerWithoutOwnerSeesAll(t *testing.T) {
	// Typed queries are server-side; absent owner in ctx, no scope is
	// applied. This is the deliberate carve-out for admin/background
	// callers — the HTTP path fails closed (see owner_test.go), but Go
	// callers can opt out of scoping by simply not providing an owner.
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-a1", "alice", "x")
	seedRow(t, db, "log-b1", "bob", "y")

	q := NewTypedQuery[typedLog](ch)
	out, err := q.Find(context.Background()) // no owner
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Errorf("admin Find len = %d, want 2 (no scope when no owner)", len(out))
	}
}

// TestTypedQuery_FirstScopesByOwner pins owner scope for First(), the
// "return the lone result" variant of Find.
func TestTypedQuery_FirstScopesByOwner(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-a1", "alice", "alice's row")
	seedRow(t, db, "log-b1", "bob", "bob's row")

	// Bob calls First — must see only bob's row, never alice's.
	q := NewTypedQuery[typedLog](ch)
	out, err := q.First(ctxAs("bob"))
	if err != nil {
		t.Fatal(err)
	}
	if out.UserID != "bob" {
		t.Errorf("First leaked cross-user row: %+v", out)
	}
}

// TestTypedQuery_ExistsScopesByOwner pins owner scope for Exists().
func TestTypedQuery_ExistsScopesByOwner(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-b1", "bob", "bob's row")

	// Alice calls Exists with no row of her own — must report false.
	q := NewTypedQuery[typedLog](ch)
	exists, err := q.Exists(ctxAs("alice"))
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Errorf("Exists() returned true for alice even though only bob has rows — cross-user leak")
	}
}

// TestTypedQuery_DeleteAllSoftDeleteRespectsOwner pins the soft-delete
// branch in DeleteAll. The non-soft-delete branch is already covered
// by TestTypedQuery_DeleteAllScopesByOwner; this confirms the parallel
// path also has owner scope.
func TestTypedQuery_DeleteAllSoftDeleteRespectsOwner(t *testing.T) {
	installOwnerExtractor(t)
	// Recreate a soft-delete-enabled fixture inline (the shared
	// fixture has no soft_delete column).
	db, err := openSqliteMem(t)
	if err != nil {
		t.Skip(err)
	}
	if _, err := db.Exec(`CREATE TABLE softlogs (
		id TEXT PRIMARY KEY, user_id TEXT NOT NULL, notes TEXT,
		deleted_at TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO softlogs (id, user_id, notes) VALUES
		('s-a','alice','a'), ('s-b','bob','b')`); err != nil {
		t.Fatal(err)
	}

	ent := buildSoftDeleteOwnerEntity()
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)

	q := NewTypedQuery[typedLog](ch)
	n, err := q.DeleteAll(ctxAs("bob"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("soft DeleteAll touched %d rows (want 1, bob's only)", n)
	}
	// Alice's row should still have NULL deleted_at; bob's should not.
	var aliceDel, bobDel any
	_ = db.QueryRow(`SELECT deleted_at FROM softlogs WHERE id='s-a'`).Scan(&aliceDel)
	_ = db.QueryRow(`SELECT deleted_at FROM softlogs WHERE id='s-b'`).Scan(&bobDel)
	if aliceDel != nil {
		t.Errorf("soft DeleteAll touched alice's row: deleted_at=%v", aliceDel)
	}
	if bobDel == nil {
		t.Errorf("soft DeleteAll didn't soft-delete bob's row")
	}
}

// softDeleteFixture builds a soft-delete-enabled handler with one
// already-soft-deleted row ('s-dead') and one live row ('s-live'),
// both owned by the same user so owner scope doesn't mask the
// soft-delete behaviour under test.
func softDeleteFixture(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db, err := openSqliteMem(t)
	if err != nil {
		t.Skip(err)
	}
	if _, err := db.Exec(`CREATE TABLE softlogs (
		id TEXT PRIMARY KEY, user_id TEXT NOT NULL, notes TEXT,
		deleted_at TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO softlogs (id, user_id, notes, deleted_at) VALUES
		('s-dead','alice','original','2020-01-01T00:00:00Z'),
		('s-live','alice','original',NULL)`); err != nil {
		t.Fatal(err)
	}
	ent := buildSoftDeleteOwnerEntity()
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	return ch, db
}

// TestTypedQuery_UpdateAllSkipsSoftDeleted asserts UpdateAll does not
// mutate rows whose deleted_at is set, even when the WHERE predicate
// would otherwise match them.
func TestTypedQuery_UpdateAllSkipsSoftDeleted(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := softDeleteFixture(t)

	// Predicate matches BOTH rows (notes = 'original').
	q := NewTypedQuery[typedLog](ch).
		Where(entity.NewStringColumn("notes").Eq("original"))
	n, err := q.UpdateAll(ctxAs("alice"), map[string]any{"notes": "touched"})
	if err != nil {
		t.Fatalf("UpdateAll: %v", err)
	}
	if n != 1 {
		t.Errorf("UpdateAll touched %d rows (want 1 — the soft-deleted row must be excluded)", n)
	}
	var deadNotes string
	if err := db.QueryRow(`SELECT notes FROM softlogs WHERE id='s-dead'`).Scan(&deadNotes); err != nil {
		t.Fatal(err)
	}
	if deadNotes != "original" {
		t.Errorf("UpdateAll mutated a soft-deleted row: notes=%q (want %q)", deadNotes, "original")
	}
}

// TestTypedQuery_DeleteAllSkipsSoftDeleted asserts DeleteAll's
// soft-delete branch does not re-stamp deleted_at on rows that are
// already soft-deleted (which would otherwise resurrect their delete
// timestamp / count them as freshly touched).
func TestTypedQuery_DeleteAllSkipsSoftDeleted(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := softDeleteFixture(t)

	q := NewTypedQuery[typedLog](ch).
		Where(entity.NewStringColumn("notes").Eq("original"))
	n, err := q.DeleteAll(ctxAs("alice"))
	if err != nil {
		t.Fatalf("DeleteAll: %v", err)
	}
	if n != 1 {
		t.Errorf("DeleteAll touched %d rows (want 1 — the already-soft-deleted row must be excluded)", n)
	}
	// The already-deleted row must keep its original timestamp.
	var deadDel string
	if err := db.QueryRow(`SELECT deleted_at FROM softlogs WHERE id='s-dead'`).Scan(&deadDel); err != nil {
		t.Fatal(err)
	}
	if deadDel != "2020-01-01T00:00:00Z" {
		t.Errorf("DeleteAll re-stamped an already-soft-deleted row: deleted_at=%q", deadDel)
	}
}

// Sanity: the test fixture uses real http context plumbing for HTTP
// tests, but typed queries go through ctx. Compile-time check that
// nothing in this file references http.
var _ = http.MethodGet
