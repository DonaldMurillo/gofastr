// Package jcs implements RFC 8785 (JSON Canonicalization Scheme) using
// only the Go standard library.
//
// Canonical JSON is the invariant byte sequence cryptographic operations
// (signing, hashing) need: whitespace-free, keys recursively sorted by
// UTF-16 code unit, the minimal ECMAScript escape set for strings, and
// IEEE 754 numbers serialized exactly as ECMAScript's
// Number::toString would (1e21 → "1e+21", 1e-7 → "1e-7", -0 → "0").
//
// Two entry points:
//
//   - CanonicalizeJSON canonicalizes raw JSON bytes. Parsing is strict
//     beyond RFC 8259 where JCS requires it: duplicate object keys,
//     unpaired surrogate escapes, invalid UTF-8, control characters in
//     string literals, and literals that overflow to ±Infinity are all
//     errors. Use this on untrusted input (a verifier canonicalizing a
//     received document).
//   - Canonicalize canonicalizes an in-memory Go value (maps, slices,
//     structs, strings, numbers) by round-tripping it through
//     encoding/json first. It inherits encoding/json's behavior of
//     silently replacing invalid UTF-8 in strings with U+FFFD; for
//     untrusted strings, parse with CanonicalizeJSON instead. NaN and
//     ±Infinity are rejected.
//
// Numbers follow the I-JSON subset (RFC 7493): every JSON number is
// parsed to an IEEE 754 double and re-serialized. An integer beyond
// 2^53 therefore rounds like any other double (RFC 8785 Appendix D
// recommends carrying such values as strings instead). Integers that
// underflow the double range (1e-400) canonicalize to 0; literals that
// overflow to ±Infinity (1e999) are rejected, as JCS does not permit
// Infinity in JSON.
//
// The output is deterministic for equal input values: object keys are
// collected and sorted before anything is written, never emitted in
// map-iteration order.
package jcs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// maxDepth bounds recursion so hostile nesting fails with an error
// instead of a stack overflow (RFC 8785 §5 recommends sanity checks on
// input data).
const maxDepth = 512

