package framework

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/datexport"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/file"
)

// TestEraseUserData_RemovesStoredFiles pins CHAIN8-R1 (incomplete erasure,
// HIGH). App.Storage is set by WithFileStorage (app.go:642-650) as "the
// default upload.Storage used by CRUD handlers to persist files for Image
// and File entity fields", and crud's saveFilePart persists the storage key
// into the row's column (crud_upload.go:229-236, body[key] = ff.URL). But
// EraseUserData (erase_data.go) is SQL-only: all three planes run
// DELETE/UPDATE statements and the whole file never references a.Storage,
// so the objects the erased rows referenced survive:
//
//	// Entity plane: hard-delete by owner column (no soft-delete filter).
//	for _, s := range ents {
//		n, err := eraseDelete(ctx, tx, s.table, s.owner, userID)
//
// The erasure contract claims export parity ("an erasure reaches exactly the
// tables an export does", erase_data.go:20-24) while ExportData dumps the
// same file-URL columns — the export plane discloses exactly the per-user
// data the erase plane cannot reach. The bytes stay publicly downloadable at
// the route uploads.md documents:
//
//	app.Router().Get("/uploads/{key...}", upload.ServeHandler(storage))
//
// Observable end to end here through the real primitives: files written via
// the production write path (file.ProcessFileField, what saveFilePart
// calls), rows owned by the erased user, then the documented download route
// mounted exactly as uploads.md shows. After a successful EraseReport the
// erased user's file must no longer be downloadable; the other user's file
// must survive.
func TestEraseUserData_RemovesStoredFiles(t *testing.T) {
	datexport.Reset(t)
	db := openSQLiteMem(t)
	store := upload.NewLocalStorage(t.TempDir())
	app := NewApp(WithDB(db), WithFileStorage(store))

	app.Registry.Register(entity.Define("profiles", entity.EntityConfig{
		Table: "profiles",
		Scope: &entity.ScopeConfig{OwnerField: "owner_id"},
		Fields: []schema.Field{
			{Name: "avatar", Type: schema.Image},
			{Name: "owner_id", Type: schema.String},
		},
	}))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	if err := EnsureAuditTable(db, ""); err != nil {
		t.Fatalf("EnsureAuditTable: %v", err)
	}

	ctx := context.Background()
	// The production write path for an Image entity field: what crud's
	// saveFilePart runs (framework/crud/crud_upload.go:229-236).
	mustUpload := func(content string) string {
		ff, err := file.ProcessFileField(ctx, store, strings.NewReader(content), "avatar.png", "profiles", "avatar")
		if err != nil {
			t.Fatalf("ProcessFileField: %v", err)
		}
		return ff.StorageRef
	}
	k1 := mustUpload("u1-avatar-bytes")
	k2 := mustUpload("u2-avatar-bytes")

	// Owner-scoped rows referencing the stored objects, seeded the way the
	// erase tests seed (direct SQL; erase_data_test.go convention).
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("seed: %v\nquery: %s", err, q)
		}
	}
	exec(`INSERT INTO profiles (id, avatar, owner_id, created_at, updated_at) VALUES
		('p1', $1, 'u1', '2024-01-01T00:00:00Z', '2024-01-02T00:00:00Z'),
		('p2', $1, 'u2', '2024-01-01T00:00:00Z', '2024-01-02T00:00:00Z')`, k1)
	exec(`UPDATE profiles SET avatar = $1 WHERE id = 'p2'`, k2)

	// The documented download route (framework/docs/content/uploads.md:410-415),
	// mounted exactly as the docs show.
	serve := func(key string) (int, string) {
		mux := http.NewServeMux()
		mux.HandleFunc("/uploads/{key...}", upload.ServeHandler(store))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/uploads/"+key, nil))
		return rec.Code, rec.Body.String()
	}

	// Pre-erase sanity: both users' files are downloadable through the
	// documented route.
	if code, body := serve(k1); code != http.StatusOK || body != "u1-avatar-bytes" {
		t.Fatalf("pre-erase: GET u1 avatar = %d %q, want 200 with the stored bytes", code, body)
	}
	if code, _ := serve(k2); code != http.StatusOK {
		t.Fatalf("pre-erase: GET u2 avatar = %d, want 200", code)
	}

	report, err := app.EraseUserData(ctx, "u1")
	if err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}
	if report.TotalErased() < 1 {
		t.Fatalf("EraseUserData reported success with TotalErased=%d, want >= 1 (the row WAS erased)", report.TotalErased())
	}
	if got := countWhere(t, db, "profiles", "owner_id", "u1"); got != 0 {
		t.Fatalf("u1 rows survived erasure: %d", got)
	}

	// The defect: the row is gone and the report claims success, but the
	// object it referenced was never deleted from storage and the
	// documented download route keeps serving the erased user's file.
	if code, body := serve(k1); code != http.StatusNotFound {
		t.Fatalf("SECURITY: [CHAIN8-R1] after a successful EraseUserData (TotalErased=%d, row deleted), the erased user's avatar is still downloadable at the documented route: GET /uploads/%s = %d, body=%q. EraseUserData never consults a.Storage (erase_data.go is SQL-only), so the personal file and every rendition survive erasure behind a successful report.", report.TotalErased(), k1, code, body)
	}
	if ok, err := store.Exists(ctx, k1); err == nil && ok {
		t.Errorf("SECURITY: [CHAIN8-R1] storage still holds the erased user's object %q after EraseUserData", k1)
	}

	// The other user's file must survive u1's erasure.
	if code, _ := serve(k2); code != http.StatusOK {
		t.Errorf("u2's avatar must survive u1's erasure, GET = %d, want 200", code)
	}
}
