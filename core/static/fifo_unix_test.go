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
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	h := Handler(Config{FS: os.DirFS(dir)})
	done := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pipe", nil))
		done <- rec.Code
	}()
	select {
	case code := <-done:
		if code != http.StatusNotFound {
			t.Errorf("GET /pipe = %d, want 404", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the request blocked on a FIFO")
	}
}
