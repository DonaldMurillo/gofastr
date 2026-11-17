package jpegcost

import "testing"

func TestProgressiveCoeffBytesRefusals(t *testing.T) {
	dac := []byte{0xFF, 0xD8, 0xFF, 0xCC, 0x00, 0x04, 0, 0}
	for _, tc := range []struct {
		name   string
		data   []byte
		want   int64
		wantOK bool
	}{
		{"extended sequential", sof(0xC1, 16, 16, 0x11), 0, true},
		{"lossless frame", sof(0xC3, 16, 16, 0x11), 0, false},
		{"no SOI", append([]byte{0xFF, 0x00}, sof(0xC2, 16, 16, 0x11)[2:]...), 0, false},
		{"scan before frame", []byte{0xFF, 0xD8, 0xFF, 0xDA, 0x00, 0x02, 0xFF, 0xD9}, 0, false},
		{"zero width", sof(0xC2, 0, 16, 0x11), 0, false},
		{"zero sampling", sof(0xC2, 16, 16, 0x01, 0x11), 0, false},
		{"DAC is skipped", append(dac, sof(0xC2, 16, 16, 0x11)[2:]...), 4 * 256, true},
		{"DHT is skipped", append([]byte{0xFF, 0xD8, 0xFF, 0xC4, 0x00, 0x04, 0, 0}, sof(0xC2, 16, 16, 0x11)[2:]...), 4 * 256, true},
	} {
		got, ok := ProgressiveCoeffBytes(tc.data)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("%s: got (%d, %v), want (%d, %v)", tc.name, got, ok, tc.want, tc.wantOK)
		}
	}
}
