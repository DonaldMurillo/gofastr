// Package iolib is the cross-package half: a function whose doesIO fact
// travels to its callers.
package iolib

import (
	"context"
	"database/sql"
)

// Purge deletes expired rows.
func Purge(ctx context.Context, db *sql.DB) error { // want Purge:`doesIO\(\(\*sql\.DB\)\.ExecContext, net=false\)`
	_, err := db.ExecContext(ctx, `DELETE FROM t WHERE expires_at < now()`)
	return err
}

// Pure does no I/O.
func Pure(n int) int { return n + 1 }
