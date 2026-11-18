package file

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf16"
)

func utf16LE(s string) []byte {
	out := []byte{0xFF, 0xFE}
	for _, u := range utf16.Encode([]rune(s)) {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

func TestWideTextScanRules(t *testing.T) {
	reject := map[string][]byte{
		"hard-upper":     utf16LE("x <SCRIPT>"),
		"soft-in-window": utf16LE(strings.Repeat("a", 40) + " <link rel=x>"),
		"sniffed-markup": utf16LE("<div>x</div>"),
	}
	for name, body := range reject {
		if err := rejectWideText(body); !errors.Is(err, ErrFileFieldUnsafeContent) {
			t.Errorf("%s: err = %v, want ErrFileFieldUnsafeContent", name, err)
		}
	}
	misaligned := append([]byte{0xFF, 0xFE, 0x41}, utf16LE("<script>")[2:]...)
	accept := map[string][]byte{
		"non-ascii-units":  utf16LE("ļscript>"),
		"misaligned":       misaligned,
		"soft-past-window": utf16LE(strings.Repeat("a", 600) + " <img src=x>"),
	}
	for name, body := range accept {
		if err := rejectWideText(body); err != nil {
			t.Errorf("%s: err = %v, want accepted", name, err)
		}
	}
}
