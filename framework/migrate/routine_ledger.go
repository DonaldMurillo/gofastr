package migrate

import (
	"context"
	"fmt"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/query"
)

// routineLedgerTable is the bookkeeping table that records every routine the
// framework has applied: one row per Routine.Name, holding the sha256 of the
// Up body at last apply. It is REPORTING-ONLY: AutoMigrate runs every
// matching routine's Up on every boot regardless of the ledger (the Up bodies
// are idempotent — CREATE OR REPLACE / DROP IF EXISTS + CREATE — so the
// ledger does not gate application, it just records what landed and when).
//
// The contract this table supports, alongside App.Start's additive-only rule:
//
//   - A routine that disappears from the registered set leaves a row behind.
//     AutoMigrate WARNs (loud, named) and DOES NOT drop the row or the DB
//     object — additive only. Drop via Routine.Down in a versioned migration,
//     or remove the ledger row explicitly.
//   - A changed Up body updates the row's checksum so introspection
//     (app_routines) can surface "this routine drifted since last boot".
const routineLedgerTable = "gofastr_routines"

// ensureRoutineLedger creates gofastr_routines when missing. Idempotent —
// called every boot inside the advisory-locked tx. Mirrors the shape of
// ensureSeedLedger and core/migrate's _migrations table.
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
	)`, query.QuoteIdent(safe), now)
	_, err := exec.ExecContext(ctx, ddl)
	return err
}

// readRoutineLedger returns every (name, checksum) row currently in the
// ledger. Called inside the migrate tx after ensureRoutineLedger so the
// snapshot reflects this boot's view of bookkeeping.
func readRoutineLedger(ctx context.Context, exec execQueryer) (map[string]string, error) {
	safe := query.MustIdent(routineLedgerTable)
	q := fmt.Sprintf("SELECT name, checksum FROM %s", query.QuoteIdent(safe))
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

// upsertRoutineLedger writes (name, checksum) for one routine, updating the
// checksum and applied_at on conflict. Dialect-aware placeholder + UPDATE —
// both SQLite ≥3.24 and Postgres accept the ON CONFLICT (col) DO UPDATE form.
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
		query.QuoteIdent(safe), placeholderName, placeholderChecksum, now,
	)
	_, err := exec.ExecContext(ctx, q, name, checksum)
	return err
}
