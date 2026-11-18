package vp8l

import "sort"

const maxCodeLength = 15

type hufLeaf struct {
	w   int
	idx int
}

func codeLengths(freq []int) []int {
	return codeLengthsLimit(freq, maxCodeLength)
}

func codeLengthsLimit(freq []int, maxLen int) []int {
	n := len(freq)
	out := make([]int, n)

	leaves := make([]hufLeaf, 0, n)
	for i, f := range freq {
		if f > 0 {
			leaves = append(leaves, hufLeaf{f, i})
		}
	}
	sort.Slice(leaves, func(i, j int) bool { return leaves[i].w < leaves[j].w })

	if len(leaves) == 0 {
		return out
	}
	if len(leaves) == 1 {
		out[leaves[0].idx] = 1
		return out
	}

	counts := packageMerge(leaves, maxLen)
	for i, c := range counts {
		out[leaves[i].idx] = c
	}
	return out
}

func packageMerge(leaves []hufLeaf, L int) []int {
	n := len(leaves)
	type item struct {
		w      int
		oStart int
		oLen   int // number of origin entries
	}
	origins := make([]int, 0, 4*n*L)
	pushItem := func(items []item, w int, starts ...int) []item {
		s := len(origins)
		origins = append(origins, starts...)
		return append(items, item{w: w, oStart: s, oLen: len(starts)})
	}
	dupOrigins := func(it item) []int {
		out := make([]int, it.oLen)
		copy(out, origins[it.oStart:it.oStart+it.oLen])
		return out
	}

	base := make([]item, 0, n)
	for i, l := range leaves {
		base = pushItem(base, l.w, i)
	}

	current := make([]item, len(base))
	copy(current, base)

	for level := 1; level < L; level++ {
		packCount := len(current) / 2
		packed := make([]item, 0, packCount)
		for k := range packCount {
			a := current[2*k]
			b := current[2*k+1]
			s := len(origins)
			origins = append(origins, dupOrigins(a)...)
			origins = append(origins, dupOrigins(b)...)
			packed = append(packed, item{
				w:      a.w + b.w,
				oStart: s,
				oLen:   a.oLen + b.oLen,
			})
		}
		next := make([]item, 0, len(base)+len(packed))
		i, j := 0, 0
		for i < len(base) && j < len(packed) {
			if base[i].w <= packed[j].w {
				next = append(next, base[i])
				i++
			} else {
				next = append(next, packed[j])
				j++
			}
		}
		next = append(next, base[i:]...)
		next = append(next, packed[j:]...)
		current = next
	}

	take := min(2*n-2, len(current))
	counts := make([]int, n)
	for _, it := range current[:take] {
		for k := 0; k < it.oLen; k++ {
			counts[origins[it.oStart+k]]++
		}
	}
	return counts
}

func canonicalCodes(lengths []int) []uint32 {
	const maxLen = maxCodeLength
	histogram := [maxLen + 1]int{}
	for _, l := range lengths {
		if l > 0 {
			histogram[l]++
		}
	}
	var curr uint32
	nextCode := [maxLen + 1]uint32{}
	for l := 1; l <= maxLen; l++ {
		curr = (curr + uint32(histogram[l-1])) << 1
		nextCode[l] = curr
	}
	codes := make([]uint32, len(lengths))
	for i, l := range lengths {
		if l > 0 {
			codes[i] = nextCode[l]
			nextCode[l]++
		}
	}
	return codes
}

var codeLengthCodeOrder = [19]int{
	17, 18, 0, 1, 2, 3, 4, 5, 16, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
}

func writeHuffmanTree(bw *bitWriter, lengths []int) (codes []uint32, effLens []int) {
	used := 0
	first := -1
	second := -1
	for i, l := range lengths {
		if l > 0 {
			if first < 0 {
				first = i
			} else if second < 0 {
				second = i
			}
			used++
		}
	}
	codes = make([]uint32, len(lengths))
	effLens = make([]int, len(lengths))

	if used <= 2 && (first >= 256 || second >= 256) {
		used = 3 // sentinel: take the normal path
	}

	switch used {
	case 0:
		bw.writeBits(1, 1) // useSimple=1
		bw.writeBits(0, 1) // nSymbols-1 = 0
		bw.writeBits(0, 1) // firstSymbolLengthCode = 0 → 1-bit symbol
		bw.writeBits(0, 1) // the symbol value (1 bit)
		return
	case 1:
		bw.writeBits(1, 1) // useSimple=1
		bw.writeBits(0, 1) // nSymbols-1 = 0
		if first < 2 {
			bw.writeBits(0, 1) // 1-bit symbol form
			bw.writeBits(uint32(first), 1)
		} else {
			bw.writeBits(1, 1) // 8-bit symbol form
			bw.writeBits(uint32(first), 8)
		}
		effLens[first] = 0
		codes[first] = 0
		return
	case 2:
		bw.writeBits(1, 1) // useSimple=1
		bw.writeBits(1, 1) // nSymbols-1 = 1
		if first < 2 {
			bw.writeBits(0, 1) // 1-bit symbol form for first
			bw.writeBits(uint32(first), 1)
		} else {
			bw.writeBits(1, 1) // 8-bit symbol form for first
			bw.writeBits(uint32(first), 8)
		}
		bw.writeBits(uint32(second), 8) // second always 8-bit
		effLens[first] = 1
		effLens[second] = 1
		codes[first] = 0
		codes[second] = 1
		return
	}

	bw.writeBits(0, 1) // useSimple=0

	clcFreq := make([]int, 19)
	for _, l := range lengths {
		clcFreq[l]++
	}
	clcLengths := codeLengthsLimit(clcFreq, 7)
	clcCodes := canonicalCodes(clcLengths)

	nCodes := 19
	for nCodes > 4 && clcLengths[codeLengthCodeOrder[nCodes-1]] == 0 {
		nCodes--
	}
	bw.writeBits(uint32(nCodes-4), 4)
	for i := 0; i < nCodes; i++ {
		bw.writeBits(uint32(clcLengths[codeLengthCodeOrder[i]]), 3)
	}

	bw.writeBits(0, 1) // useLength=0 → maxSymbol = alphabetSize

	clcUsed := 0
	for _, cl := range clcLengths {
		if cl > 0 {
			clcUsed++
		}
	}

	if clcUsed != 1 {
		for _, l := range lengths {
			bw.writeBitsRev(clcCodes[l], uint(clcLengths[l]))
		}
	}

	canon := canonicalCodes(lengths)
	for i, l := range lengths {
		effLens[i] = l
		if l > 0 {
			codes[i] = reverseBits(canon[i], uint(l))
		}
	}
	return
}
