// check-csp:ignore-file

package check

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/sourcewalk"
)

var reCSPIgnoreDirective = regexp.MustCompile(`(?m)^[ \t]*//[ \t]*check-csp:ignore-file[ \t]*\r?$`)

func hasCSPIgnoreDirective(raw []byte) bool {
	if !reCSPIgnoreDirective.Match(raw) {
		return false
	}
	file := token.NewFileSet().AddFile("source.go", -1, len(raw))
	var lexer scanner.Scanner
	lexer.Init(file, raw, nil, scanner.ScanComments)
	for {
		position, kind, literal := lexer.Scan()
		if kind == token.EOF {
			return false
		}
		if kind != token.COMMENT || !strings.HasPrefix(literal, "//") {
			continue
		}
		offset := file.Offset(position)
		start := strings.LastIndexByte(string(raw[:offset]), '\n') + 1
		end := offset
		for end < len(raw) && raw[end] != '\n' {
			end++
		}
		if reCSPIgnoreDirective.Match(raw[start:end]) {
			return true
		}
	}
}

var (
	scriptOpenRe      = regexp.MustCompile(`(?si)<script\b[^>]*>`)
	scriptSrcRe       = regexp.MustCompile(`(?si)<script\b[^>]*\ssrc\s*=`)
	scriptInertTypeRe = regexp.MustCompile(`(?si)\stype\s*=\s*["']?(application/json|application/ld\+json|text/plain|module-shim/json)\b`)
	scriptCloseRe     = regexp.MustCompile(`(?si)</script\s*>`)
)

// LintNoInlineScripts scans every .go file in dir (non-recursive) for string literals or
// render.Tag("script", ...) calls that emit an inline <script> block.
func ScanInlineScriptsIn(fset *token.FileSet, file *ast.File, filename string, raw []byte) *Result {
	if fset == nil || file == nil || hasCSPIgnoreDirective(raw) {
		return nil
	}
	result := &Result{}
	scanInlineScripts(fset, file, filename, result)
	return result
}

func LintNoInlineScripts(dir string) (*Result, error) {
	res, err := lintGoDir(dir, lintInlineScripts)
	if err != nil {
		return nil, err
	}
	return res[0], nil
}

func lintInlineScripts(fset *token.FileSet, file *ast.File, filename string, raw []byte, result *Result) {
	allowed := allowedLines(goComments(fset, file, raw), "GOFASTR1805")
	sub := &Result{}
	scanInlineScripts(fset, file, filename, sub)
	for _, v := range sub.Violations {
		if !allowed[v.Line] {
			result.Violations = append(result.Violations, v)
		}
	}
}

func lintGoDir(dir string, scans ...func(fset *token.FileSet, file *ast.File, filename string, raw []byte, result *Result)) ([]*Result, error) {
	results := make([]*Result, len(scans))
	for i := range results {
		results[i] = &Result{}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		filename := filepath.Join(dir, name)
		raw, err := os.ReadFile(filename)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", filename, err)
		}
		if hasCSPIgnoreDirective(raw) {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filename, raw, parser.AllErrors|parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", filename, err)
		}
		for i, scan := range scans {
			scan(fset, file, filename, raw, results[i])
		}
	}
	return results, nil
}

// LintNoInlineScriptsRecursive walks dir and every subdirectory, running LintNoInlineScripts on
// each.
func LintNoInlineScriptsRecursive(root string) (*Result, error) {
	return lintRecursive(root, LintNoInlineScripts)
}

