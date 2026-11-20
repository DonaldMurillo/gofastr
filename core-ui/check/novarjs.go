package check

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// LintNoVarJS scans .js files at dir (non-recursive) and reports any `var` declaration found in
// executable code (not in comments / string literals).
func LintNoVarJS(dir string) (*Result, error) {
	result := &Result{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".js") {
			continue
		}
		path := filepath.Join(dir, name)
		if err := scanJSFileForVar(path, result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// LintNoVarJSRecursive walks dir + every subdirectory, running LintNoVarJS on each.
func LintNoVarJSRecursive(root string) (*Result, error) {
	return lintRecursive(root, LintNoVarJS)
}

var reNoVarIgnoreDirective = regexp.MustCompile(`(?m)^[ \t]*//[ \t]*check-novar:ignore-file[ \t]*\r?$`)

func scanJSFileForVar(path string, result *Result) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if reNoVarIgnoreDirective.Match(raw) {
		return nil
	}

	sanitized := stripJSCommentsAndStrings(string(raw))
	scanner := bufio.NewScanner(strings.NewReader(sanitized))
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)
	line := 0
	for scanner.Scan() {
		line++
		if containsVarKeyword(scanner.Text()) {
			result.add(path, line,
				"`var` not allowed in JS runtime modules. Use `const` or `let` (ES2020+ codebase contract). "+
					"Hoisting and function-scope semantics of `var` create surprises `let`/`const` don't.")
		}
	}
	return scanner.Err()
}

func containsVarKeyword(line string) bool {
	idx := 0
	for {
		hit := strings.Index(line[idx:], "var")
		if hit < 0 {
			return false
		}
		start := idx + hit
		end := start + 3
		if start > 0 && isJSIdentChar(line[start-1]) {
			idx = end
			continue
		}
		if end < len(line) && isJSIdentChar(line[end]) {
			idx = end
			continue
		}
		return true
	}
}

func isJSIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '_' || c == '$'
}

func templateExprEnd(src string, i int) (body string, end int) {
	start := i
	depth := 1
	for i < len(src) {
		c := src[i]
		switch {
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i += 2
		case c == '\'' || c == '"' || c == '`':
			q := c
			i++
			for i < len(src) {
				if src[i] == '\\' {
					i += 2
					continue
				}
				if src[i] == q {
					i++
					break
				}
				i++
			}
		case c == '{':
			depth++
			i++
		case c == '}':
			depth--
			if depth == 0 {
				return src[start:i], i
			}
			i++
		default:
			i++
		}
	}
	return src[start:], len(src)
}

func stripJSCommentsAndStrings(src string) string {
	out := make([]byte, 0, len(src))
	i := 0
	for i < len(src) {
		c := src[i]
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			out, i = blankJSLineComment(out, src, i)
			continue
		}
		if c == '/' && i+1 < len(src) && src[i+1] == '*' {
			out, i = blankJSBlockComment(out, src, i)
			continue
		}
		if c == '/' && regexLiteralStartsAt(out) {
			out = append(out, ' ')
			i++
			inClass := false
			for i < len(src) && src[i] != '\n' {
				if src[i] == '\\' && i+1 < len(src) {
					if src[i+1] == '\n' {
						out = append(out, ' ', '\n')
					} else {
						out = append(out, ' ', ' ')
					}
					i += 2
					continue
				}
				if src[i] == '[' {
					inClass = true
				} else if src[i] == ']' {
					inClass = false
				} else if src[i] == '/' && !inClass {
					out = append(out, ' ')
					i++
					break
				}
				out = append(out, ' ')
				i++
			}
			for i < len(src) && (src[i] >= 'a' && src[i] <= 'z') {
				out = append(out, ' ')
				i++
			}
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			quote := c
			out = append(out, ' ') // replace opening quote
			i++
			for i < len(src) {
				if src[i] == '\\' && i+1 < len(src) {
					if src[i+1] == '\n' {
						out = append(out, ' ', '\n')
					} else {
						out = append(out, ' ', ' ')
					}
					i += 2
					continue
				}
				if src[i] == quote {
					out = append(out, ' ')
					i++
					break
				}
				if quote == '`' && src[i] == '$' && i+1 < len(src) && src[i+1] == '{' {
					out = append(out, ' ', ' ')
					i += 2
					body, end := templateExprEnd(src, i)
					out = append(out, stripJSCommentsAndStrings(body)...)
					i = end
					if i < len(src) && src[i] == '}' {
						out = append(out, ' ')
						i++
					}
					continue
				}
				if src[i] == '\n' {
					out = append(out, '\n')
				} else {
					out = append(out, ' ')
				}
				i++
			}
			continue
		}
		out = append(out, c)
		i++
	}
	return string(out)
}

func regexLiteralStartsAt(out []byte) bool {
	j := len(out) - 1
	for j >= 0 && (out[j] == ' ' || out[j] == '\t' || out[j] == '\n' || out[j] == '\r') {
		j--
	}
	if j < 0 {
		return true
	}
	prev := out[j]
	isWord := prev == '_' || prev == '$' || (prev >= 'a' && prev <= 'z') || (prev >= 'A' && prev <= 'Z') || (prev >= '0' && prev <= '9')
	if !isWord {
		return prev != ')' && prev != ']' && prev != '\'' && prev != '"' && prev != '`'
	}
	k := j
	for k >= 0 && (out[k] == '_' || out[k] == '$' || (out[k] >= 'a' && out[k] <= 'z') || (out[k] >= 'A' && out[k] <= 'Z') || (out[k] >= '0' && out[k] <= '9')) {
		k--
	}
	switch string(out[k+1 : j+1]) {
	case "return", "typeof", "instanceof", "in", "of", "new", "delete", "void", "throw", "case", "do", "else", "yield", "await":
		return true
	}
	return false
}
