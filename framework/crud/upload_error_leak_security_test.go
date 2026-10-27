package crud

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/teststorage"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// leakyStore fails every Save with an error naming its storage path, as
// a backend that does not scrub its own errors (S3, a custom one) would.
type leakyStore struct{ upload.Storage }

func (leakyStore) Save(context.Context, string, io.Reader) error {
	return &os.PathError{Op: "open", Path: "/srv/private-bucket/uploads/doc.png", Err: syscall.ENAMETOOLONG}
}

// A storage backend's Save failure is the server's fault: the client
// gets a 500 with the fixed phrase, never the backend's error text.
func TestUploadSaveErrorIsFixed500(t *testing.T) {
	db := setupDB(t, `CREATE TABLE uploads (id TEXT PRIMARY KEY, title TEXT, doc TEXT)`)
	ent := entity.Define("uploads", entity.EntityConfig{
		Name:   "uploads",
		Table:  "uploads",
		Fields: []schema.Field{{Name: "title", Type: schema.String}, {Name: "doc", Type: schema.File}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Storage = leakyStore{teststorage.New(t, t.TempDir())}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("doc", "doc.png")
	fw.Write([]byte("B"))
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/uploads", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	ch.Create()(rec, withTestUser(req, "u1"))

	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "private-bucket") {
		t.Fatalf("save failure = %d %s, want 500 without the backend's text", rec.Code, rec.Body.String())
	}
}

// TestMultipartSaveErrorNoAbsPathLeak pins CHAIN8-R5 (information
// disclosure via error echo, LOW). The chain:
//
//  1. A LocalStorage Save fails with an os.PathError carrying the
//     ABSOLUTE storage path.
//  2. The CRUD handlers echoed the error chain into the client response.
//     They now answer any storage Save failure with a fixed 500
//     (writeBodyError), whatever the backend's text; LocalStorage still
//     scrubs its root for the log line.
//
// The original trigger was a 250-byte client filename that UniqueFilename's
// suffix pushed past NAME_MAX; UniqueFilename now cuts the stem to fit, so
// that name saves. The trigger here is an unreadable directory on the
// key's path: resolving it fails with an lstat PathError naming the
// absolute path, and the body must not disclose the storage root.
// Root ignores directory modes, so the test skips there.
func TestMultipartSaveErrorNoAbsPathLeak(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions; the EACCES trigger needs a non-root user")
	}
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
	ch.Storage = teststorage.New(t, dir)

	// Upload keys start "uploads/" (the entity's table): with that
	// directory unreadable, the save fails resolving the key's path.
	locked := filepath.Join(dir, "uploads")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	longName := "doc.png"

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

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("multipart create into an unreadable storage dir = %d, want 500. body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), dir) {
		t.Fatalf("SECURITY: [CHAIN8-R5] multipart save error echoed the absolute storage path to the client (storage root %q disclosed in the body). body=%s", dir, rec.Body.String())
	}
}
