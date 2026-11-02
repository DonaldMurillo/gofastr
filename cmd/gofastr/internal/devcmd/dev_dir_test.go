package devcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// `gofastr dev --dir <missing>` started the loop anyway: the first build
// failed and "Fixing and saving will retry" waited forever on a tree that
// does not exist. A --dir that is not a directory is refused up front.
func TestDevRefusesMissingDir(t *testing.T) {
	file := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Join(t.TempDir(), "missing"), file} {
		type result struct {
			code int
			out  string
		}
		done := make(chan result, 1)
		go func() {
			var r result
			r.out = clitest.CovT_capStdout(t, func() {
				r.code = clitest.CovT_capExit(t, func() { RunDev([]string{"--dir", dir, "-p", "0", "--no-a11y"}) })
			})
			done <- r
		}()
		select {
		case r := <-done:
			if r.code != 1 || !strings.Contains(r.out, dir) {
				t.Errorf("dev --dir %s: exit %d, want 1 naming the dir:\n%s", dir, r.code, r.out)
			}
			if strings.Contains(r.out, "Dev server with hot reload") {
				t.Errorf("dev --dir %s started the loop:\n%s", dir, r.out)
			}
		case <-time.After(20 * time.Second):
			t.Fatalf("dev --dir %s is still running: it should refuse the directory", dir)
		}
	}
}
