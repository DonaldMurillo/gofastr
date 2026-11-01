package file_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"image/png"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/file"
)

// apngWithMetadata returns a two-frame APNG (acTL, fcTL, IDAT, fcTL,
// fdAT) carrying tEXt and an eXIf with GPS and the given orientation.
func apngWithMetadata(t *testing.T, orientation int) []byte {
	t.Helper()
	base := solidPNG(t, 2, 2)
	ihdr := base[8 : 8+25]
	var idat []byte
	for pos := 8; pos+8 <= len(base); {
		n := int(binary.BigEndian.Uint32(base[pos : pos+4]))
		if string(base[pos+4:pos+8]) == "IDAT" {
			idat = append(idat, base[pos+8:pos+8+n]...)
		}
		pos += 12 + n
	}
	u32 := func(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }
	fctl := func(seq uint32) []byte {
		b := u32(seq)
		b = append(b, u32(2)...)   // width
		b = append(b, u32(2)...)   // height
		b = append(b, u32(0)...)   // x offset
		b = append(b, u32(0)...)   // y offset
		b = append(b, 0, 1, 0, 10) // delay 1/10 s
		return append(b, 0, 0)     // dispose, blend
	}
	out := append([]byte{}, base[:8]...)
	out = append(out, ihdr...)
	out = append(out, pngChunk("acTL", append(u32(2), u32(0)...))...)
	out = append(out, pngChunk("tEXt", []byte("Comment\x00secret camera serial"))...)
	out = append(out, pngChunk("eXIf", tiffWithGPSAndOrientation(orientation))...)
	out = append(out, pngChunk("fcTL", fctl(0))...)
	out = append(out, pngChunk("IDAT", idat)...)
	out = append(out, pngChunk("fcTL", fctl(1))...)
	out = append(out, pngChunk("fdAT", append(u32(2), idat...))...)
	return append(out, pngChunk("IEND", nil)...)
}

// pngChunkTypes lists a PNG's chunk types in order.
func pngChunkTypes(data []byte) []string {
	var types []string
	for pos := 8; pos+8 <= len(data); {
		n := int(binary.BigEndian.Uint32(data[pos : pos+4]))
		types = append(types, string(data[pos+4:pos+8]))
		pos += 12 + n
	}
	return types
}

// An animated PNG keeps its animation: a re-encode through image/png
// writes only the default image, one still frame. The metadata chunks
// go and every frame chunk stays.
func TestStripKeepsAPNGFrames(t *testing.T) {
	for _, orientation := range []int{1, 6} {
		store := &captureStorage{}
		if _, err := file.ProcessFileField(context.Background(), store,
			bytes.NewReader(apngWithMetadata(t, orientation)), "anim.png", "posts", "photo", file.StripMetadata()); err != nil {
			t.Fatalf("orientation %d: %v", orientation, err)
		}
		got := pngChunkTypes(store.data)
		count := map[string]int{}
		for _, c := range got {
			count[c]++
		}
		if count["acTL"] != 1 || count["fcTL"] != 2 || count["fdAT"] != 1 {
			t.Fatalf("orientation %d: stored chunks %v; want acTL, two fcTL and the fdAT kept", orientation, got)
		}
		if count["tEXt"] != 0 || bytes.Contains(store.data, []byte("secret camera serial")) || bytes.Contains(store.data, []byte("GPS\x00")) {
			t.Errorf("SECURITY: [png-strip] stored APNG keeps metadata: %v", got)
		}
		// Frames cannot be rotated here; a non-trivial orientation stays
		// as an orientation-only eXIf, the WebP path's posture.
		if want := map[int]int{1: 0, 6: 1}[orientation]; count["eXIf"] != want {
			t.Errorf("orientation %d: %d eXIf chunks; want %d", orientation, count["eXIf"], want)
		}
		if _, err := png.Decode(bytes.NewReader(store.data)); err != nil {
			t.Fatalf("orientation %d: stripped APNG does not decode: %v", orientation, err)
		}
	}
}
