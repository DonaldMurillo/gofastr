package desktop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func TestDataDirRejectsBadID(t *testing.T) {
	if _, err := DataDir("nodot"); err == nil {
		t.Fatal("bad id accepted")
	}
	if _, err := DataDir(""); err == nil {
		t.Fatal("empty id accepted")
	}
}

func TestAppOptionsDataDirOverride(t *testing.T) {
	base := t.TempDir()
	b := New(Config{ID: "x.example.app", DataDir: base})
	if b.dataDir != base {
		t.Fatalf("dataDir = %q", b.dataDir)
	}
}

func TestDataDirEnvOverridesBase(t *testing.T) {
	base := t.TempDir()
	t.Setenv("GOFASTR_DESKTOP_DATA_DIR", base)

	dir, err := DataDir("env.example.app")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "env.example.app"); dir != want {
		t.Fatalf("DataDir = %q, want %q", dir, want)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("data dir not created: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("data dir mode = %o, want 0700", perm)
	}
}

func TestDataDirEnvMustBeAbsolute(t *testing.T) {
	t.Setenv("GOFASTR_DESKTOP_DATA_DIR", "relative/dir")
	if _, err := DataDir("rel.example.app"); err == nil || !strings.Contains(err.Error(), "GOFASTR_DESKTOP_DATA_DIR") {
		t.Fatalf("relative env override accepted: %v", err)
	}
}

func TestOpenAppDBCreatesFile(t *testing.T) {
	dir := t.TempDir()
	db, err := openAppDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	// The file exists and the DB answers queries.
	var one int
	if err := db.QueryRow("SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Fatalf("SELECT 1 = (%d, %v)", one, err)
	}
	// It accepts the grants DDL (SQLite dialect path).
	s := newSQLGrantStore(db)
	if err := s.ensureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='desktop_grants'`).Scan(&name); err != nil || name != "desktop_grants" {
		t.Fatalf("table = (%q, %v)", name, err)
	}
}
