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

const seedLedgerTable = "_gofastr_seeded"

type seedLoggerKey struct{}

// WithSeedLogger attaches a logger to ctx for RunSeeds' per-seed
// lifecycle events; without one RunSeeds logs nothing. A nil logger
// returns ctx unchanged.
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

func ensureSeedLedger(ctx context.Context, db *sql.DB, dialect Dialect) error {
	safe := query.MustIdent(seedLedgerTable)
	ddl := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		entity_name TEXT NOT NULL PRIMARY KEY,
		seeded_at TIMESTAMP NOT NULL DEFAULT %s
	)`, query.QuoteFolded(safe), nowSQL(dialect))
	_, err := db.ExecContext(ctx, ddl)
	return err
}

func readSeededSet(ctx context.Context, db *sql.DB) (map[string]struct{}, error) {
	safe := query.MustIdent(seedLedgerTable)
	q := fmt.Sprintf("SELECT entity_name FROM %s", query.QuoteFolded(safe))
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

func recordSeeded(ctx context.Context, db *sql.DB, dialect Dialect, name string) error {
	safe := query.MustIdent(seedLedgerTable)
	q := fmt.Sprintf(
		"INSERT INTO %s (entity_name) VALUES (%s) ON CONFLICT (entity_name) DO NOTHING",
		query.QuoteFolded(safe), placeholder(dialect, 1),
	)
	_, err := db.ExecContext(ctx, q, name)
	return err
}

// RunSeeds runs each entity's Seed once, in topological order, recording
// each in the _gofastr_seeded ledger; call it after AutoMigrate. Replicas
// are serialized by a seed lock (a Postgres advisory lock, a leased row on
// SQLite), except a Postgres pool with MaxOpenConns(1), which runs
// unlocked and logs a warning. A Seed that fails is not rolled back and
// runs again on the next boot, so Seeds should be idempotent. ctx reaches
// each Seed; db == nil is a no-op.
func RunSeeds(ctx context.Context, db *sql.DB, registry entity.Registry) error {
	if db == nil {
		return nil
	}
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
	dialect, err := detectDialectFailClosed(db)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	poolSize := db.Stats().MaxOpenConnections
	if dialect == DialectPostgres && poolSize != 1 {
		return coremig.WithAdvisoryLockKey(ctx, db, dialect, coremig.SeedAdvisoryLockKey, func(_ *sql.Conn) error {
			return runSeedsBody(ctx, db, dialect, merged)
		})
	}
	if dialect == DialectPostgres && poolSize == 1 {
		slog.Default().Warn("seed advisory lock skipped: Postgres pool has MaxOpenConns(1), so startup seeds are NOT serialized across replicas: raise MaxOpenConns above 1 to enable cross-replica seed coordination")
	}
	if dialect == DialectSQLite {
		sqliteSeedMu.Lock()
		defer sqliteSeedMu.Unlock()
		release, err := coremig.AcquireSQLiteLease(ctx, db, "_gofastr_seed_lock", seedLease, "seed")
		if err != nil {
			return fmt.Errorf("seed: acquire sqlite seed lock: %w", err)
		}
		defer release()
	}
	return runSeedsBody(ctx, db, dialect, merged)
}

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
		recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), seedRecordTimeout)
		err := recordSeeded(recordCtx, db, dialect, name)
		cancel()
		if err != nil {
			return fmt.Errorf("seed %s: record ledger: %w", name, err)
		}
		logger.Info("seed done", "entity", name, "elapsed", time.Since(start))
	}
	return nil
}

const seedRecordTimeout = 10 * time.Second

var sqliteSeedMu sync.Mutex

const seedLease = 60 * time.Second
