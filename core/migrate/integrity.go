package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// ErrDirty is returned by Up and Down when the tracking table records a
// migration left in a dirty state by a previously failed no-transaction
// migration. The database may be partially migrated; an operator must
// reconcile it by hand and then call Force to clear the state before
// migrations can proceed again.
var ErrDirty = errors.New("migrate: database is in a dirty state from a failed migration — reconcile manually and call Force")

// ChecksumMismatchError is returned by Up when an already-applied migration's
// recorded checksum no longer matches the registered migration's Up SQL. That
// means the migration file was edited after it was applied — a drift the
// runner refuses to paper over, because the live schema no longer matches what
// the (edited) file says it should be.
type ChecksumMismatchError struct {
	Version  uint64
	Name     string
	Recorded string
	Current  string
}

func (e *ChecksumMismatchError) Error() string {
	return fmt.Sprintf(
		"migrate: migration %d (%s) was modified after it was applied (recorded checksum %s, now %s) — applied migrations are immutable; revert the file or roll back and re-apply",
		e.Version, e.Name, short(e.Recorded), short(e.Current))
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// checksumOf returns the SHA-256 (hex) of a migration's Up SQL. It is the
// content fingerprint stored in the tracking table at apply time and compared
// on every subsequent run to detect post-apply edits.
func checksumOf(mig Migration) string {
	sum := sha256.Sum256([]byte(mig.Up))
	return hex.EncodeToString(sum[:])
}

// ensureTrackingColumns backfills the checksum and dirty columns onto a
// pre-existing _migrations table (created before those columns existed). New
// tables already include them via CreateMigrationsTable, so this is a no-op
// there. Idempotent on both engines:
//
//   - Postgres uses ADD COLUMN IF NOT EXISTS.
//   - SQLite has no IF NOT EXISTS for ADD COLUMN, so a duplicate-column error
//     is treated as success.
func (m *Migrator) ensureTrackingColumns(ctx context.Context, x connish, tbl string) error {
	adds := []struct{ col, def string }{
		{"checksum", "TEXT NOT NULL DEFAULT ''"},
		{"dirty", "BOOLEAN NOT NULL DEFAULT FALSE"},
	}
	for _, a := range adds {
		var stmt string
		if m.dialect == DialectPostgres {
			stmt = fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s", tbl, a.col, a.def)
		} else {
			stmt = fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tbl, a.col, a.def)
		}
		if _, err := x.ExecContext(ctx, stmt); err != nil {
			if m.dialect != DialectPostgres && isDuplicateColumn(err) {
				continue // column already present on this older SQLite table
			}
			return fmt.Errorf("migrate: backfill %s column: %w", a.col, err)
		}
	}
	return nil
}

// isDuplicateColumn reports whether err is SQLite's "duplicate column name"
// error from an ADD COLUMN that already exists.
func isDuplicateColumn(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate column")
}
