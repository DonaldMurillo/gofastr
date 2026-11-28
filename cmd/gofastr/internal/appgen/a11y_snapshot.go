package appgen

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/bom"
)

type ariaNode struct {
	Role     string
	Name     string
	Attrs    map[string]string
	Text     string
	Props    map[string]string
	Children []*ariaNode
	Line     int
}

// Label is the text a sighted user reads for the node: its accessible name, else its inline text.
func (n *ariaNode) Label() string {
	if n.Name != "" {
		return n.Name
	}
	return n.Text
}

const ariaSnapshotMaxDepth = 128

const ariaSnapshotMaxNodes = 50000

func parseAriaSnapshot(src string) ([]*ariaNode, error) {
	type frame struct {
		indent int
		node   *ariaNode
		open   bool
	}
	root := &ariaNode{}
	stack := []frame{{indent: -1, node: root, open: true}}
	count := 0
	src = bom.Trim(src)
	src = strings.ReplaceAll(strings.ReplaceAll(src, "\r\n", "\n"), "\r", "\n")
	for i, raw := range strings.Split(src, "\n") {
		lineNo := i + 1
		trimmed := strings.TrimRight(raw, " ")
		body := strings.TrimLeft(trimmed, " ")
		if body == "" || strings.HasPrefix(body, "#") {
			continue
		}
		indent := len(trimmed) - len(body)
		if strings.HasPrefix(body, "\t") {
			return nil, fmt.Errorf("line %d: tabs are not allowed for indentation", lineNo)
		}
		if body != "-" && !strings.HasPrefix(body, "- ") {
			return nil, fmt.Errorf("line %d: expected a list entry starting with \"- \"", lineNo)
		}
		entry := strings.TrimSpace(strings.TrimPrefix(body, "-"))
		for len(stack) > 1 && indent <= stack[len(stack)-1].indent {
			stack = stack[:len(stack)-1]
		}
		parent := stack[len(stack)-1]
		if !parent.open {
			return nil, fmt.Errorf("line %d: entry is indented under %s, which has an inline value and cannot have children", lineNo, ariaQuote(parent.node.Role))
		}
		if len(stack) > ariaSnapshotMaxDepth {
			return nil, fmt.Errorf("line %d: nesting depth exceeds %d", lineNo, ariaSnapshotMaxDepth)
		}
		if count++; count > ariaSnapshotMaxNodes {
			return nil, fmt.Errorf("line %d: snapshot has more than %d entries", lineNo, ariaSnapshotMaxNodes)
		}
		key, value, hasColon, err := splitAriaEntry(entry)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		if strings.HasPrefix(key, "/") {
			if parent.node == root {
				return nil, fmt.Errorf("line %d: property %s has no parent entry", lineNo, ariaQuote(key))
			}
			if parent.node.Props == nil {
				parent.node.Props = map[string]string{}
			}
			parent.node.Props[strings.TrimPrefix(key, "/")] = cleanAriaText(value)
			continue
		}
		node, err := parseAriaTemplate(key)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		node.Line = lineNo
		node.Text = cleanAriaText(value)
		parent.node.Children = append(parent.node.Children, node)
		stack = append(stack, frame{indent: indent, node: node, open: hasColon && value == ""})
	}
	return root.Children, nil
}

func splitAriaEntry(entry string) (key, value string, hasColon bool, err error) {
	if entry == "" {
		return "", "", false, fmt.Errorf("empty entry")
	}
	if entry[0] == '"' || entry[0] == '\'' {
		k, rest, err := unquoteYAMLScalar(entry)
		if err != nil {
			return "", "", false, err
		}
		rest = strings.TrimSpace(rest)
		switch {
		case rest == "":
			return k, "", false, nil
		case rest == ":":
			return k, "", true, nil
		case strings.HasPrefix(rest, ": "):
			v, err := ariaValue(strings.TrimSpace(rest[2:]))
			return k, v, true, err
		}
		return "", "", false, fmt.Errorf("unexpected text after quoted key: %s", ariaQuote(rest))
	}
	inQuote, inRegex, inAttr, escaped := false, false, false, false
	for i := 0; i < len(entry); i++ {
		c := entry[i]
		if escaped {
			escaped = false
			continue
		}
		switch {
		case (inQuote || inRegex) && c == '\\':
			escaped = true
		case inQuote:
			inQuote = c != '"'
		case inRegex:
			inRegex = c != '/'
		case inAttr:
			inAttr = c != ']'
		case c == '"':
			inQuote = true
		case c == '/' && i > 0 && entry[i-1] == ' ':
			inRegex = true
		case c == '[':
			inAttr = true
		case c == ':' && (i+1 == len(entry) || entry[i+1] == ' '):
			v, err := ariaValue(strings.TrimSpace(entry[i+1:]))
			return strings.TrimSpace(entry[:i]), v, true, err
		}
	}
	return entry, "", false, nil
}

