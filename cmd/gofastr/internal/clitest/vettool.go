package clitest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

var sharedVettool struct {
	once sync.Once
	path string
	err  error
}

// RepoVettool returns the repo's cmd/vettool, built on the first call.
func RepoVettool() (string, error) {
	sharedVettool.once.Do(func() {
		if moduleRoot == "" {
			sharedVettool.err = fmt.Errorf("clitest: no go.mod at or above the test's working directory")
			return
		}
		dir, err := os.MkdirTemp("", "vettool-")
		if err != nil {
			sharedVettool.err = err
			return
		}
		bin := TestExecutablePath(filepath.Join(dir, "vettool"))
		build := exec.Command("go", "build", "-buildvcs=false", "-o", bin, "github.com/DonaldMurillo/gofastr/cmd/vettool")
		build.Dir = moduleRoot
		if out, err := build.CombinedOutput(); err != nil {
			sharedVettool.err = fmt.Errorf("go build ./cmd/vettool: %w\n%s", err, out)
			return
		}
		sharedVettool.path = bin
	})
	return sharedVettool.path, sharedVettool.err
}
