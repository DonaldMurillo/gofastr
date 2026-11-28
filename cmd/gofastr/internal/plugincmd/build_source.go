package plugincmd

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/core/cliplugin"
)

func buildProjectPlugin(ctx context.Context, dir, goPackage string) (path string, cleanup func(), err error) {
	tmp, err := os.MkdirTemp("", "gofastr-build-plugin-*")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { _ = os.RemoveAll(tmp) }
	path = filepath.Join(tmp, "plugin.exe")
	cmd := exec.CommandContext(ctx, "go", "build", "-mod=readonly", "-buildvcs=false", "-trimpath", "-o", path, goPackage)
	cmd.Dir = dir
	cmd.Env = append(cliplugin.BuildExecEnv(cliplugin.EnvOptions{Environ: os.Environ()}),
		"GOOS="+runtime.GOOS, "GOARCH="+runtime.GOARCH, "CGO_ENABLED=0")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cli.Info("Building project plugin %s...", goPackage)
	if err := cmd.Run(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("build Go package %s: %w", goPackage, err)
	}
	return path, cleanup, nil
}

// Copy the exact bytes that passed the manifest probe to the derived cache
// path. os.Root prevents a symlink in the checkout from redirecting the write.
func installProjectPlugin(dir, dest, snapshot string) error {
	project, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer project.Close()
	if err := project.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	src, err := os.Open(snapshot)
	if err != nil {
		return err
	}
	defer src.Close()
	tmp := filepath.Join(filepath.Dir(dest), ".plugin-"+rand.Text())
	dst, err := project.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	defer project.Remove(tmp)
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}
	return project.Rename(tmp, dest)
}
