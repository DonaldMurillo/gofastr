package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ─── Local Storage Tests ─────────────────────────────────────────────

func TestLocalStorageSaveGetDeleteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	ls := NewLocalStorage(dir)

	ctx := context.Background()
	key := "test/file.txt"
	content := []byte("hello, world")

	// Save
	if err := ls.Save(ctx, key, bytes.NewReader(content)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Exists
	exists, err := ls.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Fatal("Exists returned false after Save")
	}

	// Get
	rc, err := ls.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("content mismatch: got %q, want %q", got, content)
	}

	// Delete
	if err := ls.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	exists, err = ls.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Exists after delete: %v", err)
	}
	if exists {
		t.Fatal("Exists returned true after Delete")
	}
}

func TestLocalStorageNestedDirectories(t *testing.T) {
	dir := t.TempDir()
	ls := NewLocalStorage(dir)

	ctx := context.Background()
	key := "deep/nested/path/file.dat"
	content := []byte("nested content")

	if err := ls.Save(ctx, key, bytes.NewReader(content)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	rc, err := ls.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("content mismatch: got %q, want %q", got, content)
	}

	// Verify file exists at expected path
	expectedPath := filepath.Join(dir, "deep", "nested", "path", "file.dat")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("file not found at expected path %q: %v", expectedPath, err)
	}
}

func TestLocalStoragePathTraversalBlocked(t *testing.T) {
	dir := t.TempDir()
	ls := NewLocalStorage(dir)

	// Create a sentinel file outside the base dir to detect escape
	sentinelDir := filepath.Dir(dir)
	sentinelPath := filepath.Join(sentinelDir, "gofastr-storage-traversal-test")
	os.WriteFile(sentinelPath, []byte("secret"), 0644)
	defer os.Remove(sentinelPath)

	ctx := context.Background()

	traversalKeys := []string{
		"../gofastr-storage-traversal-test",
		"foo/../../" + filepath.Base(dir) + "/../gofastr-storage-traversal-test",
		"..%2F..%2Fetc%2Fpasswd",
	}

	for _, key := range traversalKeys {
		if strings.Contains(key, "..") {
			// Keys with .. should be rejected
			err := ls.Save(ctx, key, bytes.NewReader([]byte("bad")))
			if err == nil {
				t.Errorf("Save(%q) should have been rejected, but succeeded", key)
			}
		}
	}

	// Absolute paths should also be rejected
	absKey := "/etc/passwd"
	err := ls.Save(ctx, absKey, bytes.NewReader([]byte("bad")))
	if err == nil {
		t.Errorf("Save(%q) should have been rejected, but succeeded", absKey)
	}
}

func TestLocalStorageAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	ls := NewLocalStorage(dir)

	ctx := context.Background()
	key := "atomic-test.txt"

	// Write a file
	content1 := []byte("initial content")
	if err := ls.Save(ctx, key, bytes.NewReader(content1)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Overwrite with new content
	content2 := []byte("updated content")
	if err := ls.Save(ctx, key, bytes.NewReader(content2)); err != nil {
		t.Fatalf("Save (overwrite): %v", err)
	}

	// Read back — should have the updated content, never partial
	rc, err := ls.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, content2) {
		t.Fatalf("content mismatch after overwrite: got %q, want %q", got, content2)
	}

	// Verify no leftover temp files
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".gofastr-tmp") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

func TestLocalStorageGetNotFound(t *testing.T) {
	dir := t.TempDir()
	ls := NewLocalStorage(dir)

	ctx := context.Background()
	_, err := ls.Get(ctx, "nonexistent.txt")
	if err == nil {
		t.Fatal("Get should return error for missing key")
	}
}

func TestLocalStorageDeleteNotFound(t *testing.T) {
	dir := t.TempDir()
	ls := NewLocalStorage(dir)

	ctx := context.Background()
	err := ls.Delete(ctx, "nonexistent.txt")
	if err != nil {
		t.Fatalf("Delete of nonexistent key should not error: %v", err)
	}
}

func TestLocalStorageCustomPermissions(t *testing.T) {
	dir := t.TempDir()
	ls := NewLocalStorage(dir, WithPermissions(0600))

	ctx := context.Background()
	key := "secret.txt"
	content := []byte("top secret")

	if err := ls.Save(ctx, key, bytes.NewReader(content)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, key))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Fatalf("file permissions: got %o, want 0600", perm)
	}
}

