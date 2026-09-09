package query

import (
	"database/sql"
	"strings"
)

// IsPostgres reports whether the database behind db answers SELECT version()
// with a PostgreSQL banner. A driver that errors on the probe or answers with
// anything else (SQLite drivers either fail on version() or return a SQLite
// banner) reports false, so callers treat "not Postgres" as the
// SQLite-compatible path.
//
// It is the canonical form of the probe formerly duplicated as
// core/a2a's detectSQLDialect, framework/outbox's detectDialect, and
// battery/queue's detectDBDialect; those now map this bool onto their local
// dialect enums. It is deliberately a single best-effort shot: boot-critical
// callers that must fail closed on an unreachable database use
// framework/migrate's retrying probe (detectDialectFailClosed) instead.
func IsPostgres(db *sql.DB) bool {
	var v string
	if err := db.QueryRow("SELECT version()").Scan(&v); err == nil {
		return strings.Contains(strings.ToLower(v), "postgresql")
	}
	return false
}
