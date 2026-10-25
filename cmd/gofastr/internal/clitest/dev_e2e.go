package clitest

import (
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
)

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

// DevTempEnv points a spawned `gofastr dev` at a temp root the test
// owns. The dev server builds its binary under os.MkdirTemp and removes
// that dir only on a clean shutdown; the harnesses SIGKILL it, so the
// dir would outlive every run. Under t.TempDir it goes with the test.
// Call it before registering the kill cleanup: cleanups run last-in
// first-out, so the process is dead before its dir is removed.
//
// It also gives the child an empty axe-coverage root. Strict mode checks
// the manifest only under `gofastr dev`, and every example shares the
// module root's manifest (or the one cmd/affected points outside the
// module): one example's axe scans, or a run that failed midway, leave a
// manifest missing another example's screens, and that example's strict
// boot panics in a test about the dev loop. No manifest is the warn
// posture.
func DevTempEnv(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	return []string{"TMPDIR=" + dir, "TMP=" + dir, "TEMP=" + dir, "GOFASTR_AXE_COVERAGE_DIR=" + t.TempDir()}
}

// BuildGofastrBinary returns the package-wide shared gofastr binary
// (testbin_test.go); the name is kept for the e2e call sites.
func BuildGofastrBinary(t *testing.T) string {
	t.Helper()
	bin, err := GofastrTestBinary()
	if err != nil {
		t.Fatalf("build gofastr: %v", err)
	}
	return bin
}

func Truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// SyncBuffer is a mutex-guarded writer for capturing live child-process
// output: os/exec's copy goroutine writes while the test goroutine reads on
// failure paths, which races on a bare strings.Builder under -race.
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
