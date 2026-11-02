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
	// A leading byte order mark is not content (YAML 1.2 section 5.2).
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
		// A comment follows whitespace, a tab as much as a space.
		if unquoted && line[i] == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
			return strings.TrimRight(line[:i], " ")
		}
		i += n
	}
	return line
}

// quoteScanner walks one line and tracks where quoted scalars are.
//
// A quote opens a quoted scalar only where a scalar BEGINS: at the start
// of the line, after a "key: " or "- " indicator, or after a flow '[',
// '{' or ','. Inside a plain scalar a quote is an ordinary character, so
// "O'Reilly # note" ends at the comment and "[O'Reilly, Jane]" splits on
// its comma. Treating every quote as an opener made both swallow the
// rest of the line.
type quoteScanner struct {
	quote byte // the open quote, 0 outside a quoted scalar
	plain bool // inside a plain (or just-closed quoted) scalar
	flow  int  // depth of unquoted '[' / '{'
}

// next consumes the byte at s[i] and returns how many bytes it took and
// whether that byte is syntax outside any quoted scalar. flow says s is
// already the inside of a flow collection, where ',' separates entries.
func (q *quoteScanner) next(s string, i int, flow bool) (n int, unquoted bool) {
	c := s[i]
	sepAfter := i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\t'
	if q.quote != 0 {
		switch {
		case q.quote == '"' && c == '\\' && i+1 < len(s):
			return 2, false
		case c == q.quote && c == '\'' && i+1 < len(s) && s[i+1] == '\'':
			return 2, false // '' is an escaped quote
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

// maxNestingDepth bounds parser recursion. Each indentation level recurses
// through parseBlock → parseMap/parseList → parseBlock, so deeply nested
// input drives unbounded recursion, a stack-exhaustion DoS on any user-
// supplied YAML. Past the cap we stop recursing and return an error,
// mirroring core/markdown's maxBlockquoteDepth.
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

// parseMapSeeded is parseMap with the duplicate-key tracker pre-populated.
//
// A "- key: value" list item builds its first key itself and then parses the
// indented continuation lines as a separate map. Without the seed that map
// starts blank, so for
//
//   - a: 1
//     a: 2
//     a: 3
//
// it catches a@3 against a@4 and reports line 3 as the first definition —
// when `a` was first defined on line 2, in the item itself. The reported
// line is the whole point of the error, so it has to be the real one.
func (p *parser) parseMapSeeded(indent int, seed map[string]int) (*Node, error) {
	out := &Node{Kind: Map, Line: p.lines[p.pos].line, Column: indent + 1, Map: map[string]*Node{}}
	// A duplicate key was silent last-wins: `enabled: false` followed later
	// by `enabled: true` took the second value while a reviewer, and grep,
	// read the first. Every mainstream parser refuses that, and this
	// parser's inputs are the security-relevant config — app specs,
	// codegen configs, kiln freeze snapshots — so the ambiguity has to fail
	// closed here rather than resolve to whichever line came last. Same
	// class as the YAML 1.2 boolean that read `auth: yes` as a string and
	// meant PUBLIC: ask which direction a misread moves safety.
	//
	// seen holds the line that first defined each key so the error can name
	// both.
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
			if p.pos >= len(p.lines) || p.lines[p.pos].indent <= indent {
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

// normalizeKey validates and canonicalises the KEY side of "key: value".
// Keys were previously taken verbatim after TrimSpace, so `"title"`,
// `&title`, and `title` were three DISTINCT map keys: a duplicate
// smuggled in under quoting or an anchor/tag decoration shadowed
// nothing, conflicted with nothing, and a reader saw one key configured
// while the parser held two — exactly the ambiguity the duplicate-key
// guard exists to refuse. Quoted keys unquote to their bare spelling so
// the guard sees them; anchor/alias/tag decorations are rejected,
// mirroring parseScalar's value-side rejection; flow characters stay
// unsupported on the key side (checked on the raw spelling, so a
// quoted key containing them is refused too).
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
			// A compact nested sequence ("- - a") is outside the subset.
			// Read as a scalar it became the string "- a", or a map key
			// "- name", with no error.
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
			// The item's first key is a mapping key like any other:
			// normalize it so flow characters and anchor/tag
			// decorations are refused here too (the plain map path
			// enforces this via normalizeKey) and a quoted spelling
			// cannot dodge the continuation-seed duplicate check.
			normKey, kErr := normalizeKey(key, line.line, line.indent+3)
			if kErr != nil {
				return nil, kErr
			}
			// The item's keys sit at keyCol, the column after "- ". A
			// first key with an empty value owns the lines indented past
			// keyCol as its own value; lines at keyCol are the item's
			// further keys. Anything else between the dash and keyCol,
			// or past it after a scalar, belongs to nothing.
			keyCol := line.indent + 1 + (len(line.text) - 1 - len(strings.TrimLeft(line.text[1:], " ")))
			value = strings.TrimSpace(value)
			if value == "" {
				if p.pos < len(p.lines) && p.lines[p.pos].indent > keyCol {
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
				// Parsed at keyCol: a line deeper than that is refused
				// there, and one shallower ends the map and is refused
				// back here as unexpected indentation.
				//
				// Seed with the key this item already defined, so a repeat
				// among the continuation lines reports the item's line as
				// the first definition rather than the first continuation.
				seed := make(map[string]int, len(child.Map))
				for k, v := range child.Map {
					seed[k] = v.Line
				}
				more, err := p.parseMapSeeded(keyCol, seed)
				if err != nil {
					return nil, err
				}
				// The second silent-merge path, and the one parseMap's own
				// `seen` cannot see: a "- key: value" item builds its first
				// key here, then the indented continuation lines are parsed
				// as a SEPARATE map and copied in. maps.Copy overwrote a
				// collision without a word. Keys are walked in sorted order
				// so the reported one is deterministic — the repo's
				// mapwriter analyzer requires that of any map iteration
				// whose output escapes.
				//
				// A key repeated across SIBLING list items is untouched:
				// those are different mappings, and repeating a key across
				// them is ordinary YAML.
				for _, k := range slices.Sorted(maps.Keys(more.Map)) {
					if first, dup := child.Map[k]; dup {
						v := more.Map[k]
						return nil, fmt.Errorf("yaml:%d:%d: duplicate mapping key %q (first defined at line %d)", v.Line, v.Column, k, first.Line)
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

// parseScalar decodes one scalar (or an inline list, which is a scalar
// syntactically). depth is the inline-nesting level: parseScalar and
// parseInlineList are mutually recursive on '[', and that recursion used to
// consult nothing — p.depth guards indentation nesting only, so an inline
// list nested a few thousand deep exhausted the goroutine stack and killed
// the process before the Kind check that rejects nested inline lists ever
// ran. `gofastr generate cli --from <URL>` hands remote YAML to Parse
// verbatim, so the input is not local-file-only.
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
	if strings.HasPrefix(raw, "&") || strings.HasPrefix(raw, "*") || strings.HasPrefix(raw, "!!") {
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
	switch strings.ToLower(raw) {
	case "true":
		return &Node{Kind: Scalar, Value: true, Line: line, Column: column}, nil
	case "false":
		return &Node{Kind: Scalar, Value: false, Line: line, Column: column}, nil
	case "null", "~":
		return &Node{Kind: Scalar, Value: nil, Line: line, Column: column}, nil
	}
	if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return &Node{Kind: Scalar, Value: i, Line: line, Column: column}, nil
	}
	//gofastr:allow(nonfinite) YAML permits .inf/.nan scalar literals; the parser preserves the float and consumers validate it, this is not a bound check.
	if f, err := strconv.ParseFloat(raw, 64); err == nil && strings.ContainsAny(raw, ".eE") {
		return &Node{Kind: Scalar, Value: f, Line: line, Column: column}, nil
	}
	if strings.Contains(raw, ": ") {
		return nil, fmt.Errorf("yaml:%d:%d: nested mapping must be on an indented line", line, column)
	}
	return &Node{Kind: Scalar, Value: raw, Line: line, Column: column}, nil
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
	// A trailing comma ends the list (YAML 1.2 allows it) and adds no
	// item: appended, the empty last part became a "" value that an
	// app-spec enum then accepted. Any other empty part ([a,,b], [,a])
	// is refused; an empty item is spelled "".
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

// yamlEscapes maps the single-character escapes of a YAML 1.2
// double-quoted scalar (section 5.7) to what they stand for.
var yamlEscapes = map[byte]string{
	'0': "\x00", 'a': "\a", 'b': "\b", 't': "\t", '\t': "\t", 'n': "\n",
	'v': "\v", 'f': "\f", 'r': "\r", 'e': "\x1b", ' ': " ", '"': `"`,
	'/': "/", '\\': `\`, 'N': "\u0085", '_': " ", 'L': " ",
	'P': " ",
}

// decodeDoubleQuoted decodes the body of a double-quoted scalar by the
// YAML 1.2 escape rules. It replaced strconv.Unquote, which reads Go
// syntax: "\xe9" came back as the lone byte 0xE9 (invalid UTF-8) where
// YAML means U+00E9 "é", and the Go-only octal "\101" read as "A" where
// YAML has no such escape. Both were wrong values with no error; an
// escape YAML does not define now fails like any unsupported syntax.
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
		// ParseUint with an explicit base takes no sign, prefix or
		// underscore, so this accepts exactly width hex digits.
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

// A mapping separator is an unquoted colon followed by whitespace or EOF.
// Colons in URLs and quoted keys are part of the scalar.
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
