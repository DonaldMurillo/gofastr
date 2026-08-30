package crud

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// Strict top-level JSON parsing on the _batch envelope surfaces.
//
// Property: every request body decoded into a struct pointer rejects
// duplicate, case-folded, and unknown top-level keys, so intermediaries
// that parse the first occurrence (proxies, WAFs, audit loggers) cannot
// disagree with the executor's last-key-wins decode. The reference
// enforcement is core/handler/bind.go::validateBodyKeys for Bind
// consumers; the three _batch envelopes decode through
// crud_upload.go::decodeJSONBody (plain json.Decode) and are the one
// request-decoded struct set outside that contract.
//
// Surfaces: BatchCreate / BatchUpdate / BatchDelete envelope decode.
// readRequestBody's map destination is correctly a no-op for this
// property and must stay one.

func batchEnvelopeHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	return setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "title", Type: schema.String, Required: true},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)
}

func batchEnvelopeRequest(t *testing.T, method, body string) *http.Request {
	t.Helper()
	req := withTestUser(httptest.NewRequest(method, "/posts/_batch", strings.NewReader(body)), "u1")
	req.Header.Set("Content-Type", "application/json")
	return req
}

func countPosts(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM posts").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func postTitle(t *testing.T, db *sql.DB, id string) string {
	t.Helper()
	var title string
	if err := db.QueryRow("SELECT title FROM posts WHERE id = ?", id).Scan(&title); err != nil {
		t.Fatal(err)
	}
	return title
}

// TestBatchEnvelopeRejectsDuplicateKeys: a duplicated top-level key must
// 400 on every _batch surface, and the smuggled occurrence must not be
// executed. Stdlib json takes the LAST occurrence silently, so an
// intermediary validating the first sees a benign batch while the
// executor runs the attacker's.
func TestBatchEnvelopeRejectsDuplicateKeys(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		ch, db := batchEnvelopeHandler(t)
		req := batchEnvelopeRequest(t, http.MethodPost,
			`{"items":[{"title":"benign"}],"items":[{"title":"evil-dup"}]}`)
		rec := httptest.NewRecorder()
		ch.BatchCreate()(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("SECURITY: [strict-json] POST _batch with duplicated top-level key %q returned %d, want 400. Attack: last-key-wins smuggles the second items array past first-occurrence validators. Body: %s", "items", rec.Code, rec.Body.String())
		}
		if n := countPosts(t, db); n != 0 {
			t.Fatalf("SECURITY: [strict-json] duplicated-key batch persisted %d rows, want 0", n)
		}
	})

	t.Run("update", func(t *testing.T) {
		ch, db := batchEnvelopeHandler(t)
		seedRows(t, db, "posts", []map[string]any{{"id": "p1", "title": "original"}})
		req := batchEnvelopeRequest(t, http.MethodPatch,
			`{"items":[{"id":"p1","title":"benign"}],"items":[{"id":"p1","title":"evil-dup"}]}`)
		rec := httptest.NewRecorder()
		ch.BatchUpdate()(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("SECURITY: [strict-json] PATCH _batch with duplicated top-level key %q returned %d, want 400. Body: %s", "items", rec.Code, rec.Body.String())
		}
		if got := postTitle(t, db, "p1"); got != "original" {
			t.Fatalf("SECURITY: [strict-json] duplicated-key update applied smuggled title %q, want %q", got, "original")
		}
	})

	t.Run("delete", func(t *testing.T) {
		ch, db := batchEnvelopeHandler(t)
		seedRows(t, db, "posts", []map[string]any{
			{"id": "p1", "title": "keep1"},
			{"id": "p2", "title": "keep2"},
		})
		req := batchEnvelopeRequest(t, http.MethodDelete,
			`{"ids":["p1"],"ids":["p2"]}`)
		rec := httptest.NewRecorder()
		ch.BatchDelete()(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("SECURITY: [strict-json] DELETE _batch with duplicated top-level key %q returned %d, want 400. Body: %s", "ids", rec.Code, rec.Body.String())
		}
		if n := countPosts(t, db); n != 2 {
			t.Fatalf("SECURITY: [strict-json] duplicated-key delete removed rows: %d remain, want 2", n)
		}
	})
}

