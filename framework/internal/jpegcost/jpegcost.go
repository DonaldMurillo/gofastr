// Package jpegcost estimates what image/jpeg will allocate for a JPEG
// from its header bytes alone, before any pixel is decoded. It is a
// stdlib-only leaf shared by framework/image and framework/file, so the
// strip path applies the pipeline's budgets without linking the image
// pipeline.
package jpegcost

// blockBytes is the size of one image/jpeg coefficient block: 64 int32s.
// libjpeg keeps int16 coefficients (128 bytes a block); Go keeps int32.
const blockBytes = 64 * 4

// ProgressiveCoeffBytes returns the bytes image/jpeg allocates for the
// coefficients of a progressive (SOF2) JPEG, which it keeps for the whole
// frame across scans: a block of 64 int32s for every block of every
// component, counted the way the decoder counts them (whole MCUs, the
// component's sampling factors, 1×1 for a grayscale frame). It reports
// zero bytes and true for a sequential JPEG, whose decoder keeps no
// coefficients. False means the header could not be read; callers must
// refuse decoding rather than silently bypass their memory budget.
//
// The walk starts after SOI, skips each segment by its length and stops
// at the first frame header, the one the decoder uses. A component no
// scan names is never allocated, so the count is an upper bound.
func ProgressiveCoeffBytes(d []byte) (int64, bool) {
	if len(d) < 2 || d[0] != 0xFF || d[1] != 0xD8 {
		return 0, false
	}
	for i := 2; i+3 < len(d); {
		if d[i] != 0xFF {
			i++
			continue
		}
		m := d[i+1]
		switch {
		case m == 0xFF: // fill byte
			i++
			continue
		case m == 0x00 || m == 0x01 || (m >= 0xD0 && m <= 0xD7): // no length field
			i += 2
			continue
		case m == 0xD9 || m == 0xDA: // EOI or a scan before any frame header
			return 0, false
		}
		n := int(d[i+2])<<8 | int(d[i+3])
		end := i + 2 + n
		if n < 2 || end > len(d) {
			return 0, false
		}
		if m >= 0xC0 && m <= 0xCF && m != 0xC4 && m != 0xC8 && m != 0xCC {
			if m == 0xC0 || m == 0xC1 {
				return 0, true
			}
			if m != 0xC2 {
				return 0, false
			}
			return sof2CoeffBytes(d[i+4 : end])
		}
		i += 2 + n
	}
	return 0, false
}

// sof2CoeffBytes reads an SOF2 payload (precision, height, width, the
// component count, then id, sampling and table per component).
func sof2CoeffBytes(p []byte) (int64, bool) {
	if len(p) < 6 {
		return 0, false
	}
	height := int64(p[1])<<8 | int64(p[2])
	width := int64(p[3])<<8 | int64(p[4])
	nComp := int(p[5])
	if height == 0 || width == 0 || nComp == 0 || len(p) < 6+3*nComp {
		return 0, false
	}
	hs := make([]int64, nComp)
	vs := make([]int64, nComp)
	maxH, maxV := int64(1), int64(1)
	for c := range nComp {
		hv := p[7+3*c]
		h, v := int64(hv>>4), int64(hv&0x0F)
		if h < 1 || v < 1 {
			return 0, false
		}
		if nComp == 1 {
			// image/jpeg reads a grayscale frame as 1×1 whatever it
			// declares (JPEG A.2: one component is non-interleaved).
			h, v = 1, 1
		}
		hs[c], vs[c] = h, v
		maxH, maxV = max(maxH, h), max(maxV, v)
	}
	mxx := (width + 8*maxH - 1) / (8 * maxH)
	myy := (height + 8*maxV - 1) / (8 * maxV)
	var blocks int64
	for c := range nComp {
		blocks += mxx * myy * hs[c] * vs[c]
	}
	return blocks * blockBytes, true
}
