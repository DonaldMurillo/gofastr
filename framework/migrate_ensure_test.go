package framework

import (
	"database/sql"
	"net/url"
	"strings"
	"testing"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
)

// TestEnsureDatabase_PostgresCreates exercises the create-DB-if-absent path
// against a real Postgres: it creates a fresh database, is idempotent on a
// second call, and the new database is connectable.
func TestEnsureDatabase_PostgresCreates(t *testing.T) {
	base, err := resolvePostgresOnce()
	if err != nil {
		t.Skipf("no postgres available: %v", err)
	}
	// Pin to IPv4 — Docker's port proxy on macOS resets the IPv6 (::1) handshake
	// that `localhost` resolves to first, which would flake this test.
	base = strings.Replace(base, "@localhost:", "@127.0.0.1:", 1)
	u, err := url.Parse(base)
	if err != nil {
		t.Skipf("base DSN is not URL form (%v) — skipping", err)
	}

	dbName := "gofastr_ensure_" + strings.ToLower(newSchemaName(t))
	quoted := `"` + dbName + `"` // dbName is [a-z0-9_], safe to quote inline

	adminU := *u
	adminU.Path = "/postgres"
	admin, err := sql.Open("postgres", adminU.String())
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	defer admin.Close()
	_, _ = admin.Exec("DROP DATABASE IF EXISTS " + quoted)
	defer admin.Exec("DROP DATABASE IF EXISTS " + quoted)

	targetU := *u
	targetU.Path = "/" + dbName
	target := targetU.String()

	created, err := coremig.EnsureDatabase("postgres", target)
	if err != nil {
		t.Fatalf("EnsureDatabase: %v", err)
	}
	if !created {
		t.Fatal("expected created=true for a fresh database")
	}

	// Idempotent.
	created2, err := coremig.EnsureDatabase("postgres", target)
	if err != nil {
		t.Fatalf("EnsureDatabase (2nd): %v", err)
	}
	if created2 {
		t.Fatal("expected created=false when the database already exists")
	}

	// The new database is connectable.
	ndb, err := sql.Open("postgres", target)
	if err != nil {
		t.Fatalf("open new db: %v", err)
	}
	if err := ndb.Ping(); err != nil {
		ndb.Close()
		t.Fatalf("ping new db: %v", err)
	}
	ndb.Close() // close before the deferred DROP DATABASE
}
