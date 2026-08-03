package imagefield_test

// End-to-end: a multipart POST through the real CRUD handler, with the real
// image pipeline wired in. This is the only test that proves the whole claim
// — "declaring a schema.Image field is what makes uploads produce renditions
// and a BlurHash" — because it is the only one that exercises the upload
// handler, the deriver, storage, and the sibling-column write together.
//
// crud is imported by the test binary only, so this does not add a
// framework/crud → framework/image edge to anything that ships.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/file"
	fwimage "github.com/DonaldMurillo/gofastr/framework/image"
	"github.com/DonaldMurillo/gofastr/framework/imagefield"
)

// productsDDL declares the image column plus two of the three optional
// sibling columns — cover_placeholder is deliberately absent so the test
// also pins that a missing sibling is skipped rather than erroring.
const productsDDL = `CREATE TABLE products (
	id TEXT PRIMARY KEY,
	name TEXT,
	cover TEXT,
	cover_blurhash TEXT,
	cover_variants TEXT
)`

func productsHandler(t *testing.T, deriver file.ImageDeriver) (*crud.CrudHandler, *sql.DB, string) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(productsDDL); err != nil {
		t.Fatalf("ddl: %v", err)
	}

	ent := entity.Define("products", entity.EntityConfig{
		Name: "products", Table: "products",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String},
			{Name: "cover", Type: schema.Image},
			{Name: "cover_blurhash", Type: schema.String},
			{Name: "cover_variants", Type: schema.JSON},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)

	dir := t.TempDir()
	ch := crud.NewCrudHandler(ent, db).WithJSONCase(crud.CaseSnake)
	ch.Storage = upload.NewLocalStorage(dir)
	ch.ImageDeriver = deriver
	return ch, db, dir
}

func postCover(t *testing.T, ch *crud.CrudHandler, field, filename string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("name", "Wide Chair")
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatalf("write part: %v", err)
	}
	mw.Close()

	req := httptest.NewRequest("POST", "/products", &buf)
	// CRUD writes are fail-closed by default, so the request needs a user.
	req = req.WithContext(handler.SetUser(req.Context(), "test-user"))
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)
	return rec
}

func TestUploadPopulatesSiblingColumns(t *testing.T) {
	d, err := imagefield.New(landscapeConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ch, db, dir := productsHandler(t, d)

	rec := postCover(t, ch, "cover", "chair.png", testPNG(t, 800, 600))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body=%s", rec.Code, rec.Body.String())
	}

	var cover, hash, variants string
	if err := db.QueryRow(`SELECT cover, cover_blurhash, cover_variants FROM products`).
		Scan(&cover, &hash, &variants); err != nil {
		t.Fatalf("scan row: %v", err)
	}

	if cover == "" {
		t.Error("cover column is empty")
	}
	if hash == "" {
		t.Fatal("cover_blurhash was not populated — the whole point of the wiring")
	}
	// The stored hash must be renderable, not merely non-empty.
	durl, err := fwimage.BlurHashDataURL(hash, fwimage.BlurHashRenderConfig{})
	if err != nil {
		t.Fatalf("stored BlurHash does not decode: %v", err)
	}
	if !strings.HasPrefix(durl, "data:image/") {
		t.Errorf("decoded placeholder is not an inline image: %.40q", durl)
	}

	var refs []file.DerivedVariant
	if err := json.Unmarshal([]byte(variants), &refs); err != nil {
		t.Fatalf("cover_variants is not valid JSON (%q): %v", variants, err)
	}
	if len(refs) != 2 {
		t.Fatalf("stored %d variants, want 2: %s", len(refs), variants)
	}
	// Every reference must resolve to bytes actually on disk — a manifest
	// pointing at missing files would render as broken images.
	store := upload.NewLocalStorage(dir)
	for _, ref := range refs {
		ok, err := store.Exists(context.Background(), ref.StorageRef)
		if err != nil || !ok {
			t.Errorf("variant %q is not in storage (err=%v)", ref.StorageRef, err)
		}
		if ref.Width <= 0 || ref.Height <= 0 || ref.MIME == "" {
			t.Errorf("variant %+v is missing srcset metadata", ref)
		}
	}
}

