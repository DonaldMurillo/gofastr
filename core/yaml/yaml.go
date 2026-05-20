package yaml

import (
	"fmt"
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

type parser struct {
	lines []line
	pos   int
}

func (p *parser) parseBlock(indent int) (*Node, error) {
	if p.pos >= len(p.lines) {
		return &Node{Kind: Map, Map: map[string]*Node{}}, nil
	}
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
	out := &Node{Kind: Map, Line: p.lines[p.pos].line, Column: indent + 1, Map: map[string]*Node{}}
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
		node, err := parseScalar(value, line.line, strings.Index(line.text, value)+line.indent+1)
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
		if key, value, ok := strings.Cut(item, ":"); ok && strings.TrimSpace(key) != "" && !strings.HasPrefix(strings.TrimSpace(item), "\"") && !strings.HasPrefix(strings.TrimSpace(item), "'") {
			child := &Node{Kind: Map, Line: line.line, Column: line.indent + 1, Map: map[string]*Node{}}
			value = strings.TrimSpace(value)
			if value == "" {
				child.Map[strings.TrimSpace(key)] = &Node{Kind: Map, Map: map[string]*Node{}, Line: line.line, Column: line.indent + 3}
			} else {
				scalar, err := parseScalar(value, line.line, strings.Index(line.text, value)+line.indent+1)
				if err != nil {
					return nil, err
				}
				child.Map[strings.TrimSpace(key)] = scalar
			}
			if p.pos < len(p.lines) && p.lines[p.pos].indent > indent {
				more, err := p.parseMap(p.lines[p.pos].indent)
				if err != nil {
					return nil, err
				}
				for k, v := range more.Map {
					child.Map[k] = v
				}
			}
			out.List = append(out.List, child)
			continue
		}
		node, err := parseScalar(item, line.line, line.indent+3)
		if err != nil {
			return nil, err
		}
		out.List = append(out.List, node)
	}
	return out, nil
}

func parseScalar(raw string, line, column int) (*Node, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return &Node{Kind: Scalar, Value: "", Line: line, Column: column}, nil
	}
	if strings.HasPrefix(raw, "{") {
		return nil, fmt.Errorf("yaml:%d:%d: inline maps are not supported", line, column)
	}
	if strings.HasPrefix(raw, "&") || strings.HasPrefix(raw, "*") || strings.HasPrefix(raw, "!!") {
		return nil, fmt.Errorf("yaml:%d:%d: anchors, aliases, and tags are not supported", line, column)
	}
	if strings.HasPrefix(raw, "[") {
		values, err := parseInlineList(raw, line, column)
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

func parseInlineList(raw string, line, column int) ([]*Node, error) {
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
		node, err := parseScalar(part, line, column)
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
