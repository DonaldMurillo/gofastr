package vp8l

type bitWriter struct {
	buf []byte
	cur uint64
	n   uint // number of bits currently buffered in cur
}

func (bw *bitWriter) writeBits(value uint32, n uint) {
	bw.cur |= uint64(value&((1<<n)-1)) << bw.n
	bw.n += n
	for bw.n >= 8 {
		bw.buf = append(bw.buf, byte(bw.cur))
		bw.cur >>= 8
		bw.n -= 8
	}
}

func (bw *bitWriter) writeBitsRev(value uint32, n uint) {
	bw.writeBits(reverseBits(value, n), n)
}

func (bw *bitWriter) flush() {
	if bw.n > 0 {
		bw.buf = append(bw.buf, byte(bw.cur))
		bw.cur = 0
		bw.n = 0
	}
}

func (bw *bitWriter) Bytes() []byte {
	bw.flush()
	return bw.buf
}

func reverseBits(v uint32, n uint) uint32 {
	var r uint32
	for range n {
		r = (r << 1) | (v & 1)
		v >>= 1
	}
	return r
}
