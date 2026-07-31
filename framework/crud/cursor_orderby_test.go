package crud

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// sqlCaptureExecutor wraps a db.Executor and records every QueryContext
// SQL string so a cursor test can assert on the generated statement
// text. QueryRowContext/ExecContext delegate unchanged. It deliberately
// omits BeginTx: the cursor read path runs no transaction, so a
// read-only executor is sufficient and keeps the wrapper tiny.
type sqlCaptureExecutor struct {
	inner   db.Executor
	queries []string
}

func (e *sqlCaptureExecutor) QueryContext(ctx context.Context, q string, a ...any) (*sql.Rows, error) {
	e.queries = append(e.queries, q)
	return e.inner.QueryContext(ctx, q, a...)
}

func (e *sqlCaptureExecutor) QueryRowContext(ctx context.Context, q string, a ...any) *sql.Row {
	return e.inner.QueryRowContext(ctx, q, a...)
}

func (e *sqlCaptureExecutor) ExecContext(ctx context.Context, q string, a ...any) (sql.Result, error) {
	return e.inner.ExecContext(ctx, q, a...)
}

// dataQueryWithOrderBy returns the first captured query that carries an
// ORDER BY clause — the cursor data query. Cursor mode emits no count
// query, so this is unambiguous when no includes are requested.
func (e *sqlCaptureExecutor) dataQueryWithOrderBy(t *testing.T) string {
	t.Helper()
	for _, q := range e.queries {
		if strings.Contains(strings.ToUpper(q), "ORDER BY") {
			return q
		}
	}
	t.Fatalf("no ORDER BY query captured; saw: %v", e.queries)
	return ""
}

// TestCursorSingleFieldNoDupOrderBy pins that a single-field cursor page
// emits exactly ONE ORDER BY clause for the cursor field. The data query
// used to read `ORDER BY f ASC, f ASC` because qb.Cursor (core/query)
// appends its own ORDER BY AND serveCursorList appends it again. The dup
// is fixed on the crud side (core/query is out of scope): the handler
// adds only the WHERE comparison and lets its single ORDER BY loop own
// ordering. Behaviourally ASC,ASC == ASC, so this is a SQL-text contract
// the capture executor observes.
func TestCursorSingleFieldNoDupOrderBy(t *testing.T) {
	ch, _ := covItems(t, func(c *entity.EntityConfig) {
		if c.Pagination == nil {
			c.Pagination = &entity.PaginationConfig{}
		}
		c.Pagination.CursorField = "seq"
	}, 3)

	// Page 1 (empty cursor) to obtain a real next-cursor token.
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/items?cursor=&limit=2", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	var p1 struct {
		Cursor string `json:"cursor"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &p1)
	if p1.Cursor == "" {
		t.Fatalf("page 1 returned no cursor: %s", rec.Body.String())
	}

	// Capture the SQL for page 2 (non-empty cursor → WHERE branch runs).
	cap := &sqlCaptureExecutor{inner: ch.DB}
	ch.DB = cap
	req2 := withTestUser(httptest.NewRequest(http.MethodGet, "/items?cursor="+p1.Cursor+"&limit=2", nil), "u1")
	rec2 := httptest.NewRecorder()
	ch.List()(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("page 2 status=%d body=%s", rec2.Code, rec2.Body.String())
	}

	dataSQL := cap.dataQueryWithOrderBy(t)
	upper := strings.ToUpper(dataSQL)
	if c := strings.Count(upper, "ORDER BY"); c != 1 {
		t.Errorf("expected exactly 1 ORDER BY clause, got %d: %q", c, dataSQL)
	}
	// The single cursor field must appear exactly once as an ORDER BY key.
	if c := strings.Count(upper, "SEQ ASC"); c != 1 {
		t.Errorf("expected ORDER BY seq ASC exactly once, got %d: %q", c, dataSQL)
	}
}

// TestCursorMultiFieldOrderByUntouched guards the composite-cursor path
// against the single-field fix: a multi-field cursor must still emit one
// ORDER BY key per declared field, in declared order, each exactly once.
func TestCursorMultiFieldOrderByUntouched(t *testing.T) {
	ch, _ := covItems(t, func(c *entity.EntityConfig) {
		if c.Pagination == nil {
			c.Pagination = &entity.PaginationConfig{}
		}
		c.Pagination.CursorFields = []string{"seq"}
		// PrimaryKey ("id") is auto-appended by cursorFields as the tiebreak.
	}, 3)

	// Empty cursor → first page still goes through the ORDER BY loop.
	cap := &sqlCaptureExecutor{inner: ch.DB}
	ch.DB = cap
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/items?cursor=&limit=2", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	dataSQL := cap.dataQueryWithOrderBy(t)
	upper := strings.ToUpper(dataSQL)
	if c := strings.Count(upper, "ORDER BY"); c != 1 {
		t.Errorf("expected exactly 1 ORDER BY clause, got %d: %q", c, dataSQL)
	}
	// seq then id tiebreak, each once.
	if c := strings.Count(upper, "SEQ ASC"); c != 1 {
		t.Errorf("expected ORDER BY seq ASC once, got %d: %q", c, dataSQL)
	}
	if c := strings.Count(upper, "ID ASC"); c != 1 {
		t.Errorf("expected ORDER BY id ASC once, got %d: %q", c, dataSQL)
	}
}
