//check-csp:ignore-file
// The linter itself references <script> in regex patterns and error
// messages; the directive exempts this file from its own checks.
package check

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Inline-script ban — compile-rule enforcement of the framework's
// strict-CSP contract.
//
// The framework's default Content-Security-Policy is
// `default-src 'self'`, which forbids inline <script>. Every script
// must be served from an external URL via <script src="…">.
// Components / screens that emit inline <script> blocks (via raw
// render.HTML strings, render.Tag("script", ...) calls with body
// content, or string concatenation) silently break the page in
// production — the browser blocks the script with a CSP error.
//
// LintNoInlineScripts walks every .go file in dir, scans string
// literals (both backtick and double-quoted), and reports any that
// contain a <script>…</script> block whose opening tag lacks a
// src= attribute. Also catches render.Tag("script", …) with
// non-empty body.
//
// Usage:
//
//	result, err := check.LintNoInlineScripts("examples/website")
//	if err != nil { ... }
//	if result.HasErrors() { t.Error(result.Error()) }
//
// Run from a test in any package that produces HTML to enforce the
// rule on every `go test ./...`.

var (
	// scriptOpenRe matches any opening <script> tag in a string literal.
	// Spans line breaks because raw-string literals often do.
	scriptOpenRe = regexp.MustCompile(`(?s)<script\b[^>]*>`)
	// scriptSrcRe matches a src= attribute on a <script> open tag.
	scriptSrcRe = regexp.MustCompile(`(?s)<script\b[^>]*\bsrc\s*=`)
	// scriptCloseRe matches a closing </script> tag. Used as a
	// secondary signal — a string literal containing both opening
	// (without src) AND closing tags is almost certainly an inline
	// script body.
	scriptCloseRe = regexp.MustCompile(`(?s)</script\s*>`)
)

// LintNoInlineScripts scans every .go file in dir (non-recursive)
// for string literals or render.Tag("script", ...) calls that emit
// an inline <script> block. Returns one Violation per offending
// site.
//
// A file can opt out by including the directive
// `//check-csp:ignore-file` somewhere in its source (typically near
// the top). Use only when the file inherently references <script>
// in non-emitting contexts — the linter itself, regex patterns, or
// CLI scaffolding.
func LintNoInlineScripts(dir string) (*Result, error) {
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
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		// Skip test files — they may legitimately embed inline
		// scripts as fixtures for assertion. The runtime rule
		// applies to production code only.
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		filename := filepath.Join(dir, name)
		raw, err := os.ReadFile(filename)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", filename, err)
		}
		// File-level escape hatch.
		if strings.Contains(string(raw), "//check-csp:ignore-file") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filename, raw, parser.AllErrors|parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", filename, err)
		}
		scanInlineScripts(fset, file, filename, result)
	}
	return result, nil
}

// LintNoInlineScriptsRecursive walks dir and every subdirectory,
// running LintNoInlineScripts on each. Skips vendor/, node_modules/,
// hidden dirs, and testdata/.
func LintNoInlineScriptsRecursive(root string) (*Result, error) {
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
		sub, err := LintNoInlineScripts(path)
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

// scanInlineScripts walks the AST and flags string literals or Tag
// calls that look like inline-script emissions.
func scanInlineScripts(fset *token.FileSet, file *ast.File, filename string, result *Result) {
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		// 1. Raw string literals: render.HTML(`<script>…</script>`),
		//    `<script>…</script>` directly, etc.
		case *ast.BasicLit:
			if node.Kind != token.STRING {
				return true
			}
			val := stripStringLiteral(node.Value)
			checkInlineScriptInString(val, fset.Position(node.Pos()).Line, filename, result)

		// 2. render.Tag("script", ...) with non-src body.
		case *ast.CallExpr:
			checkTagCall(node, fset, filename, result)
		}
		return true
	})
}

// stripStringLiteral removes the surrounding quotes/backticks of a
// Go string literal so the regex can scan the content.
func stripStringLiteral(s string) string {
	if len(s) < 2 {
		return s
	}
	first, last := s[0], s[len(s)-1]
	if first == '"' && last == '"' {
		return s[1 : len(s)-1]
	}
	if first == '`' && last == '`' {
		return s[1 : len(s)-1]
	}
	return s
}

// checkInlineScriptInString applies the regex check. A literal that
// contains an opening <script> without src=, AND a closing tag (or
// any body content following the open tag), is flagged.
func checkInlineScriptInString(s string, line int, filename string, result *Result) {
	loc := scriptOpenRe.FindStringIndex(s)
	if loc == nil {
		return
	}
	openTag := s[loc[0]:loc[1]]
	// If the open tag carries src= (external script), it's allowed.
	if scriptSrcRe.MatchString(openTag) {
		return
	}
	// Look for either a closing </script> tag OR any non-whitespace
	// content right after the open. A bare "<script></script>" is
	// technically empty and harmless, but suspicious — flag anyway.
	if scriptCloseRe.MatchString(s) {
		result.add(filename, line,
			"inline <script> block forbidden: framework strict-CSP (default-src 'self') blocks inline JS. "+
				"Serve from an external URL via <script src=\"/your-script.js\"> instead.")
		return
	}
	// Open tag with no close in the same literal — still suspicious
	// (e.g. someone building the script via concatenation).
	tail := strings.TrimSpace(s[loc[1]:])
	if tail != "" {
		result.add(filename, line,
			"<script> open tag without src= forbidden: framework strict-CSP blocks inline JS. "+
				"Use <script src=\"…\"> for external scripts.")
	}
}

// checkTagCall flags render.Tag("script", attrs, children...) and
// html.Tag("script", ...) calls where attrs lacks "src" but children
// are non-empty (inline script body).
func checkTagCall(call *ast.CallExpr, fset *token.FileSet, filename string, result *Result) {
	// Match render.Tag("script", …) / Tag("script", …) by name.
	var funcName string
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		funcName = fn.Sel.Name
	case *ast.Ident:
		funcName = fn.Name
	default:
		return
	}
	if funcName != "Tag" {
		return
	}
	if len(call.Args) < 1 {
		return
	}
	// First arg must be the literal "script" string.
	name, ok := call.Args[0].(*ast.BasicLit)
	if !ok || name.Kind != token.STRING {
		return
	}
	if stripStringLiteral(name.Value) != "script" {
		return
	}
	// Second arg is the attrs map (or nil). If it's a map literal,
	// check whether "src" is a key.
	hasSrc := false
	if len(call.Args) >= 2 {
		if lit, ok := call.Args[1].(*ast.CompositeLit); ok {
			for _, el := range lit.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := kv.Key.(*ast.BasicLit); ok && key.Kind == token.STRING {
					if stripStringLiteral(key.Value) == "src" {
						hasSrc = true
						break
					}
				}
			}
		}
	}
	// Children come from args[2:]. If src is absent and any children
	// are passed, it's an inline script.
	hasChildren := len(call.Args) >= 3
	if !hasSrc && hasChildren {
		result.add(filename, fset.Position(call.Lparen).Line,
			"render.Tag(\"script\", …) with body forbidden: framework strict-CSP blocks inline JS. "+
				"Pass attrs={\"src\": \"…\"} and no children, or serve as a separate <script src=\"…\">.")
	}
}
