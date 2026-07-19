package framework

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// TestRunSeedHooksSerialized_PostgresBranches covers both Postgres branches of
// App.WithSeed serialization: a MaxOpenConns(1) pool skips the advisory lock
// (with a WARN) and runs the hook unlocked; a multi-connection pool takes the
// lock and runs it. Skips without a Postgres/Docker test backend.
func TestRunSeedHooksSerialized_PostgresBranches(t *testing.T) {
	t.Run("pool1_warns_and_runs", func(t *testing.T) {
		db := pgtest.DB(t) // pgtest.DB sets MaxOpenConns(1) → skip-lock branch
		ran := false
		a := NewApp(WithDB(db))
		a.WithSeed(func(context.Context) error { ran = true; return nil })
		if err := a.runSeedHooksSerialized(); err != nil {
			t.Fatalf("serialized (pool=1): %v", err)
		}
		if !ran {
			t.Fatal("seed hook did not run on a MaxOpenConns(1) Postgres pool")
		}
	})

	t.Run("multiconn_locks_and_runs", func(t *testing.T) {
		db := pgtest.DB(t)
		db.SetMaxOpenConns(4) // multi-conn → take the advisory lock
		ran := false
		a := NewApp(WithDB(db))
		a.WithSeed(func(context.Context) error { ran = true; return nil })
		if err := a.runSeedHooksSerialized(); err != nil {
			t.Fatalf("serialized (multi-conn): %v", err)
		}
		if !ran {
			t.Fatal("seed hook did not run on a multi-connection Postgres pool")
		}
	})
}
