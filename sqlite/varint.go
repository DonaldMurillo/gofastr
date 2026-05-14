package sqlite

// EncodeVarint encodes a signed integer as a SQLite varint (1-9 bytes).
// Uses ZigZag encoding for signed values.
func EncodeVarint(v int64) []byte {
	// ZigZag encoding: map signed integers to unsigned
	uv := uint64(v<<1) ^ uint64(v>>63)
	return EncodeVarintRaw(uv)
}

// DecodeVarint decodes a signed SQLite varint (ZigZag encoded) from a byte slice.
func DecodeVarint(data []byte) (int64, int, error) {
	uv, n, err := DecodeVarintRaw(data)
	if err != nil {
		return 0, 0, err
	}
	// Undo ZigZag
	signed := int64(uv>>1) ^ -int64(uv&1)
	return signed, n, nil
}

// DecodeVarintRaw decodes a raw unsigned SQLite varint.
// SQLite varint format:
//   - Bytes 1-8: high bit is continuation flag, low 7 bits are data (MSB first)
//   - Byte 9 (if present): all 8 bits are data
func DecodeVarintRaw(data []byte) (uint64, int, error) {
	if len(data) == 0 {
		return 0, 0, errVarintEmpty
	}

	var result uint64
	for i := 0; i < 9 && i < len(data); i++ {
		if i == 8 {
			// 9th byte: all 8 bits are data
			result = (result << 8) | uint64(data[i])
			return result, 9, nil
		}
		result = (result << 7) | uint64(data[i]&0x7F)
		if data[i]&0x80 == 0 {
			return result, i + 1, nil
		}
	}

	return 0, 0, errVarintOverflow
}

// EncodeVarintRaw encodes an unsigned integer as a SQLite varint.
// The bytes are in MSB-first order with continuation bits on all bytes
// except the last.
// EncodeVarintInto writes the varint encoding of v into buf starting at offset.
// Returns the number of bytes written.
func EncodeVarintInto(v uint64, buf []byte, off int) int {
	if v == 0 {
		buf[off] = 0
		return 1
	}
	n := VarintSize(v)
	for i := n - 1; i >= 0; i-- {
		if i == 8 {
			buf[off+i] = byte(v)
			v >>= 8
		} else if i == n-1 {
			buf[off+i] = byte(v & 0x7F)
			v >>= 7
		} else {
			buf[off+i] = byte(v&0x7F) | 0x80
			v >>= 7
		}
	}
	return n
}

func EncodeVarintRaw(v uint64) []byte {
	if v == 0 {
		return []byte{0}
	}

	n := VarintSize(v)
	var buf [9]byte

	for i := n - 1; i >= 0; i-- {
		if i == 8 {
			buf[i] = byte(v)
			v >>= 8
		} else if i == n-1 {
			buf[i] = byte(v & 0x7F)
			v >>= 7
		} else {
			buf[i] = byte(v&0x7F) | 0x80
			v >>= 7
		}
	}

	return append([]byte(nil), buf[:n]...)
}

// VarintSize returns the number of bytes needed to encode v as a raw varint.
func VarintSize(v uint64) int {
	if v <= 0x7F {
		return 1
	}
	if v <= 0x3FFF {
		return 2
	}
	if v <= 0x1FFFFF {
		return 3
	}
	if v <= 0x0FFFFFFF {
		return 4
	}
	if v <= 0x07FFFFFFFF {
		return 5
	}
	if v <= 0x03FFFFFFFFFF {
		return 6
	}
	if v <= 0x01FFFFFFFFFFFF {
		return 7
	}
	if v <= 0x00FFFFFFFFFFFFFF {
		return 8
	}
	return 9
}

var (
	errVarintEmpty    = &varintError{"varint: empty data"}
	errVarintOverflow = &varintError{"varint: overflow"}
)

type varintError struct{ msg string }

func (e *varintError) Error() string { return e.msg }
