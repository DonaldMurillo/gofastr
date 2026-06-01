package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// ensurePingAttempts/ensurePingDelay bound the cold-start retry when EnsureDatabase
// connects to the maintenance database. A database that has just started (e.g. a
// freshly-launched container) can reset the first connection; a few short
// retries ride that out without hanging if the server is genuinely unreachable.
var (
	ensurePingAttempts = 10
	ensurePingDelay    = 250 * time.Millisecond
)

// sqlOpen is a seam so tests can substitute the admin connection.
var sqlOpen = sql.Open

// EnsureDatabase creates the target database if it does not already exist,
// returning whether it had to create it. This is the "first migration can
// create the DB" capability — call it before running migrations.
//
//   - SQLite (and any file/embedded driver): a no-op. The database file is
//     created automatically when the runner opens it, so there is nothing to
//     do; returns (false, nil).
//   - Postgres: connects to the maintenance `postgres` database (you cannot
//     CREATE a database while connected to the one being created), checks
//     pg_database, and issues CREATE DATABASE when absent. Requires the driver
//     to be linked into the binary and the role to have CREATEDB.
//
// dsn is the normal target DSN (URL form `postgres://…/dbname` or keyword form
// `host=… dbname=…`); EnsureDatabase derives the admin DSN from it.
func EnsureDatabase(driver, dsn string) (bool, error) {
	if !isPostgresTarget(driver, dsn) {
		return false, nil
	}
	dbName, adminDSN, err := postgresAdminDSN(dsn)
	if err != nil {
		return false, fmt.Errorf("ensure database: %w", err)
	}
	safeName, err := query.SafeIdent(dbName)
	if err != nil {
		return false, fmt.Errorf("ensure database: invalid database name %q: %w", dbName, err)
	}

	admin, err := sqlOpen(driver, adminDSN)
	if err != nil {
		return false, fmt.Errorf("ensure database: open admin connection: %w", err)
	}
	defer admin.Close()

	return ensureDatabaseOn(context.Background(), admin, dbName, safeName)
}

// ensureDatabaseOn is the connection-agnostic core of EnsureDatabase: probe
// pg_database on an already-open admin connection and CREATE DATABASE when
// absent. Separated so it can be exercised without a live Postgres.
func ensureDatabaseOn(ctx context.Context, admin *sql.DB, dbName, safeName string) (bool, error) {
	// Tolerate a still-starting server — the first connection can reset.
	if err := pingWithRetry(ctx, admin); err != nil {
		return false, fmt.Errorf("ensure database: connect to maintenance db: %w", err)
	}

	var exists bool
	if err := admin.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbName).Scan(&exists); err != nil {
		return false, fmt.Errorf("ensure database: probe pg_database: %w", err)
	}
	if exists {
		return false, nil
	}
	// CREATE DATABASE cannot be parameterized; the name is validated by caller.
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+query.QuoteIdent(safeName)); err != nil {
		return false, fmt.Errorf("ensure database: create %q: %w", dbName, err)
	}
	return true, nil
}

// pingWithRetry pings db up to ensurePingAttempts times, riding out the
// connection resets a just-started database emits before it's ready.
func pingWithRetry(ctx context.Context, db *sql.DB) error {
	var err error
	for i := 0; i < ensurePingAttempts; i++ {
		if err = db.PingContext(ctx); err == nil {
			return nil
		}
		time.Sleep(ensurePingDelay)
	}
	return err
}

func isPostgresTarget(driver, dsn string) bool {
	switch driver {
	case "postgres", "pgx":
		return true
	}
	return strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://")
}

// postgresAdminDSN returns the target database name and a DSN pointed at the
// maintenance `postgres` database, for both URL and keyword DSN forms.
func postgresAdminDSN(dsn string) (dbName, adminDSN string, err error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, perr := url.Parse(dsn)
		if perr != nil {
			return "", "", perr
		}
		dbName = strings.TrimPrefix(u.Path, "/")
		if dbName == "" {
			return "", "", fmt.Errorf("no database name in DSN")
		}
		u.Path = "/postgres"
		return dbName, u.String(), nil
	}
	// Keyword form: host=… dbname=… user=…
	fields := strings.Fields(dsn)
	out := make([]string, 0, len(fields))
	found := false
	for _, f := range fields {
		if strings.HasPrefix(f, "dbname=") {
			dbName = strings.TrimPrefix(f, "dbname=")
			out = append(out, "dbname=postgres")
			found = true
		} else {
			out = append(out, f)
		}
	}
	if !found || dbName == "" {
		return "", "", fmt.Errorf("no dbname= in DSN")
	}
	return dbName, strings.Join(out, " "), nil
}
