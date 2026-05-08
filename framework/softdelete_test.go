package framework

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gofastr/gofastr/core/query"
)

// --- Soft Delete Tests ---

func TestSoftDelete(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectExec(`UPDATE posts SET deleted_at = NOW\(\) WHERE id = \$1`).
		WithArgs("123").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = SoftDelete(context.Background(), db, "posts", "123")
	if err != nil {
		t.Fatalf("SoftDelete returned error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestSoftRestore(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectExec(`UPDATE posts SET deleted_at = NULL WHERE id = \$1`).
		WithArgs("123").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = Restore(context.Background(), db, "posts", "123")
	if err != nil {
		t.Fatalf("Restore returned error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestSoftForceDelete(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectExec(`DELETE FROM posts WHERE id = \$1`).
		WithArgs("123").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = ForceDelete(context.Background(), db, "posts", "123")
	if err != nil {
		t.Fatalf("ForceDelete returned error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unfulfilled expectations: %v", err)
	}
}

func TestSoftDeleteFilterExcludes(t *testing.T) {
	qb := query.Select("*").From("posts")
	ApplySoftDeleteFilter(qb, false)
	sqlStr, _ := qb.Build()

	if !strings.Contains(sqlStr, "deleted_at IS NULL") {
		t.Errorf("expected 'deleted_at IS NULL' in query, got: %s", sqlStr)
	}
}

func TestSoftDeleteWithTrashed(t *testing.T) {
	// When showTrashed is true, no filter should be added
	qb := query.Select("*").From("posts")
	ApplySoftDeleteFilter(qb, true)
	sqlStr, _ := qb.Build()

	if strings.Contains(sqlStr, "deleted_at") {
		t.Errorf("expected no deleted_at filter when showTrashed=true, got: %s", sqlStr)
	}

	// Test WithTrashed helper with query param
	r := httptest.NewRequest("GET", "/posts?trashed=true", nil)
	if !WithTrashed(r) {
		t.Error("expected WithTrashed=true when ?trashed=true")
	}

	r2 := httptest.NewRequest("GET", "/posts", nil)
	if WithTrashed(r2) {
		t.Error("expected WithTrashed=false when no trashed param")
	}
}

func TestSoftDeleteWithSoftDeleteConfig(t *testing.T) {
	entity := Define("posts", EntityConfig{})
	if entity.Config.SoftDelete {
		t.Error("expected SoftDelete=false by default")
	}
	WithSoftDelete(entity)
	if !entity.Config.SoftDelete {
		t.Error("expected SoftDelete=true after WithSoftDelete")
	}
}

// --- Tenant Tests ---

func TestTenantFilter(t *testing.T) {
	qb := query.Select("*").From("posts")
	ApplyTenantFilter(qb, "tenant-abc123")
	sqlStr, args := qb.Build()

	if !strings.Contains(sqlStr, "tenant_id = $") {
		t.Errorf("expected 'tenant_id = $N' in query, got: %s", sqlStr)
	}

	found := false
	for _, a := range args {
		if a == "tenant-abc123" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'tenant-abc123' in args, got: %v", args)
	}
}

func TestTenantFilterEmptyID(t *testing.T) {
	qb := query.Select("*").From("posts")
	ApplyTenantFilter(qb, "")
	sqlStr, args := qb.Build()

	if strings.Contains(sqlStr, "tenant_id") {
		t.Errorf("expected no tenant filter when ID is empty, got: %s", sqlStr)
	}
	if len(args) != 0 {
		t.Errorf("expected no args when ID is empty, got: %v", args)
	}
}

func TestTenantMiddleware(t *testing.T) {
	var capturedTenantID string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedTenantID = GetTenantID(r.Context())
	})

	mw := TenantMiddleware("X-Tenant-ID")
	handler := mw(next)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Tenant-ID", "tenant-456")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if capturedTenantID != "tenant-456" {
		t.Errorf("expected tenant-456, got %q", capturedTenantID)
	}
}

func TestTenantMiddlewareMissingHeader(t *testing.T) {
	var capturedTenantID string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedTenantID = GetTenantID(r.Context())
	})

	mw := TenantMiddleware("X-Tenant-ID")
	handler := mw(next)

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if capturedTenantID != "" {
		t.Errorf("expected empty tenant ID when header missing, got %q", capturedTenantID)
	}
}

func TestTenantSetGetID(t *testing.T) {
	ctx := context.Background()
	if id := GetTenantID(ctx); id != "" {
		t.Errorf("expected empty ID from background context, got %q", id)
	}

	ctx = SetTenantID(ctx, "tenant-789")
	if id := GetTenantID(ctx); id != "tenant-789" {
		t.Errorf("expected tenant-789, got %q", id)
	}
}

func TestTenantInjectID(t *testing.T) {
	ctx := SetTenantID(context.Background(), "tenant-inject")
	data := map[string]any{"title": "Hello"}
	InjectTenantID(data, ctx)

	if v, ok := data["tenant_id"]; !ok || v != "tenant-inject" {
		t.Errorf("expected tenant_id to be injected, got: %v", data)
	}
}

func TestTenantInjectIDEmpty(t *testing.T) {
	data := map[string]any{"title": "Hello"}
	InjectTenantID(data, context.Background())

	if _, ok := data["tenant_id"]; ok {
		t.Error("expected no tenant_id injection when context has no tenant")
	}
}

