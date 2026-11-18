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
	if err := os.Symlink("pipe", filepath.Join(src, "alias")); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		dir  string
		mk   func() *Builder
	}{
		{"static dir", "", func() *Builder { return &Builder{Host: uihost.New(sweepFIFOApp(), uihost.WithStaticDir(src))} }},
		{"static fs", "", func() *Builder {
			h := uihost.New(sweepFIFOApp())
			h.SetStaticFS(os.DirFS(src))
			return &Builder{Host: h}
		}},
		{"extra dir", "x", func() *Builder {
			return &Builder{Host: uihost.New(sweepFIFOApp()), ExtraDirs: map[string]fs.FS{"/x": os.DirFS(src)}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := tc.mk()
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
			for _, name := range sweepFIFONames {
				if _, err := os.Lstat(filepath.Join(b.OutDir, tc.dir, name)); !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("%s was published", filepath.Join(tc.dir, name))
				}
			}
		})
	}
}

var sweepFIFONames = []string{"pipe", "alias"}

func sweepFIFOApp() *coreapp.App {
	a := coreapp.NewApp("FIFO")
	a.Register("/", &homeScreen{}, nil)
	return a
}

func TestSweepStaticIrregularSourceKeepsGenerated(t *testing.T) {
	src := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(src, "robots.txt"), 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	if err := os.Mkdir(filepath.Join(src, "sitemap.xml"), 0o755); err != nil {
		t.Fatal(err)
	}
	host := uihost.New(sweepFIFOApp(), uihost.WithStaticDir(src),
		uihost.WithSitemap(uihost.SitemapConfig{BaseURL: "https://example.com"}),
		uihost.WithRobots(uihost.RobotsConfig{}))
	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"robots.txt", "sitemap.xml"} {
		if fi, err := os.Stat(filepath.Join(out, name)); err != nil || !fi.Mode().IsRegular() {
			t.Errorf("%s: a source entry that is not a regular file kept the generated file out (%v)", name, err)
		}
	}
}
