// Package jcs implements RFC 8785 (JSON Canonicalization Scheme) on the Go standard library.
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

const maxDepth = 512

// Canonicalize marshals v with encoding/json and passes the result to [CanonicalizeJSON].
// Encoding replaces invalid UTF-8 in strings with U+FFFD and rejects NaN and ±Infinity.
func Canonicalize(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return CanonicalizeJSON(bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
}

// CanonicalizeJSON strictly parses exactly one JSON value from raw and returns its RFC 8785 canonical form.
// Duplicate keys, lone surrogates, invalid UTF-8, raw control characters and Infinity-overflowing numbers are errors.
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
	if p.pos >= len(p.data) {
		return nil, p.errorf("unexpected end of input")
	}
	if c := p.data[p.pos]; depth >= maxDepth && (c == '{' || c == '[') {
		return nil, p.errorf("nesting exceeds %d levels", maxDepth)
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
	p.pos++
	type member struct {
		keyU16 []uint16
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
	p.pos++
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

func (p *parser) parseString() (string, error) {
	p.pos++
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

func (p *parser) parseUnicodeEscape() (rune, error) {
	hi, err := p.readHex4()
	if err != nil {
		return 0, err
	}
	if utf16.IsSurrogate(rune(hi)) {
		if hi >= 0xDC00 {
			return 0, p.errorf("unpaired low surrogate \\u%04X", hi)
		}
		if p.pos+1 >= len(p.data) || p.data[p.pos] != '\\' || p.data[p.pos+1] != 'u' {
			return 0, p.errorf("unpaired high surrogate \\u%04X", hi)
		}
		save := p.pos
		p.pos += 2
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

func (p *parser) parseNumber() ([]byte, error) {
	start := p.pos
	if p.pos < len(p.data) && p.data[p.pos] == '-' {
		p.pos++
	}
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
	if p.pos < len(p.data) && p.data[p.pos] == '.' {
		p.pos++
		if p.pos >= len(p.data) || !isDigit(p.data[p.pos]) {
			return nil, p.errorf("digit required after decimal point")
		}
		for p.pos < len(p.data) && isDigit(p.data[p.pos]) {
			p.pos++
		}
	}
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

func formatNumber(f float64) string {
	if f == 0 {
		return "0"
	}
	neg := ""
	if f < 0 || math.Signbit(f) {
		neg = "-"
		f = -f
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return neg + "NaN"
	}

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
	k := len(digits)
	n := exp + 1

	var b []byte
	switch {
	case k <= n && n <= 21:
		b = append(b, digits...)
		for i := 0; i < n-k; i++ {
			b = append(b, '0')
		}
	case 0 < n && n <= 21:
		b = append(b, digits[:n]...)
		b = append(b, '.')
		b = append(b, digits[n:]...)
	case -6 < n && n <= 0:
		b = append(b, '0', '.')
		for range -n {
			b = append(b, '0')
		}
		b = append(b, digits...)
	default:
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
