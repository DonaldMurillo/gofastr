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

// A Decimal posted in a multipart form stays the decimal string the
// JSON path takes: a record form that carries a file must save its
// money fields too.
func TestMultipartDecimalStaysAString(t *testing.T) {
	db := setupDB(t, `CREATE TABLE prices (id TEXT PRIMARY KEY, amount TEXT, logo TEXT)`)
	ent := entity.Define("prices", entity.EntityConfig{
		Name:  "prices",
		Table: "prices",
		Fields: []schema.Field{
			{Name: "amount", Type: schema.Decimal},
			{Name: "logo", Type: schema.Image},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Storage = upload.NewLocalStorage(t.TempDir())
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("amount", "99.50")
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/prices", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req = withTestUser(req, "u1")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"amount":"99.50"`) {
		t.Fatalf("a multipart decimal answered %d: %s", rec.Code, rec.Body)
	}
}
