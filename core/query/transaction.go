package query

import (
	"context"
	"database/sql"
)

// Transaction runs fn in a transaction, committing when fn returns nil and rolling back otherwise.
func Transaction(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}
