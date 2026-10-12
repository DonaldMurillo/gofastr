package entityui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// uploadsUI is a products entity with a photo and a manual, over a
// UI that serves stored files at /uploads/ and an app with storage.
func uploadsUI(t *testing.T, store bool, more ...testUIOption) *testUI {
	opts := append([]testUIOption{withAPI(map[string]string{"products": "/api/products"})}, more...)
	if store {
		opts = append(opts, withStorage(upload.NewLocalStorage(t.TempDir())))
	}
	return newTestUIExt(t,
		map[string]entity.EntityConfig{"products": {Fields: fields(
			schema.Field{Name: "name", Type: schema.String, Required: true},
			schema.Field{Name: "photo", Type: schema.Image},
			schema.Field{Name: "manual", Type: schema.File},
		), Exposure: &entity.ExposureConfig{Public: true}}},
		map[string][]map[string]any{"products": {
			{"id": "p1", "name": "Drill", "photo": "uploads/products/photo/drill.png", "manual": "uploads/products/manual/drill guide.pdf"},
		}},
		Extensions{FilesURL: "/uploads/"},
		opts...,
	)
}

// With storage, an Image or File field is an upload: a file input of
// the allowed types, beside the stored file, which is drawn from where
// the app serves it. Saving without a new file keeps the stored one.
func TestUploadFieldsWithStorage(t *testing.T) {
	x := uploadsUI(t, true)
	body := string(x.ui.Record("products", "p1").RenderCtx(x.ctx("/products/p1", "")))
	for _, want := range []string{
		`type="file"`,
		`name="photo"`,
		`accept="image/png,image/jpeg,image/gif,image/webp"`,
		`name="manual"`,
		`src="/uploads/uploads/products/photo/drill.png"`,
		`href="/uploads/uploads/products/manual/drill%20guide.pdf"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the record misses %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `value="uploads/products/photo/drill.png"`) {
		t.Errorf("the photo is still an editable URL box")
	}
	list := listHTML(t, x.ui.List("products"), x.ctx("/products", ""))
	if !strings.Contains(list, `src="/uploads/uploads/products/photo/drill.png"`) {
		t.Errorf("the list thumbnail does not use the files URL:\n%s", list)
	}
}

// Without storage the app cannot take an upload: the stored URL stays
// an editable text box, as before.
func TestUploadFieldsWithoutStorage(t *testing.T) {
	x := uploadsUI(t, false)
	body := string(x.ui.Record("products", "p1").RenderCtx(x.ctx("/products/p1", "")))
	if strings.Contains(body, `type="file"`) {
		t.Errorf("a UI without storage drew a file input:\n%s", body)
	}
}

// SECURITY: a stored key that climbs out of the files URL, or carries a
// script scheme, draws no link.
func TestUploadFileHrefRefusesEscapes(t *testing.T) {
	x := uploadsUI(t, true)
	for _, key := range []string{"../etc/passwd", "javascript:alert(1)", "a/../../b"} {
		if got := x.ui.fileHref(key); got != "" {
			t.Errorf("fileHref(%q) = %q, want none", key, got)
		}
	}
	if got := x.ui.fileHref("https://cdn.example.com/a.png"); got != "https://cdn.example.com/a.png" {
		t.Errorf("an absolute URL = %q", got)
	}
}

// An upload's prompt speaks the request's language.
func TestUploadFieldTranslated(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("de", string(i18nui.KeyFileUploadDropSingle), i18n.Message{Text: "DATEI HIER ABLEGEN"})
	x := uploadsUI(t, true, withTranslator(i18n.NewTranslator(cat, "en")))
	ctx := i18n.WithContext(x.ctx("/products/p1", ""), i18n.Locale{Tag: "de"})
	body := string(x.ui.Record("products", "p1").RenderCtx(ctx))
	if !strings.Contains(body, "DATEI HIER ABLEGEN") {
		t.Fatalf("the upload prompt is not translated:\n%s", body)
	}
}
