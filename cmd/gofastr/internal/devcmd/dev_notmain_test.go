package devcmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
)

// `gofastr dev` on a directory whose package is not main (a library, or
// an app whose main lives under cmd/) built it, since go build -o writes
// a non-main package's archive, then failed to exec the archive with
// "fork/exec /tmp/gofastr-dev-server-…/server: permission denied". The
// failure now names the package and points at --pkg.
func TestDevNamesANonMainPackage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module devlib\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lib.go"), []byte("package devlib\n\nfunc X() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, err := isolation.Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(DevServerBinaryPath(rt)) })
	var mu sync.Mutex
	var cmd *exec.Cmd
	var ok bool
	out := clitest.CovT_capStdout(t, func() {
		ok = buildAndServe(dir, ".", "localhost:0", rt, &mu, &cmd, true, nil, nil)
	})
	KillServer(&mu, &cmd)
	if ok {
		t.Fatalf("buildAndServe started a non-main package:\n%s", out)
	}
	if !strings.Contains(out, "package devlib, not a main package") || !strings.Contains(out, "--pkg") {
		t.Fatalf("the failure does not name the package or point at --pkg:\n%s", out)
	}
}
