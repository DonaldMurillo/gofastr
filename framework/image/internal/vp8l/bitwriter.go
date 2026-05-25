package vp8l

// bitWriter accumulates bits LSB-first per byte, matching the VP8L
// bitstream convention. Bits enter the accumulator from the low end
// and whole bytes flush out the bottom into buf.
//
// VP8L decoders read bytes left-to-right and consume bits LSB-first
// within each byte; this writer's output is therefore the inverse of
// that read order.
type bitWriter struct {
	buf  []byte
	cur  uint64
	n    uint // number of bits currently buffered in cur
}

// writeBits writes the low n bits of value into the stream. n must
// be in [1, 32].
func (bw *bitWriter) writeBits(value uint32, n uint) {
	bw.cur |= uint64(value&((1<<n)-1)) << bw.n
	bw.n += n
	for bw.n >= 8 {
		bw.buf = append(bw.buf, byte(bw.cur))
		bw.cur >>= 8
		bw.n -= 8
	}
}

// writeBitsRev writes the low n bits of value in reversed order (MSB
// of the input becomes LSB in the stream). VP8L Huffman codes are
// defined MSB-first in the canonical sense; emit them via this method.
func (bw *bitWriter) writeBitsRev(value uint32, n uint) {
	bw.writeBits(reverseBits(value, n), n)
}

// flush byte-aligns the stream by emitting any partial byte.
func (bw *bitWriter) flush() {
	if bw.n > 0 {
		bw.buf = append(bw.buf, byte(bw.cur))
		bw.cur = 0
		bw.n = 0
	}
}

// Bytes returns the accumulated bytes after a final flush.
func (bw *bitWriter) Bytes() []byte {
	bw.flush()
	return bw.buf
}

// reverseBits reverses the low n bits of v.
func reverseBits(v uint32, n uint) uint32 {
	var r uint32
	for i := uint(0); i < n; i++ {
		r = (r << 1) | (v & 1)
		v >>= 1
	}
	return r
}
