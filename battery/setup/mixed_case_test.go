package setup

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// AutoMigrate writes unquoted DDL, so Postgres stores a declared users
// table "Members" as members. The admin step's done check names the
// declared table and must reach the folded one.
func TestMixedCaseAdminStepDone(t *testing.T) {
	run := func(t *testing.T, db *sql.DB) {
		ctx := context.Background()
		members := entity.Define("Members", entity.EntityConfig{
			Table:  "Members",
			Fields: auth.UserEntityFields(),
		}.WithTimestamps(false))
		if err := migrate.MigrateEntity(db, members); err != nil {
			t.Fatalf("MigrateEntity: %v", err)
		}
		mgr := auth.New(auth.AuthConfig{
			JWTSecret: "test-secret",
			UserStore: auth.NewEntityUserStore(db, "Members"),
			DevMode:   true,
		})
		if err := mgr.Init(nil); err != nil {
			t.Fatalf("auth Init: %v", err)
		}
		step, done := AdminStep(mgr, db, "Members")
		if ok, err := done(ctx); err != nil || ok {
			t.Fatalf("done before the step = %v, %v; want false", ok, err)
		}
		if err := step.Run(ctx, map[string]string{
			"ADMIN_EMAIL":    "admin@mc.test",
			"ADMIN_PASSWORD": "securepass123",
		}); err != nil {
			t.Fatalf("AdminStep.Run: %v", err)
		}
		if ok, err := done(ctx); err != nil || !ok {
			t.Fatalf("done after the step = %v, %v; want true", ok, err)
		}
	}
	t.Run("sqlite", func(t *testing.T) { run(t, setupSQLite(t)) })
	t.Run("postgres", func(t *testing.T) { run(t, pgtest.DB(t)) })
}
