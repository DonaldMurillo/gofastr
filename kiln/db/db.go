// Package db owns the per-session ephemeral SQLite lifecycle for Kiln.
//
// Build mode is not production: every chat session gets a disposable
// database file under os.TempDir so destructive entity edits are safe.
// EphemeralSQLite returns the database and a cleanup that removes the
// file when the session ends. Freeze (Phase 8) emits versioned
// migrations that the user runs against their real database.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var (
	pathRegistryMu sync.RWMutex
	pathRegistry   = map[*sql.DB]string{}
)

// EphemeralSQLite opens a fresh SQLite database under a temp file. The
// caller must invoke cleanup when finished. cleanup is idempotent.
func EphemeralSQLite(prefix string) (*sql.DB, func(), error) {
	if prefix == "" {
		prefix = "kiln"
	}
	dir, err := os.MkdirTemp("", prefix+"-*")
	if err != nil {
		return nil, nil, fmt.Errorf("kiln/db: mkdir: %w", err)
	}
	path := filepath.Join(dir, "session.db")
	d, err := sql.Open("sqlite3", path)
	if err != nil {
		os.RemoveAll(dir)
		return nil, nil, fmt.Errorf("kiln/db: open: %w", err)
	}
	if err := d.Ping(); err != nil {
		d.Close()
		os.RemoveAll(dir)
		return nil, nil, fmt.Errorf("kiln/db: ping: %w", err)
	}

	pathRegistryMu.Lock()
	pathRegistry[d] = path
	pathRegistryMu.Unlock()

	once := sync.Once{}
	cleanup := func() {
		once.Do(func() {
			d.Close()
			os.RemoveAll(dir)
			pathRegistryMu.Lock()
			delete(pathRegistry, d)
			pathRegistryMu.Unlock()
		})
	}
	return d, cleanup, nil
}

// PathFor returns the temp-file path of an EphemeralSQLite-created db,
// or "" if the database was created elsewhere.
func PathFor(d *sql.DB) string {
	pathRegistryMu.RLock()
	defer pathRegistryMu.RUnlock()
	return pathRegistry[d]
}
