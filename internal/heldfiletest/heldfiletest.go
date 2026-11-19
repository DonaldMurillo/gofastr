// Package heldfiletest holds a file open for a test and reports what that
// handle keeps reading, so a test can tell a rename-over-whole write from
// a truncate-in-place one.
package heldfiletest

import (
	"io"
	"os"
	"testing"
)

// Held opens path and returns a reader of what that handle sees: a file
// renamed over path leaves it reading the old content, one truncated in
// place leaves it reading the new.
func Held(t *testing.T, path string) func() string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return func() string {
		b, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
}
