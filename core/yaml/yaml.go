package yaml

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
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
		if strings.ContainsRune(rawLine, '\t') {
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
	inQuote := rune(0)
	escaped := false
	for i, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if inQuote == '"' && r == '\\' {
			escaped = true
			continue
		}
		if inQuote != 0 {
			if r == inQuote {
				inQuote = 0
			}
			continue
		}
		if r == '"' || r == '\'' {
			inQuote = r
			continue
		}
		if r == '#' && (i == 0 || line[i-1] == ' ') {
			return strings.TrimRight(line[:i], " ")
		}
	}
	return line
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
	// parser's inputs are the security-relevant config — blueprints,
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
		key, value, ok := strings.Cut(line.text, ":")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("yaml:%d:%d: expected key: value", line.line, line.indent+1)
		}
		key = strings.TrimSpace(key)
		if strings.ContainsAny(key, "[]{}") {
			return nil, fmt.Errorf("yaml:%d:%d: unsupported key syntax %q", line.line, line.indent+1, key)
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
		if key, value, ok := strings.Cut(item, ":"); ok && strings.TrimSpace(key) != "" && !strings.HasPrefix(item, "\"") && !strings.HasPrefix(item, "'") && !strings.HasPrefix(item, "[") {
			child := &Node{Kind: Map, Line: line.line, Column: line.indent + 1, Map: map[string]*Node{}}
			value = strings.TrimSpace(value)
			if value == "" {
				child.Map[strings.TrimSpace(key)] = &Node{Kind: Map, Map: map[string]*Node{}, Line: line.line, Column: line.indent + 3}
			} else {
				scalar, err := parseScalar(value, line.line, strings.Index(line.text, value)+line.indent+1, 0)
				if err != nil {
					return nil, err
				}
				child.Map[strings.TrimSpace(key)] = scalar
			}
			if p.pos < len(p.lines) && p.lines[p.pos].indent > indent {
				// Seed with the key this item already defined, so a repeat
				// among the continuation lines reports the item's line as
				// the first definition rather than the first continuation.
				seed := make(map[string]int, len(child.Map))
				for k, v := range child.Map {
					seed[k] = v.Line
				}
				more, err := p.parseMapSeeded(p.lines[p.pos].indent, seed)
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
	start := 0
	inQuote := rune(0)
	escaped := false
	for i, r := range body {
		if escaped {
			escaped = false
			continue
		}
		if inQuote == '"' && r == '\\' {
			escaped = true
			continue
		}
		if inQuote != 0 {
			if r == inQuote {
				inQuote = 0
			}
			continue
		}
		if r == '"' || r == '\'' {
			inQuote = r
			continue
		}
		if r == ',' {
			out = append(out, strings.TrimSpace(body[start:i]))
			start = i + 1
		}
	}
	if inQuote != 0 {
		return nil, fmt.Errorf("unterminated quoted scalar")
	}
	out = append(out, strings.TrimSpace(body[start:]))
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
		return strings.ReplaceAll(raw[1:len(raw)-1], "''", "'"), nil
	}
	unquoted, err := strconv.Unquote(raw)
	if err != nil {
		return "", err
	}
	return unquoted, nil
}
