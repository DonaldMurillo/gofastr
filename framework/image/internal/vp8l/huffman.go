package vp8l

import "sort"

// maxCodeLength caps Huffman code lengths per the VP8L spec.
const maxCodeLength = 15

// hufLeaf is one active alphabet symbol with its frequency. Sorted
// ascending by weight before package-merge.
type hufLeaf struct {
	w   int
	idx int
}

// codeLengths returns optimal code lengths capped at maxCodeLength
// (the primary-alphabet bound from § 5.2 of the VP8L spec).
func codeLengths(freq []int) []int {
	return codeLengthsLimit(freq, maxCodeLength)
}

// codeLengthsLimit is codeLengths with an explicit upper bound. The
// secondary Huffman over the 19 code-length-code symbols carries each
// length in only 3 bits, so it must be invoked with maxLen=7.
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

// packageMerge runs the package-merge algorithm of Larmore & Hirschberg
// to produce optimal code lengths bounded by L bits. Input leaves must
// be sorted by weight ascending. Returns a slice of length len(leaves)
// where counts[i] is the code length for the i-th leaf in the input.
//
// Algorithm sketch: start with the leaves as items at "level L". For
// each level down to 1, pair adjacent items into "packages" and merge
// those packages back with fresh copies of the leaves (kept sorted).
// After L-1 reduction passes, the first 2N-2 items at level 1 cover
// each leaf k times where k is its code length.
func packageMerge(leaves []hufLeaf, L int) []int {
	n := len(leaves)
	// Items track origin leaves via a parallel slice of (start,end)
	// indices into a flat origins[] buffer. We deliberately avoid
	// per-item slice allocations because per-level merges churn.
	type item struct {
		w       int
		oStart  int
		oLen    int // number of origin entries
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

	// Seed: every leaf is one item with itself as the sole origin.
	base := make([]item, 0, n)
	for i, l := range leaves {
		base = pushItem(base, l.w, i)
	}

	current := make([]item, len(base))
	copy(current, base)

	for level := 1; level < L; level++ {
		// Pair items into packages.
		packCount := len(current) / 2
		packed := make([]item, 0, packCount)
		for k := 0; k < packCount; k++ {
			a := current[2*k]
			b := current[2*k+1]
			// Merge origins (a's then b's).
			s := len(origins)
			origins = append(origins, dupOrigins(a)...)
			origins = append(origins, dupOrigins(b)...)
			packed = append(packed, item{
				w:      a.w + b.w,
				oStart: s,
				oLen:   a.oLen + b.oLen,
			})
		}
		// Sort-merge packed with base.
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

	// First 2N-2 items determine code lengths.
	take := 2*n - 2
	if take > len(current) {
		take = len(current)
	}
	counts := make([]int, n)
	for _, it := range current[:take] {
		for k := 0; k < it.oLen; k++ {
			counts[origins[it.oStart+k]]++
		}
	}
	return counts
}

// canonicalCodes returns the canonical Huffman codes implied by a code-
// length sequence, MSB-first. Symbols with length 0 get code 0. The
// algorithm matches RFC 1951 § 3.2.2 and x/image/vp8l.codeLengthsToCodes.
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

// codeLengthCodeOrder is the slot order in which code-length-code
// lengths are transmitted, per VP8L spec § 5.2.2.
var codeLengthCodeOrder = [19]int{
	17, 18, 0, 1, 2, 3, 4, 5, 16, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
}

// writeHuffmanTree emits a Huffman tree description into bw and returns
// the per-symbol (code, length) tables for pixel emission. Codes are
// returned in stream-bit order: pass them to bw.writeBits directly,
// not bw.writeBitsRev. This abstracts away the simple-vs-normal
// distinction — the caller doesn't need to know which path was taken.
//
// Tree-format rules per VP8L spec:
//
//	0 used symbols → simple code, nSymbols=1, the never-read symbol "0"
//	1 used symbol  → simple code, nSymbols=1, the symbol itself
//	                 (decoder assigns codeLength 0; we emit 0 bits per pixel)
//	2 used symbols → simple code, nSymbols=2
//	                 (decoder assigns code 0 to first, code 1 to second)
//	otherwise      → normal code with the secondary code-length code header.
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

	// Simple-code path encodes the second symbol as an 8-bit value, so
	// it can't represent any symbol ≥ 256. The G alphabet with color
	// cache enabled has symbols up to 280+cacheSize-1; fall back to
	// normal-code when out of range.
	if used <= 2 && (first >= 256 || second >= 256) {
		used = 3 // sentinel: take the normal path
	}

	switch used {
	case 0:
		// Degenerate: nSymbols=1 with placeholder symbol 0. No pixel ever
		// emits a code for this channel.
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
		// Decoder builds tree with codeLength=0 → 0 bits per pixel.
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
		// Decoder assigns code 0 (1 bit) to first, code 1 (1 bit) to second.
		// These are already in stream-bit order (single bit each).
		effLens[first] = 1
		effLens[second] = 1
		codes[first] = 0
		codes[second] = 1
		return
	}

	// Normal-code path: ≥3 used symbols.
	bw.writeBits(0, 1) // useSimple=0

	clcFreq := make([]int, 19)
	for _, l := range lengths {
		// Repeat codes 16/17/18 stay at freq 0 — Phase A doesn't compress
		// length runs. They'll still receive a code slot (length 0) in
		// the code-length code.
		clcFreq[l]++
	}
	// Secondary Huffman lengths carry over a 3-bit field; cap at 7.
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

	// Count distinct code-length values that appear; the decoder's
	// build() treats a single-symbol secondary Huffman as a 0-bit code
	// (nSymbols==1 shortcut), so we must emit zero bits per primary
	// length code in that case — regardless of the 1-bit length we
	// just wrote into clcLengths.
	clcUsed := 0
	for _, cl := range clcLengths {
		if cl > 0 {
			clcUsed++
		}
	}

	if clcUsed != 1 {
		// Emit each primary length via the secondary Huffman code,
		// MSB-first → reversed for the LSB stream.
		for _, l := range lengths {
			bw.writeBitsRev(clcCodes[l], uint(clcLengths[l]))
		}
	}
	// clcUsed == 1: the decoder reads 0 bits per length code; we emit
	// nothing here. The primary alphabet symbols all share one length
	// (positively, or all zero) and the decoder reconstructs them
	// from the secondary tree alone.

	// Build the per-symbol codes for pixel emission. Reverse the
	// canonical codes once so the caller can use writeBits uniformly.
	canon := canonicalCodes(lengths)
	for i, l := range lengths {
		effLens[i] = l
		if l > 0 {
			codes[i] = reverseBits(canon[i], uint(l))
		}
	}
	return
}
