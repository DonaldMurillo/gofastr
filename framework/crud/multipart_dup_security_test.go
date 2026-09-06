package crud

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// F18 layer-mismatch parsing (content-type confusion) — pinned 2026-09-05 round 4.
// Property: the same logical write must not have a weaker duplicate-key posture on the
// multipart path than on the JSON path.
// Surfaces: crud_upload.go:parseMultipartBody (the MultipartForm.Value loop used to take
//   vals[0] and drop the rest; the File loop kept headers[0] and dropped the rest).
// Fix (this suite's green state): a repeated form key COLLECTS into one list for a
//   schema.JSON column (HTML multi-select semantics, mirroring ?field_in=), and is
//   refused with 400 for every scalar field — the JSON body path refuses duplicate
//   keys outright (TestMapBodyRejectsDuplicateKeys) because a duplicate lets a proxy
//   or WAF see a payload the server never executes. Two file parts under one key are
//   refused the same way: an Image/File column holds one URL.

func dupFormHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := setupDB(t, `CREATE TABLE dupmp_notes (id TEXT PRIMARY KEY, title TEXT)`)
	ent := entity.Define("dupmp_notes", entity.EntityConfig{
		Name:  "dupmp_notes",
		Table: "dupmp_notes",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	installSecurityOwnerExtractor(t)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Storage = upload.NewLocalStorage(t.TempDir())
	return ch, db
}

func TestMultipartDuplicateKeysRefused(t *testing.T) {
	ch, db := dupFormHandler(t)

	body := "--B\r\n" +
		"Content-Disposition: form-data; name=\"title\"\r\n\r\nfirst\r\n" +
		"--B\r\n" +
		"Content-Disposition: form-data; name=\"title\"\r\n\r\nsecond\r\n" +
		"--B--\r\n"
	req := withTestUser(httptest.NewRequest(http.MethodPost, "/dupmp_notes", strings.NewReader(body)), "alice")
	req.Header.Set("Content-Type", "multipart/form-data; boundary=B")
	rr := httptest.NewRecorder()
	ch.Create()(rr, req)

	var stored any
	scanErr := db.QueryRow("SELECT title FROM dupmp_notes LIMIT 1").Scan(&stored)
	if rr.Code == http.StatusCreated {
		t.Errorf("SECURITY: [multipart-dup]: multipart create accepted duplicate form keys (status 201, stored %q) where the JSON body path refuses the same logical write", stored)
	}
	if scanErr == nil {
		t.Errorf("SECURITY: [multipart-dup]: a refused duplicate-key create must not persist a row (stored %q)", stored)
	}

	// The multi-select carve-out: a schema.JSON column collects repeats.
	dbj := setupDB(t, `CREATE TABLE dupmp_tags (id TEXT PRIMARY KEY, tags TEXT)`)
	entj := entity.Define("dupmp_tags", entity.EntityConfig{
		Name:  "dupmp_tags",
		Table: "dupmp_tags",
		Fields: []schema.Field{
			{Name: "tags", Type: schema.JSON},
		},
	}.WithTimestamps(false))
	entj.SetDB(dbj)
	chj := NewCrudHandler(entj, dbj).WithJSONCase(CaseSnake)
	chj.Storage = upload.NewLocalStorage(t.TempDir())
	reqj := withTestUser(httptest.NewRequest(http.MethodPost, "/dupmp_tags", strings.NewReader(
		"--B\r\n"+
			"Content-Disposition: form-data; name=\"tags\"\r\n\r\nred\r\n"+
			"--B\r\n"+
			"Content-Disposition: form-data; name=\"tags\"\r\n\r\nblue\r\n"+
			"--B--\r\n")), "alice")
	reqj.Header.Set("Content-Type", "multipart/form-data; boundary=B")
	rrj := httptest.NewRecorder()
	chj.Create()(rrj, reqj)
	if rrj.Code != http.StatusCreated {
		t.Fatalf("multi-select create status=%d body=%s", rrj.Code, rrj.Body.String())
	}
	var tags string
	if err := dbj.QueryRow("SELECT tags FROM dupmp_tags LIMIT 1").Scan(&tags); err != nil {
		t.Fatalf("read tags: %v", err)
	}
	if tags != `["red","blue"]` {
		t.Errorf("multi-select collected %q, want [\"red\",\"blue\"]", tags)
	}
}
