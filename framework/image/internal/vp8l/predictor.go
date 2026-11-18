package vp8l

func avg2u8(a, b uint8) uint8 { return uint8((int32(a) + int32(b)) / 2) }

func clampU8(x int32) uint8 {
	switch {
	case x < 0:
		return 0
	case x > 255:
		return 255
	}
	return uint8(x)
}

func avg2px(a, b [4]uint8) [4]uint8 {
	return [4]uint8{
		avg2u8(a[0], b[0]),
		avg2u8(a[1], b[1]),
		avg2u8(a[2], b[2]),
		avg2u8(a[3], b[3]),
	}
}

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
		var l, t int32
		for i := range 4 {
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

func chooseBlockModes(src [][4]uint8, w, h, bits, mode int) []int {
	blockSize := 1 << bits
	subW := (w + blockSize - 1) >> bits
	subH := (h + blockSize - 1) >> bits

	modes := make([]int, subW*subH)
	for i := range modes {
		modes[i] = mode
	}
	return modes
}
