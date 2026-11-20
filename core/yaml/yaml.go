package yaml

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Kind int

const (
	Scalar Kind = iota
	Map
	List
)

type Node struct {
	Kind   Kind
	Value  any
	Line   int
	Column int
	Map    map[string]*Node
	List   []*Node
}

type line struct {
	indent int
	text   string
	line   int
}

func Parse(input string) (*Node, error) {
	input = strings.TrimPrefix(input, "\ufeff")
	lines, err := lexLines(input)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return &Node{Kind: Map, Map: map[string]*Node{}, Line: 1, Column: 1}, nil
	}
	p := parser{lines: lines}
	node, err := p.parseBlock(lines[0].indent)
	if err != nil {
		return nil, err
	}
	if p.pos < len(lines) {
		line := lines[p.pos]
		return nil, fmt.Errorf("yaml:%d:%d: unexpected indentation", line.line, line.indent+1)
	}
	return node, nil
}

func lexLines(input string) ([]line, error) {
	raw := strings.Split(strings.ReplaceAll(input, "\r\n", "\n"), "\n")
	lines := make([]line, 0, len(raw))
	for i, rawLine := range raw {
		if strings.ContainsRune(rawLine[:len(rawLine)-len(strings.TrimLeft(rawLine, " \t"))], '\t') {
			return nil, fmt.Errorf("yaml:%d:1: tabs are not supported for indentation", i+1)
		}
		stripped := stripComment(rawLine)
		if strings.TrimSpace(stripped) == "" {
			continue
		}
		indent := len(stripped) - len(strings.TrimLeft(stripped, " "))
		lines = append(lines, line{indent: indent, text: strings.TrimSpace(stripped), line: i + 1})
	}
	return lines, nil
}

func stripComment(line string) string {
	var q quoteScanner
	for i := 0; i < len(line); {
		n, unquoted := q.next(line, i, false)
		if unquoted && line[i] == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
			return strings.TrimRight(line[:i], " ")
		}
		i += n
	}
	return line
}

type quoteScanner struct {
	quote byte
	plain bool
	flow  int
}

func (q *quoteScanner) next(s string, i int, flow bool) (n int, unquoted bool) {
	c := s[i]
	sepAfter := i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\t'
	if q.quote != 0 {
		switch {
		case q.quote == '"' && c == '\\' && i+1 < len(s):
			return 2, false
		case c == q.quote && c == '\'' && i+1 < len(s) && s[i+1] == '\'':
			return 2, false
		case c == q.quote:
			q.quote, q.plain = 0, true
		}
		return 1, false
	}
	switch {
	case c == ' ' || c == '\t':
	case (c == '"' || c == '\'') && !q.plain:
		q.quote = c
		return 1, false
	case c == ':' && sepAfter:
		q.plain = false
	case c == '-' && sepAfter && !q.plain:
	case c == ',' && (flow || q.flow > 0):
		q.plain = false
	case (c == '[' || c == '{') && !q.plain:
		q.flow++
	case (c == ']' || c == '}') && q.flow > 0:
		q.flow--
		q.plain = true
	default:
		q.plain = true
	}
	return 1, true
}

const maxNestingDepth = 128

type parser struct {
	lines []line
	pos   int
	depth int
}

func (p *parser) parseBlock(indent int) (*Node, error) {
	if p.pos >= len(p.lines) {
		return &Node{Kind: Map, Map: map[string]*Node{}}, nil
	}
	if p.depth >= maxNestingDepth {
		l := p.lines[p.pos]
		return nil, fmt.Errorf("yaml:%d:%d: nesting depth exceeds maximum of %d", l.line, l.indent+1, maxNestingDepth)
	}
	p.depth++
	defer func() { p.depth-- }()
	line := p.lines[p.pos]
	if line.indent < indent {
		return &Node{Kind: Map, Map: map[string]*Node{}}, nil
	}
	if line.indent > indent {
		return nil, fmt.Errorf("yaml:%d:%d: unexpected indentation", line.line, line.indent+1)
	}
	if strings.HasPrefix(line.text, "- ") || line.text == "-" {
		return p.parseList(indent)
	}
	return p.parseMap(indent)
}

func (p *parser) parseMap(indent int) (*Node, error) {
	return p.parseMapSeeded(indent, nil)
}

