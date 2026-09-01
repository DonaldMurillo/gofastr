package crud

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestMultipartSaveErrorNoAbsPathLeak pins CHAIN8-R5 (information
// disclosure via error echo, LOW). The chain:
//
//  1. SanitizeFilename caps the sanitized name at MaxFilenameBytes=255
//     "to protect log lines, filesystem APIs, and database columns"
//     (core/upload/validate.go:81-85), but UniqueFilename then appends
//     "_"+19-digit UnixNano+"_"+16 hex before the extension
//     (core/upload/upload.go:44-52), so a ~250-byte client filename yields
//     a ~287-byte path component — past POSIX NAME_MAX=255.
//  2. core/upload LocalStorage.Save wraps the resulting os.OpenFile
//     PathError unmapped ("creating file: %w", local.go:83-89); the
//     scrubPath discipline is applied only to Get errors (local.go:186-196:
//     "an HTTP handler that surfaces this … would otherwise leak the
//     storage layout").
//  3. The CRUD handlers echo the chain verbatim into the client response:
//     writeJSONError(w, http.StatusBadRequest, err.Error()) at
//     crud.go:941-944 (Create) and crud.go:1003-1010 (Update).
//
// Deterministic trigger exercised here: a multipart create whose file part
// has a 250-byte ASCII filename. Save fails ENAMETOOLONG carrying the
// ABSOLUTE storage path, and the 400 body must not disclose it. This is the
// same no-internal-error-text property the neighboring leak tests pin for
// driver errors; asserted at the multipart save-error surface.
func TestMultipartSaveErrorNoAbsPathLeak(t *testing.T) {
	db := setupDB(t, `CREATE TABLE uploads (id TEXT PRIMARY KEY, title TEXT, doc TEXT)`)
	ent := entity.Define("uploads", entity.EntityConfig{
		Name:  "uploads",
		Table: "uploads",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "doc", Type: schema.File},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	dir := t.TempDir()
	ch.Storage = upload.NewLocalStorage(dir)

	// 246 letters + ".png" = 250 bytes: passes the 255-byte sanitizer cap,
	// then UniqueFilename's +37-byte unique suffix pushes the final path
	// component to 287 bytes > NAME_MAX, so the backend's open fails with
	// an error that embeds the absolute destination path.
	longName := strings.Repeat("a", 246) + ".png"

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("title", "leak-probe"); err != nil {
		t.Fatalf("write field: %v", err)
	}
	fw, err := mw.CreateFormFile("doc", longName)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(bytes.Repeat([]byte("B"), 64)); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/uploads", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req = withTestUser(req, "u1")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("multipart create with over-long filename = %d, want 400. body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), dir) {
		t.Fatalf("SECURITY: [CHAIN8-R5] multipart save error echoed the absolute storage path to the client (storage root %q disclosed in the 400 body). body=%s", dir, rec.Body.String())
	}
}
