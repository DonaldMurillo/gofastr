package sqlite

import (
	"testing"
)

// ============================================================================
// Varint encode/decode round-trip tests
// ============================================================================

func TestVarintZero(t *testing.T) {
	testVarintRoundtripRaw(t, 0, []byte{0})
}

func TestVarintSmallValues(t *testing.T) {
	testVarintRoundtripRaw(t, 1, []byte{1})
	testVarintRoundtripRaw(t, 127, []byte{127})
	testVarintRoundtripRaw(t, 128, []byte{0x81, 0x00})
}

func TestVarintMediumValues(t *testing.T) {
	testVarintRoundtripRaw(t, 16383, []byte{0xFF, 0x7F})
	testVarintRoundtripRaw(t, 16384, []byte{0x81, 0x80, 0x00})
}

func TestVarintLargeValues(t *testing.T) {
	testVarintRoundtripRaw(t, 2097151, []byte{0xFF, 0xFF, 0x7F})
	testVarintRoundtripRaw(t, 1<<21, nil) // Just roundtrip, don't check bytes
	testVarintRoundtripRaw(t, 1<<28-1, nil)
	testVarintRoundtripRaw(t, 1<<28, nil)
	testVarintRoundtripRaw(t, 1<<35-1, nil)
	testVarintRoundtripRaw(t, 1<<35, nil)
}

func TestVarintMaxValues(t *testing.T) {
	// Maximum uint64 value
	testVarintRoundtripRaw(t, 0xFFFFFFFFFFFFFFFF, nil)
	// Large but not max
	testVarintRoundtripRaw(t, 0x7FFFFFFFFFFFFFFF, nil)
	testVarintRoundtripRaw(t, 1, nil)
	testVarintRoundtripRaw(t, 1000000, nil)
	testVarintRoundtripRaw(t, 1000000000, nil)
}

func TestVarintPowersOfTwo(t *testing.T) {
	for i := 0; i < 64; i++ {
		v := uint64(1) << uint(i)
		testVarintRoundtripRaw(t, v, nil)
	}
}

func TestVarintDecodeExact(t *testing.T) {
	tests := []struct {
		bytes  []byte
		value  uint64
		nBytes int
	}{
		{[]byte{0x00}, 0, 1},
		{[]byte{0x01}, 1, 1},
		{[]byte{0x7F}, 127, 1},
		{[]byte{0x81, 0x00}, 128, 2},
		{[]byte{0x81, 0x01}, 129, 2},
		{[]byte{0xFF, 0x7F}, 16383, 2},
		{[]byte{0x81, 0x80, 0x00}, 16384, 3},
		{[]byte{0xFF, 0xFF, 0x7F}, 2097151, 3},
	}

	for _, tt := range tests {
		v, n, err := DecodeVarintRaw(tt.bytes)
		if err != nil {
			t.Errorf("DecodeVarintRaw(%v): unexpected error: %v", tt.bytes, err)
			continue
		}
		if v != tt.value {
			t.Errorf("DecodeVarintRaw(%v) = %d, want %d", tt.bytes, v, tt.value)
		}
		if n != tt.nBytes {
			t.Errorf("DecodeVarintRaw(%v): consumed %d bytes, want %d", tt.bytes, n, tt.nBytes)
		}
	}
}

func TestVarintDecodeEmpty(t *testing.T) {
	_, _, err := DecodeVarintRaw(nil)
	if err == nil {
		t.Error("DecodeVarintRaw(nil) should return error")
	}

	_, _, err = DecodeVarintRaw([]byte{})
	if err == nil {
		t.Error("DecodeVarintRaw([]) should return error")
	}
}