func (p *parser) parseMapSeeded(indent int, seed map[string]int) (*Node, error) {
	out := &Node{Kind: Map, Line: p.lines[p.pos].line, Column: indent + 1, Map: map[string]*Node{}}
	seen := make(map[string]int, 8+len(seed))
	maps.Copy(seen, seed)
	for p.pos < len(p.lines) {
		line := p.lines[p.pos]
		if line.indent < indent {
			break
		}
		if line.indent > indent {
			return nil, fmt.Errorf("yaml:%d:%d: unexpected indentation", line.line, line.indent+1)
		}
		if strings.HasPrefix(line.text, "- ") || line.text == "-" {
			break
		}
		key, value, ok := splitMapping(line.text)
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("yaml:%d:%d: expected key: value", line.line, line.indent+1)
		}
		key, err := normalizeKey(key, line.line, line.indent+1)
		if err != nil {
			return nil, err
		}
		if first, dup := seen[key]; dup {
			return nil, fmt.Errorf("yaml:%d:%d: duplicate mapping key %q (first defined at line %d)", line.line, line.indent+1, key, first)
		}
		seen[key] = line.line
		value = strings.TrimSpace(value)
		p.pos++
		if value == "" {
			if p.pos >= len(p.lines) || p.lines[p.pos].indent < indent ||
				(p.lines[p.pos].indent == indent && !(strings.HasPrefix(p.lines[p.pos].text, "- ") || p.lines[p.pos].text == "-")) {
				out.Map[key] = &Node{Kind: Map, Map: map[string]*Node{}, Line: line.line, Column: line.indent + 1}
				continue
			}
			child, err := p.parseBlock(p.lines[p.pos].indent)
			if err != nil {
				return nil, err
			}
			out.Map[key] = child
			continue
		}
		node, err := parseScalar(value, line.line, strings.Index(line.text, value)+line.indent+1, 0)
		if err != nil {
			return nil, err
		}
		out.Map[key] = node
	}
	return out, nil
}

func normalizeKey(raw string, line, column int) (string, error) {
	key := strings.TrimSpace(raw)
	if key == "" {
		return "", fmt.Errorf("yaml:%d:%d: empty mapping key", line, column)
	}
	if strings.HasPrefix(key, "&") || strings.HasPrefix(key, "*") || strings.HasPrefix(key, "!") {
		return "", fmt.Errorf("yaml:%d:%d: anchors, aliases, and tags are not supported in mapping keys", line, column)
	}
	if strings.ContainsAny(key, "[]{}") {
		return "", fmt.Errorf("yaml:%d:%d: unsupported key syntax %q", line, column, key)
	}
	if strings.HasPrefix(key, "\"") || strings.HasPrefix(key, "'") {
		value, err := parseQuoted(key)
		if err != nil {
			return "", fmt.Errorf("yaml:%d:%d: %w", line, column, err)
		}
		if value == "" || strings.TrimSpace(value) != value {
			return "", fmt.Errorf("yaml:%d:%d: invalid quoted mapping key %q", line, column, key)
		}
		key = value
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x20 || key[i] == 0x7f {
			return "", fmt.Errorf("yaml:%d:%d: control bytes are not supported in mapping keys", line, column)
		}
	}
	return key, nil
}

func (p *parser) parseList(indent int) (*Node, error) {
	out := &Node{Kind: List, Line: p.lines[p.pos].line, Column: indent + 1}
	for p.pos < len(p.lines) {
		line := p.lines[p.pos]
		if line.indent < indent {
			break
		}
		if line.indent > indent {
			return nil, fmt.Errorf("yaml:%d:%d: unexpected indentation", line.line, line.indent+1)
		}
		if !(strings.HasPrefix(line.text, "- ") || line.text == "-") {
			break
		}
		item := strings.TrimSpace(strings.TrimPrefix(line.text, "-"))
		p.pos++
		if strings.HasPrefix(item, "- ") || item == "-" {
			return nil, fmt.Errorf("yaml:%d:%d: a nested list on the same line as its parent dash is not supported; put the inner list on its own indented lines", line.line, line.indent+3)
		}
		if strings.HasPrefix(item, "{") {
			return nil, flowMapError(line.line, line.indent+3)
		}
		if item == "" {
			if p.pos >= len(p.lines) || p.lines[p.pos].indent <= indent {
				out.List = append(out.List, &Node{Kind: Map, Map: map[string]*Node{}, Line: line.line, Column: line.indent + 1})
				continue
			}
			child, err := p.parseBlock(p.lines[p.pos].indent)
			if err != nil {
				return nil, err
			}
			out.List = append(out.List, child)
			continue
		}
		if key, value, ok := splitMapping(item); ok && strings.TrimSpace(key) != "" && !strings.HasPrefix(item, "[") {
			child := &Node{Kind: Map, Line: line.line, Column: line.indent + 1, Map: map[string]*Node{}}
			normKey, kErr := normalizeKey(key, line.line, line.indent+3)
			if kErr != nil {
				return nil, kErr
			}
			keyCol := line.indent + 1 + (len(line.text) - 1 - len(strings.TrimLeft(line.text[1:], " ")))
			value = strings.TrimSpace(value)
			if value == "" {
				if p.pos < len(p.lines) && (p.lines[p.pos].indent > keyCol ||
					(p.lines[p.pos].indent == keyCol && (strings.HasPrefix(p.lines[p.pos].text, "- ") || p.lines[p.pos].text == "-"))) {
					owned, err := p.parseBlock(p.lines[p.pos].indent)
					if err != nil {
						return nil, err
					}
					child.Map[normKey] = owned
				} else {
					child.Map[normKey] = &Node{Kind: Map, Map: map[string]*Node{}, Line: line.line, Column: keyCol + 1}
				}
			} else {
				scalar, err := parseScalar(value, line.line, strings.Index(line.text, value)+line.indent+1, 0)
				if err != nil {
					return nil, err
				}
				child.Map[normKey] = scalar
			}
			if p.pos < len(p.lines) && p.lines[p.pos].indent > indent {
				more, err := p.parseMapSeeded(keyCol, map[string]int{normKey: line.line})
				if err != nil {
					return nil, err
				}
				for _, k := range slices.Sorted(maps.Keys(more.Map)) {
					if _, dup := child.Map[k]; dup {
						v := more.Map[k]
						return nil, fmt.Errorf("yaml:%d:%d: duplicate mapping key %q (first defined at line %d)", v.Line, v.Column, k, line.line)
					}
				}
				maps.Copy(child.Map, more.Map)
			}
			out.List = append(out.List, child)
			continue
		}
		node, err := parseScalar(item, line.line, line.indent+3, 0)
		if err != nil {
			return nil, err
		}
		out.List = append(out.List, node)
	}
	return out, nil
}

