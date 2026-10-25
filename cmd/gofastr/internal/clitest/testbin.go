package clitest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// SharedGofastrBin is the one gofastr binary the whole test package drives.
// Twenty tests used to run `go build -o <tempdir>/gofastr .` each; the
// compile is served from the build cache, but every link of the full CLI
// (framework + cgo sqlite) still costs seconds, and the suite paid it per
// test. Build once, on first use, and let TestMain remove the directory.
var SharedGofastrBin struct {
	once sync.Once
	Dir  string
	path string
	err  error
}

// GofastrTestBinary returns the path of the shared gofastr binary, building
// it on the first call. The binary is built from the working tree's
// cmd/gofastr, never a stale `gofastr` on PATH, by import path from the
// module root: the calling package's directory is not the main package.
func GofastrTestBinary() (string, error) {
	SharedGofastrBin.once.Do(func() {
		if moduleRoot == "" {
			SharedGofastrBin.err = fmt.Errorf("clitest: no go.mod at or above the test's working directory")
			return
		}
		dir, err := os.MkdirTemp("", "gofastr-testbin-")
		if err != nil {
			SharedGofastrBin.err = err
			return
		}
		SharedGofastrBin.Dir = dir
		bin := TestExecutablePath(filepath.Join(dir, "gofastr"))
		build := exec.Command("go", "build", "-o", bin, "github.com/DonaldMurillo/gofastr/cmd/gofastr")
		build.Dir = moduleRoot
		if out, err := build.CombinedOutput(); err != nil {
			SharedGofastrBin.err = fmt.Errorf("go build ./cmd/gofastr: %w\n%s", err, out)
			os.RemoveAll(dir)
			SharedGofastrBin.Dir = ""
			return
		}
		SharedGofastrBin.path = bin
	})
	return SharedGofastrBin.path, SharedGofastrBin.err
}
