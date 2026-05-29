package upload

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzSanitizeFilename pins the output invariants the sanitizer promises
// for ANY attacker-controlled filename: valid UTF-8 (no multibyte rune
// split, #67), bounded length (#62), never empty, and free of path
// separators / NUL / ASCII control bytes (traversal + log-injection).
func FuzzSanitizeFilename(f *testing.F) {
	for _, s := range []string{
		"", ".", "..", "...", "shell.php.jpg", "evil.php\x00.jpg",
		"a/b/../../etc/passwd", "  spaced  .txt", "café .png",
		strings.Repeat("A", 5000) + ".jpg", "\x00\x01\x02\x7f",
		"con:stream$(rm).txt", "résumé\\..\\x.exe",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, name string) {
		out := SanitizeFilename(name)

		if !utf8.ValidString(out) {
			t.Fatalf("output not valid UTF-8: %q (in=%q)", out, name)
		}
		if len(out) > MaxFilenameBytes {
			t.Fatalf("output %d bytes exceeds cap %d (in=%q)", len(out), MaxFilenameBytes, name)
		}
		if out == "" {
			t.Fatalf("output empty, expected fallback label (in=%q)", name)
		}
		if strings.ContainsAny(out, "/\\\x00") {
			t.Fatalf("output retains separator/NUL: %q (in=%q)", out, name)
		}
		for _, r := range out {
			if r < 0x20 || r == 0x7f {
				t.Fatalf("output retains control byte %#x: %q (in=%q)", r, out, name)
			}
		}
	})
}
