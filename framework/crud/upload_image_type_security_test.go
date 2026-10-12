package crud

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// An Image field takes a raster image and nothing else: a PDF (or any
// other type) posted to it is refused 400, sniffed from its bytes
// whatever its filename says, and nothing is stored. A File field still
// takes the PDF.
func TestMultipartImageFieldRefusesOtherTypes(t *testing.T) {
	db := setupDB(t, `CREATE TABLE logos (id TEXT PRIMARY KEY, logo TEXT, doc TEXT)`)
	ent := entity.Define("logos", entity.EntityConfig{
		Name:  "logos",
		Table: "logos",
		Fields: []schema.Field{
			{Name: "logo", Type: schema.Image},
			{Name: "doc", Type: schema.File},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	dir := t.TempDir()
	ch.Storage = upload.NewLocalStorage(dir)
	pdf := []byte("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n1 0 obj\n<<>>\nendobj\n")

	post := func(field string) int {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile(field, "logo.png")
		_, _ = fw.Write(pdf)
		_ = mw.Close()
		req := httptest.NewRequest(http.MethodPost, "/logos", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req = withTestUser(req, "u1")
		rec := httptest.NewRecorder()
		ch.Create()(rec, req)
		return rec.Code
	}
	if code := post("logo"); code != http.StatusBadRequest {
		t.Fatalf("a PDF in an Image field answered %d, want 400", code)
	}
	var files []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, _ error) error {
		if d != nil && !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	if len(files) != 0 {
		t.Fatalf("a refused image was stored: %v", files)
	}
	if code := post("doc"); code != http.StatusCreated {
		t.Fatalf("a PDF in a File field answered %d, want 201", code)
	}
}
