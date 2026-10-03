package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// sharedGofastrBin is the one gofastr binary the whole test package drives.
// Twenty tests used to run `go build -o <tempdir>/gofastr .` each; the
// compile is served from the build cache, but every link of the full CLI
// (framework + cgo sqlite) still costs seconds, and the suite paid it per
// test. Build once, on first use, and let TestMain remove the directory.
var sharedGofastrBin struct {
	once sync.Once
	dir  string
	path string
	err  error
}

// gofastrTestBinary returns the path of the shared gofastr binary, building
// it on the first call. The binary is built from this package's source, so
// it tests the working tree, never a stale `gofastr` on PATH.
func gofastrTestBinary() (string, error) {
	sharedGofastrBin.once.Do(func() {
		dir, err := os.MkdirTemp("", "gofastr-testbin-")
		if err != nil {
			sharedGofastrBin.err = err
			return
		}
		sharedGofastrBin.dir = dir
		bin := testExecutablePath(filepath.Join(dir, "gofastr"))
		build := exec.Command("go", "build", "-o", bin, ".")
		if out, err := build.CombinedOutput(); err != nil {
			sharedGofastrBin.err = fmt.Errorf("go build ./cmd/gofastr: %w\n%s", err, out)
			return
		}
		sharedGofastrBin.path = bin
	})
	return sharedGofastrBin.path, sharedGofastrBin.err
}

// removeSharedGofastrBin deletes the shared binary's directory. TestMain
// calls it after m.Run; a test must never call it.
func removeSharedGofastrBin() {
	if sharedGofastrBin.dir != "" {
		os.RemoveAll(sharedGofastrBin.dir)
	}
}
