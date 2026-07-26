package crud

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/file"
)

// fakeDeriver stands in for framework/imagefield so these tests exercise the
// upload handler's own branches without pulling the image codecs into this
// package's dependency graph (see TestUploadPathDoesNotLinkImageCodecs).
type fakeDeriver struct {
	out     *file.ImageDerivatives
	err     error
	calls   int
	lastRef string
}

func (f *fakeDeriver) DeriveImage(_ context.Context, _ upload.Storage, _ []byte, primaryRef string) (*file.ImageDerivatives, error) {
	f.calls++
	f.lastRef = primaryRef
	if f.err != nil {
		return nil, f.err
	}
	return f.out, nil
}

func fullDerivatives() *file.ImageDerivatives {
	return &file.ImageDerivatives{
		BlurHash:    "LEHV6nWB2yk8pyo0adR*.7kCMdnj",
		Placeholder: "data:image/jpeg;base64,/9j/4AAQ",
		Variants: []file.DerivedVariant{
			{StorageRef: "media/photo/x-sm.webp", MIME: "image/webp", Width: 320, Height: 240},
			{StorageRef: "media/photo/x-md.webp", MIME: "image/webp", Width: 640, Height: 480},
		},
	}
}

// deriveHandler builds a media entity whose sibling columns are declared
// selectively, so the "declared column wins, undeclared is skipped" branch is
// exercised by construction.
func deriveHandler(t *testing.T, withSiblings bool) (*CrudHandler, *sql.DB) {
	t.Helper()
	ddl := `CREATE TABLE media (id TEXT PRIMARY KEY, caption TEXT, photo TEXT, doc TEXT)`
	fields := []schema.Field{
		{Name: "caption", Type: schema.String},
		{Name: "photo", Type: schema.Image},
		{Name: "doc", Type: schema.File},
	}
	if withSiblings {
		ddl = `CREATE TABLE media (id TEXT PRIMARY KEY, caption TEXT, photo TEXT, doc TEXT,
			photo_blurhash TEXT, photo_placeholder TEXT, photo_variants TEXT)`
		fields = append(fields,
			schema.Field{Name: "photo_blurhash", Type: schema.String},
			schema.Field{Name: "photo_placeholder", Type: schema.String},
			schema.Field{Name: "photo_variants", Type: schema.JSON},
		)
	}
	db := setupDB(t, ddl)
	ent := entity.Define("media", entity.EntityConfig{
		Name: "media", Table: "media", Fields: fields,
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Storage = upload.NewLocalStorage(t.TempDir())
	return ch, db
}

func postPart(t *testing.T, ch *CrudHandler, field, filename string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("caption", "a pic")
	fw, _ := mw.CreateFormFile(field, filename)
	_, _ = fw.Write(data)
	mw.Close()
	req := withTestUser(httptest.NewRequest("POST", "/media", &buf), "u1")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)
	return rec
}

func TestDerive_PopulatesDeclaredSiblingColumns(t *testing.T) {
	ch, db := deriveHandler(t, true)
	d := &fakeDeriver{out: fullDerivatives()}
	ch.ImageDeriver = d

	rec := postPart(t, ch, "photo", "pic.png", pngBytes())
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body=%s", rec.Code, rec.Body.String())
	}
	if d.calls != 1 {
		t.Errorf("deriver called %d times, want 1", d.calls)
	}
	// The deriver is handed the primary storage key so it can place
	// renditions beside the original.
	if d.lastRef == "" {
		t.Error("deriver was not given the primary storage ref")
	}

	var hash, placeholder, variants string
	if err := db.QueryRow(`SELECT photo_blurhash, photo_placeholder, photo_variants FROM media`).
		Scan(&hash, &placeholder, &variants); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if hash != "LEHV6nWB2yk8pyo0adR*.7kCMdnj" {
		t.Errorf("photo_blurhash = %q", hash)
	}
	if placeholder == "" {
		t.Error("photo_placeholder was not populated")
	}
	var refs []file.DerivedVariant
	if err := json.Unmarshal([]byte(variants), &refs); err != nil {
		t.Fatalf("photo_variants is not JSON (%q): %v", variants, err)
	}
	if len(refs) != 2 || refs[0].Width != 320 {
		t.Errorf("photo_variants = %s", variants)
	}
}

// The columns are optional. With none declared the upload still succeeds and
// the derived values are simply dropped — that is what makes the feature
// additive rather than a schema requirement.
func TestDerive_SkipsUndeclaredSiblingColumns(t *testing.T) {
	ch, db := deriveHandler(t, false)
	ch.ImageDeriver = &fakeDeriver{out: fullDerivatives()}

	rec := postPart(t, ch, "photo", "pic.png", pngBytes())
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body=%s", rec.Code, rec.Body.String())
	}
	var photo string
	if err := db.QueryRow(`SELECT photo FROM media`).Scan(&photo); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if photo == "" {
		t.Error("primary upload lost")
	}
}

// Only schema.Image runs the pipeline; a File field is arbitrary binary.
func TestDerive_NotRunForFileFields(t *testing.T) {
	ch, _ := deriveHandler(t, true)
	d := &fakeDeriver{out: fullDerivatives()}
	ch.ImageDeriver = d

	rec := postPart(t, ch, "doc", "spec.pdf", append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte{'a'}, 32)...))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body=%s", rec.Code, rec.Body.String())
	}
	if d.calls != 0 {
		t.Errorf("deriver ran on a File field (%d calls)", d.calls)
	}
}

