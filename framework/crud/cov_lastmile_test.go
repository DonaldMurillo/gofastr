package crud

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// =====================================================================
// Group A — query / scan error propagation via the fault driver.
// Reuses covFaultRelWorld (posts/users/comments/tags on a fault DB) and
// covFaultNotes from the existing fault-test files.
// =====================================================================

// crud.go:453 — List data scan (rows.Next) error.
func TestList_ScanErr(t *testing.T) {
	ch, _ := covFaultNotes(t)
	covFault.set(func(c *covFaults) { c.nextErrOn = "title" })
	req := httptest.NewRequest("GET", "/notes", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("list scan err = %d, want 500", rec.Code)
	}
}

// crud.go:336 — List filter parse error → 400.
func TestList_IgnoresUnknownFilter(t *testing.T) {
	ch, _ := covFaultNotes(t)
	// An unknown filter param (unrecognised field/suffix) is leniently
	// ignored, not rejected — the list still succeeds.
	req := httptest.NewRequest("GET", "/notes?title_zz=x", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unknown filter = %d, want 200 (lenient)", rec.Code)
	}
}

// crud.go:517 — Get include parse error → 400.
func TestGet_BadInclude(t *testing.T) {
	ch, _, _ := covFaultRelWorld(t)
	req := httptest.NewRequest("GET", "/posts/p1?include=ghostrel", nil)
	req.SetPathValue("id", "p1")
	rec := httptest.NewRecorder()
	ch.Get()(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("get bad include = %d, want 400", rec.Code)
	}
}

// crud.go:531 — Get projection parse error → 400.
func TestGet_BadProjection(t *testing.T) {
	ch, _ := covFaultNotes(t)
	req := httptest.NewRequest("GET", "/notes/n1?fields=nope", nil)
	req.SetPathValue("id", "n1")
	rec := httptest.NewRecorder()
	ch.Get()(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("get bad projection = %d, want 400", rec.Code)
	}
}

// crud.go:559 + include.go:334/375 — Get with include where the child query
// fails → 500, exercising applyIncludeTree error propagation.
func TestGet_IncludeChildQueryErr(t *testing.T) {
	ch, _, _ := covFaultRelWorld(t)
	covFault.set(func(c *covFaults) { c.queryErrOn = `FROM "comments"` }) // only the comments query
	req := httptest.NewRequest("GET", "/posts/p1?include=comments", nil)
	req.SetPathValue("id", "p1")
	rec := httptest.NewRecorder()
	ch.Get()(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("get include child err = %d, want 500", rec.Code)
	}
}

// =====================================================================
// crud_api.go in-process query/scan + include errors.
// =====================================================================

// crud_api.go:145 — GetOne include query failure.
func TestGetOne_IncludeChildErr(t *testing.T) {
	ch, _, _ := covFaultRelWorld(t)
	covFault.set(func(c *covFaults) { c.queryErrOn = `FROM "comments"` })
	if _, err := ch.GetOne(context.Background(), "p1", []string{"comments"}); !errors.Is(err, errCovInjected) {
		t.Fatalf("GetOne include err = %v, want injected", err)
	}
}

// crud_api.go:142 — GetOne bad include name → buildIncludeNodesFromNames err.
func TestGetOne_BadInclude(t *testing.T) {
	ch, _, _ := covFaultRelWorld(t)
	if _, err := ch.GetOne(context.Background(), "p1", []string{"ghostrel"}); err == nil {
		t.Fatal("GetOne with unknown include should error")
	}
}

// crud_api.go:193 — ListAll scan error.
func TestListAll_ScanErr(t *testing.T) {
	ch, _ := covFaultNotes(t)
	covFault.set(func(c *covFaults) { c.nextErrOn = "title" })
	if _, err := ch.ListAll(context.Background(), ListOptions{}); !errors.Is(err, errCovInjected) {
		t.Fatalf("ListAll scan err = %v, want injected", err)
	}
}

// crud_api.go:199 — ListAll bad include name.
func TestListAll_BadInclude(t *testing.T) {
	ch, _, _ := covFaultRelWorld(t)
	if _, err := ch.ListAll(context.Background(), ListOptions{Includes: []string{"ghostrel"}}); err == nil {
		t.Fatal("ListAll with unknown include should error")
	}
}

// =====================================================================
// crud_cursor.go / crud_stream.go.
// =====================================================================

// crud_cursor.go:127 — cursor scan error.
func TestCursor_ScanErr(t *testing.T) {
	ch, _ := covFaultNotes(t)
	covFault.set(func(c *covFaults) { c.nextErrOn = "title" })
	req := httptest.NewRequest("GET", "/notes?cursor=", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("cursor scan err = %d, want 500", rec.Code)
	}
}

// crud_stream.go:96 — mid-stream scan error breaks the loop (no status change).
func TestStream_MidScanErr(t *testing.T) {
	ch, _ := covFaultNotes(t)
	covFault.set(func(c *covFaults) { c.nextErrOn = "title" })
	req := httptest.NewRequest("GET", "/notes?stream=true", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	// The count + data queries succeed; the scan fails mid-stream, so the
	// array is closed and the (already-200) envelope is returned.
	if !strings.Contains(rec.Body.String(), `"data":[`) {
		t.Fatalf("stream body missing envelope: %s", rec.Body.String())
	}
}

// =====================================================================
// typed_query.go.
// =====================================================================

// typed_query.go:100 — Find scan error.
func TestTypedFind_ScanErr(t *testing.T) {
	ch, _ := covFaultNotes(t)
	covFault.set(func(c *covFaults) { c.nextErrOn = "title" })
	type note struct {
		ID string `json:"id"`
	}
	if _, err := NewTypedQuery[note](ch).Find(context.Background()); !errors.Is(err, errCovInjected) {
		t.Fatalf("typed Find scan err = %v, want injected", err)
	}
}

// typed_query.go:106 — Find bad include name (via First, which calls Find).
func TestTypedFirst_BadInclude(t *testing.T) {
	ch, _, _ := covFaultRelWorld(t)
	type post struct {
		ID string `json:"id"`
	}
	if _, err := NewTypedQuery[post](ch).Include("ghostrel").First(context.Background()); err == nil {
		t.Fatal("typed First with unknown include should error")
	}
}