func flowMapError(line, column int) error {
	return fmt.Errorf(`yaml:%d:%d: flow mapping "{ ... }" is not supported; use block style (one "key: value" per indented line)`, line, column)
}

func parseScalar(raw string, line, column, depth int) (*Node, error) {
	if depth >= maxNestingDepth {
		return nil, fmt.Errorf("yaml:%d:%d: nesting depth exceeds maximum of %d", line, column, maxNestingDepth)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return &Node{Kind: Scalar, Value: "", Line: line, Column: column}, nil
	}
	if strings.HasPrefix(raw, "{") {
		return nil, flowMapError(line, column)
	}
	if strings.HasPrefix(raw, "&") || strings.HasPrefix(raw, "*") || strings.HasPrefix(raw, "!") {
		return nil, fmt.Errorf("yaml:%d:%d: anchors, aliases, and tags are not supported", line, column)
	}
	if strings.HasPrefix(raw, "[") {
		values, err := parseInlineList(raw, line, column, depth+1)
		if err != nil {
			return nil, err
		}
		return &Node{Kind: List, Line: line, Column: column, List: values}, nil
	}
	if strings.HasPrefix(raw, "\"") || strings.HasPrefix(raw, "'") {
		value, err := parseQuoted(raw)
		if err != nil {
			return nil, fmt.Errorf("yaml:%d:%d: %w", line, column, err)
		}
		return &Node{Kind: Scalar, Value: value, Line: line, Column: column}, nil
	}
	switch raw {
	case "true", "True", "TRUE":
		return &Node{Kind: Scalar, Value: true, Line: line, Column: column}, nil
	case "false", "False", "FALSE":
		return &Node{Kind: Scalar, Value: false, Line: line, Column: column}, nil
	case "null", "Null", "NULL", "~":
		return &Node{Kind: Scalar, Value: nil, Line: line, Column: column}, nil
	}
	if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return &Node{Kind: Scalar, Value: i, Line: line, Column: column}, nil
	}
	if isCoreFloat(raw) {
		//gofastr:allow(nonfinite) isCoreFloat admits only decimal digits, so no inf or nan spelling reaches ParseFloat, and the only non-finite result, an out-of-range value, is refused on its error just below.
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("yaml:%d:%d: float %s is out of range", line, column, raw)
		}
		return &Node{Kind: Scalar, Value: f, Line: line, Column: column}, nil
	}
	if strings.Contains(raw, ": ") {
		return nil, fmt.Errorf("yaml:%d:%d: nested mapping must be on an indented line", line, column)
	}
	return &Node{Kind: Scalar, Value: raw, Line: line, Column: column}, nil
}

func isCoreFloat(raw string) bool {
	i := 0
	if i < len(raw) && (raw[i] == '-' || raw[i] == '+') {
		i++
	}
	digits := func() int {
		start := i
		for i < len(raw) && raw[i] >= '0' && raw[i] <= '9' {
			i++
		}
		return i - start
	}
	intDigits := digits()
	fracOrExp := false
	if i < len(raw) && raw[i] == '.' {
		i++
		fracOrExp = true
		if digits() == 0 && intDigits == 0 {
			return false
		}
	} else if intDigits == 0 {
		return false
	}
	if i < len(raw) && (raw[i] == 'e' || raw[i] == 'E') {
		i++
		fracOrExp = true
		if i < len(raw) && (raw[i] == '-' || raw[i] == '+') {
			i++
		}
		if digits() == 0 {
			return false
		}
	}
	return fracOrExp && i == len(raw)
}