// LintNoInlineRecursive runs both inline detectors in one walk, answering the script result first.
func LintNoInlineRecursive(root string) (*Result, *Result, error) {
	scripts, styles := &Result{}, &Result{}
	err := lintRecursiveDirs(root, func(dir string) error {
		res, err := lintGoDir(dir, lintInlineScripts, lintInlineStyles)
		if err != nil {
			return err
		}
		scripts.Violations = append(scripts.Violations, res[0].Violations...)
		styles.Violations = append(styles.Violations, res[1].Violations...)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return scripts, styles, nil
}

func lintRecursiveDirs(root string, visitDir func(dir string) error, skip ...string) error {
	root = filepath.Clean(root)
	return sourcewalk.Walk(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if path != root && (strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") ||
			base == "vendor" || base == "node_modules" || base == "testdata" ||
			slices.Contains(skip, base)) {
			return filepath.SkipDir
		}
		return visitDir(path)
	})
}

func lintRecursive(root string, lint func(string) (*Result, error), skip ...string) (*Result, error) {
	result := &Result{}
	err := lintRecursiveDirs(root, func(dir string) error {
		sub, err := lint(dir)
		if err != nil {
			return err
		}
		result.Violations = append(result.Violations, sub.Violations...)
		return nil
	}, skip...)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func scanInlineScripts(fset *token.FileSet, file *ast.File, filename string, result *Result) {
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BasicLit:
			if node.Kind != token.STRING {
				return true
			}
			val := stripStringLiteral(node.Value)
			checkInlineScriptInString(val, fset.Position(node.Pos()).Line, filename, result)

		case *ast.CallExpr:
			checkTagCall(node, fset, filename, result)
		}
		return true
	})
}

func stripStringLiteral(s string) string {
	if len(s) < 2 {
		return s
	}
	first, last := s[0], s[len(s)-1]
	if first == '`' && last == '`' {
		return s[1 : len(s)-1]
	}
	if first == '"' && last == '"' {
		if unquoted, err := strconv.Unquote(s); err == nil {
			return unquoted
		}
		return s[1 : len(s)-1]
	}
	return s
}

func checkInlineScriptInString(s string, line int, filename string, result *Result) {
	for _, loc := range scriptOpenRe.FindAllStringIndex(s, -1) {
		openTag := s[loc[0]:loc[1]]
		if scriptSrcRe.MatchString(openTag) {
			continue
		}
		if scriptInertTypeRe.MatchString(openTag) {
			continue
		}
		if scriptCloseRe.MatchString(s[loc[1]:]) {
			result.add(filename, line,
				"inline <script> block forbidden: framework strict-CSP (default-src 'self') blocks inline JS. "+
					"Serve from an external URL via <script src=\"/your-script.js\"> instead.")
			continue
		}
		result.add(filename, line,
			"<script> open tag without src= forbidden: framework strict-CSP blocks inline JS. "+
				"Use <script src=\"…\"> for external scripts.")
	}
}

func isInertScriptType(t string) bool {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "application/json", "application/ld+json", "text/plain", "module-shim/json":
		return true
	}
	return false
}

func checkTagCall(call *ast.CallExpr, fset *token.FileSet, filename string, result *Result) {
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
	name, ok := call.Args[0].(*ast.BasicLit)
	if !ok || name.Kind != token.STRING {
		return
	}
	if stripStringLiteral(name.Value) != "script" {
		return
	}
	hasSrc := false
	inertType := false
	if len(call.Args) >= 2 {
		if lit, ok := call.Args[1].(*ast.CompositeLit); ok {
			for _, el := range lit.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := kv.Key.(*ast.BasicLit)
				if !ok || key.Kind != token.STRING {
					continue
				}
				switch stripStringLiteral(key.Value) {
				case "src":
					hasSrc = true
				case "type":
					if v, ok := kv.Value.(*ast.BasicLit); ok && v.Kind == token.STRING {
						if isInertScriptType(stripStringLiteral(v.Value)) {
							inertType = true
						}
					}
				}
			}
		}
	}
	hasChildren := len(call.Args) >= 3
	if !hasSrc && !inertType && hasChildren {
		result.add(filename, fset.Position(call.Lparen).Line,
			"render.Tag(\"script\", …) with body forbidden: framework strict-CSP blocks inline JS. "+
				"Pass attrs={\"src\": \"…\"} and no children, or serve as a separate <script src=\"…\">.")
	}
}
