package vp8l

import "testing"

// TestR4_DistanceSymbol_AllValues sweeps every legal distance value
// (1..2^20) and asserts the prefix-code symbol stays within the
// 40-symbol alphabet. Property test rather than spot-check — covers
// every distinct symbol partition the lz77Symbol algorithm produces.
func TestR4_DistanceSymbol_AllValues(t *testing.T) {
	for v := uint32(1); v <= (1 << 20); v++ {
		sym, extra := lz77Symbol(v)
		if sym >= nDistanceCodes {
			t.Fatalf("value=%d → symbol=%d, want <%d", v, sym, nDistanceCodes)
		}
		// Spec extra-bits cap: distance symbol's extra-bits is at most
		// 18 (for symbols 38/39 covering the upper half of 2^20).
		if extra.bits > 18 {
			t.Fatalf("value=%d → extra.bits=%d, want ≤18", v, extra.bits)
		}
	}
	// Pin specific spec values from the VP8L bitstream spec.
	for _, c := range []struct {
		v   uint32
		sym uint32
	}{
		{1, 0}, {2, 1}, {3, 2}, {4, 3},
		{5, 4}, {7, 5}, {9, 6}, {13, 7},
		{1 << 20, 39},
	} {
		sym, _ := lz77Symbol(c.v)
		if sym != c.sym {
			t.Errorf("lz77Symbol(%d) symbol = %d, want %d", c.v, sym, c.sym)
		}
	}
}

// TestR4_LengthSymbol_AllValues sweeps every legal length value
// (2..4096) and asserts the symbol fits inside the 24-symbol length
// alphabet. matchMaxLen guarantees the upper end.
func TestR4_LengthSymbol_AllValues(t *testing.T) {
	for v := uint32(2); v <= matchMaxLen; v++ {
		sym, _ := lz77Symbol(v)
		if sym >= nLengthCodes {
			t.Fatalf("length=%d → symbol=%d, want <%d", v, sym, nLengthCodes)
		}
	}
}

// TestR4_DistanceSymbol_InverseProperty asserts lz77Symbol's output
// can be inverted by the spec's lz77Param formula — i.e. encode then
// decode reproduces the input. Catches off-by-one shifts in the
// symbol-to-value math.
func TestR4_DistanceSymbol_InverseProperty(t *testing.T) {
	// Spot-check across the partitioning boundaries.
	for _, v := range []uint32{1, 4, 5, 6, 7, 8, 9, 12, 13, 16, 17, 32, 33, 100, 256, 1 << 10, 1 << 15, 1 << 20} {
		sym, extra := lz77Symbol(v)
		got := lz77Param(sym, extra.value)
		if got != v {
			t.Errorf("inverse(lz77Symbol(%d)) = %d, want %d (sym=%d extra.value=%d extra.bits=%d)",
				v, got, v, sym, extra.value, extra.bits)
		}
	}
}

// lz77Param mirrors the decoder's formula at golang.org/x/image/vp8l.
// Local copy keeps this test self-contained — it's the inverse of
// our lz77Symbol and the property test above asserts they match for
// every value across the partitioning boundaries.
func lz77Param(symbol, extra uint32) uint32 {
	if symbol < 4 {
		return symbol + 1
	}
	extraBits := (symbol - 2) >> 1
	offset := (2 + (symbol & 1)) << extraBits
	return offset + extra + 1
}
