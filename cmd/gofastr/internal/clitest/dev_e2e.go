package clitest

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// FreeAddr returns a loopback host:port that was free a moment ago.
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

// WaitForHTTP polls url for 30 seconds until it answers below 500, then
// fails the test with output, which must be safe to read while a child
// process writes it (a SyncBuffer).
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

// NextE2EPort returns a loopback port that was free a moment ago.
func NextE2EPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return strconv.Itoa(port)
}

// DevTempEnv is the environment for a spawned `gofastr dev`: temp
// directories under the test's own, and an empty axe-coverage root. Call it
// before registering the cleanup that kills the process: cleanups run last-in
// first-out, so the process is killed before its temp directories are removed.
func DevTempEnv(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	return []string{"TMPDIR=" + dir, "TMP=" + dir, "TEMP=" + dir, "GOFASTR_AXE_COVERAGE_DIR=" + t.TempDir()}
}

// BuildGofastrBinary returns GofastrTestBinary's path, failing the test on error.
func BuildGofastrBinary(t *testing.T) string {
	t.Helper()
	bin, err := GofastrTestBinary()
	if err != nil {
		t.Fatalf("build gofastr: %v", err)
	}
	return bin
}

// Truncate cuts s to n bytes and marks the cut.
func Truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// SyncBuffer is a writer a child process may write while the test reads it.
type SyncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *SyncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *SyncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
