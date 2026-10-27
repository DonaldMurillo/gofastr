package storage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/storage"
	"github.com/DonaldMurillo/gofastr/core/upload"
)

// gateReader blocks its first Read until release closes and reports on
// started that Save is past its up-front collision check.
type gateReader struct {
	started, release chan struct{}
	r                io.Reader
	once             bool
}

func (g *gateReader) Read(p []byte) (int, error) {
	if !g.once {
		g.once = true
		close(g.started)
		<-g.release
	}
	return g.r.Read(p)
}

// Overlapping saves under spellings that fold together: the second rename
// is refused on both local backends instead of overwriting the first.
func TestFoldedKeyRaceRefusedOnBoth(t *testing.T) {
	type backend interface {
		Save(context.Context, string, io.Reader) error
		Close() error
	}
	for _, name := range []string{"storage", "upload"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "probe-A"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(dir, "probe-a")); err != nil {
				t.Skip("filesystem is case-sensitive; distinct keys cannot alias here")
			}
			var store backend
			if name == "storage" {
				store = storage.NewLocalStorage(dir)
			} else {
				store = upload.NewLocalStorage(dir)
			}
			t.Cleanup(func() { store.Close() })
			ctx := context.Background()
			slow := &gateReader{started: make(chan struct{}), release: make(chan struct{}), r: strings.NewReader("first")}
			done := make(chan error, 1)
			go func() { done <- store.Save(ctx, "t/Report.txt", slow) }()
			<-slow.started
			if err := store.Save(ctx, "t/report.txt", bytes.NewReader([]byte("second"))); err != nil {
				t.Fatalf("save report.txt: %v", err)
			}
			close(slow.release)
			if err := <-done; !errors.Is(err, upload.ErrInvalidKey) {
				t.Fatalf("overlapping save = %v, want ErrInvalidKey", err)
			}
			got, err := os.ReadFile(filepath.Join(dir, "t", "report.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "second" {
				t.Fatalf("report.txt holds %q, want %q", got, "second")
			}
		})
	}
}
