// Package jpegcost estimates what image/jpeg will allocate for a JPEG from its header alone: ProgressiveCoeffBytes.
package jpegcost

const blockBytes = 64 * 4

// ProgressiveCoeffBytes returns the bytes image/jpeg keeps for a progressive frame's coefficients, zero for a
// sequential one; false means the header is unreadable and the caller must refuse to decode.
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
		case m == 0xFF:
			i++
			continue
		case m == 0x00 || m == 0x01 || (m >= 0xD0 && m <= 0xD7):
			i += 2
			continue
		case m == 0xD9 || m == 0xDA:
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
