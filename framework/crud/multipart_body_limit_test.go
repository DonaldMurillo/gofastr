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

// multipartLimitHandler builds a handler over an entity with a schema.File
// field plus storage, for exercising the multipart wire-cap paths.
func multipartLimitHandler(t *testing.T) *CrudHandler {
	t.Helper()
	db := setupDB(t, `CREATE TABLE uploads (id TEXT PRIMARY KEY, title TEXT, doc TEXT)`)
	ent := entity.Define("uploads", entity.EntityConfig{
		Name: "uploads", Table: "uploads",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "doc", Type: schema.File},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Storage = upload.NewLocalStorage(t.TempDir())
	return ch
}

// multipartRequest builds a POST/PATCH request whose "doc" file part is
// size bytes of inert data (no markup tokens, so the content sniffer
// accepts it).
func multipartRequest(method, path string, size int) *http.Request {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("title", "has-file")
	fw, _ := mw.CreateFormFile("doc", "blob.bin")
	_, _ = fw.Write(bytes.Repeat([]byte{0x42}, size))
	_ = mw.Close()
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// TestMultipartCreateAcceptsLargeFile: a 2 MiB multipart upload must
// succeed. Create wraps r.Body in a 1 MiB MaxBytesReader sized for JSON
// bodies; multipart requests must be capped at the multipart limit
// instead, or every upload above 1 MiB dies with a bogus 400 and the
// 32 MiB MaxMultipartMemory constant is a dead letter.
func TestMultipartCreateAcceptsLargeFile(t *testing.T) {
	ch := multipartLimitHandler(t)
	req := withTestUser(multipartRequest("POST", "/uploads", 2<<20), "u1")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("2 MiB multipart create = %d, want 201. body=%s", rec.Code, rec.Body.String())
	}
}

// TestMultipartUpdateAcceptsLargeFile: the Update sibling of the above,
// PATCH with a >1 MiB multipart body must succeed, not 400.
func TestMultipartUpdateAcceptsLargeFile(t *testing.T) {
	ch := multipartLimitHandler(t)
	req := withTestUser(httptest.NewRequest("POST", "/uploads",
		strings.NewReader(`{"title":"x"}`)), "u1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed create = %d body=%s", rec.Code, rec.Body.String())
	}
	id, _ := decodeSingleResponse(t, rec.Body.Bytes())["id"].(string)

	preq := withTestUser(multipartRequest("PATCH", "/uploads/"+id, 2560<<10), "u1")
	preq.SetPathValue("id", id)
	prec := httptest.NewRecorder()
	ch.Update()(prec, preq)
	if prec.Code != http.StatusOK {
		t.Fatalf("2.5 MiB multipart update = %d, want 200. body=%s", prec.Code, prec.Body.String())
	}
}

// TestMultipartCreateOverWireCapIs413: a multipart body over the
// multipart wire cap must be rejected 413 (too large), not 400 (parse
// error). Over-limit is a size problem; reporting it as a malformed
// request misleads clients into retrying the same bytes.
func TestMultipartCreateOverWireCapIs413(t *testing.T) {
	ch := multipartLimitHandler(t)
	req := withTestUser(multipartRequest("POST", "/uploads", int(MaxMultipartBodyBytes)+(64<<10)), "u1")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over-cap multipart create = %d, want 413. body=%s", rec.Code, rec.Body.String())
	}
}

// TestMultipartMixedCaseTypeLargeFile: media types are case-insensitive
// (RFC 9110 §8.3.1). enforceJSONContentType parses the header with
// mime.ParseMediaType, which lowercases, so `Multipart/Form-Data`
// passes the content-type gate, but isMultipart used a case-sensitive
// prefix check and routed the request down the JSON path: the 1 MiB
// MaxJSONBodyBytes cap applied and every >1 MiB upload died. The two
// checks must agree, whatever the case.
func TestMultipartMixedCaseTypeLargeFile(t *testing.T) {
	ch := multipartLimitHandler(t)
	req := withTestUser(multipartRequest("POST", "/uploads", 2<<20), "u1")
	ct := req.Header.Get("Content-Type")
	req.Header.Set("Content-Type", "Multipart/Form-Data"+strings.TrimPrefix(ct, "multipart/form-data"))
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("2 MiB mixed-case multipart create = %d, want 201. body=%s", rec.Code, rec.Body.String())
	}
}
