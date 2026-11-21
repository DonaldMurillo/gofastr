package vp8l

import (
	"bytes"
	"testing"
)

func TestSweepVP8LOneLengthTreeWritesNoPerSymbolBits(t *testing.T) {
	lengths := make([]int, 256)
	for i := range lengths {
		lengths[i] = 8
	}
	var bw bitWriter
	writeHuffmanTree(&bw, lengths)
	if n := len(bw.Bytes()); n > 12 {
		t.Fatalf("a tree whose 256 symbols share one length took %d bytes, want the header alone", n)
	}
}

func TestUnusedSymbolForcesLengthBits(t *testing.T) {
	lengths := []int{2, 2, 2, 2, 0}
	var bw bitWriter
	writeHuffmanTree(&bw, lengths)
	const headerBits = 1 + 4 + 5*3 + 1
	wantBits := headerBits + len(lengths)
	if got := len(bw.buf)*8 + int(bw.n); got != wantBits {
		t.Fatalf("tree wrote %d bits, want %d (header and one bit per length)", got, wantBits)
	}
}

func TestSweepVP8LKeepsTheSmallestCandidate(t *testing.T) {
	src := gradient(48, 32)
	var all bytes.Buffer
	if err := Encode(&all, src); err != nil {
		t.Fatal(err)
	}
	for _, mode := range candidatePredictorModes {
		one, err := encodeWithMode(src, 48, 32, src.Bounds(), mode)
		if err != nil {
			t.Fatal(err)
		}
		if all.Len() > len(one) {
			t.Fatalf("Encode wrote %d bytes, mode %d alone %d", all.Len(), mode, len(one))
		}
	}
}

func TestSweepVP8LLengthLimitHoldsOnFibonacci(t *testing.T) {
	freq := make([]int, 30)
	a, b := 1, 1
	for i := range freq {
		freq[i] = a
		a, b = b, a+b
	}
	for _, limit := range []int{7, maxCodeLength} {
		kraft := 0.0
		for _, l := range codeLengthsLimit(freq, limit) {
			if l > limit {
				t.Fatalf("limit %d: a code of length %d", limit, l)
			}
			if l > 0 {
				kraft += 1 / float64(uint64(1)<<l)
			}
		}
		if kraft > 1+1e-9 {
			t.Fatalf("limit %d: Kraft sum %f", limit, kraft)
		}
	}
}