func TestVarintWithExtraData(t *testing.T) {
	data := []byte{0x81, 0x00, 0xFF, 0xFF}
	v, n, err := DecodeVarintRaw(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 128 {
		t.Errorf("got %d, want 128", v)
	}
	if n != 2 {
		t.Errorf("consumed %d bytes, want 2", n)
	}
}

func TestVarintSize(t *testing.T) {
	tests := []struct {
		v    uint64
		size int
	}{
		{0, 1},
		{1, 1},
		{127, 1},
		{128, 2},
		{16383, 2},
		{16384, 3},
		{2097151, 3},
		{2097152, 4},
	}

	for _, tt := range tests {
		s := VarintSize(tt.v)
		if s != tt.size {
			t.Errorf("VarintSize(%d) = %d, want %d", tt.v, s, tt.size)
		}
	}
}

func TestVarintSignedRoundtrip(t *testing.T) {
	values := []int64{
		0, 1, -1, 2, -2,
		127, -127, 128, -128,
		255, -255, 256, -256,
		65535, -65535, 65536, -65536,
		2147483647, -2147483648,
		9223372036854775807, -9223372036854775808,
	}

	for _, v := range values {
		encoded := EncodeVarint(v)
		decoded, n, err := DecodeVarint(encoded)
		if err != nil {
			t.Errorf("DecodeVarint(%d): unexpected error: %v", v, err)
			continue
		}
		if decoded != v {
			t.Errorf("roundtrip(%d): got %d", v, decoded)
		}
		if n != len(encoded) {
			t.Errorf("roundtrip(%d): consumed %d bytes, encoded as %d", v, n, len(encoded))
		}
	}
}

func TestVarintFuzzValues(t *testing.T) {
	// Deterministic "fuzz" - test a wide range of values
	for i := int64(0); i < 10000; i++ {
		testVarintSignedRoundtrip(t, i)
		testVarintSignedRoundtrip(t, -i)
	}
}

func TestVarintExhaustive1Byte(t *testing.T) {
	// Every value that fits in 1 byte
	for v := uint64(0); v <= 127; v++ {
		enc := EncodeVarintRaw(v)
		if len(enc) != 1 {
			t.Errorf("EncodeVarintRaw(%d) = %d bytes, want 1", v, len(enc))
		}
		dec, n, err := DecodeVarintRaw(enc)
		if err != nil {
			t.Errorf("DecodeVarintRaw(%d): error: %v", v, err)
		}
		if dec != v {
			t.Errorf("roundtrip(%d): got %d", v, dec)
		}
		if n != 1 {
			t.Errorf("consumed %d bytes for %d, want 1", n, v)
		}
	}
}

// ============================================================================
// Helpers
// ============================================================================

func testVarintRoundtripRaw(t *testing.T, v uint64, expected []byte) {
	t.Helper()
	encoded := EncodeVarintRaw(v)
	if expected != nil && !bytesEqual(encoded, expected) {
		t.Errorf("EncodeVarintRaw(%d) = %v, want %v", v, encoded, expected)
	}

	decoded, n, err := DecodeVarintRaw(encoded)
	if err != nil {
		t.Errorf("DecodeVarintRaw(%d): unexpected error: %v", v, err)
		return
	}
	if decoded != v {
		t.Errorf("roundtrip(%d): got %d", v, decoded)
	}
	if n != len(encoded) {
		t.Errorf("roundtrip(%d): consumed %d, encoded %d bytes", v, n, len(encoded))
	}

	// Verify size prediction
	if predicted := VarintSize(v); predicted != len(encoded) {
		t.Errorf("VarintSize(%d) = %d, but encoded as %d bytes", v, predicted, len(encoded))
	}
}

func testVarintSignedRoundtrip(t *testing.T, v int64) {
	t.Helper()
	encoded := EncodeVarint(v)
	decoded, n, err := DecodeVarint(encoded)
	if err != nil {
		t.Errorf("signed roundtrip(%d): error: %v", v, err)
		return
	}
	if decoded != v {
		t.Errorf("signed roundtrip(%d): got %d", v, decoded)
	}
	if n != len(encoded) {
		t.Errorf("signed roundtrip(%d): consumed %d, encoded %d", v, n, len(encoded))
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