func TestLocalStorageCustomTempDir(t *testing.T) {
	dir := t.TempDir()
	tempDir := t.TempDir()
	ls := NewLocalStorage(dir, WithTempDir(tempDir))

	ctx := context.Background()
	key := "with-temp-dir.txt"
	content := []byte("content")

	if err := ls.Save(ctx, key, bytes.NewReader(content)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	rc, err := ls.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	rc.Close()
}

func TestLocalStorageCancelledContext(t *testing.T) {
	dir := t.TempDir()
	ls := NewLocalStorage(dir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := ls.Save(ctx, "cancelled.txt", bytes.NewReader([]byte("data")))
	if err == nil {
		t.Fatal("Save should fail with cancelled context")
	}
}

// ─── Memory Storage Tests ────────────────────────────────────────────

func TestMemoryStorageSaveGetDeleteRoundTrip(t *testing.T) {
	ms := NewMemoryStorage()
	ctx := context.Background()
	key := "test/memory-file.txt"
	content := []byte("in-memory content")

	// Save
	if err := ms.Save(ctx, key, bytes.NewReader(content)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Exists
	exists, err := ms.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Fatal("Exists returned false after Save")
	}

	// Get
	rc, err := ms.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("content mismatch: got %q, want %q", got, content)
	}

	// Delete
	if err := ms.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	exists, err = ms.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Exists after delete: %v", err)
	}
	if exists {
		t.Fatal("Exists returned true after Delete")
	}

	// Get after delete
	_, err = ms.Get(ctx, key)
	if err == nil {
		t.Fatal("Get should fail after Delete")
	}
}

func TestMemoryStorageOverwrite(t *testing.T) {
	ms := NewMemoryStorage()
	ctx := context.Background()
	key := "overwrite.txt"

	ms.Save(ctx, key, bytes.NewReader([]byte("v1")))
	ms.Save(ctx, key, bytes.NewReader([]byte("v2")))

	rc, _ := ms.Get(ctx, key)
	defer rc.Close()
	got, _ := io.ReadAll(rc)

	if string(got) != "v2" {
		t.Fatalf("expected overwritten content, got %q", got)
	}
}

func TestMemoryStorageEmptyKey(t *testing.T) {
	ms := NewMemoryStorage()
	err := ms.Save(context.Background(), "", bytes.NewReader([]byte("data")))
	if err == nil {
		t.Fatal("Save with empty key should fail")
	}
}

