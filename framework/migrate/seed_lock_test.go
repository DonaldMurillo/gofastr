package migrate_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"

	_ "github.com/mattn/go-sqlite3"
)

// seededEntityBuilder builds an entity whose Seed fn runs fn. Mirrors the
// internal seededEntity helper, reproduced here because the test is in the
// external migrate_test package.
func seededEntityBuilder(name string, seed func(context.Context, *sql.DB) error) *entity.Entity {
	e := &entity.Entity{
		Config: entity.EntityConfig{
			Name:   name,
			Table:  name,
			Fields: []schema.Field{{Name: "x", Type: schema.String}},
		},
	}
	e.PrimaryKey = "x"
	e.Config.Seed = seed
	return e
}

// pgLockDB returns a Postgres test DB with pool size 4. pgtest.DB defaults
// to MaxOpenConns(1) (for tests that run their body ON the pinned conn);
// RunSeeds runs its body on the pool, so it needs more. Each contender in
// WithAdvisoryLockKey's poll loop pins a conn for the whole call, so pool
// size must exceed the number of concurrent contenders (one conn each)
// plus one for the winner's body. 4 covers 3 contenders + 1 body with
// headroom.
func pgLockDB(t *testing.T) *sql.DB {
	t.Helper()
	db := pgtest.DB(t)
	db.SetMaxOpenConns(4)
	return db
}

// simpleRegistry is a minimal entity.Registry for RunSeeds.
type simpleRegistry map[string]*entity.Entity

func (r simpleRegistry) All() map[string]*entity.Entity { return r }
func (r simpleRegistry) AllSorted() []*entity.Entity {
	out := make([]*entity.Entity, 0, len(r))
	for _, e := range r {
		out = append(out, e)
	}
	return out
}
func (r simpleRegistry) Get(name string) (*entity.Entity, error) {
	if e, ok := r[name]; ok {
		return e, nil
	}
	return nil, errNotFound
}

type notFoundErr struct{}

func (notFoundErr) Error() string { return "entity not found" }

var errNotFound = notFoundErr{}

// TestRunSeeds_AdvisoryLockSerializesAcrossReplicas: two RunSeeds calls
// against the SAME Postgres — simulating two replicas booting at once —
// run the Seed body exactly ONCE (ledger + lock). The sentinel row the
// Seed inserts appears exactly once; both calls return nil.
func TestRunSeeds_AdvisoryLockSerializesAcrossReplicas(t *testing.T) {
	db := pgLockDB(t) // skips if PG is unreachable; pool=2 for lock+body
	ctx := context.Background()

	// Sentinel table the Seed writes into. Created here (not via AutoMigrate)
	// to keep the test focused on the seed lock, not the migration lock.
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS seed_lock_sentinel (id INT PRIMARY KEY)`); err != nil {
		t.Fatalf("create sentinel table: %v", err)
	}
	// Belt-and-braces: clear any row from a prior run inside this schema.
	if _, err := db.ExecContext(ctx, `DELETE FROM seed_lock_sentinel`); err != nil {
		t.Fatalf("clear sentinel table: %v", err)
	}
	// Same for the ledger — a stale ledger row would short-circuit the test
	// and mask the race we're trying to exercise.
	if _, err := db.ExecContext(ctx, `DELETE FROM _gofastr_seeded WHERE entity_name = 'seed_lock_sentinel'`); err != nil {
		// Tolerate the ledger not existing yet (first run in this schema).
		t.Logf("clear ledger (ok if absent): %v", err)
	}

	var seedCalls int64
	reg := simpleRegistry{
		"seed_lock_sentinel": seededEntityBuilder("seed_lock_sentinel", func(ctx context.Context, db *sql.DB) error {
			atomic.AddInt64(&seedCalls, 1)
			// INSERT ON CONFLICT keeps the Seed idempotent if both replicas
			// DO end up running it (defense in depth — the lock should
			// already prevent this, but a Seed that crashes mid-flight is
			// re-run on next boot).
			_, err := db.ExecContext(ctx,
				`INSERT INTO seed_lock_sentinel (id) VALUES (1) ON CONFLICT DO NOTHING`)
			return err
		}),
	}

	// Two replicas booting concurrently against the same DB.
	const replicas = 2
	var wg sync.WaitGroup
	errs := make([]error, replicas)
	wg.Add(replicas)
	startGate := make(chan struct{})
	for i := range replicas {
		go func(i int) {
			defer wg.Done()
			<-startGate // release both at once to maximize the race window
			errs[i] = migrate.RunSeeds(ctx, db, reg)
		}(i)
	}
	close(startGate)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("replica %d RunSeeds error: %v", i, err)
		}
	}

	// The Seed body ran at most once. With the lock it's exactly once; the
	// assertion documents "no double-run", which is the contract. (Either
	// replica may win — that's fine.)
	if got := atomic.LoadInt64(&seedCalls); got != 1 {
		t.Fatalf("Seed body executed %d times, want exactly 1 (ledger + advisory lock)", got)
	}

	// The sentinel row exists exactly once.
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM seed_lock_sentinel`).Scan(&n); err != nil {
		t.Fatalf("count sentinel: %v", err)
	}
	if n != 1 {
		t.Fatalf("sentinel row count = %d, want 1", n)
	}

	// And the ledger row was recorded, so a follow-up RunSeeds is a no-op.
	for range replicas {
		if err := migrate.RunSeeds(ctx, db, reg); err != nil {
			t.Fatalf("follow-up RunSeeds error: %v", err)
		}
	}
	if got := atomic.LoadInt64(&seedCalls); got != 1 {
		t.Fatalf("Seed body executed %d times after follow-up, want still 1 (ledger short-circuit)", got)
	}
}

// TestRunSeeds_NilDBIsNoOp: locking path is skipped for nil db, matching
// AutoMigrate. No panic, no error.
func TestRunSeeds_NilDBIsNoOp(t *testing.T) {
	if err := migrate.RunSeeds(context.Background(), nil, simpleRegistry{}); err != nil {
		t.Fatalf("RunSeeds(nil) returned err: %v", err)
	}
}

// TestRunSeeds_LockReleasedAfterError: when a Seed fails, the advisory
// lock must still be released (a follow-up RunSeeds with a working Seed
// must proceed, not block forever). Catches a regression where the unlock
// is skipped on the error path.
func TestRunSeeds_LockReleasedAfterError(t *testing.T) {
	db := pgLockDB(t)
	ctx := context.Background()

	// Clean slate for this entity.
	if _, err := db.ExecContext(ctx, `DELETE FROM _gofastr_seeded WHERE entity_name = 'sentinel_fail'`); err != nil {
		t.Logf("clear ledger (ok if absent): %v", err)
	}

	boom := errors.New("seed boom")
	reg := simpleRegistry{
		"sentinel_fail": seededEntityBuilder("sentinel_fail", func(context.Context, *sql.DB) error {
			return boom
		}),
	}

	// First call fails inside the lock.
	if err := migrate.RunSeeds(ctx, db, reg); err == nil {
		t.Fatal("expected first RunSeeds to fail")
	}

	// Second call with a no-op Seed must NOT block — the lock from the
	// failed call must have been released.
	done := make(chan error, 1)
	go func() {
		reg2 := simpleRegistry{
			"sentinel_fail": seededEntityBuilder("sentinel_fail", func(context.Context, *sql.DB) error {
				return nil
			}),
		}
		done <- migrate.RunSeeds(ctx, db, reg2)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("second RunSeeds after failure: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second RunSeeds blocked — advisory lock not released after error")
	}
}
