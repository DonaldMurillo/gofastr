package clitest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// SharedGofastrBin is the one gofastr binary a test package drives.
var SharedGofastrBin struct {
	once sync.Once
	Dir  string
	path string
	err  error
}

// GofastrTestBinary returns the shared binary, built from the working tree on the first call.
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
