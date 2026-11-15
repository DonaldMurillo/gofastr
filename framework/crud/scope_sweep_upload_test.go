package crud

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
	"github.com/DonaldMurillo/gofastr/internal/teststorage"
)

// A read-only file column takes a file part under WithServerWrites, which
// writes such columns.
func TestServerWritesTakeReadOnlyFilePart(t *testing.T) {
	db := setupDB(t, `CREATE TABLE sw_media (id TEXT PRIMARY KEY, caption TEXT NOT NULL, photo TEXT)`)
	ent := entity.Define("sw_media", entity.EntityConfig{
		Name:   "sw_media",
		Table:  "sw_media",
		Fields: []schema.Field{{Name: "caption", Type: schema.String, Required: true}, {Name: "photo", Type: schema.Image, ReadOnly: true}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Storage = teststorage.New(t, t.TempDir())

	req := orphanMultipartRequest(t, map[string]string{"caption": "ok"})
	req = req.WithContext(WithServerWrites(req.Context()))
	body, saved, err := ch.parseMultipartBody(req)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if body["photo"] == nil || len(saved) == 0 {
		t.Fatalf("server write dropped the read-only file part: body %v saved %v", body, saved)
	}
}

// Two file parts under one key are refused: the column holds one key, and
// keeping either would drop a part a proxy may have inspected.
func TestMultipartTwoFilePartsOneKeyRefused(t *testing.T) {
	db := setupDB(t, `CREATE TABLE dup_media (id TEXT PRIMARY KEY, photo TEXT)`)
	ent := entity.Define("dup_media", entity.EntityConfig{
		Name: "dup_media", Table: "dup_media",
		Fields: []schema.Field{{Name: "photo", Type: schema.Image}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Storage = teststorage.New(t, t.TempDir())

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, name := range []string{"a.png", "b.png"} {
		fw, _ := mw.CreateFormFile("photo", name)
		_, _ = fw.Write(pngBytes())
	}
	mw.Close()
	req := withTestUser(httptest.NewRequest(http.MethodPost, "/dup_media", &buf), "u1")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if _, _, err := ch.parseMultipartBody(req); err == nil {
		t.Fatal("two file parts under one key were accepted")
	}
}

// A multipart Float value is converted only in the JSON path's canonical
// spelling; NaN, Inf and hex floats stay strings for validation to refuse.
func TestMultipartFloatTakesCanonicalSpellingOnly(t *testing.T) {
	ent := entity.Define("fl", entity.EntityConfig{Table: "fl", Fields: []schema.Field{{Name: "f", Type: schema.Float}}})
	if got := coerceFormValue(ent, "f", "1.5"); got != 1.5 {
		t.Fatalf("1.5 coerced to %#v", got)
	}
	for _, raw := range []string{"NaN", "Inf", "0x1p-2"} {
		if got := coerceFormValue(ent, "f", raw); got != raw {
			t.Errorf("%q coerced to %#v, want the raw string", raw, got)
		}
	}
}

type failingDeleteStorage struct{ upload.Storage }

func (failingDeleteStorage) Delete(context.Context, string) error { return errors.New("backend down") }

// The parse ledger keeps a key whose delete failed, so the compensating
// cleanup still names the object, and drops one whose delete succeeded.
func TestSavedKeyLedgerKeepsFailedDelete(t *testing.T) {
	ctx := context.Background()
	failing := &savedKeyLedger{Storage: failingDeleteStorage{teststorage.New(t, t.TempDir())}}
	if err := failing.Save(ctx, "k1", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	_ = failing.Delete(ctx, "k1")
	if !slices.Contains(failing.saved(), "k1") {
		t.Fatal("a key whose delete failed left the ledger")
	}
	ok := &savedKeyLedger{Storage: teststorage.New(t, t.TempDir())}
	if err := ok.Save(ctx, "k2", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	if err := ok.Delete(ctx, "k2"); err != nil || len(ok.saved()) != 0 {
		t.Fatalf("a deleted key stayed in the ledger: %v %v", err, ok.saved())
	}
}

// WithUploadedKeys adds to the keys an outer call recorded.
func TestWithUploadedKeysAccumulates(t *testing.T) {
	ctx := WithUploadedKeys(WithUploadedKeys(context.Background(), "a"), "b")
	if got := uploadedKeys(ctx); !got["a"] || !got["b"] {
		t.Fatalf("uploaded keys = %v, want a and b", got)
	}
}

// A handler bound to a caller's *sql.Tx has no commit queue, so a delete
// through it leaves the row's file in storage: nothing says the commit
// happened, and a rollback would leave a row naming a deleted object.
func TestTxBoundDeleteKeepsStoredFile(t *testing.T) {
	ch, db := clnWorld(t, false)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	bound := *ch
	bound.DB = tx
	if err := bound.DeleteOne(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	if !clnExists(t, ch, clnKey) {
		t.Fatal("a delete on a tx-bound handler removed the file before any commit")
	}
}

// The current keys an upload may resend are read under the caller's tenant:
// another tenant's row holds none for it.
func TestCurrentMediaKeysAreTenantScoped(t *testing.T) {
	ddl := `CREATE TABLE tmedia (id TEXT PRIMARY KEY, photo TEXT, tenant_id TEXT)`
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("tmedia", "tmedia", "", []schema.Field{
		{Name: "photo", Type: schema.Image},
		{Name: "tenant_id", Type: schema.String},
	}, func(c *entity.EntityConfig) { c.Scope.MultiTenant = true }), ddl)
	seedRows(t, db, "tmedia", []map[string]any{{"id": "b1", "photo": "media/b-key", "tenant_id": "tb"}})

	req := makeRequest(t, RequestOpts{Method: http.MethodPut, Path: "/tmedia/b1"})
	req = req.WithContext(tenant.SetTenantID(req.Context(), "ta"))
	keys, err := ch.currentMediaKeys(req.Context(), req, "", []any{"b1"})()
	if err != nil {
		t.Fatal(err)
	}
	if keys["media/b-key"] {
		t.Fatal("another tenant's row counted as the caller's current file")
	}
}

// The delete of released objects outlives the request: a caller that hung
// up after the commit still has its released file deleted.
func TestReleasedObjectDeleteOutlivesCancel(t *testing.T) {
	ch, _ := clnWorld(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch.deleteAfterCommit(ctx, []string{clnNew})
	if clnExists(t, ch, clnNew) {
		t.Fatal("a cancelled request context stopped the delete of a released object")
	}
}

// The compensating delete of a failed write's uploads outlives the request.
func TestSavedUploadDeleteOutlivesCancel(t *testing.T) {
	ch, _ := clnWorld(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch.deleteSavedUploads(ctx, []string{clnNew})
	if clnExists(t, ch, clnNew) {
		t.Fatal("a cancelled request context stopped the compensating delete")
	}
}

type panickingDeleteStorage struct{ upload.Storage }

func (panickingDeleteStorage) Delete(context.Context, string) error { panic("backend bug") }

// The post-commit delete runs after the handler's panic net is gone, so a
// backend that panics there is recovered, not left to crash the process.
func TestReleasedObjectDeleteRecoversPanic(t *testing.T) {
	ch, _ := clnWorld(t, false)
	ch.Storage = panickingDeleteStorage{ch.Storage}
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("a panicking storage Delete escaped deleteAfterCommit: %v", rec)
		}
	}()
	ch.deleteAfterCommit(context.Background(), []string{clnNew})
}
