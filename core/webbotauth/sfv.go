package webbotauth

// sfv.go: the Structured Fields (RFC 9651) subset HTTP Message Signatures
// needs, standard library only.
//
// RFC 9421's Signature-Input and Signature fields and the Web Bot Auth
// Signature-Agent field are all Structured Fields:
//
//   - Signature-Input: a Dictionary whose member values are parameterized
//     Inner Lists (the covered components plus signature parameters).
//   - Signature: a Dictionary whose member values are Byte Sequences.
//   - Signature-Agent: a Dictionary whose member values are String Items
//     (the legacy form is a bare String Item).
//
// Parsing follows RFC 9651 section 4.2 (parse then validate, error on
// malformed input), serialization follows section 4.1 (canonical form,
// byte-exact). Parameter order is preserved on both paths: RFC 9421
// section 2.3 requires the serialized signature parameters to keep the
// sender's chosen order, so a map would corrupt every signature base.

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type sfType uint8

const (
	sfToken sfType = iota
	sfString
	sfBytes
	sfInt
	sfDecimal
	sfBool
)

// sfParams is an ordered parameter list: bare parameters have val ==
// sfBool true.
type sfParams struct{ list []sfParam }

type sfParam struct {
	key string
	val sfItem
}

func (p sfParams) empty() bool { return len(p.list) == 0 }

// get returns the parameter value and whether it was present.
func (p sfParams) get(key string) (sfItem, bool) {
	for _, pr := range p.list {
		if pr.key == key {
			return pr.val, true
		}
	}
	return sfItem{}, false
}

// has reports presence regardless of value.
func (p sfParams) has(key string) bool {
	_, ok := p.get(key)
	return ok
}

// sfItem is one Structured Field Item.
type sfItem struct {
	typ sfType
	str string // token / string content
	bs  []byte // byte sequence
	i   int64  // integer; bool 0/1; decimal scaled by sfDecScale
	b   bool
	p   sfParams
}

// sfInnerList is a parameterized Inner List.
type sfInnerList struct {
	items  []sfItem
	params sfParams
}

// sfMember is one Dictionary member.
type sfMember struct {
	key  string
	item *sfItem      // set when the value is an Item
	list *sfInnerList // set when the value is an Inner List
	// bare is true for a member with no "= value" (implicit ?1 bool).
	bare bool
}

// sfDictionary is a Dictionary preserving member order.
type sfDictionary struct{ members []sfMember }

func (d *sfDictionary) get(key string) *sfMember {
	for i := range d.members {
		if d.members[i].key == key {
			return &d.members[i]
		}
	}
	return nil
}

func (d *sfDictionary) keys() []string {
	out := make([]string, len(d.members))
	for i, m := range d.members {
		out[i] = m.key
	}
	return out
}

var errSF = errors.New("malformed structured field")

type sfParser struct {
	s   string
	pos int
}

func (p *sfParser) eof() bool  { return p.pos >= len(p.s) }
func (p *sfParser) peek() byte { return p.s[p.pos] }

func (p *sfParser) skipOWS() {
	for p.pos < len(p.s) && (p.s[p.pos] == ' ' || p.s[p.pos] == '\t') {
		p.pos++
	}
}

// parseSFDictionary parses a full Dictionary field value.
func parseSFDictionary(s string) (*sfDictionary, error) {
	p := &sfParser{s: s}
	d := &sfDictionary{}
	p.skipOWS()
	if p.eof() {
		return d, nil
	}
	for {
		m, err := p.parseMember()
		if err != nil {
			return nil, err
		}
		d.members = append(d.members, *m)
		p.skipOWS()
		if p.eof() {
			return d, nil
		}
		if p.peek() != ',' {
			return nil, errSF
		}
		p.pos++
		p.skipOWS()
		if p.eof() { // trailing comma
			return nil, errSF
		}
	}
}

func (p *sfParser) parseMember() (*sfMember, error) {
	key, err := p.parseKey()
	if err != nil {
		return nil, err
	}
	p.skipOWS() // RFC 9651 allows SP after the key before '='
	m := &sfMember{key: key}
	if p.eof() || p.peek() != '=' {
		m.bare = true
		return m, nil
	}
	p.pos++
	// "x=" ends the input right here. The line above already guards eof
	// before reading '='; this peek needs the same guard, or a truncated
	// member indexes one past the end and panics the request.
	if p.eof() {
		return nil, errSF
	}
	if p.peek() == '(' {
		il, err := p.parseInnerList()
		if err != nil {
			return nil, err
		}
		m.list = il
	} else {
		it, err := p.parseItem()
		if err != nil {
			return nil, err
		}
		m.item = it
	}
	return m, nil
}

