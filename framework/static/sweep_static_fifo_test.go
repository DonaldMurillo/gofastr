//go:build unix

package static

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

func TestSweepStaticSkipsFIFO(t *testing.T) {
	src := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(src, "pipe"), 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	for name, mk := range map[string]func() *Builder{
		"static dir": func() *Builder { return &Builder{Host: uihost.New(sweepFIFOApp(), uihost.WithStaticDir(src))} },
		"static fs": func() *Builder {
			h := uihost.New(sweepFIFOApp())
			h.SetStaticFS(os.DirFS(src))
			return &Builder{Host: h}
		},
		"extra dir": func() *Builder {
			return &Builder{Host: uihost.New(sweepFIFOApp()), ExtraDirs: map[string]fs.FS{"/x": os.DirFS(src)}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			b := mk()
			b.OutDir = t.TempDir()
			done := make(chan error, 1)
			go func() { _, err := b.Build(context.Background()); done <- err }()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("Build blocked on a FIFO in the source")
			}
			if _, err := os.Stat(filepath.Join(b.OutDir, "pipe")); !errors.Is(err, fs.ErrNotExist) {
				t.Error("the FIFO was published")
			}
		})
	}
}

func sweepFIFOApp() *coreapp.App {
	a := coreapp.NewApp("FIFO")
	a.Register("/", &homeScreen{}, nil)
	return a
}
