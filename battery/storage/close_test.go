package storage_test

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/storage"
	"github.com/DonaldMurillo/gofastr/core/upload"
)

func TestLocalStorageCloseReleasesDirectory(t *testing.T) {
	type backend interface {
		Save(context.Context, string, io.Reader) error
		Close() error
	}
	for _, name := range []string{"storage", "upload"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			var store backend
			if name == "storage" {
				store = storage.NewLocalStorage(dir)
			} else {
				store = upload.NewLocalStorage(dir)
			}
			t.Cleanup(func() { store.Close() })
			if err := store.Save(context.Background(), "object", strings.NewReader("content")); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatalf("second Close: %v", err)
			}
			if err := os.RemoveAll(dir); err != nil {
				t.Fatalf("closed storage still holds its directory: %v", err)
			}
		})
	}
}
