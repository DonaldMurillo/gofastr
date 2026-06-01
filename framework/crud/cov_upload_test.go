package crud

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestIsSafeMediaURL(t *testing.T) {
	cases := map[string]bool{
		"https://cdn.example.com/a.png": true,
		"http://example.com/x":          true,
		"/uploads/posts/img/a.png":      true,
		"a.png":                         true,
		"folder/a.png":                  true,
		"javascript:alert(1)":           false,
		"data:text/html,xss":            false,
		"file:///etc/passwd":            false,
		"../../etc/passwd":              false,
		"//evil.com/x":                  false,
		"http://a%0d%0aSet-Cookie":      false,
		"bad\x00path":                   false,
	}
	for in, want := range cases {
		if got := isSafeMediaURL(in); got != want {
			t.Errorf("isSafeMediaURL(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestEnforceJSONContentType(t *testing.T) {
	mk := func(ct string) *http.Request {
		r := httptest.NewRequest("POST", "/", strings.NewReader("{}"))
		if ct != "" {
			r.Header.Set("Content-Type", ct)
		} else {
			r.Header.Del("Content-Type")
		}
		return r
	}
	if err := enforceJSONContentType(mk("application/json")); err != nil {
		t.Errorf("json should pass: %v", err)
	}
	if err := enforceJSONContentType(mk("multipart/form-data; boundary=x")); err != nil {
		t.Errorf("multipart should pass: %v", err)
	}
	for _, bad := range []string{"", "text/plain", "application/x-www-form-urlencoded", "@@@invalid@@@"} {
		if err := enforceJSONContentType(mk(bad)); err == nil {
			t.Errorf("content-type %q should be rejected", bad)
		}
	}
}

func TestCoerceFormValue(t *testing.T) {
	ent := entity.Define("t", entity.EntityConfig{
		Name: "t", Table: "t",
		Fields: []schema.Field{
			{Name: "n", Type: schema.Int},
			{Name: "f", Type: schema.Float},
			{Name: "b", Type: schema.Bool},
			{Name: "s", Type: schema.String},
		},
	}.WithTimestamps(false))
	if got := coerceFormValue(ent, "n", "42"); got != int64(42) {
		t.Errorf("int coerce = %v (%T)", got, got)
	}
	if got := coerceFormValue(ent, "f", "3.5"); got != 3.5 {
		t.Errorf("float coerce = %v", got)
	}
	if got := coerceFormValue(ent, "b", "yes"); got != true {
		t.Errorf("bool true coerce = %v", got)
	}
	if got := coerceFormValue(ent, "b", "off"); got != false {
		t.Errorf("bool false coerce = %v", got)
	}
	// Bool with unparseable → raw string.
	if got := coerceFormValue(ent, "b", "maybe"); got != "maybe" {
		t.Errorf("bool fallthrough = %v", got)
	}
	// Int with bad value → raw string.
	if got := coerceFormValue(ent, "n", "abc"); got != "abc" {
		t.Errorf("int bad = %v", got)
	}
	// String field stays string.
	if got := coerceFormValue(ent, "s", "hi"); got != "hi" {
		t.Errorf("string = %v", got)
	}
	// Unknown field → raw.
	if got := coerceFormValue(ent, "unknown", "z"); got != "z" {
		t.Errorf("unknown field = %v", got)
	}
}

// covUploadHandler builds a media entity with an Image field + storage.
func covUploadHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := setupDB(t, `CREATE TABLE media (id TEXT PRIMARY KEY, caption TEXT, photo TEXT, count INTEGER)`)
	ent := entity.Define("media", entity.EntityConfig{
		Name: "media", Table: "media",
		Fields: []schema.Field{
			{Name: "caption", Type: schema.String},
			{Name: "photo", Type: schema.Image},
			{Name: "count", Type: schema.Int},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Storage = upload.NewLocalStorage(t.TempDir())
	return ch, db
}

// pngBytes is a minimal valid PNG header the content sniffer accepts.
func pngBytes() []byte {
	return append([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{0}, 32)...)
}

func TestMultipartCreate_SavesFile(t *testing.T) {
	ch, db := covUploadHandler(t)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("caption", "a pic")
	_ = mw.WriteField("count", "7")
	fw, _ := mw.CreateFormFile("photo", "pic.png")
	_, _ = fw.Write(pngBytes())
	mw.Close()

	req := httptest.NewRequest("POST", "/media", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("multipart create = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["caption"] != "a pic" {
		t.Errorf("caption = %v", got["caption"])
	}
	photo, _ := got["photo"].(string)
	if photo == "" || !strings.Contains(photo, "media") {
		t.Errorf("photo url not stored: %v", got["photo"])
	}
	// count was coerced to int.
	if got["count"] != float64(7) {
		t.Errorf("count = %v", got["count"])
	}
	// Verify persisted.
	var caption string
	_ = db.QueryRow("SELECT caption FROM media WHERE id = ?", got["id"]).Scan(&caption)
	if caption != "a pic" {
		t.Errorf("stored caption = %q", caption)
	}
}

func TestMultipartCreate_NoStorageConfigured(t *testing.T) {
	ch, _ := covUploadHandler(t)
	ch.Storage = nil

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("photo", "pic.png")
	_, _ = fw.Write(pngBytes())
	mw.Close()

	req := httptest.NewRequest("POST", "/media", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)
	if rec.Code == http.StatusCreated {
		t.Error("file part without storage should fail")
	}
}

func TestJSONCreate_RejectsUnsafeMediaURL(t *testing.T) {
	ch, _ := covUploadHandler(t)
	req := httptest.NewRequest("POST", "/media",
		strings.NewReader(`{"caption":"x","photo":"javascript:alert(1)"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unsafe media URL create = %d, want 400. body=%s", rec.Code, rec.Body.String())
	}
}

func TestJSONCreate_AcceptsSafeMediaURL(t *testing.T) {
	ch, _ := covUploadHandler(t)
	req := httptest.NewRequest("POST", "/media",
		strings.NewReader(`{"caption":"x","photo":"https://cdn.example.com/a.png"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("safe media URL create = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestValidateMediaURLs_Direct(t *testing.T) {
	ch, _ := covUploadHandler(t)
	// Non-string value is ignored.
	if err := ch.validateMediaURLs(map[string]any{"photo": 123}); err != nil {
		t.Errorf("non-string media value should be ignored: %v", err)
	}
	// Empty string ignored.
	if err := ch.validateMediaURLs(map[string]any{"photo": ""}); err != nil {
		t.Errorf("empty media value should be ignored: %v", err)
	}
	// Absent field ignored.
	if err := ch.validateMediaURLs(map[string]any{"caption": "x"}); err != nil {
		t.Errorf("absent media field should be ignored: %v", err)
	}
	// Unsafe rejected.
	if err := ch.validateMediaURLs(map[string]any{"photo": "../etc/passwd"}); err == nil {
		t.Error("unsafe media URL should be rejected")
	}
}
