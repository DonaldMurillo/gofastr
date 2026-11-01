package file_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
	"unicode/utf16"

	"github.com/DonaldMurillo/gofastr/framework/file"
)

// utf16Body encodes s as UTF-16 in the given byte order, optionally
// behind a BOM. The bytes are NUL-interleaved, the way a real UTF-16
// file is, so an ASCII token scan over them finds nothing.
func utf16Body(s string, order binary.ByteOrder, bom bool) []byte {
	units := utf16.Encode([]rune(s))
	if bom {
		units = append([]uint16{0xFEFF}, units...)
	}
	out := make([]byte, 2*len(units))
	for i, u := range units {
		order.PutUint16(out[2*i:], u)
	}
	return out
}

func utf32Body(s string, order binary.ByteOrder) []byte {
	runes := append([]rune{0xFEFF}, []rune(s)...)
	out := make([]byte, 4*len(runes))
	for i, r := range runes {
		order.PutUint32(out[4*i:], uint32(r))
	}
	return out
}

// TestWideTextMarkupRejected: an XML parser decodes UTF-16 and UTF-32
// from the BOM (and UTF-16 from a leading "<" per XML Appendix F), so a
// NUL-interleaved <svg onload> renders as SVG. The token scan must run
// over the decoded text, not the raw wide bytes.
func TestWideTextMarkupRejected(t *testing.T) {
	t.Parallel()
	const svg = `<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`
	cases := map[string][]byte{
		"utf16le-bom":    utf16Body(svg, binary.LittleEndian, true),
		"utf16be-bom":    utf16Body(svg, binary.BigEndian, true),
		"utf16le-nobom":  utf16Body(`<?xml version="1.0"?>`+svg, binary.LittleEndian, false),
		"utf16be-nobom":  utf16Body(`<?xml version="1.0"?>`+svg, binary.BigEndian, false),
		"utf32le-bom":    utf32Body(svg, binary.LittleEndian),
		"utf32be-bom":    utf32Body(svg, binary.BigEndian),
		"utf16le-script": utf16Body("hello\n<script>alert(1)</script>", binary.LittleEndian, true),
		"utf16le-js-uri": utf16Body(`<a href="javascript:alert(1)">x</a>`, binary.LittleEndian, true),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := &captureStorage{}
			if _, err := file.ProcessFileField(context.Background(), store, bytes.NewReader(body), "x.svg", "posts", "attachment"); err == nil {
				t.Fatalf("SECURITY: [filefield-wide-text] ProcessFileField stored %s markup. Attack: a UTF-16/32 SVG renders and runs its onload handler when the bucket is served by extension.", name)
			}
		})
	}
}

// TestWideTextPlainAccepted: decoding must not reject harmless UTF-16
// text, the way a Windows Notepad export arrives.
func TestWideTextPlainAccepted(t *testing.T) {
	t.Parallel()
	body := utf16Body("Grocery list: eggs, milk, café, 東京\n", binary.LittleEndian, true)
	store := &captureStorage{}
	if _, err := file.ProcessFileField(context.Background(), store, bytes.NewReader(body), "notes.txt", "posts", "attachment"); err != nil {
		t.Fatalf("plain UTF-16 text refused: %v", err)
	}
}
