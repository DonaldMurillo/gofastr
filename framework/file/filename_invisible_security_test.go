package file_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/file"
)

// Pins the invisible/bidi/C1 refusal at FileField's stored strings, found
// by the 2026-09-05 red-probe round (round 4); fixed by Validate's
// forged-rune gate (core/textsafe's C1 + invisible predicates) plus the
// widened upload.SanitizeFilename the constructor shares for Filename.
// Property: no invisible, bidi, or C1 control codepoint survives into a
// stored FileField string — those fields are persisted and echoed into
// Content-Disposition, HTML attributes, and admin/list displays where the
// codepoint spoofs rather than decorates: an RLO flips the rendered name
// (the classic "photo\u202Egnp.jpg" trojan) and zero-width codepoints
// manufacture visually identical twin filenames.

// TestFileFieldRejectsInvisibleChars pins the invisible/bidi/C1 property
// at both FileField surfaces: the validator (hand-built FileField from an
// untrusted JSON body) and the constructor (multipart filename).
func TestFileFieldRejectsInvisibleChars(t *testing.T) {
	invisible := []struct {
		name string
		r    rune
	}{
		{"rlo-override", '\u202E'},
		{"zero-width-space", '\u200B'},
		{"bom", '\uFEFF'},
		{"c1-osc-8bit", '\u009B'},
	}

	// Surface 1: Validate must refuse a FileField string field carrying
	// the codepoint, whatever the field.
	for _, tc := range invisible {
		shot := "inv" + string(tc.r) + "oice.pdf"
		fields := map[string]func(*file.FileField) string{
			"Filename":   func(ff *file.FileField) string { ff.Filename = shot; return ff.Filename },
			"URL":        func(ff *file.FileField) string { ff.URL = "uploads/posts/cover/" + shot; return ff.URL },
			"StorageRef": func(ff *file.FileField) string { ff.StorageRef = "uploads/posts/cover/" + shot; return ff.StorageRef },
		}
		for fname, set := range fields {
			ff := &file.FileField{URL: "uploads/posts/avatar/photo_123.png", MimeType: "image/png", Size: 1024, StorageRef: "uploads/posts/avatar/photo_123.png"}
			got := set(ff)
			if err := ff.Validate(); err == nil {
				t.Errorf("SECURITY: [filefield-invisible] Validate accepted %s U+%04X in %s (%q). Attack: the stored value is echoed into Content-Disposition filename= and admin/list displays, where RLO flips the rendered name and zero-width codepoints manufacture visually identical twins.", tc.name, tc.r, fname, got)
			}
		}
	}

	// Surface 2: the constructor must not produce a Filename carrying
	// the codepoint (strip it or reject the upload), the same
	// meets-own-contract shape TestProcessFileFieldMeetsOwnContract
	// pins for control bytes and traversal.
	png := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0x0D, 'I', 'H', 'D', 'R'}
	for _, tc := range invisible {
		name := "inv" + string(tc.r) + "oice.pdf"
		ff, err := file.ProcessFileField(context.Background(), &captureStorage{}, bytes.NewReader(png), name, "posts", "cover")
		if err != nil {
			continue // rejection is an acceptable fix shape
		}
		if strings.ContainsRune(ff.Filename, tc.r) {
			t.Errorf("SECURITY: [filefield-invisible] ProcessFileField stored client filename %q as Filename=%q: %s U+%04X survives into the persisted name that Content-Disposition and file lists render.", name, ff.Filename, tc.name, tc.r)
		}
	}
}