// TestBatchEnvelopeRejectsCaseFoldedKey: a case-folded key ("Items",
// "IDs") decodes onto the same struct field via stdlib json's
// case-insensitive fallback and must 400, mirroring validateBodyKeys'
// exact-tag requirement.
func TestBatchEnvelopeRejectsCaseFoldedKey(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		ch, db := batchEnvelopeHandler(t)
		req := batchEnvelopeRequest(t, http.MethodPost,
			`{"Items":[{"title":"evil-fold"}]}`)
		rec := httptest.NewRecorder()
		ch.BatchCreate()(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("SECURITY: [strict-json] POST _batch with case-folded key %q returned %d, want 400. Body: %s", "Items", rec.Code, rec.Body.String())
		}
		if n := countPosts(t, db); n != 0 {
			t.Fatalf("SECURITY: [strict-json] case-folded batch persisted %d rows, want 0", n)
		}
	})

	t.Run("delete", func(t *testing.T) {
		ch, db := batchEnvelopeHandler(t)
		seedRows(t, db, "posts", []map[string]any{{"id": "p1", "title": "keep"}})
		req := batchEnvelopeRequest(t, http.MethodDelete,
			`{"IDs":["p1"]}`)
		rec := httptest.NewRecorder()
		ch.BatchDelete()(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("SECURITY: [strict-json] DELETE _batch with case-folded key %q returned %d, want 400. Body: %s", "IDs", rec.Code, rec.Body.String())
		}
		if n := countPosts(t, db); n != 1 {
			t.Fatalf("SECURITY: [strict-json] case-folded delete removed rows: %d remain, want 1", n)
		}
	})
}

// TestBatchEnvelopeRejectsUnknownKey: an unknown top-level key must 400,
// mirroring bindBody's DisallowUnknownFields + exact-tag check, so the
// envelope cannot carry extra fields only some parsers see.
func TestBatchEnvelopeRejectsUnknownKey(t *testing.T) {
	ch, db := batchEnvelopeHandler(t)
	req := batchEnvelopeRequest(t, http.MethodPost,
		`{"items":[{"title":"a"}],"smuggled":"x"}`)
	rec := httptest.NewRecorder()
	ch.BatchCreate()(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("SECURITY: [strict-json] POST _batch with unknown top-level key %q returned %d, want 400. Body: %s", "smuggled", rec.Code, rec.Body.String())
	}
	if n := countPosts(t, db); n != 0 {
		t.Fatalf("SECURITY: [strict-json] unknown-key batch persisted %d rows, want 0", n)
	}
}

// TestBatchEnvelopeAcceptsSingleKeyBody is the happy path: a clean
// single-key envelope is accepted on every surface, so strict-key
// enforcement cannot regress legitimate batches.
func TestBatchEnvelopeAcceptsSingleKeyBody(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		ch, db := batchEnvelopeHandler(t)
		req := batchEnvelopeRequest(t, http.MethodPost,
			`{"items":[{"title":"ok"}]}`)
		rec := httptest.NewRecorder()
		ch.BatchCreate()(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("clean create envelope returned %d, want 200. Body: %s", rec.Code, rec.Body.String())
		}
		if n := countPosts(t, db); n != 1 {
			t.Fatalf("clean create envelope persisted %d rows, want 1", n)
		}
	})

	t.Run("update", func(t *testing.T) {
		ch, db := batchEnvelopeHandler(t)
		seedRows(t, db, "posts", []map[string]any{{"id": "p1", "title": "original"}})
		req := batchEnvelopeRequest(t, http.MethodPatch,
			`{"items":[{"id":"p1","title":"updated"}]}`)
		rec := httptest.NewRecorder()
		ch.BatchUpdate()(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("clean update envelope returned %d, want 200. Body: %s", rec.Code, rec.Body.String())
		}
		if got := postTitle(t, db, "p1"); got != "updated" {
			t.Fatalf("clean update envelope left title %q, want %q", got, "updated")
		}
	})

	t.Run("delete", func(t *testing.T) {
		ch, db := batchEnvelopeHandler(t)
		seedRows(t, db, "posts", []map[string]any{{"id": "p1", "title": "gone"}})
		req := batchEnvelopeRequest(t, http.MethodDelete,
			`{"ids":["p1"]}`)
		rec := httptest.NewRecorder()
		ch.BatchDelete()(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("clean delete envelope returned %d, want 200. Body: %s", rec.Code, rec.Body.String())
		}
		if n := countPosts(t, db); n != 0 {
			t.Fatalf("clean delete envelope left %d rows, want 0", n)
		}
	})
}
