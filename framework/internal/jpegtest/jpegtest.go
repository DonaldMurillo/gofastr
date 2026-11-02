// Package jpegtest builds hostile JPEG fixtures for the tests of the
// framework's image-adjacent packages (framework/image and
// framework/file). Tests only: nothing outside a _test.go file imports
// it, so it never links into a binary.
package jpegtest

import "bytes"

// ScanBomb returns a valid grayscale progressive JPEG of w×h pixels made
// of scans copies of one AC scan. Each scan is a single end-of-band run
// code repeated to cover every block, so it costs about ten bytes per
// 330,000 blocks while the decoder walks every block of the frame again:
// decode CPU grows with scans × pixels and the file with scans alone.
// w and h must each fit in 16 bits.
func ScanBomb(w, h, scans int) []byte {
	var b bytes.Buffer
	b.Write([]byte{0xFF, 0xD8}) // SOI
	// DQT: table 0, all ones.
	b.Write([]byte{0xFF, 0xDB, 0x00, 67, 0x00})
	b.Write(bytes.Repeat([]byte{1}, 64))
	// SOF2 (progressive): 8-bit, h×w, one component, 1×1 sampling, table 0.
	b.Write([]byte{0xFF, 0xC2, 0x00, 11, 8, byte(h >> 8), byte(h), byte(w >> 8), byte(w), 1, 1, 0x11, 0})
	// DHT: AC table 0 with one 1-bit code, symbol 0xE0 (EOBRUN, 14 extra bits).
	b.Write([]byte{0xFF, 0xC4, 0x00, 20, 0x10, 1})
	b.Write(make([]byte, 15))
	b.WriteByte(0xE0)
	blocks := ((w + 7) / 8) * ((h + 7) / 8)
	for range scans {
		// SOS: one component, AC table 0, spectral 1..63, no approximation.
		b.Write([]byte{0xFF, 0xDA, 0x00, 8, 1, 1, 0x00, 1, 63, 0x00})
		var bw bitWriter
		for covered := 0; covered < blocks; covered += 1<<15 - 1 {
			bw.put(0, 1)       // the EOBRUN code
			bw.put(0x3FFF, 14) // run of 2^14 + 0x3FFF = 32767 blocks
		}
		bw.flush()
		b.Write(bw.out.Bytes())
	}
	b.Write([]byte{0xFF, 0xD9}) // EOI
	return b.Bytes()
}

// bitWriter packs entropy-coded bits MSB first, stuffing a zero byte
// after every 0xFF so the data never reads as a marker.
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

// flush pads the last byte with one bits, as the format requires.
func (w *bitWriter) flush() {
	for w.nacc != 0 {
		w.put(1, 1)
	}
}