func parseInlineList(raw string, line, column, depth int) ([]*Node, error) {
	if !strings.HasSuffix(raw, "]") {
		return nil, fmt.Errorf("yaml:%d:%d: unterminated inline list", line, column)
	}
	body := strings.TrimSpace(raw[1 : len(raw)-1])
	if body == "" {
		return nil, nil
	}
	parts, err := splitInline(body)
	if err != nil {
		return nil, fmt.Errorf("yaml:%d:%d: %w", line, column, err)
	}
	out := make([]*Node, 0, len(parts))
	for _, part := range parts {
		node, err := parseScalar(part, line, column, depth)
		if err != nil {
			return nil, err
		}
		if node.Kind != Scalar {
			return nil, fmt.Errorf("yaml:%d:%d: inline lists may only contain scalar values", line, column)
		}
		out = append(out, node)
	}
	return out, nil
}

func splitInline(body string) ([]string, error) {
	var out []string
	var q quoteScanner
	start := 0
	for i := 0; i < len(body); {
		n, unquoted := q.next(body, i, true)
		if unquoted && body[i] == ',' {
			out = append(out, strings.TrimSpace(body[start:i]))
			start = i + 1
		}
		i += n
	}
	if q.quote != 0 {
		return nil, fmt.Errorf("unterminated quoted scalar")
	}
	if last := strings.TrimSpace(body[start:]); last != "" || len(out) == 0 {
		out = append(out, last)
	}
	for _, part := range out {
		if part == "" {
			return nil, fmt.Errorf("empty inline list item; write \"\" for an empty string")
		}
	}
	return out, nil
}

func parseQuoted(raw string) (string, error) {
	if len(raw) < 2 {
		return "", fmt.Errorf("unterminated quoted scalar")
	}
	quote := raw[0]
	if raw[len(raw)-1] != quote {
		return "", fmt.Errorf("unterminated quoted scalar")
	}
	if quote == '\'' {
		body := raw[1 : len(raw)-1]
		for i := 0; i < len(body); i++ {
			if body[i] == '\'' {
				if i+1 >= len(body) || body[i+1] != '\'' {
					return "", fmt.Errorf("unescaped quote in scalar")
				}
				i++
			}
		}
		return strings.ReplaceAll(body, "''", "'"), nil
	}
	return decodeDoubleQuoted(raw[1 : len(raw)-1])
}

var yamlEscapes = map[byte]string{
	'0': "\x00", 'a': "\a", 'b': "\b", 't': "\t", '\t': "\t", 'n': "\n",
	'v': "\v", 'f': "\f", 'r': "\r", 'e': "\x1b", ' ': " ", '"': `"`,
	'/': "/", '\\': `\`, 'N': "\u0085", '_': " ", 'L': " ",
	'P': " ",
}

func decodeDoubleQuoted(body string) (string, error) {
	if !strings.ContainsAny(body, `\"`) {
		return body, nil
	}
	var b strings.Builder
	b.Grow(len(body))
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c == '"' {
			return "", fmt.Errorf("unescaped quote in scalar")
		}
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(body) {
			return "", fmt.Errorf("unterminated quoted scalar")
		}
		e := body[i]
		if s, ok := yamlEscapes[e]; ok {
			b.WriteString(s)
			continue
		}
		width := map[byte]int{'x': 2, 'u': 4, 'U': 8}[e]
		if width == 0 {
			return "", fmt.Errorf("unknown escape \\%c in double-quoted scalar", e)
		}
		if i+1+width > len(body) {
			return "", fmt.Errorf("escape \\%c needs %d hex digits", e, width)
		}
		hex := body[i+1 : i+1+width]
		n, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			return "", fmt.Errorf("escape \\%c needs %d hex digits", e, width)
		}
		r := rune(n)
		if n > utf8.MaxRune || (r >= 0xD800 && r <= 0xDFFF) {
			return "", fmt.Errorf("escape \\%c%s is not a Unicode scalar value", e, hex)
		}
		b.WriteRune(r)
		i += width
	}
	return b.String(), nil
}

func splitMapping(s string) (string, string, bool) {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if quote == '"' && c == '\\' {
				i++
				continue
			}
			if c == quote {
				if quote == '\'' && i+1 < len(s) && s[i+1] == quote {
					i++
					continue
				}
				quote = 0
			}
			continue
		}
		if (c == '"' || c == '\'') && strings.TrimSpace(s[:i]) == "" {
			quote = c
			continue
		}
		if c == ':' && (i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\t') {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}
