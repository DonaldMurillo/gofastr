package framework

import (
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
)

// Thin shims keeping the original package-private helper names. The real
// implementations now live in framework/internal/testdb so framework_test
// (the external test package) can share them without an import cycle.

var Dialects = testdb.Dialects

func openTestDB(t *testing.T, dialect Dialect) *sql.DB {
	return testdb.Open(t, dialect)
}

func forEachDialect(t *testing.T, fn func(t *testing.T, db *sql.DB, dialect Dialect)) {
	testdb.ForEachDialect(t, fn)
}

func newSchemaName(t *testing.T) string { return testdb.NewSchemaName(t) }

func resolvePostgresOnce() (string, error) { return testdb.ResolvePostgresOnce() }

func waitPGReady(db *sql.DB) error { return testdb.WaitPGReady(db) }

func redactDSN(dsn string) string { return testdb.RedactDSN(dsn) }
