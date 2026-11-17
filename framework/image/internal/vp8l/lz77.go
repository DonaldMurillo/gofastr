package vp8l

const matchMinLen = 2

const matchMaxLen = 4096

const maxBackrefDistance = (1 << 20) - 120

const (
	hashBits = 14
	hashSize = 1 << hashBits
	hashMask = hashSize - 1
)

const chainDepth = 16

type symKind uint8

const (
	symLiteral symKind = iota
	symCacheHit
	symBackref
)

type sym struct {
	kind symKind

	g, r, b, a uint8

	cacheIdx uint32

	lenSym, distSym             uint32
	extraLen, extraDist         uint32
	extraLenBits, extraDistBits uint8
}

type extra struct {
	value uint32
	bits  uint
}

func buildStream(pixels [][4]uint8, cacheBits uint) (
	stream []sym,
	gFreq, rFreq, bFreq, aFreq, dFreq []int,
) {
	cacheSize := 1 << cacheBits
	gAlpha := nLiteralCodes + nLengthCodes + cacheSize

	gFreq = make([]int, gAlpha)
	rFreq = make([]int, nLiteralCodes)
	bFreq = make([]int, nLiteralCodes)
	aFreq = make([]int, nLiteralCodes)
	dFreq = make([]int, nDistanceCodes)
	stream = make([]sym, 0, len(pixels))

	cache := make([]uint32, cacheSize)
	head := make([]int32, hashSize)
	chain := make([]int32, len(pixels))
	for i := range head {
		head[i] = -1
	}
	for i := range chain {
		chain[i] = -1
	}

	insertHash := func(p int) {
		if p+1 >= len(pixels) {
			return
		}
		h := hashPair(pixels, p) & hashMask
		chain[p] = head[h]
		head[h] = int32(p)
	}

	findMatch := func(p int) (length, dist int) {
		if p+matchMinLen > len(pixels) {
			return 0, 0
		}
		h := hashPair(pixels, p) & hashMask
		bestLen, bestDist := 0, 0
		for cand, depth := head[h], 0; cand >= 0 && depth < chainDepth; depth++ {
			c := int(cand)
			cand = chain[cand]
			if c >= p {
				continue
			}
			d := p - c
			if d > maxBackrefDistance {
				continue
			}
			n := 0
			limit := min(len(pixels)-p, matchMaxLen)
			for n < limit && pixels[c+n] == pixels[p+n] {
				n++
			}
			if n > bestLen {
				bestLen = n
				bestDist = d
				if bestLen >= matchMaxLen {
					break
				}
			}
		}
		if bestLen < matchMinLen {
			return 0, 0
		}
		return bestLen, bestDist
	}

	cacheUpdate := func(px [4]uint8) {
		argb := packARGB(px[1], px[0], px[2], px[3])
		cache[cacheHash(argb, cacheBits)] = argb
	}

	i := 0
	for i < len(pixels) {
		matchLen, matchDist := findMatch(i)
		px := pixels[i]
		argb := packARGB(px[1], px[0], px[2], px[3])
		h := cacheHash(argb, cacheBits)

		switch {
		case matchLen >= matchMinLen:
			lenSym, lenExtra := lz77Symbol(uint32(matchLen))
			distCode := uint32(matchDist) + 120
			distSym, distExtra := lz77Symbol(distCode)
			gFreq[nLiteralCodes+int(lenSym)]++
			dFreq[distSym]++
			stream = append(stream, sym{
				kind:          symBackref,
				lenSym:        lenSym,
				distSym:       distSym,
				extraLen:      lenExtra.value,
				extraDist:     distExtra.value,
				extraLenBits:  uint8(lenExtra.bits),
				extraDistBits: uint8(distExtra.bits),
			})
			for k := range matchLen {
				insertHash(i + k)
				cacheUpdate(pixels[i+k])
			}
			i += matchLen

		case cache[h] == argb:
			stream = append(stream, sym{
				kind:     symCacheHit,
				cacheIdx: h,
			})
			gFreq[nLiteralCodes+nLengthCodes+int(h)]++
			insertHash(i)
			cache[h] = argb
			i++

		default:
			stream = append(stream, sym{
				kind: symLiteral,
				g:    px[0], r: px[1], b: px[2], a: px[3],
			})
			gFreq[px[0]]++
			rFreq[px[1]]++
			bFreq[px[2]]++
			aFreq[px[3]]++
			insertHash(i)
			cache[h] = argb
			i++
		}
	}
	return
}

func hashPair(pixels [][4]uint8, p int) uint32 {
	a := packARGB(pixels[p][1], pixels[p][0], pixels[p][2], pixels[p][3])
	b := uint32(0)
	if p+1 < len(pixels) {
		b = packARGB(pixels[p+1][1], pixels[p+1][0], pixels[p+1][2], pixels[p+1][3])
	}
	return a*0x9E3779B1 ^ (b * colorCacheMultiplier)
}

func lz77Symbol(value uint32) (symbol uint32, e extra) {
	if value <= 4 {
		return value - 1, extra{}
	}
	k := uint(0)
	for (uint32(2) << (k + 1)) < value {
		k++
	}
	threshold := uint32(3) << k
	if value <= threshold {
		symbol = 2*uint32(k) + 2
		e.value = value - (uint32(2) << k) - 1
	} else {
		symbol = 2*uint32(k) + 3
		e.value = value - threshold - 1
	}
	e.bits = k
	return
}
