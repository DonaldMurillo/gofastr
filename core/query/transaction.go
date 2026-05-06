package query

import (
	"context"
	"database/sql"
)

// Transaction executes fn inside a database transaction.
// It begins a transaction, calls fn, and commits on success or rolls back on error.
func Transaction(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}
