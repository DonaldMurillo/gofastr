package crud

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// Property: a file persisted as part of a write request must not outlive that
// request's failed transaction — no orphaned storage object may remain when
// no row references it.
//
// Found red in the 2026-09-05 adversarial pass round 4 (F20, HIGH):
// crud_upload.go:parseMultipartBody saved every file part through
// file.ProcessFileField → Storage.Save BEFORE validation, hooks, or the
// INSERT ran, and nothing compensated on failure, so an authenticated caller
// with create permission could repeat a doomed write forever — each attempt
// left a real, unreferenced object in storage (also F19: unbounded disk-fill
// with no cap, no reaper). Verified shapes, each leaving the uploaded PNG on
// disk with zero rows:
//
//   - another field fails validation → 400 "validation failed", file remained;
//   - a BeforeCreate hook rejects the write → 400, file remained;
//   - a multipart Update targeting a nonexistent id → 404, file remained
//     (the body is parsed and saved before the row lookup).
//
// Fixed by the savedKeyLedger seam in crud_upload.go: the multipart parse
// wraps ch.Storage in a recording view, readRequestBody returns the ledger,
// and Create/Update call deleteSavedUploads on every failure path AFTER the
// parse (readRequestBody error, inTx error: validation, hook rejection,
// rolled-back insert, missing-id 404). A successful write keeps every key.

func orphanMediaHandler(t *testing.T) (*CrudHandler, *sql.DB, string) {
	t.Helper()
	dir := t.TempDir()
	db := setupDB(t, `CREATE TABLE orph_media (id TEXT PRIMARY KEY, caption TEXT NOT NULL, photo TEXT, count INTEGER)`)
	ent := entity.Define("orph_media", entity.EntityConfig{
		Name:  "orph_media",
		Table: "orph_media",
		Fields: []schema.Field{
			{Name: "caption", Type: schema.String, Required: true},
			{Name: "photo", Type: schema.Image},
			{Name: "count", Type: schema.Int},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	installSecurityOwnerExtractor(t)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Storage = upload.NewLocalStorage(dir)
	return ch, db, dir
}

// orphanMultipartRequest builds a multipart create carrying one valid PNG part
// for the photo field plus the given form fields.
func orphanMultipartRequest(t *testing.T, fields map[string]string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	fw, _ := mw.CreateFormFile("photo", "pic.png")
	_, _ = fw.Write(pngBytes())
	mw.Close()
	req := withTestUser(httptest.NewRequest(http.MethodPost, "/orph_media", &buf), "u1")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// orphanFiles counts every file under dir, recursively.
func orphanFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk storage dir: %v", err)
	}
	return n
}

// TestFailedCreateLeavesNoOrphanUpload drives the three failure shapes and
// requires zero unreferenced files after each.
func TestFailedCreateLeavesNoOrphanUpload(t *testing.T) {
	t.Run("validation failure on another field", func(t *testing.T) {
		ch, db, dir := orphanMediaHandler(t)
		rr := httptest.NewRecorder()
		ch.Create()(rr, orphanMultipartRequest(t, map[string]string{"count": "not-a-number"}))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 validation failure, got %d (%s)", rr.Code, rr.Body.String())
		}
		var rows int
		if err := db.QueryRow("SELECT COUNT(*) FROM orph_media").Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if n := orphanFiles(t, dir); n != 0 {
			t.Errorf("SECURITY: [orphan-upload] create failed validation (%d rows persisted) but %d file(s) remain in storage with no row referencing them", rows, n)
		}
	})

	t.Run("before-create hook rejection", func(t *testing.T) {
		ch, db, dir := orphanMediaHandler(t)
		hooks := hook.NewHookRegistry()
		hooks.RegisterHook(hook.BeforeCreate, func(_ context.Context, _ any) error {
			return fmt.Errorf("rejected")
		})
		ch.Hooks = hooks
		rr := httptest.NewRecorder()
		ch.Create()(rr, orphanMultipartRequest(t, map[string]string{"caption": "ok", "count": "1"}))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 hook rejection, got %d (%s)", rr.Code, rr.Body.String())
		}
		var rows int
		if err := db.QueryRow("SELECT COUNT(*) FROM orph_media").Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if n := orphanFiles(t, dir); n != 0 {
			t.Errorf("SECURITY: [orphan-upload] BeforeCreate hook rejected the write (%d rows persisted) but %d file(s) remain in storage with no row referencing them", rows, n)
		}
	})

	t.Run("multipart update on missing id", func(t *testing.T) {
		ch, db, dir := orphanMediaHandler(t)
		req := orphanMultipartRequest(t, map[string]string{"caption": "ok", "count": "1"})
		req.SetPathValue("id", "does-not-exist")
		rr := httptest.NewRecorder()
		ch.Update()(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for missing id, got %d (%s)", rr.Code, rr.Body.String())
		}
		var rows int
		if err := db.QueryRow("SELECT COUNT(*) FROM orph_media").Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if n := orphanFiles(t, dir); n != 0 {
			t.Errorf("SECURITY: [orphan-upload] update targeted a nonexistent id (404, %d rows persisted) but %d file(s) were already written to storage", rows, n)
		}
	})
}
