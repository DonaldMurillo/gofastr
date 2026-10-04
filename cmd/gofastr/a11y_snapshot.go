package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// ariaNode is one entry of a Playwright aria snapshot: the YAML-shaped
// accessibility tree that `locator.ariaSnapshot()` returns and Playwright
// MCP's browser_snapshot prints. A line reads
// `- role "accessible name" [attr] [attr=value]: inline text`, with
// children indented under an entry that ends in a colon, and
// `- /url: …`-style lines carrying a property of their parent.
type ariaNode struct {
	Role     string
	Name     string
	Attrs    map[string]string
	Text     string            // inline value after the colon
	Props    map[string]string // "/url", "/placeholder", … (slash dropped)
	Children []*ariaNode
	Line     int
}

// Label is the text a sighted user reads for the node: its accessible
// name, else its inline text.
func (n *ariaNode) Label() string {
	if n.Name != "" {
		return n.Name
	}
	return n.Text
}

// ariaSnapshotMaxDepth bounds nesting. The parser is iterative, so depth
// costs no stack, but the mapper that walks the tree recurses.
const ariaSnapshotMaxDepth = 128

// ariaSnapshotMaxNodes bounds the tree a single snapshot may describe.
// Real page snapshots run to a few thousand entries.
const ariaSnapshotMaxNodes = 50000

// parseAriaSnapshot parses a Playwright aria snapshot into its top-level
// entries. It is a line parser for the snapshot grammar rather than a
// YAML decode: entries are YAML-shaped, but a key such as
// `link "a: b"` or `heading /Wel+come/` holds colons and slashes a
// general YAML subset splits wrongly.
func parseAriaSnapshot(src string) ([]*ariaNode, error) {
	type frame struct {
		indent int
		node   *ariaNode // nil for the root
		open   bool      // entry ended in a bare colon: children may follow
	}
	root := &ariaNode{}
	stack := []frame{{indent: -1, node: root, open: true}}
	count := 0
	src = strings.TrimPrefix(src, "\uFEFF")
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
			// A property line (`- /url: /home`) annotates its parent.
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

// splitAriaEntry splits an entry into its key and inline value. The key
// may be a YAML-quoted scalar (Playwright quotes a key that would not
// survive as plain YAML); otherwise the split is the first colon that sits
// outside a quoted name, a /regex/ name, and an [attr] list, and is
// followed by a space or the end of the line.
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

// ariaValue reads an inline value: a quoted YAML scalar or plain text.
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

// unquoteYAMLScalar reads one quoted scalar from the start of s and
// returns it with the remainder. Double quotes take JSON escapes (what
// Playwright writes); single quotes double an embedded quote.
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

// parseAriaTemplate reads `role "name" [attr] [attr=value]`.
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
		// A /regex/ name matches a family of names. The regex source is
		// the closest readable label there is.
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

// cleanAriaText drops control and invisible characters. Names and text
// flow into generated source, rendered pages, and terminal warnings, so a
// bidi override or escape sequence in a captured page stops here.
func cleanAriaText(s string) string {
	return strings.TrimSpace(textsafe.StripUnsafe(s))
}

// ariaQuote quotes a piece of snapshot input for an error message: Go
// quoting escapes control and bidi bytes, so a hostile line cannot drive
// the terminal, and the cut keeps a multi-megabyte line out of the output.
func ariaQuote(s string) string {
	const max = 60
	if r := []rune(s); len(r) > max {
		return strconv.Quote(string(r[:max])) + "…"
	}
	return strconv.Quote(s)
}
