package vp8l

import "sort"

// maxCodeLength caps Huffman code lengths per the VP8L spec.
const maxCodeLength = 15

// codeLengths returns code lengths for the given symbol frequencies,
// using a length-limited Huffman construction. Symbols with zero
// frequency receive length 0 (i.e., "unused"). When fewer than two
// symbols have non-zero frequency the result still satisfies the
// canonical Huffman property by promoting a sentinel symbol to a
// 1-bit code so a decoder can distinguish "no symbols" from "one
// symbol always".
func codeLengths(freq []int) []int {
	n := len(freq)
	out := make([]int, n)

	// Collect non-zero symbols sorted by frequency ascending.
	type sym struct {
		idx int
		f   int
	}
	syms := make([]sym, 0, n)
	for i, f := range freq {
		if f > 0 {
			syms = append(syms, sym{i, f})
		}
	}
	sort.Slice(syms, func(i, j int) bool { return syms[i].f < syms[j].f })

	// Degenerate cases: 0 or 1 used symbols.
	if len(syms) == 0 {
		return out
	}
	if len(syms) == 1 {
		out[syms[0].idx] = 1
		return out
	}

	// Build a Huffman tree.
	type node struct {
		freq, left, right int
		sym               int // leaf only; -1 for internal
	}
	nodes := make([]node, 0, 2*len(syms)-1)
	for _, s := range syms {
		nodes = append(nodes, node{freq: s.f, sym: s.idx, left: -1, right: -1})
	}

	// Use a simple repeated-min approach. Each iteration combines the
	// two lowest-frequency live nodes into a new internal node.
	live := make([]int, len(nodes))
	for i := range live {
		live[i] = i
	}
	sort.Slice(live, func(i, j int) bool { return nodes[live[i]].freq < nodes[live[j]].freq })

	for len(live) > 1 {
		a, b := live[0], live[1]
		newIdx := len(nodes)
		nodes = append(nodes, node{
			freq:  nodes[a].freq + nodes[b].freq,
			left:  a,
			right: b,
			sym:   -1,
		})
		live = append(live[2:], newIdx)
		// Bubble newIdx left until live is sorted by freq ascending.
		for i := len(live) - 1; i > 0 && nodes[live[i-1]].freq > nodes[live[i]].freq; i-- {
			live[i], live[i-1] = live[i-1], live[i]
		}
	}

	// Walk the tree, accumulating depths into out[].
	var walk func(idx, depth int)
	walk = func(idx, depth int) {
		nd := nodes[idx]
		if nd.sym >= 0 {
			out[nd.sym] = depth
			return
		}
		walk(nd.left, depth+1)
		walk(nd.right, depth+1)
	}
	walk(live[0], 0)

	// Length-limit by iteratively bumping the deepest leaf up and the
	// shallowest non-zero leaf down until the max is within bound.
	// This is a simple but suboptimal approach; the VP8L spec caps at
	// 15 bits which most natural inputs hit without intervention.
	clampLengths(out)
	return out
}

// clampLengths reduces every entry in out to at most maxCodeLength while
// preserving the Kraft inequality (sum of 2^-len <= 1).
func clampLengths(out []int) {
	for {
		maxLen := 0
		for _, l := range out {
			if l > maxLen {
				maxLen = l
			}
		}
		if maxLen <= maxCodeLength {
			return
		}
		// Find a deepest leaf and a shallowest-non-zero leaf that's
		// shorter than maxCodeLength. Push them one bit each way.
		deepest := -1
		shallowest := -1
		for i, l := range out {
			if l == maxLen {
				deepest = i
			}
			if l > 0 && l < maxCodeLength {
				if shallowest < 0 || out[shallowest] > l {
					shallowest = i
				}
			}
		}
		if deepest < 0 || shallowest < 0 || deepest == shallowest {
			return
		}
		out[deepest]--
		out[shallowest]++
	}
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
	clcLengths := codeLengths(clcFreq)
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

	// Emit each symbol's code length via the secondary (code-length)
	// Huffman code. Canonical codes are MSB-first; reverse for the
	// LSB-first stream.
	for _, l := range lengths {
		bw.writeBitsRev(clcCodes[l], uint(clcLengths[l]))
	}

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
