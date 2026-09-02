package check

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// No-var JS lint, bans the legacy `var` keyword from the runtime
// modules. We're an ES2020+ codebase (see runtime.js banner); `var`
// brings hoisting and function-scoped surprises that `let` / `const`
// don't. The check is intentionally narrow:
//
//   - Scans `core-ui/runtime/*.js` and `core-ui/runtime/src/*.js`.
//   - Flags any line containing the `var ` keyword that isn't inside
//     a string literal, a // line comment, or a /* */ block comment.
//
// A file can opt out via the `//check-novar:ignore-file` directive
// (kept for emergency only, the codebase has zero exemptions today).
//
// Run from a test:
//
//	res, err := check.LintNoVarJS("core-ui/runtime")
//	if err != nil { ... }
//	if res.HasErrors() { t.Error(res.Error()) }

// LintNoVarJS scans .js files at dir (non-recursive) and reports any
// `var` declaration found in executable code (not in comments / string
// literals).
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

// LintNoVarJSRecursive walks dir + every subdirectory, running
// LintNoVarJS on each. Skips vendor/, node_modules/, hidden dirs, and
// testdata/.
func LintNoVarJSRecursive(root string) (*Result, error) {
	result := &Result{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if path != root && (strings.HasPrefix(base, ".") ||
			base == "vendor" || base == "node_modules" || base == "testdata") {
			return filepath.SkipDir
		}
		sub, err := LintNoVarJS(path)
		if err != nil {
			return err
		}
		result.Violations = append(result.Violations, sub.Violations...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func scanJSFileForVar(path string, result *Result) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if strings.Contains(string(raw), "//check-novar:ignore-file") {
		return nil
	}

	// Strip comments + string literals to a sanitized stream, keeping
	// line breaks so reported line numbers match the original file.
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

// containsVarKeyword reports whether the sanitized line contains a
// standalone `var` keyword. Requires word boundaries on both sides so
// identifiers like `varietyName` or `myvar` don't false-positive.
func containsVarKeyword(line string) bool {
	idx := 0
	for {
		hit := strings.Index(line[idx:], "var")
		if hit < 0 {
			return false
		}
		start := idx + hit
		end := start + 3
		// Boundary before: start of line, or a non-identifier char.
		if start > 0 && isJSIdentChar(line[start-1]) {
			idx = end
			continue
		}
		// Boundary after: end of line, or a non-identifier char.
		if end < len(line) && isJSIdentChar(line[end]) {
			idx = end
			continue
		}
		return true
	}
}

// isJSIdentChar reports whether c can be part of a JS identifier
// (alpha-numeric, underscore, or dollar, ASCII-only for the lint;
// unicode identifiers are vanishingly rare in our runtime and would
// false-positive only by matching `varX` where X is a non-ASCII letter).
func isJSIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '_' || c == '$'
}

// stripJSCommentsAndStrings replaces the contents of JS line comments,
// block comments, and string/template literals with whitespace so the
// var-keyword scan doesn't false-positive on the word "var" appearing
// in a comment or string. Preserves line breaks so reported lines
// align with the original source.
// templateExprEnd returns the body of a template interpolation starting at
// i (just past the "${") and the index of its closing brace.
//
// Brace counting alone is not enough: a brace inside a string or a comment
// is not a brace. Scanning those the same way the main loop does is what
// stops `${ "}" }` from ending one character early.
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
		// Line comment
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			for i < len(src) && src[i] != '\n' {
				if src[i] == '\n' {
					out = append(out, '\n')
				} else {
					out = append(out, ' ')
				}
				i++
			}
			continue
		}
		// Block comment
		if c == '/' && i+1 < len(src) && src[i+1] == '*' {
			i += 2
			out = append(out, ' ', ' ')
			for i < len(src) {
				if src[i] == '*' && i+1 < len(src) && src[i+1] == '/' {
					out = append(out, ' ', ' ')
					i += 2
					break
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
		// Regex literal: /…/flags. Blanked like a string, because its body
		// is pattern text, not code — `/var\s+\w+/` is a pattern that
		// MATCHES var declarations, not one. Whether a `/` opens a regex
		// or is division depends on what precedes it: after a value
		// (identifier, number, `)`, `]`) it divides; after an operator,
		// an opener, a separator, or a keyword like return/typeof it
		// starts a pattern. That is the same heuristic every JS tokenizer
		// without a parser uses, and it is enough here because the lint
		// only needs the body blanked, never parsed.
		if c == '/' && regexLiteralStartsAt(out) {
			out = append(out, ' ')
			i++
			inClass := false
			for i < len(src) && src[i] != '\n' {
				if src[i] == '\\' && i+1 < len(src) {
					// Keep the newline an escape swallows: the scanner
					// counts lines from this stream, and dropping one
					// would shift every later line number by one.
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
			// Flags are identifier characters that cannot spell `var`
			// on their own, but blank them for symmetry.
			for i < len(src) && (src[i] >= 'a' && src[i] <= 'z') {
				out = append(out, ' ')
				i++
			}
			continue
		}
		// String literal (single, double, backtick), handle escapes.
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
				// A template literal's ${...} body is EXECUTABLE JS, not
				// string content. Blanking it hid a `var` declared inside
				// an interpolation from this lint entirely.
				//
				// The body is run back through this same scanner rather
				// than copied verbatim. Copying was the first attempt and
				// was wrong twice over: a string inside the interpolation
				// (`${"var x = 1"}`) was then scanned as code and reported
				// a violation that is not there, and a `}` inside a string
				// or comment ended the interpolation early, so real code
				// after it went back to being treated as template text and
				// a `var` there was missed. Recursing gets both for free,
				// because finding the true end of the body is the same
				// problem as blanking its contents.
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

// regexLiteralStartsAt reports whether a `/` at the current position
// opens a regex literal, judged by the last non-space byte already
// emitted. A value before it (identifier, number, closing paren or
// bracket) makes it division; anything else — the start of the input,
// an operator, an opener, a separator — makes it a pattern. The keyword
// case (`return /x/`, `typeof /x/`) is caught because the keyword ends
// in a letter: the identifier rule would call it division, so the few
// keywords that can precede an expression are checked by name.
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
		return prev != ')' && prev != ']'
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
