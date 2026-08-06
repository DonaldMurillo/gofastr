package framework

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// TestTypedHook_AfterCreateErrorPropagates is the AfterCreate mirror of the
// existing BeforeCreate rollback test. A typed AfterCreate hook returning an
// error must surface that error to the caller (the row is already committed,
// but the response must not read as success) — the crud layer wraps and
// returns it (crud_ops.go doCreate → ExecuteHooks(AfterCreate) → return).
// Without this, a typed AfterCreate hook that rejects (e.g. post-commit
// validation) would silently succeed from the caller's perspective.
func TestTypedHook_AfterCreateErrorPropagates(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app, ch := hookApp(t, db)
		ran := false
		OnAfterCreate(app, "posts", func(ctx context.Context, p *hookTestPost) error {
			ran = true
			return errors.New("typed after-create reject")
		})
		_, err := ch.CreateOne(context.Background(), map[string]any{"title": "x"})
		if !ran {
			t.Fatal("AfterCreate hook did not run")
		}
		if err == nil {
			t.Fatal("AfterCreate hook error did not propagate to CreateOne")
		}
	})
}

// TestTypedHook_AfterUpdateErrorPropagates is the AfterUpdate mirror: a typed
// AfterUpdate hook returning an error surfaces to UpdateOne, so a post-update
// rejection is observable rather than swallowed.
func TestTypedHook_AfterUpdateErrorPropagates(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app, ch := hookApp(t, db)
		created, err := ch.CreateOne(context.Background(), map[string]any{"title": "orig"})
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		id := created["id"].(string)

		ran := false
		OnAfterUpdate(app, "posts", func(ctx context.Context, p *hookTestPost) error {
			ran = true
			return errors.New("typed after-update reject")
		})
		_, err = ch.UpdateOne(context.Background(), id, map[string]any{"title": "new"})
		if !ran {
			t.Fatal("AfterUpdate hook did not run")
		}
		if err == nil {
			t.Fatal("AfterUpdate hook error did not propagate to UpdateOne")
		}
	})
}
