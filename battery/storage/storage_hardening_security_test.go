package storage_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/storage"
	"github.com/DonaldMurillo/gofastr/core/upload"
)

func TestLocalStorage_DefaultFilesAreNotWorldReadable(t *testing.T) {
	requirePOSIXPermissions(t)
	t.Parallel()
	dir := t.TempDir()
	ls := storage.NewLocalStorage(dir)

	if err := ls.Save(context.Background(), "private/report.txt", strings.NewReader("secret")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "private", "report.txt"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if info.Mode().Perm()&0o044 != 0 {
		t.Fatalf("SECURITY: [storage-local] default saved file mode is too permissive: %o", info.Mode().Perm())
	}
}

func TestLocalStorage_DefaultDirectoriesAreNotWorldTraversable(t *testing.T) {
	requirePOSIXPermissions(t)
	t.Parallel()
	dir := t.TempDir()
	ls := storage.NewLocalStorage(dir)

	if err := ls.Save(context.Background(), "tenant-a/private/report.txt", strings.NewReader("secret")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "tenant-a", "private"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if info.Mode().Perm()&0o055 != 0 {
		t.Fatalf("SECURITY: [storage-local] default directory mode is too permissive: %o", info.Mode().Perm())
	}
}

// Property (CHAIN8-R6): ServeHandler's documented error classification
// ("Missing keys are 404; keys rejected by backend sanitization
// (traversal / escaped paths) are 400; anything else is a generic 500" —
// core/upload/serve.go serveStoreError) holds for the storage backends
// the docs tell hosts to mount (storage.md wires battery/storage via
// WithFileStorage; uploads.md mounts ServeHandler over that storage).
// battery/storage's not-found and key-validation errors are plain
// fmt.Errorf values that wrap neither upload.ErrNotFound nor
// os.ErrNotExist, so a missing object answers 500 "internal server
// error" and a traversal-rejected key answers 500, defeating clients
// and caches distinguishing gone (404) from broken (500) on the
// documented wiring. Nothing is leaked either way — this is a contract
// and availability defect, which is why it is pinned at the backend
// boundary rather than asserted against either package's internals.
func TestServeHandlerErrorContractBackends(t *testing.T) {
	t.Parallel()

	seed := func(t *testing.T, s storage.Storage) {
		t.Helper()
		if err := s.Save(context.Background(), "reports/q1.txt", strings.NewReader("data")); err != nil {
			t.Fatalf("seed Save: %v", err)
		}
	}

	cases := []struct {
		name string
		make func(*testing.T) storage.Storage
		key  string
		want int
	}{
		{
			name: "battery LocalStorage missing key is 404",
			make: func(t *testing.T) storage.Storage {
				ls := storage.NewLocalStorage(t.TempDir())
				seed(t, ls)
				return ls
			},
			key:  "reports/missing.txt",
			want: http.StatusNotFound,
		},
		{
			name: "battery MemoryStorage missing key is 404",
			make: func(t *testing.T) storage.Storage {
				ms := storage.NewMemoryStorage()
				seed(t, ms)
				return ms
			},
			key:  "reports/missing.txt",
			want: http.StatusNotFound,
		},
		{
			name: "battery LocalStorage traversal key is 400",
			make: func(t *testing.T) storage.Storage {
				ls := storage.NewLocalStorage(t.TempDir())
				seed(t, ls)
				return ls
			},
			key:  "reports/../../etc/passwd",
			want: http.StatusBadRequest,
		},
		{
			name: "core/upload LocalStorage missing key is 404 (control)",
			make: func(t *testing.T) storage.Storage {
				ls := upload.NewLocalStorage(t.TempDir())
				seed(t, ls)
				return ls
			},
			key:  "reports/missing.txt",
			want: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			handler := upload.ServeHandler(tc.make(t))
			rec := httptest.NewRecorder()
			handler(rec, httptest.NewRequest(http.MethodGet, "/"+tc.key, nil))

			if rec.Code != tc.want {
				t.Fatalf("SECURITY: [upload-serve-contract] GET %q through %s: got %d, want %d (body %q). Attack surface: the documented 404/400 classification of ServeHandler is false for the documented battery/storage wiring — untyped backend errors fall to the 500 arm, so gone and broken are indistinguishable to clients and caches (serveStoreError matches only upload.ErrNotFound/upload.ErrInvalidKey sentinels).", tc.key, tc.name, rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
