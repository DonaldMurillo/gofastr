package crud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBatchCreate_AnonymousIsRejected pins the security gate on the
// batch-create handler. Without it, an anonymous attacker hitting
// /api/logs/_batch can flood the DB with rows owned by no one
// (extractor returns nothing → InjectOwner stamps nothing → NULL
// user_id), or — worse — exploit a hook that synthesises an owner
// elsewhere. Either way, anonymous writes on an OwnerField entity
// must 401 before the JSON decode.
func TestBatchCreate_AnonymousIsRejected(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupOwnerScopedHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/logs/_batch",
		strings.NewReader(`{"items":[{"notes":"a"},{"notes":"b"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.BatchCreate()(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous BatchCreate status = %d (want 401). body=%s", rec.Code, rec.Body.String())
	}
}

func TestBatchUpdate_AnonymousIsRejected(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-a1", "alice", "alice original")

	req := httptest.NewRequest(http.MethodPost, "/api/logs/_batch_update",
		strings.NewReader(`{"items":[{"id":"log-a1","notes":"hijacked"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.BatchUpdate()(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous BatchUpdate status = %d (want 401). body=%s", rec.Code, rec.Body.String())
	}
	// Confirm the row stayed unchanged.
	var notes string
	if err := db.QueryRow(`SELECT notes FROM logs WHERE id=?`, "log-a1").Scan(&notes); err != nil {
		t.Fatal(err)
	}
	if notes != "alice original" {
		t.Errorf("anonymous BatchUpdate mutated row: %q", notes)
	}
}

func TestBatchDelete_AnonymousIsRejected(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-a1", "alice", "alice's row")

	req := httptest.NewRequest(http.MethodPost, "/api/logs/_batch_delete",
		strings.NewReader(`{"ids":["log-a1"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.BatchDelete()(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous BatchDelete status = %d (want 401). body=%s", rec.Code, rec.Body.String())
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM logs WHERE id=?`, "log-a1").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("anonymous BatchDelete removed row")
	}
}
