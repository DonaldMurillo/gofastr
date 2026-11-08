package storage_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/storage"
	"github.com/DonaldMurillo/gofastr/core/upload"
)

type heldUpload struct {
	entered chan struct{}
	release chan struct{}
	data    io.Reader
	once    bool
}

func (r *heldUpload) Read(p []byte) (int, error) {
	if !r.once {
		r.once = true
		close(r.entered)
		<-r.release
	}
	return r.data.Read(p)
}

func TestFreshSymlinkUploadsKeepRoot(t *testing.T) {
	type backend interface {
		Save(context.Context, string, io.Reader) error
		Close() error
	}
	for _, kind := range []string{"upload", "storage"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			real := filepath.Join(dir, "real")
			if err := os.Mkdir(real, 0o700); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(dir, "link")
			if err := os.Symlink(real, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			base := filepath.Join(link, "fresh")
			var store backend = upload.NewLocalStorage(base)
			if kind == "storage" {
				store = storage.NewLocalStorage(base)
			}
			defer store.Close()
			r := &heldUpload{entered: make(chan struct{}), release: make(chan struct{}), data: strings.NewReader("first upload")}
			done := make(chan error, 1)
			go func() { done <- store.Save(t.Context(), "first.txt", r) }()
			select {
			case <-r.entered:
			case err := <-done:
				t.Fatalf("first upload never copied: %v", err)
			case <-time.After(3 * time.Second):
				t.Fatal("first upload stalled")
			}
			secondErr := store.Save(t.Context(), "other.txt", strings.NewReader("other upload"))
			close(r.release)
			firstErr := <-done
			if firstErr != nil || secondErr != nil {
				t.Fatalf("concurrent saves: first=%v second=%v", firstErr, secondErr)
			}
			entries, err := os.ReadDir(base)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 2 {
				t.Fatalf("staging file left behind: %v", entries)
			}
			got, err := os.ReadFile(filepath.Join(base, "first.txt"))
			if err != nil || string(got) != "first upload" {
				t.Fatalf("first object=%q err=%v", got, err)
			}
		})
	}
}
