package clitest

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func FreeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

// output is any Stringer so callers can pass the mutex-guarded syncBuffer.
// os/exec copies a child's output from its own goroutines until Wait returns,
// so reading a plain bytes.Buffer here races the copier; taking the interface
// lets a caller hand over a writer that is safe to read mid-run.
func WaitForHTTP(t *testing.T, url string, output fmt.Stringer) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("server did not become ready at %s\n%s", url, output.String())
}

func WriteTestFile(t *testing.T, path, content string) {
	t.Helper()
	//gofastr:allow(worldreadable) test fixture in the calling test's temp dir
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// WriteFileAll writes body to path, creating its parent directories.
func WriteFileAll(t *testing.T, path, body string) {
	t.Helper()
	//gofastr:allow(worldreadable) test fixture in the calling test's temp dir
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	//gofastr:allow(worldreadable) test fixture in the calling test's temp dir
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func AssertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("missing %q in:\n%s", needle, haystack)
	}
}

// CaptureStdout is CovT_capStdout under the name the appspec-era tests use.
func CaptureStdout(t *testing.T, fn func()) string {
	return CovT_capStdout(t, fn)
}
