package framework

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// A CRUD write that joins app.InTx and fails undoes its own writes, so a
// caller that handles the error and commits keeps only what succeeded.
// The joined path used to run with no savepoint: a create refused by its
// AfterCreate hook left its row for the outer commit, and on Postgres a
// failed statement aborted the whole transaction.
func TestFailedJoinedCreateLeavesNoRow(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db))
		app.Registry.Register(entity.Define("posts", entity.EntityConfig{
			Table:  "posts",
			Fields: []schema.Field{{Name: "slug", Type: schema.String, Unique: true, Required: true}},
		}.WithTimestamps(false)))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		refused := errors.New("refused")
		app.HookRegistry("posts").RegisterHook(hook.AfterCreate, func(_ context.Context, data any) error {
			if m, ok := data.(map[string]any); ok && m["slug"] == "bad" {
				return refused
			}
			return nil
		})
		ch := app.MustCrudHandler("posts")

		err := app.InTx(context.Background(), func(ctx context.Context, _ *sql.Tx) error {
			if _, err := ch.CreateOne(ctx, map[string]any{"slug": "ok"}); err != nil {
				return err
			}
			if _, err := ch.CreateOne(ctx, map[string]any{"slug": "bad"}); !errors.Is(err, refused) {
				t.Errorf("refused create: err = %v, want the hook's refusal", err)
			}
			if _, err := ch.CreateOne(ctx, map[string]any{"slug": "ok"}); err == nil {
				t.Error("duplicate slug: want a unique violation")
			}
			_, err := ch.CreateOne(ctx, map[string]any{"slug": "after"})
			return err
		})
		if err != nil {
			t.Fatalf("InTx: %v", err)
		}
		var slugs []string
		rows, err := db.Query("SELECT slug FROM posts ORDER BY slug")
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatalf("scan: %v", err)
			}
			slugs = append(slugs, s)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("rows: %v", err)
		}
		if len(slugs) != 2 || slugs[0] != "after" || slugs[1] != "ok" {
			t.Fatalf("committed slugs = %v, want [after ok]", slugs)
		}
	})
}