// parseSFItem parses a bare Item field value (the legacy
// Signature-Agent form).
func parseSFItem(s string) (*sfItem, error) {
	p := &sfParser{s: s}
	p.skipOWS()
	it, err := p.parseItem()
	if err != nil {
		return nil, err
	}
	p.skipOWS()
	if !p.eof() {
		return nil, errSF
	}
	return it, nil
}

func (p *sfParser) parseInnerList() (*sfInnerList, error) {
	il := &sfInnerList{}
	if p.peek() != '(' {
		return nil, errSF
	}
	p.pos++
	for {
		p.skipOWS()
		if p.eof() {
			return nil, errSF
		}
		if p.peek() == ')' {
			p.pos++
			params, err := p.parseParams()
			if err != nil {
				return nil, err
			}
			il.params = params
			return il, nil
		}
		it, err := p.parseItem()
		if err != nil {
			return nil, err
		}
		il.items = append(il.items, *it)
		// After an item (with its parameters) only the item separator
		// (1*SP, consumed by the loop head) or the closing paren may
		// follow. Checking before skipping is the point: a separator
		// skipped here would let the next peek fall on the item.
		if p.eof() || (p.peek() != ' ' && p.peek() != ')') {
			return nil, errSF
		}
	}
}

func (p *sfParser) parseItem() (*sfItem, error) {
	it, err := p.parseBareItem()
	if err != nil {
		return nil, err
	}
	params, err := p.parseParams()
	if err != nil {
		return nil, err
	}
	it.p = params
	return it, nil
}

// parseBareItem parses an item without parameters: numbers, strings,
// byte sequences, booleans, tokens. Parameter values are bare items
// (RFC 9651 ABNF: parameter-value = bare-item), so parsing a parameter
// value must not swallow the following parameters as its own.
func (p *sfParser) parseBareItem() (*sfItem, error) {
	it := &sfItem{}
	if p.eof() {
		return nil, errSF
	}
	c := p.peek()
	switch {
	case c == '"':
		it.typ = sfString
		s, err := p.parseString()
		if err != nil {
			return nil, err
		}
		it.str = s
	case c == ':':
		it.typ = sfBytes
		b, err := p.parseByteSeq()
		if err != nil {
			return nil, err
		}
		it.bs = b
	case c == '?':
		it.typ = sfBool
		p.pos++
		if p.eof() || (p.peek() != '0' && p.peek() != '1') {
			return nil, errSF
		}
		it.b = p.peek() == '1'
		p.pos++
	case c == '-' || (c >= '0' && c <= '9'):
		if err := p.parseNumber(it); err != nil {
			return nil, err
		}
	case isAlpha(c):
		it.typ = sfToken
		s, err := p.parseToken()
		if err != nil {
			return nil, err
		}
		it.str = s
	default:
		return nil, errSF
	}
	return it, nil
}

func (p *sfParser) parseNumber(it *sfItem) error {
	start := p.pos
	if p.peek() == '-' {
		p.pos++
	}
	if p.eof() || !isDigit(p.peek()) {
		return errSF
	}
	for !p.eof() && isDigit(p.peek()) {
		p.pos++
	}
	isDecimal := false
	if !p.eof() && p.peek() == '.' {
		isDecimal = true
		p.pos++
		if p.eof() || !isDigit(p.peek()) {
			return errSF
		}
		for !p.eof() && isDigit(p.peek()) {
			p.pos++
		}
	}
	text := p.s[start:p.pos]
	if isDecimal {
		intPart, frac := text[:strings.IndexByte(text, '.')], text[strings.IndexByte(text, '.')+1:]
		if len(frac) > 3 || len(intPart) > 13 {
			return errSF
		}
		it.typ = sfDecimal
		v, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return errSF
		}
		it.i = int64(v * 1000)
		return nil
	}
	if len(text) > 16 {
		return errSF
	}
	it.typ = sfInt
	v, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return errSF
	}
	it.i = v
	return nil
}

func (p *sfParser) parseString() (string, error) {
	p.pos++ // opening quote
	var b strings.Builder
	for {
		if p.eof() {
			return "", errSF
		}
		c := p.peek()
		switch {
		case c == '\\':
			p.pos++
			if p.eof() || (p.peek() != '\\' && p.peek() != '"') {
				return "", errSF
			}
			b.WriteByte(p.peek())
			p.pos++
		case c == '"':
			p.pos++
			return b.String(), nil
		case c < 0x20 || c == 0x7f:
			return "", errSF
		default:
			b.WriteByte(c)
			p.pos++
		}
	}
}

