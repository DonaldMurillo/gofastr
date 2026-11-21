package auth

import (
	"database/sql"
	"log/slog"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

type storeDialect struct {
	resolved atomic.Pointer[migrate.Dialect]
}

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
