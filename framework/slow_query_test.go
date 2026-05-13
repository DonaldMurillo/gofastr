package framework

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/slowquery"
)

// slowQueryHarness wires a slow-query logger around a real DB at a known
// threshold and exposes the captured slog output.
type slowQueryHarness struct {
	db      *sql.DB
	wrapper *slowquery.SlowQueryLogger
	logs    *bytes.Buffer
}

func newSlowQueryHarness(t *testing.T, db *sql.DB, threshold time.Duration) *slowQueryHarness {
	t.Helper()
	if _, err := db.Exec("CREATE TABLE rows (id TEXT PRIMARY KEY, n INTEGER)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return &slowQueryHarness{
		db:      db,
		wrapper: slowquery.NewSlowQueryLogger(db, threshold, logger),
		logs:    buf,
	}
}

// ============================================================================
// Below-threshold queries do not log
// ============================================================================

func TestSlowQuery_BelowThreshold_NotLogged(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		h := newSlowQueryHarness(t, db, 5*time.Second) // huge — never trips
		_, err := h.wrapper.ExecContext(context.Background(), "INSERT INTO rows(id, n) VALUES ($1, $2)", "r1", 1)
		if err != nil {
			t.Fatalf("exec: %v", err)
		}
		if h.wrapper.Hits() != 0 {
			t.Fatalf("expected 0 hits, got %d", h.wrapper.Hits())
		}
		if strings.Contains(h.logs.String(), "slow query") {
			t.Fatalf("did not expect slow-query log line, got:\n%s", h.logs.String())
		}
	})
}

// ============================================================================
// Above-threshold queries log a structured warning
// ============================================================================

func TestSlowQuery_AboveThreshold_Logged(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		h := newSlowQueryHarness(t, db, 1*time.Nanosecond) // anything trips
		if _, err := h.wrapper.ExecContext(context.Background(),
			"INSERT INTO rows(id, n) VALUES ($1, $2)", "r1", 1); err != nil {
			t.Fatalf("exec: %v", err)
		}
		if h.wrapper.Hits() == 0 {
			t.Fatalf("expected at least one hit, got %d", h.wrapper.Hits())
		}
		out := h.logs.String()
		if !strings.Contains(out, "slow query") {
			t.Fatalf("expected slow-query log line, got:\n%s", out)
		}
		if !strings.Contains(out, "kind=exec") {
			t.Fatalf("expected kind=exec attribute, got:\n%s", out)
		}
		if !strings.Contains(out, "sql=") {
			t.Fatalf("expected sql attribute, got:\n%s", out)
		}
	})
}

// ============================================================================
// Threshold of 0 disables logging entirely (zero-value behaviour)
// ============================================================================

func TestSlowQuery_ZeroThreshold_NoLogging(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		h := newSlowQueryHarness(t, db, 0)
		_, _ = h.wrapper.ExecContext(context.Background(), "INSERT INTO rows(id, n) VALUES ($1, $2)", "r1", 1)
		_ = h.wrapper.QueryRowContext(context.Background(), "SELECT n FROM rows WHERE id = $1", "r1")
		if h.wrapper.Hits() != 0 {
			t.Fatalf("expected zero hits with threshold=0, got %d", h.wrapper.Hits())
		}
	})
}

// ============================================================================
// The wrapper is a drop-in DBExecutor — feed it to a CrudHandler.
// List goes through the wrapper directly (no tx). Mutations open a tx via
// the wrapper's BeginTx, then run their queries on the raw *sql.Tx — those
// escape the slow-query wrapper today (documented limitation).
// ============================================================================

func TestSlowQuery_AsCrudHandlerDB(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		createPostsTestTable(t, db)
		// Seed one row directly so the List has something to scan.
		if _, err := db.Exec("INSERT INTO posts(id, title, body) VALUES ($1, $2, $3)", "p1", "hi", ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
				{Name: "body", Type: schema.Text},
			},
		}.WithTimestamps(false))

		buf := &bytes.Buffer{}
		logger := slog.New(slog.NewTextHandler(buf, nil))
		wrapped := slowquery.NewSlowQueryLogger(db, time.Nanosecond, logger)

		ent, _ := app.Registry.Get("posts")
		ch := crud.NewCrudHandler(ent, wrapped)
		ch.Registry = app.Registry

		_, err := ch.ListAll(context.Background(), crud.ListOptions{})
		if err != nil {
			t.Fatalf("ListAll: %v", err)
		}
		if wrapped.Hits() == 0 {
			t.Fatal("expected at least one slow-query hit via CrudHandler ListAll")
		}
	})
}

// ============================================================================
// slowquery.TrimSQL collapses whitespace + truncates at 240 chars.
// ============================================================================

func TestSlowQuery_TrimSQL(t *testing.T) {
	in := "SELECT  *\n\tFROM\nposts\nWHERE   id = $1"
	out := slowquery.TrimSQL(in)
	if strings.Contains(out, "\t") || strings.Contains(out, "\n") {
		t.Fatalf("expected whitespace collapsed: %q", out)
	}
	if !strings.Contains(out, "SELECT * FROM posts WHERE id = $1") {
		t.Fatalf("unexpected output: %q", out)
	}
	// Truncation
	long := strings.Repeat("x", 500)
	trimmed := slowquery.TrimSQL(long)
	if len(trimmed) > 250 {
		t.Fatalf("expected truncated <=250 bytes, got %d", len(trimmed))
	}
}