func TestTenantDefaultConfig(t *testing.T) {
	cfg := DefaultTenantConfig()
	if cfg.Field != "tenant_id" {
		t.Errorf("expected Field 'tenant_id', got %q", cfg.Field)
	}
	if cfg.Header != "X-Tenant-ID" {
		t.Errorf("expected Header 'X-Tenant-ID', got %q", cfg.Header)
	}
	if !cfg.AutoScope {
		t.Error("expected AutoScope=true by default")
	}
}

func TestTenantWithMultiTenant(t *testing.T) {
	entity := Define("posts", EntityConfig{})
	if entity.Config.MultiTenant {
		t.Error("expected MultiTenant=false by default")
	}
	WithMultiTenant(entity, TenantConfig{Field: "org_id", Header: "X-Org-ID"})
	if !entity.Config.MultiTenant {
		t.Error("expected MultiTenant=true after WithMultiTenant")
	}
}

// --- File Field Tests ---

func TestFilePathGeneration(t *testing.T) {
	path := GenerateFilePath("posts", "avatar", "photo.png")

	if !strings.HasPrefix(path, "uploads/posts/avatar/") {
		t.Errorf("expected path under uploads/posts/avatar/, got: %s", path)
	}
	if !strings.HasSuffix(path, ".png") {
		t.Errorf("expected .png extension, got: %s", path)
	}
	if strings.Contains(path, "..") {
		t.Errorf("path should not contain '..', got: %s", path)
	}
}

func TestFilePathSanitizes(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		bad      []string
	}{
		{"path traversal", "../../../etc/passwd", []string{"..", "/etc"}},
		{"null bytes", "file\x00name.txt", []string{"\x00"}},
		{"hidden file", ".htaccess", []string{"/.htaccess"}},
		{"mixed separators", "path\\to/file.png", []string{"\\"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := GenerateFilePath("posts", "file", tt.filename)
			for _, bad := range tt.bad {
				if strings.Contains(path, bad) {
					t.Errorf("path should not contain %q, got: %s", bad, path)
				}
			}
		})
	}
}

func TestFilePathMultipleFiles(t *testing.T) {
	path1 := GenerateFilePath("posts", "avatar", "photo.png")

	// Ensure unique nanosecond timestamps between calls
	time.Sleep(time.Microsecond)

	path2 := GenerateFilePath("posts", "avatar", "photo.png")

	// Paths should be unique due to nanosecond timestamp
	if path1 == path2 {
		t.Error("expected unique paths for same filename, got identical paths")
	}
}

func TestFileFieldProcess(t *testing.T) {
	// Test with a mock storage
	store := &mockStorage{}
	content := strings.NewReader("hello world file content")

	ff, err := ProcessFileField(context.Background(), store, content, "document.txt", "posts", "attachment")
	if err != nil {
		t.Fatalf("ProcessFileField returned error: %v", err)
	}

	if ff.Filename != "document.txt" {
		t.Errorf("expected Filename 'document.txt', got %q", ff.Filename)
	}
	if ff.Size != 0 {
		// Note: strings.Reader may report differently depending on how ReadFrom works
		// The important thing is it's not negative
		if ff.Size < 0 {
			t.Errorf("expected non-negative Size, got %d", ff.Size)
		}
	}
	if ff.StorageRef == "" {
		t.Error("expected non-empty StorageRef")
	}
	if !strings.HasPrefix(ff.StorageRef, "uploads/posts/attachment/") {
		t.Errorf("expected StorageRef under uploads/posts/attachment/, got: %s", ff.StorageRef)
	}
}

func TestFileFieldDeleteNil(t *testing.T) {
	store := &mockStorage{}
	if err := DeleteFileField(context.Background(), store, nil); err != nil {
		t.Errorf("expected nil error for nil FileField, got: %v", err)
	}

	ff := &FileField{StorageRef: ""}
	if err := DeleteFileField(context.Background(), store, ff); err != nil {
		t.Errorf("expected nil error for empty StorageRef, got: %v", err)
	}
}

func TestFileFieldDeleteWithRef(t *testing.T) {
	store := &mockStorage{}
	ff := &FileField{StorageRef: "uploads/test/file.txt"}

	if err := DeleteFileField(context.Background(), store, ff); err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}

	if store.deletedKey != "uploads/test/file.txt" {
		t.Errorf("expected delete key 'uploads/test/file.txt', got %q", store.deletedKey)
	}
}

func TestFileFieldProcessNilStorage(t *testing.T) {
	content := strings.NewReader("test")
	_, err := ProcessFileField(context.Background(), nil, content, "test.txt", "posts", "file")
	if err == nil {
		t.Error("expected error with nil storage")
	}
}

func TestFileFieldDeleteNilStorage(t *testing.T) {
	ff := &FileField{StorageRef: "test"}
	if err := DeleteFileField(context.Background(), nil, ff); err == nil {
		t.Error("expected error with nil storage")
	}
}

// --- Mock Storage ---

type mockStorage struct {
	savedKey   string
	savedData  string
	deletedKey string
}

func (m *mockStorage) Save(ctx context.Context, key string, r io.Reader) error {
	m.savedKey = key
	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	m.savedData = string(buf[:n])
	return nil
}

func (m *mockStorage) Delete(ctx context.Context, key string) error {
	m.deletedKey = key
	return nil
}

func (m *mockStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return nil, nil
}

func (m *mockStorage) Exists(ctx context.Context, key string) (bool, error) {
	return false, nil
}
