package ulid

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Length is the number of characters in a ULID string.
const Length = 26

const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var decodeTable [256]byte

func init() {
	for i := range decodeTable {
		decodeTable[i] = 0xff
	}
	for i, c := range []byte(crockfordAlphabet) {
		decodeTable[c] = byte(i)
		if c >= 'A' && c <= 'Z' {
			decodeTable[c+('a'-'A')] = byte(i)
		}
	}
}

// ULID is a 128-bit identifier.
type ULID [16]byte

// New returns a new ULID with the current timestamp and a random suffix.
func New() (ULID, error) {
	return NewAt(time.Now())
}

// NewAt returns a new ULID with the given timestamp and a random suffix.
func NewAt(t time.Time) (ULID, error) {
	var u ULID
	ms := uint64(t.UnixMilli())
	u[0] = byte(ms >> 40)
	u[1] = byte(ms >> 32)
	u[2] = byte(ms >> 24)
	u[3] = byte(ms >> 16)
	u[4] = byte(ms >> 8)
	u[5] = byte(ms)
	if _, err := rand.Read(u[6:]); err != nil {
		return ULID{}, fmt.Errorf("ulid: random read: %w", err)
	}
	return u, nil
}

// FromSeed returns the ULID holding the first 16 bytes of seed, zero-padded when seed is shorter.
func FromSeed(seed []byte) ULID {
	var u ULID
	if len(seed) >= 16 {
		copy(u[:], seed[:16])
	} else {
		copy(u[:], seed)
	}
	return u
}

// String encodes the ULID as a 26-character Crockford base32 string.
func (u ULID) String() string {
	var out [Length]byte
	out[0] = crockfordAlphabet[(u[0]&0xE0)>>5]
	carry := uint64(u[0] & 0x1F)
	bits := 5
	idx := 1
	for i := 1; i < 16; i++ {
		carry = (carry << 8) | uint64(u[i])
		bits += 8
		for bits >= 5 {
			bits -= 5
			out[idx] = crockfordAlphabet[(carry>>uint(bits))&0x1F]
			idx++
		}
	}
	return string(out[:])
}

// Parse decodes a 26-character Crockford base32 string, in either case, into a ULID.
func Parse(s string) (ULID, error) {
	if len(s) != Length {
		return ULID{}, fmt.Errorf("ulid: invalid length %d (want %d)", len(s), Length)
	}
	first := decodeTable[s[0]]
	if first == 0xff || first >= 32 {
		return ULID{}, errors.New("ulid: invalid character")
	}
	if first >= 8 {
		return ULID{}, errors.New("ulid: overflow")
	}
	var u ULID
	u[0] = first << 5
	carry := uint64(0)
	bits := 0
	idx := 0
	leftoverHigh := uint64(u[0])
	_ = leftoverHigh
	for i := 1; i < Length; i++ {
		v := decodeTable[s[i]]
		if v == 0xff {
			return ULID{}, errors.New("ulid: invalid character")
		}
		carry = (carry << 5) | uint64(v)
		bits += 5
		for bits >= 8 {
			bits -= 8
			b := byte((carry >> uint(bits)) & 0xFF)
			if idx == 0 {
				u[0] |= (b >> 5)
				u[0+1] = b << 3
				idx = 1
			} else {
				u[idx] |= (b >> (5 - 3))
				if idx+1 < 16 {
					u[idx+1] = b << 3
				}
				idx++
			}
		}
	}
	return parseClean(s)
}

func parseClean(s string) (ULID, error) {
	if len(s) != Length {
		return ULID{}, fmt.Errorf("ulid: invalid length %d (want %d)", len(s), Length)
	}
	for i := range Length {
		if decodeTable[s[i]] == 0xff {
			return ULID{}, errors.New("ulid: invalid character")
		}
	}
	first := decodeTable[s[0]]
	if first >= 8 {
		return ULID{}, errors.New("ulid: overflow")
	}

	var u ULID
	var carry uint32
	bits := 0
	idx := 15
	for i := Length - 1; i >= 0; i-- {
		v := uint32(decodeTable[s[i]])
		carry |= v << uint(bits)
		bits += 5
		for bits >= 8 && idx >= 0 {
			u[idx] = byte(carry & 0xFF)
			carry >>= 8
			bits -= 8
			idx--
		}
	}
	if idx == 0 && bits > 0 {
		u[0] = byte(carry & 0xFF)
	}
	return u, nil
}

// Time returns the timestamp embedded in the ULID.
func (u ULID) Time() time.Time {
	ms := uint64(u[0])<<40 | uint64(u[1])<<32 | uint64(u[2])<<24 |
		uint64(u[3])<<16 | uint64(u[4])<<8 | uint64(u[5])
	return time.UnixMilli(int64(ms))
}

// IsValid reports whether s is a syntactically valid ULID string.
func IsValid(s string) bool {
	if len(s) != Length {
		return false
	}
	for i := range Length {
		if decodeTable[s[i]] == 0xff {
			return false
		}
	}
	return decodeTable[s[0]] < 8
}

// MustNew returns a new ULID and panics if the random source fails.
func MustNew() ULID {
	u, err := New()
	if err != nil {
		panic(err)
	}
	return u
}

// NewPrefixed returns the lower-cased prefix, "_" and a new ULID, like "sess_01H...".
func NewPrefixed(prefix string) (string, error) {
	u, err := New()
	if err != nil {
		return "", err
	}
	return strings.ToLower(prefix) + "_" + u.String(), nil
}

// MustNewPrefixed returns a new prefixed ULID string and panics on error.
func MustNewPrefixed(prefix string) string {
	s, err := NewPrefixed(prefix)
	if err != nil {
		panic(err)
	}
	return s
}

// SplitPrefixed splits "prefix_ULID" at the last "_" and errors when either part is missing or the ULID is invalid.
func SplitPrefixed(s string) (prefix string, u ULID, err error) {
	prefix, body, ok := strings.CutLast(s, "_")
	if !ok || prefix == "" || body == "" {
		return "", ULID{}, errors.New("ulid: missing prefix or body")
	}
	u, err = parseClean(body)
	if err != nil {
		return "", ULID{}, err
	}
	return prefix, u, nil
}