func TestMemoryStorageConcurrentAccess(t *testing.T) {
	ms := NewMemoryStorage()
	ctx := context.Background()

	const writers = 50
	const readers = 50

	var wg sync.WaitGroup
	wg.Add(writers + readers)

	errCh := make(chan error, writers+readers)

	// Concurrent writers
	for i := 0; i < writers; i++ {
		go func(id int) {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", id%10) // overlapping keys
			content := []byte(fmt.Sprintf("content-%d-%d", id, time.Now().UnixNano()))
			if err := ms.Save(ctx, key, bytes.NewReader(content)); err != nil {
				errCh <- err
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < readers; i++ {
		go func(id int) {
			defer wg.Done()
			key := fmt.Sprintf("key-%d", id%10)
			rc, err := ms.Get(ctx, key)
			if err != nil {
				// Key may not exist yet — that's fine
				return
			}
			defer rc.Close()
			if _, err := io.ReadAll(rc); err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent access error: %v", err)
	}
}

// ─── Registry Tests ──────────────────────────────────────────────────

func TestRegistry(t *testing.T) {
	// Register local backend
	Register(Local, func(config map[string]interface{}) (Storage, error) {
		dir, _ := config["dir"].(string)
		if dir == "" {
			dir = t.TempDir()
		}
		return NewLocalStorage(dir), nil
	})

	// Should be able to create via registry
	s, err := New(Local, map[string]interface{}{"dir": t.TempDir()})
	if err != nil {
		t.Fatalf("New(Local): %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil storage")
	}
}

func TestRegistryUnknownType(t *testing.T) {
	_, err := New(StorageType(99), nil)
	if err == nil {
		t.Fatal("New with unknown type should fail")
	}
}

// ─── S3 Storage Tests (interface only, no real client) ───────────────

func TestS3StorageNoClient(t *testing.T) {
	s := NewS3Storage("bucket", "us-east-1")
	ctx := context.Background()

	err := s.Save(ctx, "key", bytes.NewReader([]byte("data")))
	if err == nil {
		t.Fatal("Save without client should fail")
	}

	_, err = s.Get(ctx, "key")
	if err == nil {
		t.Fatal("Get without client should fail")
	}

	err = s.Delete(ctx, "key")
	if err == nil {
		t.Fatal("Delete without client should fail")
	}

	_, err = s.Exists(ctx, "key")
	if err == nil {
		t.Fatal("Exists without client should fail")
	}
}

// mockS3Client is a minimal mock for testing S3Storage with a client.
type mockS3Client struct {
	objects map[string][]byte
}

func newMockS3Client() *mockS3Client {
	return &mockS3Client{objects: make(map[string][]byte)}
}

func (m *mockS3Client) PutObject(_ context.Context, _, key string, r io.Reader, _ int64, _ string) error {
	data, _ := io.ReadAll(r)
	m.objects[key] = data
	return nil
}

func (m *mockS3Client) GetObject(_ context.Context, _, key string) (io.ReadCloser, error) {
	data, ok := m.objects[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *mockS3Client) DeleteObject(_ context.Context, _, key string) error {
	delete(m.objects, key)
	return nil
}

func (m *mockS3Client) HeadObject(_ context.Context, _, key string) (bool, error) {
	_, ok := m.objects[key]
	return ok, nil
}

func TestS3StorageWithClient(t *testing.T) {
	client := newMockS3Client()
	s := NewS3Storage("test-bucket", "us-east-1", WithS3Client(client))
	ctx := context.Background()
	key := "uploads/photo.jpg"
	content := []byte("fake image data")

	// Save
	if err := s.Save(ctx, key, bytes.NewReader(content)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Exists
	exists, err := s.Exists(ctx, key)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Fatal("Exists should be true after Save")
	}

	// Get
	rc, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("content mismatch: got %q, want %q", got, content)
	}

	// Delete
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	exists, _ = s.Exists(ctx, key)
	if exists {
		t.Fatal("Exists should be false after Delete")
	}
}

// ─── S3 Key Validation Tests ────────────────────────────────────────

func TestS3KeyValidationEmptyKey(t *testing.T) {
	client := newMockS3Client()
	s := NewS3Storage("bucket", "us-east-1", WithS3Client(client))
	ctx := context.Background()

	// Save with empty key should fail.
	if err := s.Save(ctx, "", bytes.NewReader([]byte("data"))); err == nil {
		t.Error("Save with empty key should fail")
	}

	// Get with empty key should fail.
	if _, err := s.Get(ctx, ""); err == nil {
		t.Error("Get with empty key should fail")
	}

	// Delete with empty key should fail.
	if err := s.Delete(ctx, ""); err == nil {
		t.Error("Delete with empty key should fail")
	}

	// Exists with empty key should fail.
	if _, err := s.Exists(ctx, ""); err == nil {
		t.Error("Exists with empty key should fail")
	}
}

func TestS3KeyValidationPathTraversal(t *testing.T) {
	client := newMockS3Client()
	s := NewS3Storage("bucket", "us-east-1", WithS3Client(client))
	ctx := context.Background()

	traversalKeys := []string{"../etc/passwd", "foo/../../secret", ".."}
	for _, key := range traversalKeys {
		if err := s.Save(ctx, key, bytes.NewReader([]byte("data"))); err == nil {
			t.Errorf("Save(%q) should reject path traversal", key)
		}
		if _, err := s.Get(ctx, key); err == nil {
			t.Errorf("Get(%q) should reject path traversal", key)
		}
		if err := s.Delete(ctx, key); err == nil {
			t.Errorf("Delete(%q) should reject path traversal", key)
		}
		if _, err := s.Exists(ctx, key); err == nil {
			t.Errorf("Exists(%q) should reject path traversal", key)
		}
	}
}

func TestS3PresignedKeyValidation(t *testing.T) {
	s := NewS3Storage("bucket", "us-east-1", WithPresigner(&mockPresigner{}))
	ctx := context.Background()

	// Empty key.
	if _, err := s.PresignedGetURL(ctx, "", time.Hour); err == nil {
		t.Error("PresignedGetURL with empty key should fail")
	}
	if _, err := s.PresignedPutURL(ctx, "", time.Hour); err == nil {
		t.Error("PresignedPutURL with empty key should fail")
	}

	// Path traversal.
	if _, err := s.PresignedGetURL(ctx, "../secret", time.Hour); err == nil {
		t.Error("PresignedGetURL with traversal key should fail")
	}
	if _, err := s.PresignedPutURL(ctx, "../secret", time.Hour); err == nil {
		t.Error("PresignedPutURL with traversal key should fail")
	}
}

// mockPresigner is a minimal mock for testing presigned URL key validation.
type mockPresigner struct{}

func (m *mockPresigner) PresignGet(_ context.Context, _, _ string, _ time.Duration) (*url.URL, error) {
	return &url.URL{Scheme: "https", Host: "s3.amazonaws.com", Path: "/bucket/key"}, nil
}

func (m *mockPresigner) PresignPut(_ context.Context, _, _ string, _ time.Duration) (*url.URL, error) {
	return &url.URL{Scheme: "https", Host: "s3.amazonaws.com", Path: "/bucket/key"}, nil
}

// ─── StorageType String ─────────────────────────────────────────────

func TestStorageTypeString(t *testing.T) {
	tests := []struct {
		t    StorageType
		want string
	}{
		{Local, "local"},
		{S3, "s3"},
		{Memory, "memory"},
		{StorageType(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.t.String(); got != tt.want {
			t.Errorf("StorageType(%d).String() = %q, want %q", tt.t, got, tt.want)
		}
	}
}
