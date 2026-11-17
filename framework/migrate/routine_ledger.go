package migrate

import (
	"context"
	"fmt"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/query"
)

// routineLedgerTable records each applied routine's Up checksum. It is
// reporting-only: it never gates an Up, and a routine that leaves the plan
// keeps its row and its database object.
const routineLedgerTable = "gofastr_routines"

func ensureRoutineLedger(ctx context.Context, exec execQueryer, dialect Dialect) error {
	safe := query.MustIdent(routineLedgerTable)
	now := "CURRENT_TIMESTAMP"
	if dialect == coremig.DialectPostgres {
		now = "NOW()"
	}
	ddl := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		name       TEXT NOT NULL PRIMARY KEY,
		checksum   TEXT NOT NULL,
		applied_at TIMESTAMP NOT NULL DEFAULT %s
	)`, query.QuoteFolded(safe), now)
	_, err := exec.ExecContext(ctx, ddl)
	return err
}

func readRoutineLedger(ctx context.Context, exec execQueryer) (map[string]string, error) {
	safe := query.MustIdent(routineLedgerTable)
	q := fmt.Sprintf("SELECT name, checksum FROM %s", query.QuoteFolded(safe))
	rows, err := exec.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var name, checksum string
		if err := rows.Scan(&name, &checksum); err != nil {
			return nil, err
		}
		out[name] = checksum
	}
	return out, rows.Err()
}

func upsertRoutineLedger(ctx context.Context, exec execQueryer, dialect Dialect, name, checksum string) error {
	safe := query.MustIdent(routineLedgerTable)
	now := "CURRENT_TIMESTAMP"
	placeholderName, placeholderChecksum := "?", "?"
	if dialect == coremig.DialectPostgres {
		placeholderName, placeholderChecksum = "$1", "$2"
		now = "NOW()"
	}
	q := fmt.Sprintf(
		"INSERT INTO %s (name, checksum, applied_at) VALUES (%s, %s, %s) "+
			"ON CONFLICT (name) DO UPDATE SET checksum = EXCLUDED.checksum, applied_at = EXCLUDED.applied_at",
		query.QuoteFolded(safe), placeholderName, placeholderChecksum, now,
	)
	_, err := exec.ExecContext(ctx, q, name, checksum)
	return err
}
