//go:build unix

package static

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestServeRefusesFIFO(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"pipe", "docs/index.html"} {
		if err := syscall.Mkfifo(filepath.Join(dir, filepath.FromSlash(p)), 0o644); err != nil {
			t.Skipf("mkfifo: %v", err)
		}
	}
	h := Handler(Config{FS: os.DirFS(dir)})
	for _, target := range []string{"/pipe", "/docs", "/docs/"} {
		done := make(chan int, 1)
		go func() {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
			done <- rec.Code
		}()
		select {
		case code := <-done:
			if code != http.StatusNotFound {
				t.Errorf("GET %s = %d, want 404", target, code)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("GET %s blocked on a FIFO", target)
		}
	}
}
