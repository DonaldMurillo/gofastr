package image

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// longBase is a stem already at the filesystem name limit, the shape
// core/upload.UniqueFilename hands back for a long upload name: the
// readable head, then the timestamp and random token at the end.
func longBase() string {
	token := "_1730419200000000000_0123456789abcdef"
	return strings.Repeat("é", (MaxVariantNameBytes-len(token))/2) + "x" + token
}

func nameFitSet() VariantSet {
	return VariantSet{
		BaseName: longBase(),
		Variants: []Variant{
			{Width: 8, Format: FormatJPEG, Suffix: "sm"},
			{Width: 16, Format: FormatPNG, Suffix: "thumbnail"},
			{Width: 12, Format: FormatJPEG},
		},
	}
}

// TestVariantNamesFitNameLimit: a rendition name is the base plus
// "-<suffix>.<ext>", so a base at the 255-byte limit made every
// rendition name too long for a local filesystem and failed the upload.
// The base is cut from the front, the same amount for every variant, so
// names stay distinct and keep the base's unique tail.
func TestVariantNamesFitNameLimit(t *testing.T) {
	t.Parallel()
	src := FromImage(gradient(20, 10), FormatPNG)
	set := nameFitSet()
	res, err := set.Process(src)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	var streamed []string
	if _, err := set.ProcessTo(src, func(h VariantHeader, r io.Reader) error {
		streamed = append(streamed, h.Name)
		_, err := io.Copy(io.Discard, r)
		return err
	}); err != nil {
		t.Fatalf("ProcessTo: %v", err)
	}
	var names []string
	for _, v := range res.Variants {
		names = append(names, v.Name)
	}
	for _, list := range [][]string{names, streamed} {
		seen := map[string]bool{}
		stem := ""
		for i, n := range list {
			if len(n) > MaxVariantNameBytes {
				t.Errorf("name %d is %d bytes, over the %d-byte limit", i, len(n), MaxVariantNameBytes)
			}
			if !strings.Contains(n, "_0123456789abcdef-") {
				t.Errorf("name %q lost the base's unique tail", n)
			}
			if !bytes.Equal([]byte(n), bytes.ToValidUTF8([]byte(n), nil)) {
				t.Errorf("name %q is not valid UTF-8", n)
			}
			if seen[n] {
				t.Errorf("duplicate name %q", n)
			}
			seen[n] = true
			s := n[:strings.LastIndex(n, "_0123456789abcdef-")]
			if stem != "" && s != stem {
				t.Errorf("variants cut the base differently: %q vs %q", s, stem)
			}
			stem = s
		}
	}
}

// TestShortBaseNameUnchanged: a base that fits is used verbatim.
func TestShortBaseNameUnchanged(t *testing.T) {
	t.Parallel()
	set := nameFitSet()
	set.BaseName = "photo"
	res, err := set.Process(FromImage(gradient(20, 10), FormatPNG))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got := res.Variants[0].Name; got != "photo-sm.jpg" {
		t.Errorf("name = %q, want photo-sm.jpg", got)
	}
}

// TestSuffixOverNameLimitRefused: a suffix that leaves no room for any
// base can never be stored on a filesystem; refuse it at config time.
func TestSuffixOverNameLimitRefused(t *testing.T) {
	t.Parallel()
	vs := []Variant{{Width: 8, Format: FormatJPEG, Suffix: strings.Repeat("s", MaxVariantNameBytes)}}
	if err := CheckVariantNames(vs); err == nil {
		t.Fatal("CheckVariantNames accepted a suffix longer than the name limit")
	}
}

// A suffix that fits but leaves the base only a few bytes would cut two
// uploads' names to the same rendition key (2026-11-01 review): it is
// refused, and one that leaves exactly MinVariantBaseBytes is not.
func TestSuffixLeavesRoomForToken(t *testing.T) {
	t.Parallel()
	// "-" + suffix + ".jpg" is len(suffix)+5 bytes.
	tight := []Variant{{Width: 8, Format: FormatJPEG, Suffix: strings.Repeat("s", MaxVariantNameBytes-6)}}
	if err := CheckVariantNames(tight); err == nil {
		t.Fatal("CheckVariantNames accepted a suffix leaving one byte for the base")
	}
	room := []Variant{{Width: 8, Format: FormatJPEG, Suffix: strings.Repeat("s", MaxVariantNameBytes-MinVariantBaseBytes-5)}}
	if err := CheckVariantNames(room); err != nil {
		t.Fatalf("CheckVariantNames refused a suffix leaving MinVariantBaseBytes: %v", err)
	}
}
