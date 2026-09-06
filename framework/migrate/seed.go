package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// seedLedgerTable is the bookkeeping table that records which entities
// have had their Seed function run. One row per entity name; subsequent
// restarts short-circuit on presence.
const seedLedgerTable = "_gofastr_seeded"

type seedLoggerKey struct{}

// WithSeedLogger attaches a slog.Logger to ctx so RunSeeds emits per-seed
// lifecycle events under it. When no logger is attached, RunSeeds writes
// to a discard handler. Operators opt in.
func WithSeedLogger(ctx context.Context, logger *slog.Logger) context.Context {
	if logger == nil {
		return ctx
	}
	return context.WithValue(ctx, seedLoggerKey{}, logger)
}

func seedLoggerFromCtx(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(seedLoggerKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ensureSeedLedger creates the _gofastr_seeded tracking table when
// missing. Mirrors the shape of core/migrate's _migrations table.
func ensureSeedLedger(ctx context.Context, db *sql.DB, dialect Dialect) error {
	// seedLedgerTable is a compile-time constant valid identifier: MustIdent
	// (panic on invalid) over SafeIdent avoids an unreachable error branch.
	safe := query.MustIdent(seedLedgerTable)
	now := "CURRENT_TIMESTAMP"
	if dialect == coremig.DialectPostgres {
		now = "NOW()"
	}
	ddl := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		entity_name TEXT NOT NULL PRIMARY KEY,
		seeded_at TIMESTAMP NOT NULL DEFAULT %s
	)`, query.QuoteIdent(safe), now)
	_, err := db.ExecContext(ctx, ddl)
	return err
}

// readSeededSet returns the set of entity_name values already in the
// ledger, in a single round-trip. Avoids the N+1 SELECT-per-entity that
// dominated boot latency against managed-Postgres deployments.
func readSeededSet(ctx context.Context, db *sql.DB) (map[string]struct{}, error) {
	safe := query.MustIdent(seedLedgerTable)
	q := fmt.Sprintf("SELECT entity_name FROM %s", query.QuoteIdent(safe))
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := make(map[string]struct{})
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		set[name] = struct{}{}
	}
	return set, rows.Err()
}

// recordSeeded inserts the ledger row marking the entity as seeded.
// Uses dialect-aware conflict handling so a concurrent RunSeeds (e.g.
// two processes racing through startup) doesn't error on the
// duplicate-PK path: whichever process inserts second silently no-ops
// instead of failing App.Start.
func recordSeeded(ctx context.Context, db *sql.DB, dialect Dialect, name string) error {
	safe := query.MustIdent(seedLedgerTable)
	placeholder := "?"
	if dialect == coremig.DialectPostgres {
		placeholder = "$1"
	}
	// SQLite ≥3.24 and Postgres both accept ON CONFLICT … DO NOTHING.
	// Future dialects (MySQL, MSSQL) need their own branch. Split here
	// so the dialect mapping is the only thing that has to change.
	q := fmt.Sprintf(
		"INSERT INTO %s (entity_name) VALUES (%s) ON CONFLICT (entity_name) DO NOTHING",
		query.QuoteIdent(safe), placeholder,
	)
	_, err := db.ExecContext(ctx, q, name)
	return err
}

// RunSeeds runs each entity's Seed function exactly once, tracked in the
// _gofastr_seeded ledger. Subsequent restarts short-circuit when the
// entity already has a ledger row. Call after AutoMigrate.
//
// Multi-replica safety: the ensure-ledger → read-ledger → run → record
// sequence runs while holding [coremig.SeedAdvisoryLockKey] (a Postgres
// advisory lock DISTINCT from the migration lock). Combined with the
// ledger, this makes an entity's Seed run ONCE globally: whichever
// replica wins the lock runs the body and records the row; the others
// wait, then short-circuit on the ledger. SQLite has no advisory locks,
// so it gets a twin with the same shape: a leased lock row in
// _gofastr_seed_lock (atomic INSERT ... ON CONFLICT ... DO UPDATE WHERE
// the lease expired), held across read → run → record and renewed by a
// heartbeat, plus a process-level mutex — SQLite's file-level locking
// serializes individual statements, not the read → run → record
// sequence, so without the twin two replicas both pass the empty-ledger
// check and both run the body. A crashed lock holder's lease expires and
// the next boot re-runs the un-recorded Seed, the same posture as a
// crashed Postgres lock holder.
//
// Exception, MaxOpenConns(1): the advisory lock pins a connection, so a
// Postgres pool capped at ONE connection would deadlock the seed body's
// own queries. Such a pool SKIPS the lock (logging a WARN) and runs
// unlocked, so N single-connection replicas are NOT coordinated and can
// race a Seed. Keep the pool above 1 connection for cross-replica seed
// serialization (the default unlimited pool is fine).
//
// Contract:
//   - Seed implementations SHOULD be idempotent. The framework now
//     serializes startup seeds across replicas, so the legacy race is
//     closed; idempotency is still the right posture because a Seed
//     that crashed mid-flight is NOT rolled back, and the next boot
//     re-runs it (no ledger row was recorded).
//   - Seeds run serially in topological order. Independent seeds run
//     one at a time; batch parallel work inside a single Seed func
//     when needed.
//   - The supplied ctx propagates into each Seed call. Cancelling ctx
//     unblocks Seed implementations that respect it.
//   - db == nil is a silent no-op, matching AutoMigrate's behaviour.
//   - Attach a logger via [WithSeedLogger] to capture per-seed
//     start/done/skip lifecycle events.
func RunSeeds(ctx context.Context, db *sql.DB, registry entity.Registry) error {
	if db == nil {
		return nil
	}
	// Route through the version union, NOT Registry.All(). All() returns one
	// representative per name, so a Seed declared only on a non-representative
	// version is invisible. hasSeed stays false and the seed silently never
	// runs. The union propagates the sole seed (registration guarantees at
	// most one) into the merged entity regardless of which version is the
	// representative (F11).
	merged := UnionEntities(registry)

	hasSeed := false
	for _, ent := range merged {
		if ent.Config.Seed != nil {
			hasSeed = true
			break
		}
	}
	if !hasSeed {
		return nil
	}
	// Detect the dialect lazily, only once we know a Seed will run. The
	// dialect gates the cross-replica seed advisory lock, so a transiently-
	// unreachable database must fail closed here rather than be guessed (the
	// v0.62 migration invariant). Seedless registries, the common case,
	// skip the probe entirely.
	dialect, err := detectDialectFailClosed(db)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// Serialize the seed phase across replicas behind a Postgres advisory
	// lock DISTINCT from the migration lock. Seed funcs receive the pool db
	// (their signature requires *sql.DB), so the lock pins its own conn and
	// the body runs against the pool, correct on Postgres where every conn
	// shares the database. A MaxOpenConns(1) pool (e.g. test harness, or a
	// deployment that deliberately serializes all DB access on one conn)
	// would deadlock: the pinned lock conn IS the only conn, so the body's
	// pool queries block forever. Skip the lock in that case: a
	// single-conn pool already serializes this process's access, and the
	// lock only coordinates ACROSS processes.
	poolSize := db.Stats().MaxOpenConnections
	if dialect == DialectPostgres && poolSize != 1 {
		return coremig.WithAdvisoryLockKey(ctx, db, dialect, coremig.SeedAdvisoryLockKey, func(_ *sql.Conn) error {
			return runSeedsBody(ctx, db, dialect, merged)
		})
	}
	if dialect == DialectPostgres && poolSize == 1 {
		// Cannot take the advisory lock on a 1-conn pool: WithAdvisoryLock
		// pins a connection, leaving none for the seed body's pool queries →
		// deadlock. We run UNLOCKED here, so seeds are NOT coordinated across
		// replicas in this configuration: N replicas each with a 1-conn pool
		// can each observe "not seeded" and race a Seed (the ledger's ON
		// CONFLICT DO NOTHING dedupes the row, not the Seed execution). Warn
		// loudly on the default logger (the ctx seed logger defaults to
		// Discard, and this gap must always surface) rather than silently
		// weaken the single-run guarantee.
		slog.Default().Warn("seed advisory lock skipped: Postgres pool has MaxOpenConns(1), so startup seeds are NOT serialized across replicas: raise MaxOpenConns above 1 to enable cross-replica seed coordination")
	}
	// SQLite twin of the advisory lock, in two layers. The leased lock row
	// serializes ACROSS processes against a shared file. The process-level
	// mutex serializes within one process, where two concurrent RunSeeds
	// calls may not even land on connections that share a database (an
	// in-memory SQLite pool is per-connection), so no DB-level lock could
	// order them; it also keeps a second goroutine from re-checking the
	// ledger before the first has recorded.
	if dialect == DialectSQLite {
		sqliteSeedMu.Lock()
		defer sqliteSeedMu.Unlock()
		release, err := acquireSQLiteSeedLease(ctx, db)
		if err != nil {
			return fmt.Errorf("seed: acquire sqlite seed lock: %w", err)
		}
		defer release()
	}
	return runSeedsBody(ctx, db, dialect, merged)
}

// runSeedsBody is the ensure-ledger → read-ledger → run → record loop,
// extracted so RunSeeds can wrap it in the advisory lock on Postgres and
// call it directly on SQLite. db is the pool; seed funcs receive it as-is.
// `all` is the version-union map (one entity per name, with the sole Seed
// propagated) computed by RunSeeds via UnionEntities.
func runSeedsBody(ctx context.Context, db *sql.DB, dialect Dialect, all map[string]*entity.Entity) error {
	logger := seedLoggerFromCtx(ctx)
	if err := ensureSeedLedger(ctx, db, dialect); err != nil {
		return fmt.Errorf("seed: ensure ledger: %w", err)
	}

	seeded, err := readSeededSet(ctx, db)
	if err != nil {
		return fmt.Errorf("seed: ledger read: %w", err)
	}
	logger.Debug("seed ledger read", "already_seeded", len(seeded))

	ordered, err := topoSortEntities(all)
	if err != nil {
		return err
	}

	for _, ent := range ordered {
		// Honour context cancellation between seeds as well as during
		// a Seed call, keeping the loop responsive even when a previous
		// Seed completed but a SIGTERM landed mid-loop.
		if err := ctx.Err(); err != nil {
			return err
		}

		cfg := ent.Config
		name := ent.GetName()
		if cfg.Seed == nil {
			continue
		}
		if _, ok := seeded[name]; ok {
			logger.Debug("seed skip", "entity", name, "reason", "already_seeded")
			continue
		}

		start := time.Now()
		logger.Info("seed start", "entity", name)
		seedCtx := entity.WithSeedDataContext(ctx, cfg.SeedFS, cfg.SeedPath)
		if err := cfg.Seed(seedCtx, db); err != nil {
			logger.Error("seed failed", "entity", name, "err", err)
			return fmt.Errorf("seed %s: %w", name, err)
		}
		if err := recordSeeded(ctx, db, dialect, name); err != nil {
			return fmt.Errorf("seed %s: record ledger: %w", name, err)
		}
		logger.Info("seed done", "entity", name, "elapsed", time.Since(start))
	}
	return nil
}

// sqliteSeedMu serializes RunSeeds calls within one process on SQLite. See
// the SQLite arm of RunSeeds for why the leased lock row alone cannot order
// two goroutines whose pool connections may not even share a database.
var sqliteSeedMu sync.Mutex

// seedLease is how long a _gofastr_seed_lock row counts as held before
// another process may steal it. A holder renews at lease/3, so a live
// process never loses the lock; a crashed one blocks other boots for at
// most one lease.
const seedLease = 60 * time.Second

// acquireSQLiteSeedLease takes the cross-process seed lock: one row in
// _gofastr_seed_lock, acquired by a single atomic upsert whose DO UPDATE
// fires only when the previous lease has expired (SQLite's own clock via
// strftime('%s','now'), so processes disagreeing about wall time don't
// stretch or shrink the lease). While held, a heartbeat renews it; the
// returned release func deletes the row. Waiting respects ctx: cancel it
// to stop waiting for the current holder.
func acquireSQLiteSeedLease(ctx context.Context, db *sql.DB) (release func(), err error) {
	lockTable := query.QuoteIdent(query.MustIdent("_gofastr_seed_lock"))
	if _, err := db.ExecContext(ctx, fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (id INTEGER PRIMARY KEY CHECK (id = 1), expires_at INTEGER NOT NULL)", lockTable)); err != nil {
		return nil, err
	}
	acquire := fmt.Sprintf(`INSERT INTO %s (id, expires_at)
VALUES (1, CAST(strftime('%%s','now') AS INTEGER) + ?)
ON CONFLICT (id) DO UPDATE SET expires_at = excluded.expires_at
WHERE %s.expires_at <= CAST(strftime('%%s','now') AS INTEGER)`, lockTable, lockTable)
	leaseSeconds := int64(seedLease / time.Second)
	for {
		res, err := db.ExecContext(ctx, acquire, leaseSeconds)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			break
		}
		// Held by another process. Wait and retry; the holder either
		// releases (row deleted), its lease expires (steal succeeds), or
		// ctx is cancelled (fail closed, nothing seeded by us).
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	// Heartbeat: a Seed can run arbitrarily long, far past the lease, so
	// renew at lease/3 while the body is alive. If this process dies the
	// heartbeats stop and the lease expires, which is the crash-release
	// path — there is no SQLite session cleanup to do it for us.
	hbCtx, stopHB := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(seedLease / 3)
		defer ticker.Stop()
		renew := fmt.Sprintf(
			"UPDATE %s SET expires_at = CAST(strftime('%%s','now') AS INTEGER) + ? WHERE id = 1", lockTable)
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				// A failed renewal is survivable but not silent: if it keeps
				// failing the lease expires and another process may steal the
				// seed phase, so surface it.
				if _, err := db.ExecContext(hbCtx, renew, leaseSeconds); err != nil {
					slog.Warn("seed lock lease renewal failed; the lease expires if this keeps failing",
						"err", err)
				}
			}
		}
	}()
	return func() {
		stopHB() // stops the goroutine before the DELETE races a renewal
		<-done
		if _, err := db.ExecContext(context.WithoutCancel(ctx), fmt.Sprintf("DELETE FROM %s WHERE id = 1", lockTable)); err != nil {
			// The lock still opens: the lease expires on its own after
			// seedLease. Other boots wait that long instead of running
			// immediately, which deserves a log line, not silence.
			slog.Warn("seed lock release failed; the lease expires instead", "err", err)
		}
	}, nil
}
