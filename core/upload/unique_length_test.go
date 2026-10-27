package upload

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// UniqueFilename appends a ~37-byte "_<unixnano>_<random>" suffix to a
// name SanitizeFilename already let reach 255 bytes, so a long client
// name produced a filename component the filesystem refuses and every
// such upload failed. The stem is cut to leave room, on a rune boundary,
// keeping the extension.
func TestUniqueFilenameFitsComponent(t *testing.T) {
	store := NewLocalStorage(t.TempDir())
	t.Cleanup(func() { _ = store.Close() })
	cases := []struct{ in, ext string }{
		{strings.Repeat("a", 250) + ".png", ".png"},
		{strings.Repeat("a", 219) + ".txt", ".txt"},
		{strings.Repeat("é", 125) + ".pdf", ".pdf"},
		{strings.Repeat("日", 80) + ".jpeg", ".jpeg"},
		{"x." + strings.Repeat("b", 240), ""},
		{"short.png", ".png"},
		// One byte of stem room, and the first rune needs three.
		{"日日." + strings.Repeat("b", 216), "." + strings.Repeat("b", 216)},
	}
	for _, c := range cases {
		got := UniqueFilename(c.in)
		if len(got) > MaxFilenameBytes {
			t.Errorf("UniqueFilename(%d-byte name) = %d bytes, want <= %d", len(c.in), len(got), MaxFilenameBytes)
		}
		if strings.HasPrefix(got, "_") {
			t.Errorf("UniqueFilename(%q) = %q, lost its whole stem", c.in, got)
		}
		if !utf8.ValidString(got) {
			t.Errorf("UniqueFilename(%q) = %q, not valid UTF-8", c.in, got)
		}
		if c.ext != "" && filepath.Ext(got) != c.ext {
			t.Errorf("UniqueFilename(%q) = %q, lost extension %q", c.in, got, c.ext)
		}
		if err := store.Save(context.Background(), got, strings.NewReader("x")); err != nil {
			t.Errorf("Save(UniqueFilename(%d-byte name)): %v", len(c.in), err)
		}
	}
}