func (p *sfParser) parseByteSeq() ([]byte, error) {
	p.pos++ // ':'
	end := strings.IndexByte(p.s[p.pos:], ':')
	if end < 0 {
		return nil, errSF
	}
	enc := p.s[p.pos : p.pos+end]
	p.pos += end + 1
	b, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return nil, errSF
	}
	return b, nil
}

func (p *sfParser) parseToken() (string, error) {
	start := p.pos
	for !p.eof() && isTChar(p.peek()) {
		p.pos++
	}
	if p.pos == start {
		return "", errSF
	}
	return p.s[start:p.pos], nil
}

func (p *sfParser) parseKey() (string, error) {
	start := p.pos
	if p.eof() || (!isLCAlpha(p.peek()) && p.peek() != '*') {
		return "", errSF
	}
	p.pos++
	for !p.eof() && (isLCAlpha(p.peek()) || isDigit(p.peek()) ||
		p.peek() == '_' || p.peek() == '-' || p.peek() == '.' || p.peek() == '*') {
		p.pos++
	}
	return p.s[start:p.pos], nil
}

func (p *sfParser) parseParams() (sfParams, error) {
	var out sfParams
	for {
		// No whitespace before the ';': parameters attach directly to
		// the item (RFC 9651: parameters = *( ";" *SP parameter )).
		// Skipping here would swallow the inner-list item separator.
		if p.eof() || p.peek() != ';' {
			return out, nil
		}
		p.pos++ // consume ';'
		p.skipOWS()
		key, err := p.parseKey()
		if err != nil {
			return sfParams{}, err
		}
		pr := sfParam{key: key, val: sfItem{typ: sfBool, b: true}} // bare => ?1
		if !p.eof() && p.peek() == '=' {
			p.pos++
			it, err := p.parseBareItem()
			if err != nil {
				return sfParams{}, err
			}
			pr.val = *it
		}
		out.list = append(out.list, pr)
	}
}

func isAlpha(c byte) bool   { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isDigit(c byte) bool   { return c >= '0' && c <= '9' }
func isLCAlpha(c byte) bool { return c >= 'a' && c <= 'z' }

func isTChar(c byte) bool {
	if isAlpha(c) || isDigit(c) {
		return true
	}
	switch c {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return false
}

// ── Canonical serialization (RFC 9651 section 4.1) ──────────────────

func serializeItem(it sfItem) string {
	var b strings.Builder
	writeItem(&b, it)
	return b.String()
}

func writeItem(b *strings.Builder, it sfItem) {
	switch it.typ {
	case sfToken:
		b.WriteString(it.str)
	case sfString:
		b.WriteByte('"')
		for i := range len(it.str) {
			c := it.str[i]
			if c == '"' || c == '\\' {
				b.WriteByte('\\')
			}
			b.WriteByte(c)
		}
		b.WriteByte('"')
	case sfBytes:
		b.WriteByte(':')
		b.WriteString(base64.StdEncoding.EncodeToString(it.bs))
		b.WriteByte(':')
	case sfInt:
		b.WriteString(strconv.FormatInt(it.i, 10))
	case sfDecimal:
		neg := it.i < 0
		v := it.i
		if neg {
			v = -v
			b.WriteByte('-')
		}
		fmt.Fprintf(b, "%d.%03d", v/1000, v%1000)
	case sfBool:
		if it.b {
			b.WriteString("?1")
		} else {
			b.WriteString("?0")
		}
	}
	writeParams(b, it.p)
}

func writeParams(b *strings.Builder, p sfParams) {
	for _, pr := range p.list {
		b.WriteByte(';')
		b.WriteString(pr.key)
		if pr.val.typ != sfBool || !pr.val.b { // omit "=?1" for bare params
			b.WriteByte('=')
			writeItem(b, pr.val)
		}
	}
}

func serializeInnerList(il sfInnerList) string {
	var b strings.Builder
	b.WriteByte('(')
	for i, it := range il.items {
		if i > 0 {
			b.WriteByte(' ')
		}
		writeItem(&b, it)
	}
	b.WriteByte(')')
	writeParams(&b, il.params)
	return b.String()
}

// serializeMemberValue serializes a Dictionary member's value (Item or
// Inner List) without its key: the RFC 9421 ";key=" component rule.
func serializeMemberValue(m *sfMember) (string, error) {
	if m.bare {
		return "?1", nil
	}
	if m.item != nil {
		return serializeItem(*m.item), nil
	}
	if m.list != nil {
		return serializeInnerList(*m.list), nil
	}
	return "", errSF
}