// Canonicalize returns the RFC 8785 canonical form of v. v may be any
// value encoding/json can marshal; json.Number values pass through as
// literals and are then validated and reformatted by the canonical
// number rules. NaN and ±Infinity return an error.
func Canonicalize(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return CanonicalizeJSON(bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
}

// CanonicalizeJSON parses raw as JSON (strictly, see the package doc)
// and returns its RFC 8785 canonical form. raw must contain exactly one
// JSON value; trailing content is an error.
func CanonicalizeJSON(raw []byte) ([]byte, error) {
	p := &parser{data: raw}
	p.skipWS()
	out, err := p.parseValue(0)
	if err != nil {
		return nil, err
	}
	p.skipWS()
	if p.pos != len(p.data) {
		return nil, p.errorf("unexpected trailing data at offset %d", p.pos)
	}
	return out, nil
}

// ─── Strict JSON parser → canonical bytes ───────────────────────────

type parser struct {
	data []byte
	pos  int
}

func (p *parser) errorf(format string, args ...any) error {
	return fmt.Errorf("jcs: offset %d: %s", p.pos, fmt.Sprintf(format, args...))
}

func (p *parser) skipWS() {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *parser) parseValue(depth int) ([]byte, error) {
	if depth > maxDepth {
		return nil, p.errorf("nesting exceeds %d levels", maxDepth)
	}
	if p.pos >= len(p.data) {
		return nil, p.errorf("unexpected end of input")
	}
	switch c := p.data[p.pos]; {
	case c == '{':
		return p.parseObject(depth)
	case c == '[':
		return p.parseArray(depth)
	case c == '"':
		s, err := p.parseString()
		if err != nil {
			return nil, err
		}
		return appendCanonicalString(nil, s), nil
	case c == 't':
		return p.parseLiteral("true")
	case c == 'f':
		return p.parseLiteral("false")
	case c == 'n':
		return p.parseLiteral("null")
	case c == '-' || (c >= '0' && c <= '9'):
		return p.parseNumber()
	default:
		return nil, p.errorf("unexpected character %q", string(rune(c)))
	}
}

func (p *parser) parseLiteral(lit string) ([]byte, error) {
	if p.pos+len(lit) > len(p.data) || string(p.data[p.pos:p.pos+len(lit)]) != lit {
		return nil, p.errorf("invalid literal (expected %q)", lit)
	}
	p.pos += len(lit)
	return []byte(lit), nil
}

func (p *parser) parseObject(depth int) ([]byte, error) {
	p.pos++ // consume '{'
	type member struct {
		keyU16 []uint16 // key as UTF-16 code units, the JCS sort key
		keyStr string
		val    []byte
	}
	var members []member
	seen := map[string]bool{}

	p.skipWS()
	if p.pos < len(p.data) && p.data[p.pos] == '}' {
		p.pos++
		return []byte("{}"), nil
	}
	for {
		p.skipWS()
		if p.pos >= len(p.data) || p.data[p.pos] != '"' {
			return nil, p.errorf("expected object key string")
		}
		key, err := p.parseString()
		if err != nil {
			return nil, err
		}
		if seen[key] {
			return nil, p.errorf("duplicate object key %q (I-JSON forbids duplicate names)", key)
		}
		seen[key] = true

		p.skipWS()
		if p.pos >= len(p.data) || p.data[p.pos] != ':' {
			return nil, p.errorf("expected ':' after object key")
		}
		p.pos++
		p.skipWS()
		val, err := p.parseValue(depth + 1)
		if err != nil {
			return nil, err
		}
		members = append(members, member{
			keyU16: utf16.Encode([]rune(key)),
			keyStr: key,
			val:    val,
		})

		p.skipWS()
		if p.pos >= len(p.data) {
			return nil, p.errorf("unterminated object")
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case '}':
			p.pos++
			// RFC 8785 §3.2.3: sort members by property name as arrays
			// of UTF-16 code units before emitting anything.
			sort.Slice(members, func(i, j int) bool {
				return utf16Less(members[i].keyU16, members[j].keyU16)
			})
			out := make([]byte, 0, 64)
			out = append(out, '{')
			for i := range members {
				if i > 0 {
					out = append(out, ',')
				}
				out = appendCanonicalString(out, members[i].keyStr)
				out = append(out, ':')
				out = append(out, members[i].val...)
			}
			return append(out, '}'), nil
		default:
			return nil, p.errorf("expected ',' or '}' in object")
		}
	}
}

func (p *parser) parseArray(depth int) ([]byte, error) {
	p.pos++ // consume '['
	out := []byte{'['}
	p.skipWS()
	if p.pos < len(p.data) && p.data[p.pos] == ']' {
		p.pos++
		return append(out, ']'), nil
	}
	for {
		p.skipWS()
		val, err := p.parseValue(depth + 1)
		if err != nil {
			return nil, err
		}
		if len(out) > 1 {
			out = append(out, ',')
		}
		out = append(out, val...)

		p.skipWS()
		if p.pos >= len(p.data) {
			return nil, p.errorf("unterminated array")
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return append(out, ']'), nil
		default:
			return nil, p.errorf("expected ',' or ']' in array")
		}
	}
}

// parseString scans a JSON string literal (opening quote at p.pos) and
// returns the decoded Go string. It rejects raw control characters,
// unpaired \uXXXX surrogate escapes, and invalid UTF-8 in raw bytes.
func (p *parser) parseString() (string, error) {
	p.pos++ // consume '"'
	var sb []byte
	for {
		if p.pos >= len(p.data) {
			return "", p.errorf("unterminated string")
		}
		c := p.data[p.pos]
		switch {
		case c == '"':
			p.pos++
			if !utf8.Valid(sb) {
				return "", p.errorf("invalid UTF-8 in string")
			}
			return string(sb), nil
		case c == '\\':
			p.pos++
			if p.pos >= len(p.data) {
				return "", p.errorf("unterminated escape")
			}
			e := p.data[p.pos]
			p.pos++
			switch e {
			case '"', '\\', '/':
				sb = append(sb, e)
			case 'b':
				sb = append(sb, '\b')
			case 'f':
				sb = append(sb, '\f')
			case 'n':
				sb = append(sb, '\n')
			case 'r':
				sb = append(sb, '\r')
			case 't':
				sb = append(sb, '\t')
			case 'u':
				r, err := p.parseUnicodeEscape()
				if err != nil {
					return "", err
				}
				sb = utf8.AppendRune(sb, r)
			default:
				return "", p.errorf("invalid escape \\%s", string(rune(e)))
			}
		case c < 0x20:
			return "", p.errorf("raw control character U+%04X in string", c)
		default:
			sb = append(sb, c)
			p.pos++
		}
	}
}

// parseUnicodeEscape reads the four hex digits after \u (p.pos is on
// the first digit), combines surrogate pairs, and rejects lone
// surrogates per RFC 8785 §3.2.2.2.
func (p *parser) parseUnicodeEscape() (rune, error) {
	hi, err := p.readHex4()
	if err != nil {
		return 0, err
	}
	if utf16.IsSurrogate(rune(hi)) {
		// Must be a \uXXXX\uYYYY pair; a lone surrogate is invalid
		// Unicode data and MUST be rejected (RFC 8785 §3.2.2.2).
		if hi >= 0xDC00 {
			return 0, p.errorf("unpaired low surrogate \\u%04X", hi)
		}
		if p.pos+1 >= len(p.data) || p.data[p.pos] != '\\' || p.data[p.pos+1] != 'u' {
			return 0, p.errorf("unpaired high surrogate \\u%04X", hi)
		}
		save := p.pos
		p.pos += 2 // skip \u
		lo, err := p.readHex4()
		if err != nil || lo < 0xDC00 || lo > 0xDFFF {
			p.pos = save
			return 0, p.errorf("unpaired high surrogate \\u%04X", hi)
		}
		return utf16.DecodeRune(rune(hi), rune(lo)), nil
	}
	return rune(hi), nil
}

func (p *parser) readHex4() (int, error) {
	if p.pos+4 > len(p.data) {
		return 0, p.errorf("truncated \\u escape")
	}
	v := 0
	for i := range 4 {
		c := p.data[p.pos+i]
		var d int
		switch {
		case c >= '0' && c <= '9':
			d = int(c - '0')
		case c >= 'a' && c <= 'f':
			d = int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = int(c-'A') + 10
		default:
			return 0, p.errorf("invalid hex digit %q in \\u escape", string(rune(c)))
		}
		v = v<<4 | d
	}
	p.pos += 4
	return v, nil
}

// parseNumber scans a JSON number literal per RFC 8259 §6 and returns
// its canonical ECMAScript serialization.
func (p *parser) parseNumber() ([]byte, error) {
	start := p.pos
	if p.pos < len(p.data) && p.data[p.pos] == '-' {
		p.pos++
	}
	// int part: "0" or [1-9][0-9]*
	if p.pos >= len(p.data) {
		return nil, p.errorf("truncated number")
	}
	if p.data[p.pos] == '0' {
		p.pos++
	} else if p.data[p.pos] >= '1' && p.data[p.pos] <= '9' {
		for p.pos < len(p.data) && isDigit(p.data[p.pos]) {
			p.pos++
		}
	} else {
		return nil, p.errorf("invalid number")
	}
	// frac
	if p.pos < len(p.data) && p.data[p.pos] == '.' {
		p.pos++
		if p.pos >= len(p.data) || !isDigit(p.data[p.pos]) {
			return nil, p.errorf("digit required after decimal point")
		}
		for p.pos < len(p.data) && isDigit(p.data[p.pos]) {
			p.pos++
		}
	}
	// exp
	if p.pos < len(p.data) && (p.data[p.pos] == 'e' || p.data[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.data) && (p.data[p.pos] == '+' || p.data[p.pos] == '-') {
			p.pos++
		}
		if p.pos >= len(p.data) || !isDigit(p.data[p.pos]) {
			return nil, p.errorf("digit required in exponent")
		}
		for p.pos < len(p.data) && isDigit(p.data[p.pos]) {
			p.pos++
		}
	}
	lit := p.data[start:p.pos]
	f, err := strconv.ParseFloat(string(lit), 64)
	if err != nil {
		// Grammar already validated syntax, so the only error is
		// range. Overflow parses to ±Inf, which JSON does not permit;
		// underflow parses to ±0 and canonicalizes to "0".
		var numErr *strconv.NumError
		if errors.As(err, &numErr) && numErr.Err == strconv.ErrRange {
			if math.IsInf(f, 0) {
				return nil, p.errorf("number %s overflows to Infinity (not valid JSON)", lit)
			}
			return []byte(formatNumber(f)), nil
		}
		return nil, p.errorf("invalid number %s", lit)
	}
	return []byte(formatNumber(f)), nil
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// ─── Canonical string serialization (RFC 8785 §3.2.2.2) ────────────

// appendCanonicalString appends s as a canonical JSON string: the
// short escapes \b \t \n \f \r \" \\, other characters below U+0020 as
// lowercase \u00xx, everything else literal UTF-8. '/' is never
// escaped.
func appendCanonicalString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			dst = append(dst, '\\', '"')
		case c == '\\':
			dst = append(dst, '\\', '\\')
		case c == '\b':
			dst = append(dst, '\\', 'b')
		case c == '\t':
			dst = append(dst, '\\', 't')
		case c == '\n':
			dst = append(dst, '\\', 'n')
		case c == '\f':
			dst = append(dst, '\\', 'f')
		case c == '\r':
			dst = append(dst, '\\', 'r')
		case c < 0x20:
			dst = append(dst, '\\', 'u', '0', '0', hexDigit(c>>4), hexDigit(c&0xF))
		default:
			dst = append(dst, c)
		}
	}
	return append(dst, '"')
}

