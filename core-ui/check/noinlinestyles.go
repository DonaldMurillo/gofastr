//check-csp:ignore-file
// The linter itself references `style="…"` in regex patterns and
// error messages; the directive exempts this file from its own checks.
package check

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Inline-style ban — compile-rule enforcement of the framework's
// strict-CSP contract.
//
// The framework's default Content-Security-Policy is
// `default-src 'self'` with no `'unsafe-inline'` for `style-src`,
// which forbids inline `style="…"` attributes on every HTML element.
// A demo page or production screen that ships such an attribute
// silently breaks in the browser: the layout collapses because the
// attribute is stripped before paint.
//
// LintNoInlineStyles walks every .go file in dir (recursive variant
// below) and reports any source location that would emit a forbidden
// inline style attribute. Three detection paths:
//
//   1. Raw string literals containing `style="…"` or `style='…'`
//      anywhere in the body. Catches `render.HTML("<div style=…>")`
//      and back-tick raw strings alike.
//   2. Map / composite literals with a "style" key passed to any of
//      the HTML helpers — render.Tag(…, map[string]string{"style": …},
//      html.Attrs{"style": …}, html.DivConfig{Attrs: html.Attrs{"style":
//      …}}, etc.
//   3. html.* config struct literals whose Attrs field literal carries
//      a "style" key (covered by case 2 because Attrs is a map).
//
// Files that legitimately need to embed a style string (the linter
// itself, the CSP-test fixtures, the style-builder package emitting
// stylesheets) opt out with the `//check-csp:ignore-file` directive.
// _test.go files are skipped automatically since they often build
// fixtures that intentionally contain known-bad strings for
// assertion.
//
// Usage mirrors LintNoInlineScripts — run from `cmd/check-csp` and
// from any package test that wants the rule enforced on every
// `go test ./...`.

var (
	// inlineStyleAttrRe matches a `style="…"` or `style='…'` attribute
	// inside a raw HTML string literal. Anchored to start-of-string or
	// whitespace so prefixes like `data-fui-style=`, `font-style=`,
	// `text-style=` (CSS, data-attributes, custom-named props) don't
	// trigger false positives. The non-greedy quoted body keeps the
	// match short — we just need to know IT EXISTS.
	inlineStyleAttrRe = regexp.MustCompile(`(?s)(?:^|\s)style\s*=\s*("[^"]*"|'[^']*')`)
)

// LintNoInlineStyles scans every .go file in dir (non-recursive) for
// inline style attributes in HTML, and for "style" keys passed as
// attrs to render.Tag / html.Attrs literals. Returns one violation
// per offending source location.
func LintNoInlineStyles(dir string) (*Result, error) {
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
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		filename := filepath.Join(dir, name)
		raw, err := os.ReadFile(filename)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", filename, err)
		}
		if strings.Contains(string(raw), "//check-csp:ignore-file") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filename, raw, parser.AllErrors|parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", filename, err)
		}
		scanInlineStyles(fset, file, filename, result)
	}
	return result, nil
}

// LintNoInlineStylesRecursive walks dir and every subdirectory.
// Skips vendor/, node_modules/, hidden dirs, and testdata/ —
// matching the script linter's recursion contract.
func LintNoInlineStylesRecursive(root string) (*Result, error) {
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
		sub, err := LintNoInlineStyles(path)
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

func scanInlineStyles(fset *token.FileSet, file *ast.File, filename string, result *Result) {
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BasicLit:
			if node.Kind != token.STRING {
				return true
			}
			// Use strconv.Unquote so escape sequences in double-quoted
			// strings (e.g. `\"`) are resolved to their literal chars
			// before the regex scan. stripStringLiteral leaves them
			// escaped, which would hide `style=\"…\"` from the pattern.
			val, err := strconv.Unquote(node.Value)
			if err != nil {
				val = stripStringLiteral(node.Value)
			}
			checkInlineStyleInString(val, fset.Position(node.Pos()).Line, filename, result)
		case *ast.CompositeLit:
			checkStyleKeyInComposite(node, fset, filename, result)
		}
		return true
	})
}

// checkInlineStyleInString flags any string literal whose body
// contains a `style="…"` attribute. False positives are rare — the
// pattern is specific enough that it almost only appears in actual
// HTML emission contexts. When it does fire in a legitimate non-CSP
// context (printing diagnostic HTML, generating an SVG icon for a
// non-CSP'd surface), use `//check-csp:ignore-file` on the file.
func checkInlineStyleInString(s string, line int, filename string, result *Result) {
	if !inlineStyleAttrRe.MatchString(s) {
		return
	}
	result.add(filename, line,
		"inline style=\"…\" attribute forbidden: framework strict-CSP "+
			"(default-src 'self', no 'unsafe-inline' for styles) strips the "+
			"attribute in the browser, collapsing the element's layout. "+
			"Move the rule into a registered stylesheet (style.NewStyleSheet "+
			"/ framework theme.go) and reference via a class name.")
}

// checkStyleKeyInComposite flags `{"style": "…"}` in composite
// literals — typically `render.Tag(…, map[string]string{"style": …}, …)`
// or `html.Attrs{"style": …}`. Catches the case where the value is
// computed at runtime (string concat, fmt.Sprintf) which the
// string-literal scan can't see.
//
// Skips composites whose "style" entry has a non-string value
// (e.g. `map[string]bool{"style": true}` in a sanitizer's forbidden-
// attribute set, which is metadata about the attribute name, not an
// emission of one).
func checkStyleKeyInComposite(node *ast.CompositeLit, fset *token.FileSet, filename string, result *Result) {
	for _, el := range node.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.BasicLit)
		if !ok || key.Kind != token.STRING {
			continue
		}
		if stripStringLiteral(key.Value) != "style" {
			continue
		}
		if !valueLooksLikeString(kv.Value) {
			continue
		}
		result.add(filename, fset.Position(key.Pos()).Line,
			"\"style\" key in HTML attrs forbidden: framework strict-CSP "+
				"strips inline style attributes. Use a class name backed by a "+
				"registered stylesheet (style.NewStyleSheet / registry.RegisterStyle / "+
				"theme.go) instead.")
	}
}

// valueLooksLikeString returns true when the expression is plausibly
// a string at runtime — string literal, fmt.Sprintf, concatenation,
// identifier of a string-typed variable, etc. False for bool/int
// literals (which appear in metadata maps like sanitizer denylists).
func valueLooksLikeString(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.BasicLit:
		return v.Kind == token.STRING
	case *ast.Ident:
		// `true`/`false`/`nil` are predeclared idents — skip those.
		switch v.Name {
		case "true", "false", "nil", "iota":
			return false
		}
		return true
	case *ast.BinaryExpr:
		return valueLooksLikeString(v.X) || valueLooksLikeString(v.Y)
	case *ast.CallExpr:
		return true // fmt.Sprintf / string concat / formatter — assume stringy
	case *ast.SelectorExpr, *ast.IndexExpr:
		return true
	}
	return false
}
