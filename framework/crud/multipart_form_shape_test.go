package crud

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A browser form posted multipart keeps the JSON body's shape. A bool's
// hidden "false" plus its checked box is one value, the last; any other
// repeat of a scalar is still refused. A file input left empty arrives
// as an empty value with no file, and keeps the stored file rather
// than clearing it.
func TestMultipartBrowserFormShape(t *testing.T) {
	db := setupDB(t, `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT, active INTEGER, logo TEXT)`)
	ent := entity.Define("posts", entity.EntityConfig{
		Name:  "posts",
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "active", Type: schema.Bool},
			{Name: "logo", Type: schema.Image},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Storage = upload.NewLocalStorage(t.TempDir())
	if _, err := db.Exec(`INSERT INTO posts (id, title, active, logo) VALUES ('p1', 'a', 0, 'https://cdn.example.com/a.png')`); err != nil {
		t.Fatal(err)
	}
	put := func(fields [][2]string, emptyFile bool) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		for _, f := range fields {
			_ = mw.WriteField(f[0], f[1])
		}
		if emptyFile {
			h := textproto.MIMEHeader{}
			h.Set("Content-Disposition", `form-data; name="logo"; filename=""`)
			h.Set("Content-Type", "application/octet-stream")
			_, _ = mw.CreatePart(h)
		}
		_ = mw.Close()
		req := httptest.NewRequest(http.MethodPut, "/posts/p1", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.SetPathValue("id", "p1")
		req = withTestUser(req, "u1")
		rec := httptest.NewRecorder()
		ch.Update()(rec, req)
		return rec
	}
	rec := put([][2]string{{"title", "b"}, {"active", "false"}, {"active", "true"}}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("the browser form answered %d: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"active":true`) || !strings.Contains(rec.Body.String(), `"logo":"https://cdn.example.com/a.png"`) {
		t.Fatalf("the pair or the empty file was misread: %s", rec.Body)
	}
	var logo string
	if err := db.QueryRow(`SELECT logo FROM posts WHERE id = 'p1'`).Scan(&logo); err != nil || logo != "https://cdn.example.com/a.png" {
		t.Fatalf("the stored logo is %q (%v)", logo, err)
	}
	if rec := put([][2]string{{"title", "x"}, {"title", "y"}}, false); rec.Code != http.StatusBadRequest {
		t.Fatalf("a repeated scalar answered %d, want 400", rec.Code)
	}
	if rec := put([][2]string{{"active", "true"}, {"active", "false"}}, false); rec.Code != http.StatusBadRequest {
		t.Fatalf("a bool pair not led by the hidden false answered %d, want 400", rec.Code)
	}
}