// A missing sibling column must be skipped, not error — that is what makes
// the feature additive: you adopt a column by adding it.
func TestUploadSkipsUndeclaredSiblingColumns(t *testing.T) {
	cfg := landscapeConfig()
	cfg.Placeholder = &fwimage.PlaceholderOptions{Width: 20}
	d, _ := imagefield.New(cfg)
	ch, db, _ := productsHandler(t, d)

	// cover_placeholder is not in the DDL, and the deriver produces one.
	rec := postCover(t, ch, "cover", "chair.png", testPNG(t, 400, 300))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body=%s", rec.Code, rec.Body.String())
	}
	var hash string
	if err := db.QueryRow(`SELECT cover_blurhash FROM products`).Scan(&hash); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if hash == "" {
		t.Error("declared sibling column was not populated")
	}
}

// Without a deriver the upload path must behave exactly as before.
func TestUploadWithoutDeriverLeavesSiblingsEmpty(t *testing.T) {
	ch, db, _ := productsHandler(t, nil)
	rec := postCover(t, ch, "cover", "chair.png", testPNG(t, 200, 150))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body=%s", rec.Code, rec.Body.String())
	}
	var cover, hash string
	if err := db.QueryRow(`SELECT cover, COALESCE(cover_blurhash, '') FROM products`).Scan(&cover, &hash); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if cover == "" {
		t.Error("cover should still be stored without a deriver")
	}
	if hash != "" {
		t.Errorf("cover_blurhash = %q, want empty without a deriver", hash)
	}
}

// A non-image on an image field fails the request rather than storing a row
// whose renditions silently never existed.
func TestUploadRejectsNonImageOnImageField(t *testing.T) {
	d, _ := imagefield.New(landscapeConfig())
	ch, db, _ := productsHandler(t, d)

	// A PNG magic header followed by garbage passes the upload content
	// sniffer but cannot be decoded, which is exactly the case that used to
	// slip through as a file with no renditions.
	junk := append([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{0}, 32)...)
	rec := postCover(t, ch, "cover", "chair.png", junk)
	if rec.Code == http.StatusCreated {
		t.Fatalf("undecodable image was accepted: %s", rec.Body.String())
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM products`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("a failed derive still wrote %d row(s)", n)
	}
}

// A File field is any binary — a PDF, a CSV. Running the image pipeline over
// it would fail every upload, so only schema.Image is wired.
func TestUploadOnFileFieldSkipsPipeline(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE docs (id TEXT PRIMARY KEY, name TEXT, attachment TEXT, attachment_blurhash TEXT)`); err != nil {
		t.Fatalf("ddl: %v", err)
	}
	ent := entity.Define("docs", entity.EntityConfig{
		Name: "docs", Table: "docs",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String},
			{Name: "attachment", Type: schema.File},
			{Name: "attachment_blurhash", Type: schema.String},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)

	d, _ := imagefield.New(landscapeConfig())
	ch := crud.NewCrudHandler(ent, db).WithJSONCase(crud.CaseSnake)
	ch.Storage = upload.NewLocalStorage(t.TempDir())
	ch.ImageDeriver = d

	// A PDF on a File field: accepted and stored, with no derive attempted.
	pdf := append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte{'a'}, 64)...)
	rec := postCover(t, ch, "attachment", "spec.pdf", pdf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("File-field upload = %d, body=%s", rec.Code, rec.Body.String())
	}
	var attachment, hash string
	if err := db.QueryRow(`SELECT attachment, COALESCE(attachment_blurhash, '') FROM docs`).Scan(&attachment, &hash); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if attachment == "" {
		t.Error("attachment was not stored")
	}
	if hash != "" {
		t.Errorf("File field got image renditions: attachment_blurhash = %q", hash)
	}
}
