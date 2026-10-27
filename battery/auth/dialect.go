package auth

import (
	"database/sql"
	"log/slog"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// storeDialect resolves a store's SQL dialect once and keeps it. Each auth
// SQL store used to probe (SELECT version(), retried on a transient
// failure) at every decision: six round trips per user-store boot and one
// per OAuth unlink, and a probe that failed mid-request read as SQLite, so
// a Postgres store took the SQLite unlink path, the one without the row
// lock, for that call. The first successful probe is kept; a failed one
// answers SQLite for that call, as migrate.DetectDialect does, and is not
// kept, so a store built while the database was unreachable still finds
// Postgres once it answers. The zero value is ready to use.
type storeDialect struct {
	resolved atomic.Pointer[migrate.Dialect]
}

// of returns db's dialect, probing only until one probe succeeds.
func (c *storeDialect) of(db *sql.DB) migrate.Dialect {
	if d := c.resolved.Load(); d != nil {
		return *d
	}
	d, err := migrate.DetectDialectStrict(db)
	if err != nil {
		slog.Default().Warn("auth: dialect probe failed; assuming SQLite for this call", "err", err)
		return migrate.DialectSQLite
	}
	c.resolved.Store(&d)
	return d
}

func (s *EntityUserStore) sqlDialect() migrate.Dialect { return s.dialect.of(s.db) }
