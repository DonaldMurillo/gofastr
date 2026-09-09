package query

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func TestParseDBTimeAcceptsDriverShapes(t *testing.T) {
	want := time.Date(2026, 7, 20, 23, 59, 59, 0, time.UTC)
	cases := []struct {
		name string
		src  any
	}{
		{"time.Time", want},
		{"*time.Time", &want},
		{"rfc3339", "2026-07-20T23:59:59Z"},
		{"rfc3339nano", "2026-07-20T23:59:59.000000000Z"},
		{"space offset", "2026-07-20 23:59:59+00:00"},
		{"space bare", "2026-07-20 23:59:59"},
		{"bytes", []byte("2026-07-20T23:59:59Z")},
	}
	for _, c := range cases {
		got, err := ParseDBTime(c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !got.Equal(want) {
			t.Fatalf("%s: got %v, want %v", c.name, got, want)
		}
	}
}

func TestParseDBTimeRejectsUnsupported(t *testing.T) {
	var nilTime *time.Time
	for _, src := range []any{nil, nilTime, 42, "not-a-time", []byte("still not")} {
		if _, err := ParseDBTime(src); err == nil {
			t.Errorf("ParseDBTime(%#v) accepted", src)
		}
	}
	if _, err := ParseDBTimeString("2026/07/20"); err == nil {
		t.Error("ParseDBTimeString accepted a slash date")
	}
}

func TestProbeSQLiteBindLayout(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	layout := ProbeSQLiteBindLayout(context.Background(), db)
	ref := time.Date(2001, 2, 3, 4, 5, 6, 789012345, time.UTC)
	var got string
	if err := db.QueryRow(`SELECT CAST($1 AS TEXT)`, ref).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if ref.Format(layout) != got {
		t.Fatalf("probed layout %q renders %q, driver bound %q", layout, ref.Format(layout), got)
	}
	// A database that cannot answer the probe falls back to RFC3339Nano.
	closed, _ := sql.Open("sqlite3", ":memory:")
	closed.Close()
	if got := ProbeSQLiteBindLayout(context.Background(), closed); got != time.RFC3339Nano {
		t.Fatalf("closed db: got %q, want RFC3339Nano", got)
	}
}

func TestIsPostgres(t *testing.T) {
	lite, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer lite.Close()
	if IsPostgres(lite) {
		t.Fatal("sqlite reported as Postgres")
	}
	pg, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	mock.ExpectQuery("SELECT version").WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow("PostgreSQL 16.2 on aarch64"))
	if !IsPostgres(pg) {
		t.Fatal("PostgreSQL banner not recognised")
	}
	mock.ExpectQuery("SELECT version").WillReturnError(sql.ErrConnDone)
	if IsPostgres(pg) {
		t.Fatal("a failed probe must report not-Postgres")
	}
}

func TestSafeTableName(t *testing.T) {
	for _, ok := range []string{"jobs", "_gofastr_queue", "Events2", "a"} {
		if !SafeTableName(ok) {
			t.Errorf("SafeTableName(%q) = false", ok)
		}
	}
	long := make([]byte, 65)
	for i := range long {
		long[i] = 'a'
	}
	for _, bad := range []string{"", "1tbl", "public.jobs", "jobs;", "jo bs", "select", "USERS", "Sessions", string(long)} {
		if SafeTableName(bad) {
			t.Errorf("SafeTableName(%q) = true", bad)
		}
	}
	if !ReservedIdent("DROP") || ReservedIdent("dropped") {
		t.Error("ReservedIdent must match whole words case-insensitively")
	}
}
