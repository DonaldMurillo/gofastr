package migrate

import "testing"

func TestEnsureDatabase_SQLiteNoop(t *testing.T) {
	created, err := EnsureDatabase("sqlite3", "file:/tmp/whatever.db")
	if err != nil {
		t.Fatalf("EnsureDatabase(sqlite): %v", err)
	}
	if created {
		t.Fatal("SQLite EnsureDatabase should be a no-op (file is created on open)")
	}
}

func TestPostgresAdminDSN_URLForm(t *testing.T) {
	name, admin, err := postgresAdminDSN("postgres://user:pass@localhost:5432/myapp?sslmode=disable")
	if err != nil {
		t.Fatalf("postgresAdminDSN: %v", err)
	}
	if name != "myapp" {
		t.Errorf("dbName = %q, want myapp", name)
	}
	if admin != "postgres://user:pass@localhost:5432/postgres?sslmode=disable" {
		t.Errorf("adminDSN = %q", admin)
	}
}

func TestPostgresAdminDSN_KeywordForm(t *testing.T) {
	name, admin, err := postgresAdminDSN("host=localhost user=u password=p dbname=myapp sslmode=disable")
	if err != nil {
		t.Fatalf("postgresAdminDSN: %v", err)
	}
	if name != "myapp" {
		t.Errorf("dbName = %q, want myapp", name)
	}
	if want := "host=localhost user=u password=p dbname=postgres sslmode=disable"; admin != want {
		t.Errorf("adminDSN = %q, want %q", admin, want)
	}
}

func TestPostgresAdminDSN_NoDatabaseName(t *testing.T) {
	if _, _, err := postgresAdminDSN("postgres://user@localhost:5432/"); err == nil {
		t.Error("expected an error for a URL DSN with no database name")
	}
	if _, _, err := postgresAdminDSN("host=localhost user=u"); err == nil {
		t.Error("expected an error for a keyword DSN with no dbname=")
	}
}

func TestIsPostgresTarget(t *testing.T) {
	cases := []struct {
		driver, dsn string
		want        bool
	}{
		{"postgres", "anything", true},
		{"pgx", "anything", true},
		{"sqlite3", "postgres://x/y", true}, // scheme wins
		{"sqlite3", "file:app.db", false},
		{"", "postgresql://x/y", true},
	}
	for _, c := range cases {
		if got := isPostgresTarget(c.driver, c.dsn); got != c.want {
			t.Errorf("isPostgresTarget(%q,%q) = %v, want %v", c.driver, c.dsn, got, c.want)
		}
	}
}
