package slowquery

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib" // registers the "sqlite3" driver
)

func TestSlowQueryRowLogsItsExecutionError(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	wrapped := NewSlowQueryLogger(db, time.Nanosecond, logger)
	if err := wrapped.QueryRowContext(t.Context(), "SELECT * FROM missing_table").Err(); err == nil {
		t.Fatal("query against missing table succeeded")
	}
	if got := logs.String(); !strings.Contains(got, `err=database_error`) || !strings.Contains(got, `driver_code=`) {
		t.Fatalf("slow query log omits execution error metadata: %s", got)
	}
}

// TestSlowQueryMetricsCollector emits slow_queries_total with the logger's
// running hit count. A real in-memory SQLite backs the wrapper so Hits is
// exercised end to end; the threshold is a nanosecond, so the single seeded
// query is guaranteed to cross it.
func TestSlowQueryMetricsCollector(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	// Guard the "sqlite unavailable in the env" case: if the pure-Go driver
	// can't serve a connection here, skip rather than fail the suite.
	if err := db.Ping(); err != nil {
		t.Skipf("sqlite unavailable: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE rows (id TEXT PRIMARY KEY)"); err != nil {
		t.Fatalf("create: %v", err)
	}

	wrapped := NewSlowQueryLogger(db, time.Nanosecond, nil) // nil logger -> slog.Default
	if _, err := wrapped.ExecContext(context.Background(),
		"INSERT INTO rows(id) VALUES ($1)", "r1"); err != nil {
		t.Fatalf("exec: %v", err)
	}

	hits := wrapped.Hits()
	if hits == 0 {
		t.Fatalf("expected at least one slow-query hit, got %d", hits)
	}

	var buf bytes.Buffer
	MetricsCollector(wrapped)(&buf)

	out := buf.String()
	for _, want := range []string{
		"# HELP slow_queries_total DB queries that exceeded the slow-query threshold.",
		"# TYPE slow_queries_total counter",
		fmt.Sprintf("slow_queries_total %d", hits),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics output missing %q\ngot:\n%s", want, out)
		}
	}
}

// TestSlowQueryMetricsCollector_NilLogger is defensive: a nil *SlowQueryLogger
// must not panic and should report zero hits rather than crash /metrics.
func TestSlowQueryMetricsCollector_NilLogger(t *testing.T) {
	var buf bytes.Buffer
	MetricsCollector(nil)(&buf)

	out := buf.String()
	for _, want := range []string{
		"# HELP slow_queries_total DB queries that exceeded the slow-query threshold.",
		"# TYPE slow_queries_total counter",
		"slow_queries_total 0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics output missing %q\ngot:\n%s", want, out)
		}
	}
}
