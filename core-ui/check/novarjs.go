package check

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// No-var JS lint — bans the legacy `var` keyword from the runtime
// modules. We're an ES2020+ codebase (see runtime.js banner); `var`
// brings hoisting and function-scoped surprises that `let` / `const`
// don't. The check is intentionally narrow:
//
//   - Scans `core-ui/runtime/*.js` and `core-ui/runtime/src/*.js`.
//   - Flags any line containing the `var ` keyword that isn't inside
//     a string literal, a // line comment, or a /* */ block comment.
//
// A file can opt out via the `//check-novar:ignore-file` directive
// (kept for emergency only — the codebase has zero exemptions today).
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
				"`var` not allowed in JS runtime modules — use `const` or `let` (ES2020+ codebase contract). "+
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
// (alpha-numeric, underscore, or dollar — ASCII-only for the lint;
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
		// String literal (single, double, backtick) — handle escapes.
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
