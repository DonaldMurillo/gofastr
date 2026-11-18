// Package jpegtest builds hostile JPEG fixtures for framework/image and framework/file tests.
package jpegtest

import "bytes"

// ScanBomb returns a valid grayscale progressive JPEG of w×h pixels whose scans AC scans each walk every block.
func ScanBomb(w, h, scans int) []byte {
	return Progressive(w, h, 1, scans)
}

// Progressive returns a valid progressive JPEG of w×h pixels, comps components (1 or 3) and scans AC scans.
func Progressive(w, h, comps, scans int) []byte {
	var b bytes.Buffer
	b.Write([]byte{0xFF, 0xD8})
	b.Write([]byte{0xFF, 0xDB, 0x00, 67, 0x00})
	b.Write(bytes.Repeat([]byte{1}, 64))
	n := 8 + 3*comps
	b.Write([]byte{0xFF, 0xC2, byte(n >> 8), byte(n), 8, byte(h >> 8), byte(h), byte(w >> 8), byte(w), byte(comps)})
	for c := range comps {
		b.Write([]byte{byte(c + 1), 0x11, 0})
	}
	b.Write([]byte{0xFF, 0xC4, 0x00, 20, 0x10, 1})
	b.Write(make([]byte, 15))
	b.WriteByte(0xE0)
	blocks := ((w + 7) / 8) * ((h + 7) / 8)
	for s := range scans {
		b.Write([]byte{0xFF, 0xDA, 0x00, 8, 1, byte(s%comps + 1), 0x00, 1, 63, 0x00})
		var bw bitWriter
		for covered := 0; covered < blocks; covered += 1<<15 - 1 {
			bw.put(0, 1)
			bw.put(0x3FFF, 14)
		}
		bw.flush()
		b.Write(bw.out.Bytes())
	}
	b.Write([]byte{0xFF, 0xD9})
	return b.Bytes()
}

type bitWriter struct {
	out  bytes.Buffer
	acc  byte
	nacc uint
}

func (w *bitWriter) put(v uint32, n uint) {
	for i := int(n) - 1; i >= 0; i-- {
		w.acc = w.acc<<1 | byte(v>>uint(i))&1
		w.nacc++
		if w.nacc == 8 {
			w.out.WriteByte(w.acc)
			if w.acc == 0xFF {
				w.out.WriteByte(0)
			}
			w.acc, w.nacc = 0, 0
		}
	}
}

func (w *bitWriter) flush() {
	for w.nacc != 0 {
		w.put(1, 1)
	}
}
