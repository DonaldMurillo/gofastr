package framework

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/internal/renderdiag"
	"github.com/DonaldMurillo/gofastr/internal/retired"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
	_ "github.com/lib/pq"
)

// openBenchDB mirrors openTestDB but for *testing.B. SQLite is in-memory with
// foreign keys on; Postgres reuses the process-wide container and creates a
// fresh per-benchmark schema. Postgres benchmarks are skipped (not failed)
// when no PG is reachable.
func openBenchDB(b *testing.B, dialect Dialect) *sql.DB {
	b.Helper()
	switch dialect {
	case DialectSQLite:
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			b.Fatalf("open sqlite: %v", err)
		}
		// In-memory SQLite without shared cache gives each connection its own
		// private database. Cap to a single connection so concurrent
		// benchmarks see one consistent DB (and serialise on writes the way
		// SQLite does in practice anyway).
		db.SetMaxOpenConns(1)
		if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
			b.Fatalf("pragma fk: %v", err)
		}
		b.Cleanup(func() { db.Close() })
		return db
	case DialectPostgres:
		base, err := resolvePostgresOnce()
		if err != nil {
			b.Skipf("Postgres unavailable: %v", err)
		}
		db, err := sql.Open("postgres", base)
		if err != nil {
			b.Fatalf("open pg: %v", err)
		}
		db.SetMaxOpenConns(1)
		if err := waitPGReady(db); err != nil {
			b.Fatalf("ping pg: %v", err)
		}
		schemaName := benchSchemaName(b)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
			b.Fatalf("create schema %s: %v", schemaName, err)
		}
		if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
			b.Fatalf("set search_path: %v", err)
		}
		b.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			db.ExecContext(ctx, "DROP SCHEMA "+schemaName+" CASCADE")
			db.Close()
		})
		return db
	}
	b.Fatalf("unknown dialect: %s", dialect)
	return nil
}

// forEachBenchDialect mirrors forEachDialect: runs fn as a sub-benchmark per
// dialect. Postgres is skipped when unavailable, or when BENCH_SKIP_PG=1 is
// set (so `make bench-sqlite` can deliberately exclude Postgres even when
// it would otherwise be reachable).
func forEachBenchDialect(b *testing.B, fn func(b *testing.B, db *sql.DB, dialect Dialect)) {
	b.Helper()
	skipPG := os.Getenv("BENCH_SKIP_PG") == "1"
	for _, dialect := range Dialects {
		d := dialect
		b.Run(string(d), func(b *testing.B) {
			if d == DialectPostgres && skipPG {
				b.Skip("BENCH_SKIP_PG=1")
			}
			db := openBenchDB(b, d)
			fn(b, db, d)
		})
	}
}

var benchSchemaCounter atomic.Uint64

func benchSchemaName(b *testing.B) string {
	id := benchSchemaCounter.Add(1)
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '_'
		}
	}, b.Name())
	if len(clean) > 40 {
		clean = clean[:40]
	}
	return fmt.Sprintf("b_%s_%d", clean, id)
}

// benchRequest builds a request that takes the production path. A Go test
// binary arms two test-only wrappers on every request (testing.Testing()):
// the retired-markup tee on the App router and the render-500 wrapper in
// uihost. Without the exemptions a benchmark times dev instrumentation that
// production never runs.
func benchRequest(b *testing.B, method, target string, body io.Reader) *http.Request {
	return httptest.NewRequestWithContext(benchContexts(b).prod, method, target, body)
}

// benchAuthed is benchRequest with an authenticated user in its context, the
// way production auth middleware hands a request to the CRUD layer.
// Generated CRUD requires a session by default, and the bench entities set
// no OwnerField, Access or Config.Public, so a bare request 401s before the
// handler body runs and the bench measures the rejection instead of the path
// it names.
func benchAuthed(b *testing.B, method, target string, body io.Reader) *http.Request {
	return httptest.NewRequestWithContext(benchContexts(b).authed, method, target, body)
}

const benchUser = "bench-user"

type benchCtx struct{ prod, authed context.Context }

// benchCtxs holds each benchmark's contexts. They are built once per
// benchmark, not per request: each exemption registers a b.Cleanup, and a
// request context built in the loop would allocate on the timed path that
// the net/http baselines don't.
var benchCtxs sync.Map // *testing.B → benchCtx

func benchContexts(b *testing.B) benchCtx {
	if c, ok := benchCtxs.Load(b); ok {
		return c.(benchCtx)
	}
	prod := renderdiag.ProductionContext(b, retired.ProductionContext(b, context.Background()))
	c := benchCtx{prod: prod, authed: handler.SetUser(prod, benchUser)}
	if prev, loaded := benchCtxs.LoadOrStore(b, c); loaded {
		return prev.(benchCtx)
	}
	b.Cleanup(func() { benchCtxs.Delete(b) })
	return c
}