func hexDigit(v byte) byte {
	if v < 10 {
		return '0' + v
	}
	return 'a' + v - 10
}

// ─── Canonical number serialization (RFC 8785 §3.2.2.3) ────────────

// formatNumber renders f exactly as ECMAScript's Number::toString
// (ECMA-262 §7.1.12.1 with Note 2): shortest round-trip digits, plain
// decimal for 1e-7 ≤ |f| < 1e21, exponential outside that range with
// no exponent padding, and both zeroes as "0".
func formatNumber(f float64) string {
	if f == 0 {
		return "0" // covers -0: String(-0) is "0"
	}
	neg := ""
	if f < 0 || math.Signbit(f) {
		neg = "-"
		f = -f
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		// Unreachable from JSON input; guards direct callers.
		return neg + "NaN"
	}

	// Shortest round-trip digits in exponential form:
	// "d.dddde±XX" → digits d1..dk, with value = d1.d2..dk × 10^exp,
	// i.e. digit string s = d1..dk and ECMAScript's n = exp + 1.
	sci := strconv.FormatFloat(f, 'e', -1, 64)
	epos := byteIndex(sci, 'e')
	mantissa := sci[:epos]
	exp, _ := strconv.Atoi(sci[epos+1:])
	digits := make([]byte, 0, len(mantissa))
	for i := range mantissa {
		if mantissa[i] != '.' {
			digits = append(digits, mantissa[i])
		}
	}
	k := len(digits) // digit count in s
	n := exp + 1     // position of the decimal point relative to s

	var b []byte
	switch {
	case k <= n && n <= 21:
		// s followed by n−k zeros: an integer like 100 or 2^68's
		// 295147905179352830000.
		b = append(b, digits...)
		for i := 0; i < n-k; i++ {
			b = append(b, '0')
		}
	case 0 < n && n <= 21:
		// Decimal point inside s: "333333333.3333333".
		b = append(b, digits[:n]...)
		b = append(b, '.')
		b = append(b, digits[n:]...)
	case -6 < n && n <= 0:
		// "0." + (−n) zeros + s: 0.002, 0.000001.
		b = append(b, '0', '.')
		for range -n {
			b = append(b, '0')
		}
		b = append(b, digits...)
	default:
		// Exponential: d1 ["." d2..dk] "e" ("+"|"−") (n−1), exponent
		// never zero-padded: 1e+21, 1e-7, 1.0000000000000001e+23.
		b = append(b, digits[0])
		if k > 1 {
			b = append(b, '.')
			b = append(b, digits[1:]...)
		}
		b = append(b, 'e')
		if n-1 >= 0 {
			b = append(b, '+')
		} else {
			b = append(b, '-')
		}
		b = strconv.AppendInt(b, int64(abs(n-1)), 10)
	}
	return neg + string(b)
}

func byteIndex(s string, c byte) int {
	for i := range s {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// ─── UTF-16 key ordering (RFC 8785 §3.2.3) ──────────────────────────

// sortKeysByUTF16 sorts keys by their UTF-16 code unit sequences,
// which differs from Go's byte-wise string order for non-BMP
// characters: U+10000+ encode as surrogate pairs (0xD800–0xDBFF) and
// sort before U+E000–U+FFFF, while their UTF-8 bytes sort after.
//
// Used by tests and kept here as the reference comparator.
func sortKeysByUTF16(keys []string) {
	u16s := make([][]uint16, len(keys))
	for i, k := range keys {
		u16s[i] = utf16.Encode([]rune(k))
	}
	sort.SliceStable(keys, func(i, j int) bool {
		return utf16Less(u16s[i], u16s[j])
	})
}

func utf16Less(a, b []uint16) bool {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := range n {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}
