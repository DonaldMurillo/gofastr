package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
)

var (
	ensurePingAttempts = 10
	ensurePingDelay    = 250 * time.Millisecond
)

var sqlOpen = sql.Open

// EnsureDatabase creates the Postgres database dsn names when it is
// missing and reports whether it did; for any other target it does nothing.
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

func ensureDatabaseOn(ctx context.Context, admin *sql.DB, dbName, safeName string) (bool, error) {
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
	//gofastr:allow(quoteident) the DSN connects to its database name with the case kept
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+query.QuoteIdent(safeName)); err != nil {
		var state interface{ SQLState() string }
		if errors.As(err, &state) && state.SQLState() == "42P04" {
			return false, nil
		}
		if errors.As(err, &state) && state.SQLState() == "23505" {
			var nowExists bool
			if qerr := admin.QueryRowContext(ctx,
				"SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbName).Scan(&nowExists); qerr == nil && nowExists {
				return false, nil
			}
		}
		return false, fmt.Errorf("ensure database: create %q: %w", dbName, err)
	}
	return true, nil
}

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
	fields := strings.Fields(dsn)
	out := make([]string, 0, len(fields))
	found := false
	for _, f := range fields {
		if after, ok := strings.CutPrefix(f, "dbname="); ok {
			dbName = after
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
