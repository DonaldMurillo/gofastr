package query

import (
	"database/sql"
	"strings"
)

// IsPostgres reports whether db answers SELECT version() with a PostgreSQL banner; any error or other banner reports false.
func IsPostgres(db *sql.DB) bool {
	var v string
	if err := db.QueryRow("SELECT version()").Scan(&v); err == nil {
		return strings.Contains(strings.ToLower(v), "postgresql")
	}
	return false
}
