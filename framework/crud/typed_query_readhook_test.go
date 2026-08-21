package crud

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// TypedQuery.Find/First used to scan, decode and return without ever calling
// runAfterList/runAfterGet, so crud.WithReadHooks was a no-op on a typed repo:
// a screen backed by one printed the stored value its own API masks. Find
// mirrors the list route (AfterList); First mirrors GET /{id} (AfterGet).

type maskedRow struct {
	ID     string `json:"id"`
	Secret string `json:"secret"`
}

// maskedRowHandler builds a one-row handler whose only hook is `chain`,
// masking `secret`.
func maskedRowHandler(t *testing.T, chain hook.HookType) *CrudHandler {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE masked (id TEXT PRIMARY KEY, secret TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO masked (id, secret) VALUES ('1', 'stored-secret')`); err != nil {
		t.Fatal(err)
	}

	ent := entity.Define("masked", entity.EntityConfig{
		Fields: []schema.Field{{Name: "secret", Type: schema.String}},
	}.WithTimestamps(false))
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(chain, func(_ context.Context, data any) error {
		switch p := data.(type) {
		case *hook.ListPayload:
			for _, row := range p.Results {
				row["secret"] = "***"
			}
		case *hook.GetPayload:
			p.Result["secret"] = "***"
		}
		return nil
	})
	return ch
}

func TestTypedFindRunsAfterList(t *testing.T) {
	ch := maskedRowHandler(t, hook.AfterList)

	rows, err := NewTypedQuery[maskedRow](ch).Find(WithReadHooks(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].Secret != "***" {
		t.Fatalf("WithReadHooks returned %q, want the masked value", rows[0].Secret)
	}
}

func TestTypedFirstRunsAfterGet(t *testing.T) {
	ch := maskedRowHandler(t, hook.AfterGet)

	row, err := NewTypedQuery[maskedRow](ch).First(WithReadHooks(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	if row.Secret != "***" {
		t.Fatalf("WithReadHooks returned %q, want the masked value", row.Secret)
	}
}

// Without the opt-in a typed read still hands back stored values, the
// read-modify-write round trip every typed repo depends on.
func TestTypedReadWithoutOptInIsRaw(t *testing.T) {
	ch := maskedRowHandler(t, hook.AfterList)

	rows, err := NewTypedQuery[maskedRow](ch).Find(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Secret != "stored-secret" {
		t.Fatalf("plain Find should return stored values, got %+v", rows)
	}

	getCh := maskedRowHandler(t, hook.AfterGet)
	row, err := NewTypedQuery[maskedRow](getCh).First(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if row.Secret != "stored-secret" {
		t.Fatalf("plain First should return stored values, got %+v", row)
	}
}

// failingHookHandler is maskedRowHandler with a hook that refuses. A read
// hook that errors must abort the read: returning rows the hook declined to
// approve is exactly the leak WithReadHooks exists to prevent.
func failingHookHandler(t *testing.T, chain hook.HookType) *CrudHandler {
	t.Helper()
	ch := maskedRowHandler(t, chain)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(chain, func(_ context.Context, _ any) error {
		return errors.New("hook refused")
	})
	return ch
}

func TestTypedFindPropagatesHookError(t *testing.T) {
	ch := failingHookHandler(t, hook.AfterList)
	if _, err := NewTypedQuery[maskedRow](ch).Find(WithReadHooks(context.Background())); err == nil {
		t.Fatal("Find returned rows despite AfterList refusing")
	}
}

func TestTypedFirstPropagatesHookError(t *testing.T) {
	ch := failingHookHandler(t, hook.AfterGet)
	if _, err := NewTypedQuery[maskedRow](ch).First(WithReadHooks(context.Background())); err == nil {
		t.Fatal("First returned a row despite AfterGet refusing")
	}
}

// First reports "not found" as sql.ErrNoRows so callers can use errors.Is.
func TestTypedFirstNoRows(t *testing.T) {
	ch := maskedRowHandler(t, hook.AfterGet)
	_, err := NewTypedQuery[maskedRow](ch).
		Where(entity.NewStringColumn("id").Eq("nope")).
		First(context.Background())
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("First on an empty result = %v, want sql.ErrNoRows", err)
	}
}
