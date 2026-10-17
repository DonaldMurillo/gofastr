package upload

import (
	"context"
	"io"
	"strings"
	"testing"
)

func readKey(t *testing.T, s *LocalStorage, key string) string {
	t.Helper()
	rc, err := s.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	return string(b)
}

// A canceled Save or Delete leaves the stored object as it was.
func TestCanceledMutationsKeepObject(t *testing.T) {
	s := newTestLocalStorage(t, t.TempDir())
	if err := s.Save(context.Background(), "a", strings.NewReader("old")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s.Save(ctx, "a", strings.NewReader("new")) == nil {
		t.Fatal("canceled Save succeeded")
	}
	if s.Delete(ctx, "a") == nil {
		t.Fatal("canceled Delete succeeded")
	}
	if got := readKey(t, s, "a"); got != "old" {
		t.Fatalf("object = %q, want old", got)
	}
}
