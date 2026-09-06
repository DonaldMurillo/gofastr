package upload_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/upload"
)

// Pins the invisible/bidi half of storage-key sanitisation, found by the
// 2026-09-05 red-probe round (round 4); fixed by widening
// isStrippableControl (the set behind SanitizeFilename and every key
// built from it) with core/textsafe's C1 + invisible predicates.
// Property: a storage key derived from a client filename carries no
// invisible or bidi codepoint — the key becomes the on-disk file name and
// the /uploads/{key...} URL path segment, both of which are rendered to
// humans: a bidi override flips the visible extension (the URL-shape twin
// of the Content-Disposition trojan) and zero-width codepoints make
// visually identical twin keys.

// TestStoredKeyStripsInvisibleChars pins the key-construction half of
// the invisible-character property: whatever the client filename, the
// generated storage key must not carry an invisible/bidi codepoint.
func TestStoredKeyStripsInvisibleChars(t *testing.T) {
	for _, tc := range []struct {
		name string
		r    rune
	}{
		{"rlo-override", '\u202E'},
		{"zero-width-space", '\u200B'},
		{"bom", '\uFEFF'},
		{"lrm-mark", '\u200E'},
	} {
		key := upload.UniqueFilename("inv" + string(tc.r) + "1ce.exe.pdf")
		if strings.ContainsRune(key, tc.r) {
			t.Errorf("SECURITY: [upload-key-invisible] UniqueFilename kept %s U+%04X in storage key %q. Attack: the key is the served /uploads/... path segment and the on-disk file name — a bidi override flips the extension a user sees (inv exe.pdf reads as a pdf), and zero-width codepoints make visually identical twin keys.", tc.name, tc.r, key)
		}
	}
}
