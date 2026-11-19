package testdb

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

func TestOpenSQLiteIsOneConnectionWithForeignKeys(t *testing.T) {
	db := Open(t, migrate.DialectSQLite)
	var fk int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("PRAGMA foreign_keys = %d, %v; want 1", fk, err)
	}
	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("MaxOpenConnections = %d, want 1 so every query sees one :memory: database", got)
	}
}

func TestNewSchemaNameFitsUnderALongTestName(t *testing.T) {
	t.Run(strings.Repeat("long_subtest_name_", 6), func(t *testing.T) {
		if name := NewSchemaName(t); len(name) > 63 || !strings.Contains(name, fmt.Sprintf("_%d_", os.Getpid())) {
			t.Fatalf("NewSchemaName() = %q (%d bytes), want at most 63 bytes keeping the pid", name, len(name))
		}
	})
}

func TestResolvePostgresWithoutDSNIsAnError(t *testing.T) {
	if os.Getenv("TEST_POSTGRES_DSN") != "" {
		t.Skip("TEST_POSTGRES_DSN is set")
	}
	dsn, err := ResolvePostgresOnce()
	if dsn != "" || err != pgtest.ErrNoPostgres {
		t.Fatalf("ResolvePostgresOnce() = %q, %v; want empty dsn and the shared no-Postgres error", dsn, err)
	}
}

func TestOpenPostgresSkipsWithoutDSN(t *testing.T) {
	if os.Getenv("TEST_POSTGRES_DSN") != "" {
		t.Skip("TEST_POSTGRES_DSN is set")
	}
	skipped := false
	t.Run("pg", func(t *testing.T) {
		defer func() { skipped = t.Skipped() }()
		Open(t, migrate.DialectPostgres)
	})
	if !skipped {
		t.Fatal("Open(Postgres) without a server must skip, not fail")
	}
}
