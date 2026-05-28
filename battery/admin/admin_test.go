package admin

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/battery/queue"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// ----- helpers --------------------------------------------------------------

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newDBQueue(t *testing.T, db *sql.DB) *queue.DBQueue {
	t.Helper()
	q, err := queue.NewDBQueue(db)
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	return q
}

func newAuditTable(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`CREATE TABLE audit_log (
		id          TEXT PRIMARY KEY,
		entity      TEXT NOT NULL,
		op          TEXT NOT NULL,
		record_id   TEXT NOT NULL,
		actor_id    TEXT,
		created_at  DATETIME NOT NULL,
		diff        TEXT
	)`)
	if err != nil {
		t.Fatalf("audit table: %v", err)
	}
}

// mountAdmin returns a router with the admin pages mounted AND a
// request-time middleware that injects a stand-in admin user, so the
// page-render tests don't all 401 on the new requireUser gate. Tests
// that need the unauthenticated path use mountAdminBare instead.
func mountAdmin(t *testing.T, cfg Config) http.Handler {
	t.Helper()
	bare := mountAdminBare(t, cfg)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(handler.SetUser(r.Context(), struct{}{}))
		bare.ServeHTTP(w, r)
	})
}

// mountAdminBare mounts the admin pages without injecting a user — use
// for the auth-bypass security tests which assert that anonymous
// callers get 401.
func mountAdminBare(t *testing.T, cfg Config) http.Handler {
	t.Helper()
	b := New(cfg)
	r := router.New()
	b.RegisterRoutes(r)
	return r
}

// ----- index ---------------------------------------------------------------

func TestAdmin_IndexLandingShowsSections(t *testing.T) {
	h := mountAdmin(t, Config{})
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Overview") {
		t.Fatalf("expected Overview heading; got %q", body[:min(200, len(body))])
	}
	if !strings.Contains(body, "Queue") || !strings.Contains(body, "Audit log") {
		t.Fatalf("expected both section names")
	}
}

func TestAdmin_IndexWithoutQueueShowsStub(t *testing.T) {
	h := mountAdmin(t, Config{})
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if !strings.Contains(rr.Body.String(), "No queue wired") {
		t.Fatalf("expected queue-stub message")
	}
	if !strings.Contains(rr.Body.String(), "No audit log wired") {
		t.Fatalf("expected audit-stub message")
	}
}

// ----- queue page ----------------------------------------------------------

func TestAdmin_QueuePageListsJobs(t *testing.T) {
	db := newDB(t)
	q := newDBQueue(t, db)

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := q.Enqueue(ctx, queue.Job{Type: "send.email", ScheduledAt: time.Now()}); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}

	h := mountAdmin(t, Config{Queue: q})
	req := httptest.NewRequest(http.MethodGet, "/admin/queue", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "send.email") {
		t.Fatalf("expected job type in body; got %q", body[:min(400, len(body))])
	}
	// Filter chips should appear with the per-status counts.
	if !strings.Contains(body, "pending (3)") {
		t.Fatalf("expected pending filter count, got %q", body[strings.Index(body, "filters"):])
	}
}

func TestAdmin_QueuePageWithoutWiringShowsStub(t *testing.T) {
	h := mountAdmin(t, Config{})
	req := httptest.NewRequest(http.MethodGet, "/admin/queue", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if !strings.Contains(rr.Body.String(), "No queue is wired") {
		t.Fatalf("expected stub message")
	}
}

// ----- audit page ----------------------------------------------------------

func TestAdmin_AuditPageRendersRows(t *testing.T) {
	db := newDB(t)
	newAuditTable(t, db)

	_, err := db.Exec(`INSERT INTO audit_log (id, entity, op, record_id, actor_id, created_at, diff)
		VALUES ('a1', 'users', 'create', 'u-42', 'admin', ?, '{"name":"alice"}')`,
		time.Now().UTC())
	if err != nil {
		t.Fatalf("seed audit row: %v", err)
	}

	h := mountAdmin(t, Config{DB: db})
	req := httptest.NewRequest(http.MethodGet, "/admin/audit", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "users") || !strings.Contains(body, "u-42") {
		t.Fatalf("expected audit row contents; got %q", body[:min(400, len(body))])
	}
}

func TestAdmin_AuditPageNoTableShowsStub(t *testing.T) {
	h := mountAdmin(t, Config{})
	req := httptest.NewRequest(http.MethodGet, "/admin/audit", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if !strings.Contains(rr.Body.String(), "No DB / audit table") {
		t.Fatalf("expected stub: %q", rr.Body.String())
	}
}

// ----- response headers ----------------------------------------------------

func TestAdmin_ResponseHeadersAreCacheSafe(t *testing.T) {
	h := mountAdmin(t, Config{})
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("admin pages must Cache-Control: no-store; got %q", rr.Header().Get("Cache-Control"))
	}
	if !strings.HasPrefix(rr.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("admin pages must be HTML; got %q", rr.Header().Get("Content-Type"))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
