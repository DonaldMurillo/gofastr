package jpegcost

import "testing"

// sof builds SOI, an APP0 whose payload holds marker-shaped bytes, then a
// frame header of the given marker over the given components (sampling
// bytes, one per component).
func sof(marker byte, w, h int, sampling ...byte) []byte {
	d := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x06, 0xFF, 0xC2, 0xFF, 0xDA}
	n := 8 + 3*len(sampling)
	d = append(d, 0xFF, marker, byte(n>>8), byte(n), 8, byte(h>>8), byte(h), byte(w>>8), byte(w), byte(len(sampling)))
	for i, hv := range sampling {
		d = append(d, byte(i+1), hv, 0)
	}
	return d
}

func TestProgressiveCoeffBytes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		data   []byte
		want   int64
		wantOK bool
	}{
		// 4:2:0 over 16×16: one MCU, four luma blocks and one per chroma.
		{"4:2:0", sof(0xC2, 16, 16, 0x22, 0x11, 0x11), 6 * 256, true},
		// 4:4:4 over 17×9 rounds up to whole blocks: 3×2 per component.
		{"4:4:4 partial blocks", sof(0xC2, 17, 9, 0x11, 0x11, 0x11), 3 * 6 * 256, true},
		// A grayscale frame declaring 2×2 is read as 1×1.
		{"gray 2x2", sof(0xC2, 16, 8, 0x22), 2 * 256, true},
		{"8192 square 4:4:4", sof(0xC2, 8192, 8192, 0x11, 0x11, 0x11), 3 * 1024 * 1024 * 256, true},
		{"baseline", sof(0xC0, 8192, 8192, 0x11, 0x11, 0x11), 0, false},
		{"truncated", sof(0xC2, 16, 16, 0x11, 0x11, 0x11)[:20], 0, false},
	} {
		got, ok := ProgressiveCoeffBytes(tc.data)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("%s: got (%d, %v), want (%d, %v)", tc.name, got, ok, tc.want, tc.wantOK)
		}
	}
}
