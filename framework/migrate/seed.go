package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
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
// to a discard handler — operators opt in.
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
	// seedLedgerTable is a compile-time constant valid identifier — MustIdent
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
	// Future dialects (MySQL, MSSQL) need their own branch — split here
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
// wait, then short-circuit on the ledger. SQLite serializes at the file
// level so the lock is a no-op there. A crashed lock holder's
// session-level lock is released automatically by Postgres — no
// permanent block.
//
// Exception — MaxOpenConns(1): the advisory lock pins a connection, so a
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
	dialect := DetectDialect(db)

	hasSeed := false
	for _, ent := range registry.All() {
		if ent.Config.Seed != nil {
			hasSeed = true
			break
		}
	}
	if !hasSeed {
		return nil
	}

	// Serialize the seed phase across replicas behind a Postgres advisory
	// lock DISTINCT from the migration lock. Seed funcs receive the pool db
	// (their signature requires *sql.DB), so the lock pins its own conn and
	// the body runs against the pool — correct on Postgres where every conn
	// shares the database. A MaxOpenConns(1) pool (e.g. test harness, or a
	// deployment that deliberately serializes all DB access on one conn)
	// would deadlock: the pinned lock conn IS the only conn, so the body's
	// pool queries block forever. Skip the lock in that case — a
	// single-conn pool already serializes this process's access, and the
	// lock only coordinates ACROSS processes. SQLite is single-process and
	// file-serialized, so the lock is meaningless there too; run unwrapped.
	poolSize := db.Stats().MaxOpenConnections
	if dialect == DialectPostgres && poolSize != 1 {
		return coremig.WithAdvisoryLockKey(ctx, db, dialect, coremig.SeedAdvisoryLockKey, func(_ *sql.Conn) error {
			return runSeedsBody(ctx, db, dialect, registry)
		})
	}
	if dialect == DialectPostgres && poolSize == 1 {
		// Cannot take the advisory lock on a 1-conn pool: WithAdvisoryLock
		// pins a connection, leaving none for the seed body's pool queries →
		// deadlock. We run UNLOCKED here, so seeds are NOT coordinated across
		// replicas in this configuration — N replicas each with a 1-conn pool
		// can each observe "not seeded" and race a Seed (the ledger's ON
		// CONFLICT DO NOTHING dedupes the row, not the Seed execution). Warn
		// loudly on the default logger (the ctx seed logger defaults to
		// Discard, and this gap must always surface) rather than silently
		// weaken the single-run guarantee.
		slog.Default().Warn("seed advisory lock skipped: Postgres pool has MaxOpenConns(1), so startup seeds are NOT serialized across replicas — raise MaxOpenConns above 1 to enable cross-replica seed coordination")
	}
	return runSeedsBody(ctx, db, dialect, registry)
}

// runSeedsBody is the ensure-ledger → read-ledger → run → record loop,
// extracted so RunSeeds can wrap it in the advisory lock on Postgres and
// call it directly on SQLite. db is the pool; seed funcs receive it as-is.
func runSeedsBody(ctx context.Context, db *sql.DB, dialect Dialect, registry entity.Registry) error {
	logger := seedLoggerFromCtx(ctx)
	if err := ensureSeedLedger(ctx, db, dialect); err != nil {
		return fmt.Errorf("seed: ensure ledger: %w", err)
	}

	seeded, err := readSeededSet(ctx, db)
	if err != nil {
		return fmt.Errorf("seed: ledger read: %w", err)
	}
	logger.Debug("seed ledger read", "already_seeded", len(seeded))

	ordered, err := topoSortEntities(registry.All())
	if err != nil {
		return err
	}

	for _, ent := range ordered {
		// Honour context cancellation between seeds as well as during
		// a Seed call — keeps the loop responsive even when a previous
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