func ariaValue(v string) (string, error) {
	if v == "" || (v[0] != '"' && v[0] != '\'') {
		return v, nil
	}
	s, rest, err := unquoteYAMLScalar(v)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(rest) != "" {
		return "", fmt.Errorf("unexpected text after quoted value: %s", ariaQuote(rest))
	}
	return s, nil
}

func unquoteYAMLScalar(s string) (string, string, error) {
	quote := s[0]
	for i := 1; i < len(s); i++ {
		switch {
		case quote == '"' && s[i] == '\\':
			i++
		case quote == '\'' && s[i] == '\'' && i+1 < len(s) && s[i+1] == '\'':
			i++
		case s[i] == quote:
			if quote == '\'' {
				return strings.ReplaceAll(s[1:i], "''", "'"), s[i+1:], nil
			}
			var out string
			if err := json.Unmarshal([]byte(s[:i+1]), &out); err != nil {
				return "", "", fmt.Errorf("bad quoted string %s: %w", ariaQuote(s[:i+1]), err)
			}
			return out, s[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("unterminated quoted string: %s", ariaQuote(s))
}

func parseAriaTemplate(key string) (*ariaNode, error) {
	i := 0
	for i < len(key) && (isASCIILetter(key[i]) || (i > 0 && (key[i] == '-' || key[i] == '_'))) {
		i++
	}
	if i == 0 {
		return nil, fmt.Errorf("entry %s does not start with a role", ariaQuote(key))
	}
	node := &ariaNode{Role: strings.ToLower(key[:i])}
	rest := strings.TrimSpace(key[i:])
	switch {
	case strings.HasPrefix(rest, "\""):
		name, after, err := unquoteYAMLScalar(rest)
		if err != nil {
			return nil, err
		}
		node.Name = cleanAriaText(name)
		rest = strings.TrimSpace(after)
	case strings.HasPrefix(rest, "/"):
		end := 1
		for end < len(rest) && rest[end] != '/' {
			if rest[end] == '\\' {
				end++
			}
			end++
		}
		if end >= len(rest) {
			return nil, fmt.Errorf("unterminated /regex/ name in %s", ariaQuote(key))
		}
		node.Name = cleanAriaText(rest[1:end])
		rest = strings.TrimSpace(rest[end+1:])
	}
	for rest != "" {
		if rest[0] != '[' {
			return nil, fmt.Errorf("unexpected %s in %s", ariaQuote(rest), ariaQuote(key))
		}
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			return nil, fmt.Errorf("unterminated [attribute] in %s", ariaQuote(key))
		}
		attr := strings.TrimSpace(rest[1:end])
		k, v, ok := strings.Cut(attr, "=")
		if !ok {
			v = "true"
		}
		if node.Attrs == nil {
			node.Attrs = map[string]string{}
		}
		node.Attrs[strings.ToLower(strings.TrimSpace(k))] = strings.Trim(strings.TrimSpace(v), "\"")
		rest = strings.TrimSpace(rest[end+1:])
	}
	return node, nil
}

func isASCIILetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

func cleanAriaText(s string) string {
	return strings.TrimSpace(textsafe.StripUnsafe(s))
}

func ariaQuote(s string) string {
	const max = 60
	if r := []rune(s); len(r) > max {
		return strconv.Quote(string(r[:max])) + "…"
	}
	return strconv.Quote(s)
}
