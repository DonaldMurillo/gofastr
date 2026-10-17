package storage

import (
	"context"
	"io"
	"strings"
	"testing"
)

// cancelingReader cancels the save's context once the copy has started.
type cancelingReader struct {
	r      io.Reader
	cancel context.CancelFunc
}

func (c cancelingReader) Read(p []byte) (int, error) {
	c.cancel()
	return c.r.Read(p)
}

// A Save canceled while copying does not replace the stored object.
func TestSaveCanceledMidCopyKeepsObject(t *testing.T) {
	for name, opts := range map[string][]LocalOption{
		"root":        nil,
		"outside-tmp": {WithTempDir(t.TempDir())},
	} {
		t.Run(name, func(t *testing.T) {
			s := newTestLocalStorage(t, t.TempDir(), opts...)
			if err := s.Save(context.Background(), "a", strings.NewReader("old")); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if s.Save(ctx, "a", cancelingReader{strings.NewReader("new"), cancel}) == nil {
				t.Fatal("Save canceled mid-copy succeeded")
			}
			rc, err := s.Get(context.Background(), "a")
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			if b, _ := io.ReadAll(rc); string(b) != "old" {
				t.Fatalf("object = %q, want old", b)
			}
		})
	}
}
