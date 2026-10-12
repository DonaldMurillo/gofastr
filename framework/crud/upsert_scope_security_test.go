package crud

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// UpsertOne used to check ownership in a preflight SELECT that ran BEFORE
// the BeforeCreate hooks, and the ON CONFLICT DO UPDATE carried no scope of
// its own. A hook that derives or normalizes the primary key (a documented
// hook use) moved the conflict onto another owner's row after the check, and
// the DO UPDATE re-stamped that row to the caller.

// pagesUpsertWorld seeds alice's row id=pricing on an owner-scoped entity
// and installs hk as the BeforeCreate hook.
func pagesUpsertWorld(t *testing.T, hk hook.HookFunc, mutate ...func(*entity.EntityConfig)) (*CrudHandler, *sql.DB) {
	t.Helper()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE pages (id TEXT PRIMARY KEY, owner_id TEXT, title TEXT, body TEXT, deleted_at TEXT)`
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("pages", "pages", "owner_id", []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "owner_id", Type: schema.String},
		{Name: "title", Type: schema.String},
		{Name: "body", Type: schema.String},
	}, mutate...), ddl)
	seedRows(t, db, "pages", []map[string]any{
		{"id": "pricing", "owner_id": "alice", "title": "Pricing", "body": "alice original"},
	})
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeCreate, hk)
	return ch, db
}

// lowerPKHook normalizes a supplied id to lowercase.
func lowerPKHook(_ context.Context, data any) error {
	m := data.(map[string]any)
	if s, ok := m["id"].(string); ok {
		m["id"] = strings.ToLower(s)
	}
	return nil
}

// slugPKHook derives the id from the title when none was sent.
func slugPKHook(_ context.Context, data any) error {
	m := data.(map[string]any)
	if _, ok := m["id"]; !ok {
		if s, ok := m["title"].(string); ok {
			m["id"] = strings.ToLower(s)
		}
	}
	return nil
}

func assertAliceRowIntact(t *testing.T, db *sql.DB) {
	t.Helper()
	var o, b string
	if err := db.QueryRow("SELECT owner_id, body FROM pages WHERE id = $1", "pricing").Scan(&o, &b); err != nil {
		t.Fatalf("read alice row: %v", err)
	}
	if o != "alice" || b != "alice original" {
		t.Fatalf("alice's row taken over: owner_id=%q body=%q", o, b)
	}
}

func TestUpsertNormalizedPKCannotHijack(t *testing.T) {
	ch, db := pagesUpsertWorld(t, lowerPKHook)
	_, err := ch.UpsertOne(upsertSecurityContext("bob", ""),
		map[string]any{"id": "PRICING", "title": "Pricing", "body": "bob overwrote"})
	if !errors.Is(err, errUpsertForeignRow) {
		t.Fatalf("err = %v, want errUpsertForeignRow", err)
	}
	assertAliceRowIntact(t, db)
}

func TestUpsertDerivedPKCannotHijack(t *testing.T) {
	ch, db := pagesUpsertWorld(t, slugPKHook)
	_, err := ch.UpsertOne(upsertSecurityContext("bob", ""),
		map[string]any{"title": "Pricing", "body": "bob overwrote"})
	if !errors.Is(err, errUpsertForeignRow) {
		t.Fatalf("err = %v, want errUpsertForeignRow", err)
	}
	assertAliceRowIntact(t, db)
	// Positive control: alice saving her own row through the same hook works.
	row, err := ch.UpsertOne(upsertSecurityContext("alice", ""),
		map[string]any{"title": "Pricing", "body": "alice edited"})
	if err != nil {
		t.Fatalf("owner upsert: %v", err)
	}
	if row["body"] != "alice edited" {
		t.Fatalf("owner upsert returned %v", row)
	}
}

// A hook-normalized key that lands on a soft-deleted row keeps the distinct
// soft-delete refusal rather than resurrecting the row.
func TestUpsertNormalizedPKKeepsSoftDelete(t *testing.T) {
	ch, db := pagesUpsertWorld(t, lowerPKHook, func(c *entity.EntityConfig) {
		c.Scope.SoftDelete = true
	})
	if _, err := db.Exec(`INSERT INTO pages (id, owner_id, title, body, deleted_at) VALUES ('gone','bob','Gone','bob old','2024-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	_, err := ch.UpsertOne(upsertSecurityContext("bob", ""),
		map[string]any{"id": "GONE", "title": "Gone", "body": "revived"})
	if !errors.Is(err, errSoftDeletedResurrection) {
		t.Fatalf("err = %v, want errSoftDeletedResurrection", err)
	}
	var b string
	if err := db.QueryRow("SELECT body FROM pages WHERE id = 'gone'").Scan(&b); err != nil || b != "bob old" {
		t.Fatalf("soft-deleted row mutated: body=%q err=%v", b, err)
	}
}

// UpsertOne must run the belongs_to write-side scope check that CreateOne and
// UpdateOne run, on both the insert arm and the update arm.
func TestUpsertChecksBelongsToScope(t *testing.T) {
	ch := relwriteWorld(t)
	alice := upsertSecurityContext("alice", "")

	if _, err := ch.UpsertOne(alice, map[string]any{"id": "i2", "label": "x", "order_id": "o-bob"}); !errors.Is(err, errNotFound) {
		t.Fatalf("insert arm onto bob's order: err = %v, want not found", err)
	}
	var n int
	if err := ch.DB.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM items WHERE id = 'i2'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("i2 persisted (n=%d err=%v)", n, err)
	}

	if _, err := ch.UpsertOne(alice, map[string]any{"id": "i3", "label": "x", "order_id": "o-alice"}); err != nil {
		t.Fatalf("positive control on own order: %v", err)
	}
	if _, err := ch.UpsertOne(alice, map[string]any{"id": "i3", "label": "y", "order_id": "o-bob"}); !errors.Is(err, errNotFound) {
		t.Fatalf("update arm onto bob's order: err = %v, want not found", err)
	}
	var oid string
	if err := ch.DB.QueryRowContext(context.Background(), "SELECT order_id FROM items WHERE id = 'i3'").Scan(&oid); err != nil || oid != "o-alice" {
		t.Fatalf("i3 order_id = %q (err=%v), want o-alice", oid, err)
	}
}
