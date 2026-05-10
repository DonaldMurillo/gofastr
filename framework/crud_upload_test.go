package framework

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofastr/gofastr/core/schema"
	"github.com/gofastr/gofastr/core/upload"
	_ "github.com/mattn/go-sqlite3"
)

// setupUploadDB creates a posts table with an avatar TEXT column to receive
// the URL of the uploaded file.
func setupUploadDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE posts (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		avatar TEXT DEFAULT ''
	)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	return db
}

// uploadApp registers a posts entity with one Image field (avatar) and wires a
// LocalStorage backed by a temp directory.
func uploadApp(t *testing.T, db *sql.DB) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	store := upload.NewLocalStorage(dir)
	app := NewApp(WithDB(db), WithoutDefaultMiddleware(), WithFileStorage(store))
	app.Entity("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "avatar", Type: schema.Image},
		},
	}.WithTimestamps(false))
	return app, dir
}

// buildMultipartBody assembles a multipart form body. files map field name →
// (filename, content). values map name → string.
func buildMultipartBody(t *testing.T, files map[string][2]string, values map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for name, val := range values {
		if err := mw.WriteField(name, val); err != nil {
			t.Fatalf("write field %q: %v", name, err)
		}
	}
	for name, fc := range files {
		filename, content := fc[0], fc[1]
		fw, err := mw.CreateFormFile(name, filename)
		if err != nil {
			t.Fatalf("create form file %q: %v", name, err)
		}
		if _, err := io.WriteString(fw, content); err != nil {
			t.Fatalf("write file %q: %v", name, err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close mw: %v", err)
	}
	return &buf, mw.FormDataContentType()
}

// ============================================================================
// Test: multipart create stores the file and persists the URL on the entity
// ============================================================================

func TestUpload_Create_StoresFileAndPersistsURL(t *testing.T) {
	db := setupUploadDB(t)
	app, dir := uploadApp(t, db)
	ta := TestHarness(t, app)

	body, ct := buildMultipartBody(t,
		map[string][2]string{"avatar": {"hello.png", "fake-png-bytes"}},
		map[string]string{"title": "Hello"},
	)

	resp := ta.Request(http.MethodPost, "/posts", nil).
		WithHeader("Content-Type", ct).
		WithBody(body).
		Execute()

	resp.AssertStatus(t, http.StatusCreated)

	var got map[string]any
	if err := json.Unmarshal([]byte(resp.Body()), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	avatar, _ := got["avatar"].(string)
	if !strings.HasPrefix(avatar, "uploads/posts/avatar/") {
		t.Fatalf("expected avatar URL under uploads/posts/avatar/, got %q", avatar)
	}

	// File must exist on disk
	stored := filepath.Join(dir, avatar)
	if _, err := readFile(stored); err != nil {
		t.Fatalf("expected stored file at %s: %v", stored, err)
	}
}

// ============================================================================
// Test: multipart request with no Storage configured returns 400 with a clear msg
// ============================================================================

func TestUpload_NoStorage_RejectsMultipart(t *testing.T) {
	db := setupUploadDB(t)
	// Build app without WithFileStorage
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "avatar", Type: schema.Image},
		},
	}.WithTimestamps(false))
	ta := TestHarness(t, app)

	body, ct := buildMultipartBody(t,
		map[string][2]string{"avatar": {"hello.png", "fake"}},
		map[string]string{"title": "Hello"},
	)
	resp := ta.Request(http.MethodPost, "/posts", nil).
		WithHeader("Content-Type", ct).
		WithBody(body).
		Execute()
	resp.AssertStatus(t, http.StatusBadRequest).
		AssertBodyContains(t, "no file storage configured")
}

// ============================================================================
// Test: multipart create still validates required non-file fields
// ============================================================================

func TestUpload_MissingRequiredField_400(t *testing.T) {
	db := setupUploadDB(t)
	app, _ := uploadApp(t, db)
	ta := TestHarness(t, app)

	body, ct := buildMultipartBody(t,
		map[string][2]string{"avatar": {"a.png", "ok"}},
		map[string]string{}, // no title
	)
	resp := ta.Request(http.MethodPost, "/posts", nil).
		WithHeader("Content-Type", ct).
		WithBody(body).
		Execute()
	resp.AssertStatus(t, http.StatusBadRequest)
}

// ============================================================================
// Test: multipart update replaces the avatar URL on the existing record
// ============================================================================

func TestUpload_Update_ReplacesURL(t *testing.T) {
	db := setupUploadDB(t)
	if _, err := db.Exec("INSERT INTO posts(id, title, avatar) VALUES (?, ?, ?)", "p1", "Original", "old-url"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	app, dir := uploadApp(t, db)
	ta := TestHarness(t, app)

	body, ct := buildMultipartBody(t,
		map[string][2]string{"avatar": {"new.png", "new-bytes"}},
		map[string]string{"title": "Updated"},
	)
	resp := ta.Request(http.MethodPut, "/posts/p1", nil).
		WithHeader("Content-Type", ct).
		WithBody(body).
		Execute()
	resp.AssertStatus(t, http.StatusOK)

	var got string
	if err := db.QueryRow("SELECT avatar FROM posts WHERE id = ?", "p1").Scan(&got); err != nil {
		t.Fatalf("read avatar: %v", err)
	}
	if !strings.HasPrefix(got, "uploads/posts/avatar/") {
		t.Fatalf("expected new avatar URL, got %q", got)
	}
	stored := filepath.Join(dir, got)
	if _, err := readFile(stored); err != nil {
		t.Fatalf("expected new file on disk at %s: %v", stored, err)
	}
}

// ============================================================================
// Test: JSON requests still work after multipart wiring (regression pin)
// ============================================================================

func TestUpload_JSONStillWorks(t *testing.T) {
	db := setupUploadDB(t)
	app, _ := uploadApp(t, db)
	ta := TestHarness(t, app)

	resp := ta.Post("/posts", map[string]any{"title": "JSON Path", "avatar": "external://url"})
	resp.AssertStatus(t, http.StatusCreated)

	var got map[string]any
	if err := json.Unmarshal([]byte(resp.Body()), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["avatar"] != "external://url" {
		t.Fatalf("expected JSON-supplied avatar to round-trip, got %v", got["avatar"])
	}
}

// ============================================================================
// Test: form values are coerced to schema types (bool, int)
// ============================================================================

func TestUpload_CoercesFormValues(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT NOT NULL, views INTEGER, published BOOLEAN)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	dir := t.TempDir()
	store := upload.NewLocalStorage(dir)
	app := NewApp(WithDB(db), WithoutDefaultMiddleware(), WithFileStorage(store))
	app.Entity("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "views", Type: schema.Int},
			{Name: "published", Type: schema.Bool},
		},
	}.WithTimestamps(false))
	ta := TestHarness(t, app)

	body, ct := buildMultipartBody(t,
		nil,
		map[string]string{"title": "Coerced", "views": "42", "published": "true"},
	)
	resp := ta.Request(http.MethodPost, "/posts", nil).
		WithHeader("Content-Type", ct).
		WithBody(body).
		Execute()
	resp.AssertStatus(t, http.StatusCreated)

	var views int
	var pub bool
	if err := db.QueryRow("SELECT views, published FROM posts").Scan(&views, &pub); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if views != 42 {
		t.Fatalf("expected views=42, got %d", views)
	}
	if !pub {
		t.Fatalf("expected published=true, got %v", pub)
	}
}

// readFile reads a file from disk for assertion purposes.
func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
