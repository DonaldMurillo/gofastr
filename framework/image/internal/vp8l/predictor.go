package vp8l

// Predictor modes per VP8L spec § 3.1. Each function takes the four
// neighbour pixels (L, T, TR, TL) and returns the predicted pixel.
// The encoder subtracts the prediction from the current pixel to
// produce a residual; the decoder adds the prediction back.

// avg2u8 averages two 8-bit values (floor).
func avg2u8(a, b uint8) uint8 { return uint8((int32(a) + int32(b)) / 2) }

// clampU8 clamps a signed 32-bit int to the [0, 255] range.
func clampU8(x int32) uint8 {
	switch {
	case x < 0:
		return 0
	case x > 255:
		return 255
	}
	return uint8(x)
}

// avg2px averages two pixels component-wise.
func avg2px(a, b [4]uint8) [4]uint8 {
	return [4]uint8{
		avg2u8(a[0], b[0]),
		avg2u8(a[1], b[1]),
		avg2u8(a[2], b[2]),
		avg2u8(a[3], b[3]),
	}
}

// predictMode returns the predicted pixel for the given mode and
// neighbours. L = left, T = top, TR = top-right, TL = top-left.
// All four neighbours must exist in the caller's frame of reference;
// boundary handling lives in the caller.
func predictMode(mode int, L, T, TR, TL [4]uint8) [4]uint8 {
	switch mode {
	case 0:
		return [4]uint8{0, 0, 0, 0xFF}
	case 1:
		return L
	case 2:
		return T
	case 3:
		return TR
	case 4:
		return TL
	case 5:
		return avg2px(avg2px(L, TR), T)
	case 6:
		return avg2px(L, TL)
	case 7:
		return avg2px(L, T)
	case 8:
		return avg2px(TL, T)
	case 9:
		return avg2px(T, TR)
	case 10:
		return avg2px(avg2px(L, TL), avg2px(T, TR))
	case 11:
		// Select: pick L or T based on which is closer to TL.
		var l, t int32
		for i := 0; i < 4; i++ {
			c := int32(TL[i])
			l += absInt32(c - int32(T[i]))
			t += absInt32(c - int32(L[i]))
		}
		if l < t {
			return L
		}
		return T
	case 12:
		return [4]uint8{
			clampU8(int32(L[0]) + int32(T[0]) - int32(TL[0])),
			clampU8(int32(L[1]) + int32(T[1]) - int32(TL[1])),
			clampU8(int32(L[2]) + int32(T[2]) - int32(TL[2])),
			clampU8(int32(L[3]) + int32(T[3]) - int32(TL[3])),
		}
	case 13:
		m := avg2px(L, T)
		return [4]uint8{
			clampU8(int32(m[0]) + (int32(m[0])-int32(TL[0]))/2),
			clampU8(int32(m[1]) + (int32(m[1])-int32(TL[1]))/2),
			clampU8(int32(m[2]) + (int32(m[2])-int32(TL[2]))/2),
			clampU8(int32(m[3]) + (int32(m[3])-int32(TL[3]))/2),
		}
	}
	return [4]uint8{}
}

func absInt32(x int32) int32 {
	if x < 0 {
		return -x
	}
	return x
}

// pixelAt returns src[y*w+x] when (x, y) is in bounds, otherwise an
// opaque-black sentinel. Used so the encoder can evaluate predictor
// modes at block edges without special-casing every neighbour lookup.
func pixelAt(src [][4]uint8, x, y, w, h int) [4]uint8 {
	if x < 0 || y < 0 || x >= w || y >= h {
		return [4]uint8{0, 0, 0, 0xFF}
	}
	return src[y*w+x]
}

// scoreModeBlock returns a Huffman-cost proxy for a block under the
// given mode. We count residual entries that are non-zero (each is a
// "rare" symbol the Huffman tree must spend bits on) plus the L1
// magnitude — counting zeros first concentrates on the metric that
// actually drives compression ratio, while L1 is a fast tiebreaker.
// The decoder's per-block-corner hardcoded modes (0/1/2 at edges)
// apply regardless of mode here.
func scoreModeBlock(src [][4]uint8, w, h, x0, y0, x1, y1, mode int) int {
	nonZero, l1 := 0, 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			cur := src[y*w+x]
			var pred [4]uint8
			switch {
			case x == 0 && y == 0:
				pred = [4]uint8{0, 0, 0, 0xFF}
			case y == 0:
				pred = pixelAt(src, x-1, 0, w, h)
			case x == 0:
				pred = pixelAt(src, 0, y-1, w, h)
			default:
				L := pixelAt(src, x-1, y, w, h)
				T := pixelAt(src, x, y-1, w, h)
				var TR [4]uint8
				if x+1 < w {
					TR = pixelAt(src, x+1, y-1, w, h)
				} else {
					TR = src[y*w]
				}
				TL := pixelAt(src, x-1, y-1, w, h)
				pred = predictMode(mode, L, T, TR, TL)
			}
			for i := 0; i < 4; i++ {
				r := int32(cur[i]) - int32(pred[i])
				if r < 0 {
					r = -r
				}
				if r != 0 {
					nonZero++
				}
				l1 += int(r)
			}
		}
	}
	// Number of non-zero residual entries is the most direct Huffman-
	// cost proxy: zero is by far the most common literal symbol, so
	// each non-zero residual incurs a few bits of branching that a
	// fully-zero pixel does not. L1 magnitude is a fine-grained
	// tiebreaker — modes that are tied on nonzero count are then
	// ranked by total residual magnitude.
	return nonZero<<8 | (l1 & 0xff)
}

// chooseBlockModes returns a uniform sub-image where every block uses
// the same predictor mode. Per-block adaptive selection requires a
// Huffman-aware cost metric to beat uniform mode in practice — a
// simple L1 / nonzero-count proxy ends up fragmenting the residual
// distribution and inflating the sub-image. scoreModeBlock stays in
// this file as the foundation for a future per-block-adaptive pass
// once a proper Huffman cost model is wired in.
//
// The current encoder picks between several uniform-mode candidates
// at the top level (Encode in vp8l.go) and emits the smallest result;
// chooseBlockModes is parameterised so each candidate produces its
// own sub-image.
func chooseBlockModes(src [][4]uint8, w, h, bits, mode int) []int {
	blockSize := 1 << bits
	subW := (w + blockSize - 1) >> bits
	subH := (h + blockSize - 1) >> bits

	_ = scoreModeBlock // reserved for future Huffman-aware selection
	modes := make([]int, subW*subH)
	for i := range modes {
		modes[i] = mode
	}
	return modes
}
