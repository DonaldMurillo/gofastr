package storage

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/upload"
)

// The twin of core/upload's TestDirectoryKeyIsNotAnObject: a key naming
// a directory of the store is a prefix, not an object, so Get, GetRange
// and Exists report it not found instead of handing back the directory.
func TestLocalDirectoryKeyIsNotAnObject(t *testing.T) {
	ctx := context.Background()
	s := NewLocalStorage(t.TempDir())
	t.Cleanup(func() { s.Close() })
	if err := s.Save(ctx, "uploads/posts/a.png", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"uploads", "uploads/posts"} {
		if _, err := s.Get(ctx, key); !errors.Is(err, upload.ErrNotFound) {
			t.Errorf("Get(%q) = %v, want ErrNotFound", key, err)
		}
		if _, err := s.GetRange(ctx, key); !errors.Is(err, upload.ErrNotFound) {
			t.Errorf("GetRange(%q) = %v, want ErrNotFound", key, err)
		}
		if ok, err := s.Exists(ctx, key); ok || err != nil {
			t.Errorf("Exists(%q) = %v, %v; want false, nil", key, ok, err)
		}
	}
}