func TestDerive_NoDeriverLeavesSiblingsNull(t *testing.T) {
	ch, db := deriveHandler(t, true)
	// ch.ImageDeriver deliberately unset.
	rec := postPart(t, ch, "photo", "pic.png", pngBytes())
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body=%s", rec.Code, rec.Body.String())
	}
	var hash string
	if err := db.QueryRow(`SELECT COALESCE(photo_blurhash,'') FROM media`).Scan(&hash); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if hash != "" {
		t.Errorf("photo_blurhash = %q without a deriver", hash)
	}
}

// A derive failure fails the request. Storing a row whose renditions never
// existed would surface much later as a page with no srcset and nothing in
// the logs pointing back here.
func TestDerive_FailureFailsTheRequest(t *testing.T) {
	ch, db := deriveHandler(t, true)
	ch.ImageDeriver = &fakeDeriver{err: errors.New("not a decodable image")}

	rec := postPart(t, ch, "photo", "pic.png", pngBytes())
	if rec.Code == http.StatusCreated {
		t.Fatalf("failed derive was accepted: %s", rec.Body.String())
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM media`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("failed derive still wrote %d row(s)", n)
	}
}

// Metadata that would not survive FileField.Validate must not reach a column.
func TestDerive_RejectsUnsafeDerivedMetadata(t *testing.T) {
	ch, db := deriveHandler(t, true)
	ch.ImageDeriver = &fakeDeriver{out: &file.ImageDerivatives{
		Variants: []file.DerivedVariant{{StorageRef: "../../etc/passwd", MIME: "image/webp", Width: 1, Height: 1}},
	}}
	rec := postPart(t, ch, "photo", "pic.png", pngBytes())
	if rec.Code == http.StatusCreated {
		t.Fatalf("traversal in a derived ref was accepted: %s", rec.Body.String())
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM media`).Scan(&n)
	if n != 0 {
		t.Errorf("unsafe derive still wrote %d row(s)", n)
	}
}

// Empty derived fields must not write empty strings over columns that would
// otherwise stay NULL.
func TestDerive_EmptyValuesAreNotWritten(t *testing.T) {
	ch, db := deriveHandler(t, true)
	ch.ImageDeriver = &fakeDeriver{out: &file.ImageDerivatives{
		BlurHash: "LEHV6nWB2yk8pyo0adR*.7kCMdnj",
		// Placeholder and Variants intentionally empty.
	}}
	rec := postPart(t, ch, "photo", "pic.png", pngBytes())
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body=%s", rec.Code, rec.Body.String())
	}
	var placeholder, variants any
	if err := db.QueryRow(`SELECT photo_placeholder, photo_variants FROM media`).Scan(&placeholder, &variants); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if placeholder != nil {
		t.Errorf("photo_placeholder = %v, want NULL", placeholder)
	}
	if variants != nil {
		t.Errorf("photo_variants = %v, want NULL", variants)
	}
}

// One app-wide config cannot describe every image, so a field may override it.
func TestDerive_PerFieldOverrideWins(t *testing.T) {
	ch, db := deriveHandler(t, true)
	def := &fakeDeriver{out: fullDerivatives()}
	override := &fakeDeriver{out: &file.ImageDerivatives{BlurHash: "OVERRIDDEN0000000000000000ab"}}
	ch.ImageDeriver = def
	ch.FieldImageDerivers = map[string]file.ImageDeriver{"photo": override}

	if rec := postPart(t, ch, "photo", "pic.png", pngBytes()); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body=%s", rec.Code, rec.Body.String())
	}
	if override.calls != 1 || def.calls != 0 {
		t.Errorf("override calls=%d default calls=%d, want 1 and 0", override.calls, def.calls)
	}
	var hash string
	if err := db.QueryRow(`SELECT photo_blurhash FROM media`).Scan(&hash); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if hash != "OVERRIDDEN0000000000000000ab" {
		t.Errorf("photo_blurhash = %q, want the override's output", hash)
	}
}

// An explicit nil override opts one field out without disturbing the default.
func TestDerive_PerFieldNilOptsOut(t *testing.T) {
	ch, db := deriveHandler(t, true)
	def := &fakeDeriver{out: fullDerivatives()}
	ch.ImageDeriver = def
	ch.FieldImageDerivers = map[string]file.ImageDeriver{"photo": nil}

	if rec := postPart(t, ch, "photo", "pic.png", pngBytes()); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body=%s", rec.Code, rec.Body.String())
	}
	if def.calls != 0 {
		t.Errorf("default deriver ran despite a nil override (%d calls)", def.calls)
	}
	var hash string
	if err := db.QueryRow(`SELECT COALESCE(photo_blurhash,'') FROM media`).Scan(&hash); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if hash != "" {
		t.Errorf("photo_blurhash = %q, want empty", hash)
	}
}

// A field with no entry falls through to the app-wide default.
func TestDerive_UnlistedFieldUsesDefault(t *testing.T) {
	ch, _ := deriveHandler(t, true)
	def := &fakeDeriver{out: fullDerivatives()}
	ch.ImageDeriver = def
	ch.FieldImageDerivers = map[string]file.ImageDeriver{"other_field": nil}

	if rec := postPart(t, ch, "photo", "pic.png", pngBytes()); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body=%s", rec.Code, rec.Body.String())
	}
	if def.calls != 1 {
		t.Errorf("default deriver calls = %d, want 1", def.calls)
	}
}
