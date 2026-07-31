// Package ulid implements a minimal pure-stdlib ULID generator.
//
// ULIDs are 128-bit identifiers encoded as 26-character Crockford
// base32 strings. They are lexicographically sortable by creation
// time and URL-safe. The first 48 bits are the millisecond
// timestamp; the remaining 80 bits are random.
//
// This implementation is intentionally minimal — it covers what
// the harness needs (generate, parse, validate) and nothing else.
// Spec: https://github.com/ulid/spec
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

// crockfordAlphabet is Crockford's base32: 0-9 + A-Z, omitting I L O U.
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// decodeTable maps a Crockford byte to its 5-bit value, or 0xff if invalid.
var decodeTable [256]byte

func init() {
	for i := range decodeTable {
		decodeTable[i] = 0xff
	}
	for i, c := range []byte(crockfordAlphabet) {
		decodeTable[c] = byte(i)
		// Crockford is case-insensitive on input.
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
	// First 6 bytes: timestamp in milliseconds, big-endian.
	u[0] = byte(ms >> 40)
	u[1] = byte(ms >> 32)
	u[2] = byte(ms >> 24)
	u[3] = byte(ms >> 16)
	u[4] = byte(ms >> 8)
	u[5] = byte(ms)
	// Remaining 10 bytes: cryptographic random.
	if _, err := rand.Read(u[6:]); err != nil {
		return ULID{}, fmt.Errorf("ulid: random read: %w", err)
	}
	return u, nil
}

// FromSeed returns a deterministic ULID derived from the given 16-byte
// seed (typically a SHA-256 prefix). Used for branch ID rewriting where
// two clients branching the same boundary must produce the same new IDs.
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
//
// The 128-bit value is encoded as 26 base32 characters (130 bits with
// the top 2 bits zero). The first character covers the top 3 bits of
// the timestamp; the remaining 25 characters cover 125 bits, totaling
// 128 bits + 2 leading zero bits.
func (u ULID) String() string {
	// Spec-accurate encoding: 25 characters of 5 bits each = 125 bits,
	// plus 1 character of 3 bits = 128 bits total.
	var out [Length]byte
	// First character: top 3 bits of byte 0.
	out[0] = crockfordAlphabet[(u[0]&0xE0)>>5]
	// Next characters pack 5 bits each from the 128-bit value.
	// We treat the remaining 125 bits as a stream starting at bit 3 of byte 0.
	// Easiest: shift-register approach.
	carry := uint64(u[0] & 0x1F) // 5 bits
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

// Parse decodes a 26-character Crockford base32 string into a ULID.
func Parse(s string) (ULID, error) {
	if len(s) != Length {
		return ULID{}, fmt.Errorf("ulid: invalid length %d (want %d)", len(s), Length)
	}
	// First character contributes the top 3 bits.
	first := decodeTable[s[0]]
	if first == 0xff || first >= 32 {
		return ULID{}, errors.New("ulid: invalid character")
	}
	if first >= 8 {
		// First char encodes 5 bits, but only the bottom 3 can fit.
		// Crockford ULIDs with first char > '7' overflow 128 bits.
		return ULID{}, errors.New("ulid: overflow")
	}
	var u ULID
	u[0] = first << 5
	// Shift-register decode for the remaining 25 characters.
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
	// Simpler approach: re-encode and compare on round-trip for tests.
	// The bit-stuffing above is error-prone; we use a cleaner re-derivation.
	return parseClean(s)
}

// parseClean is a straightforward decoder used as the canonical
// implementation. It treats the ULID as a 130-bit big-endian integer
// (top 2 bits forced to zero) and writes the bottom 128 bits to the ULID.
func parseClean(s string) (ULID, error) {
	if len(s) != Length {
		return ULID{}, fmt.Errorf("ulid: invalid length %d (want %d)", len(s), Length)
	}
	// Validate.
	for i := 0; i < Length; i++ {
		if decodeTable[s[i]] == 0xff {
			return ULID{}, errors.New("ulid: invalid character")
		}
	}
	first := decodeTable[s[0]]
	if first >= 8 {
		return ULID{}, errors.New("ulid: overflow")
	}

	// Accumulate the 26 base32 chars (130 bits) into a 16-byte big-endian
	// integer (128 bits with the top 2 bits dropped — they must be zero).
	var u ULID
	var carry uint32
	bits := 0
	idx := 15 // write from LSB up
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
	for i := 0; i < Length; i++ {
		if decodeTable[s[i]] == 0xff {
			return false
		}
	}
	return decodeTable[s[0]] < 8
}

// MustNew returns a new ULID and panics on error.
// Only the random source can fail; in practice this never happens.
func MustNew() ULID {
	u, err := New()
	if err != nil {
		panic(err)
	}
	return u
}

// NewPrefixed returns a new prefixed ULID string like "sess_01H...".
// The prefix is lower-cased and joined with "_".
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

// SplitPrefixed splits a prefixed ULID into (prefix, ulid). The ULID
// portion is validated. Returns an error on malformed input.
func SplitPrefixed(s string) (prefix string, u ULID, err error) {
	idx := strings.LastIndex(s, "_")
	if idx <= 0 || idx >= len(s)-1 {
		return "", ULID{}, errors.New("ulid: missing prefix or body")
	}
	body := s[idx+1:]
	u, err = parseClean(body)
	if err != nil {
		return "", ULID{}, err
	}
	return s[:idx], u, nil
}
