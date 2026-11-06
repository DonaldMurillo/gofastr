package jpegcost

import "testing"

func TestJPEGCostSkipsExtraneousBytes(t *testing.T) {
	clean := sof(0xC2, 64, 64, 0x11, 0x11, 0x11)
	for _, junk := range [][]byte{{0x42}, {0xFF, 0}, {0x42, 0xFF, 0, 0x55}, {0xFF, 0xFF, 0xFF}} {
		data := append(append(append([]byte{}, clean[:10]...), junk...), clean[10:]...)
		if cost, ok := ProgressiveCoeffBytes(data); !ok || cost != 192*256 {
			t.Errorf("junk %x: cost=(%d, %v), want (49152, true)", junk, cost, ok)
		}
	}
}
