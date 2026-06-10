package vp8l

// Phase C LZ77 + color cache symbol stream construction.
//
// Walks the post-transform pixel slice left to right and decides, per
// pixel, whether to emit a literal, a backward reference, or a colour-
// cache hit. Output is a slice of `sym` records the caller turns into
// Huffman codes; histograms for each alphabet are returned alongside
// so a single pass produces both.

// matchMinLen is the shortest backreference worth emitting.
const matchMinLen = 2

// matchMaxLen caps backreference lengths. The length-prefix alphabet
// reaches 2^24 but practical matches are far shorter; this also bounds
// the inner match-extension loop.
const matchMaxLen = 4096

// maxBackrefDistance caps pixel-distance backreferences so the distance
// prefix code (40-symbol alphabet, max representable value 2^20) never
// overflows. The encoder offsets distances by +120 to skip the spatial
// LUT region of the spec's distance encoding, so usable raw distances
// max out at (1<<20) - 120.
const maxBackrefDistance = (1 << 20) - 120

// hashBits / hashSize control the LZ77 hash table. 14 bits → 16384
// buckets — good locality, low collision rate for typical inputs.
const (
	hashBits = 14
	hashSize = 1 << hashBits
	hashMask = hashSize - 1
)

// chainDepth caps how many hash-chain candidates we inspect per
// position. Higher = better matches but slower encode.
const chainDepth = 16

// symKind tags a stream record.
type symKind uint8

const (
	symLiteral symKind = iota
	symCacheHit
	symBackref
)

// sym is one element of the encoded pixel stream.
type sym struct {
	kind symKind

	// Literal payload (kind == symLiteral).
	g, r, b, a uint8

	// Cache-hit payload (kind == symCacheHit).
	cacheIdx uint32

	// Backref payload (kind == symBackref).
	lenSym, distSym             uint32
	extraLen, extraDist         uint32
	extraLenBits, extraDistBits uint8
}

// extra is the extra-bit payload that follows a length or distance
// prefix code: `value` is the integer; `bits` is the bit width.
type extra struct {
	value uint32
	bits  uint
}

// buildStream emits the LZ77 + cache symbol stream for pixels.
// Returns the symbol slice plus per-alphabet frequency histograms.
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
			limit := len(pixels) - p
			if limit > matchMaxLen {
				limit = matchMaxLen
			}
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
			for k := 0; k < matchLen; k++ {
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

// hashPair returns a 32-bit hash over pixels[p] and pixels[p+1].
// Callers must ensure p+1 < len(pixels) (insertHash guards this).
func hashPair(pixels [][4]uint8, p int) uint32 {
	a := packARGB(pixels[p][1], pixels[p][0], pixels[p][2], pixels[p][3])
	b := uint32(0)
	if p+1 < len(pixels) {
		b = packARGB(pixels[p+1][1], pixels[p+1][0], pixels[p+1][2], pixels[p+1][3])
	}
	return a*0x9E3779B1 ^ (b * colorCacheMultiplier)
}

// lz77Symbol returns the prefix-code symbol and any extra-bit payload
// for the given value (≥ 1). It is the inverse of x/image/vp8l's
// lz77Param.
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
