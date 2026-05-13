package check

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// Violation represents a linter error.
type Violation struct {
	File    string
	Line    int
	Message string
}

// Result holds all violations found in a file or package.
type Result struct {
	Violations []Violation
}

func (r *Result) add(file string, line int, msg string) {
	r.Violations = append(r.Violations, Violation{
		File:    file,
		Line:    line,
		Message: msg,
	})
}

// HasErrors returns true if any violations were found.
func (r *Result) HasErrors() bool {
	return len(r.Violations) > 0
}

// Error returns a formatted string of all violations.
func (r *Result) Error() string {
	if !r.HasErrors() {
		return ""
	}
	var sb strings.Builder
	for _, v := range r.Violations {
		fmt.Fprintf(&sb, "%s:%d: %s\n", v.File, v.Line, v.Message)
	}
	return sb.String()
}

// allowedImports is the set of packages that may be imported in .ui.go files.
var allowedImports = map[string]bool{
	"fmt":           true,
	"strings":       true,
	"strconv":       true,
	"html/template": true,
	"html":          true,
	"math":          true,
	"time":          true,
	"errors":        true,

	"github.com/DonaldMurillo/gofastr/core/render":       true,
	"github.com/DonaldMurillo/gofastr/core-ui/html":  true,
	"github.com/DonaldMurillo/gofastr/core-ui/component": true,
	"github.com/DonaldMurillo/gofastr/core-ui/signal":    true,
	"github.com/DonaldMurillo/gofastr/core-ui/style":     true,
}

// forbiddenBuiltinCalls maps built-in function names to violation messages.
var forbiddenBuiltinCalls = map[string]string{
	"close":   "channel close not allowed in .ui.go files",
	"recover": "recover not allowed in .ui.go files",
}

// forbiddenImportMessages maps well-known dangerous packages to messages.
var forbiddenImportMessages = map[string]string{
	"os":           "os package not allowed in .ui.go files",
	"io":           "io package not allowed in .ui.go files",
	"io/ioutil":    "io/ioutil package not allowed in .ui.go files",
	"reflect":      "reflect package not allowed in .ui.go files",
	"net":          "net package not allowed in .ui.go files",
	"net/http":     "net/http package not allowed in .ui.go files",
	"syscall":      "syscall package not allowed in .ui.go files",
	"unsafe":       "unsafe package not allowed in .ui.go files",
	"runtime":      "runtime package not allowed in .ui.go files",
	"sync":         "sync package not allowed in .ui.go files",
	"sync/atomic":  "sync/atomic package not allowed in .ui.go files",
	"context":      "context package not allowed in .ui.go files",
	"database/sql": "database/sql package not allowed in .ui.go files",
	"log":          "log package not allowed in .ui.go files",
}

// LintFile lints a single .ui.go file.
func LintFile(filename string) (*Result, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, nil, parser.AllErrors|parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	return lintFile(fset, file, filename), nil
}

// elementRequiredFields maps element function names to their required field rules.
// Each entry maps a config struct field name to whether it's required.
// For fields where EITHER of two fields must be set (e.g. Label/LabelledBy),
// use an "or" group key.
var elementRequiredFields = map[string]fieldRule{
	// Structural: Label OR LabelledBy
	"Nav":     {orFields: []string{"Label", "LabelledBy"}},
	"Section": {orFields: []string{"Label", "LabelledBy"}},
	"Aside":   {orFields: []string{"Label", "LabelledBy"}},

	// Group: Role required
	"Group": {required: []string{"Role"}},

	// Interactive
	"Button":   {required: []string{"Label"}},
	"Link":     {required: []string{"Href", "Text"}},
	"LinkHTML": {required: []string{"Href", "Content"}},
	"Form":     {required: []string{"Method"}},
	"Input":    {required: []string{"Type", "Name"}},
	"Label":    {required: []string{"For", "Text"}},
	"Select":   {required: []string{"Name"}},
	"TextArea": {required: []string{"Name"}},
	"FieldSet": {required: []string{"Legend"}},

	// Text
	"Heading": {required: []string{"Level"}},
	"Abbr":    {required: []string{"Title"}},
	"Time":    {required: []string{"Datetime"}},

	// Media
	"Image":  {required: []string{"Src", "Alt"}},
	"Source": {required: []string{"Src", "Type"}},
}

type fieldRule struct {
	required []string // all must be set
	orFields []string // at least one must be set
}

// LintPackage lints all .go files in a directory.
func LintPackage(dir string) (*Result, error) {
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
		filename := filepath.Join(dir, name)
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filename, nil, parser.AllErrors|parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse error in %s: %w", name, err)
		}
		sub := lintFile(fset, file, filename)
		result.Violations = append(result.Violations, sub.Violations...)
	}
	return result, nil
}

func lintFile(fset *token.FileSet, file *ast.File, filename string) *Result {
	result := &Result{}

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		// 1. Goroutines
		case *ast.GoStmt:
			result.add(filename, fset.Position(node.Go).Line,
				"goroutines not allowed in .ui.go files")

		// 2. Channel send: ch <- val
		case *ast.SendStmt:
			result.add(filename, fset.Position(node.Arrow).Line,
				"channel sends not allowed in .ui.go files")

		// 2. Channel receive: <-ch
		case *ast.UnaryExpr:
			if node.Op == token.ARROW {
				result.add(filename, fset.Position(node.OpPos).Line,
					"channel receives not allowed in .ui.go files")
			}

		// 9. Type switch: switch v := x.(type) { ... }
		case *ast.TypeSwitchStmt:
			result.add(filename, fset.Position(node.Switch).Line,
				"type switches not allowed in .ui.go files")

		// Check forbidden built-in calls, make(chan ...), and element config required fields
		case *ast.CallExpr:
			// Check for html.Xxx(XxxConfig{...}) calls with missing required fields
			checkElementConfig(node, filename, fset, result)

			ident, ok := node.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			// Forbidden built-ins (close, recover)
			if msg, forbidden := forbiddenBuiltinCalls[ident.Name]; forbidden {
				result.add(filename, fset.Position(ident.NamePos).Line, msg)
			}
			// make(chan ...) detection
			if ident.Name == "make" && len(node.Args) > 0 {
				if isChanType(node.Args[0]) {
					result.add(filename, fset.Position(node.Lparen).Line,
						"channel creation (make(chan)) not allowed in .ui.go files")
				}
			}

		// 3. Import validation
		case *ast.ImportSpec:
			checkImport(node, filename, fset, result)
		}
		return true
	})

	return result
}

// isChanType returns true if the expression is a channel type.
func isChanType(expr ast.Expr) bool {
	_, ok := expr.(*ast.ChanType)
	return ok
}

func checkImport(imp *ast.ImportSpec, filename string, fset *token.FileSet, result *Result) {
	if imp.Path == nil {
		return
	}
	path := strings.Trim(imp.Path.Value, `"`)

	// Allowed imports pass
	if allowedImports[path] {
		return
	}

	// Known forbidden packages get specific messages
	if msg, isForbidden := forbiddenImportMessages[path]; isForbidden {
		pos := fset.Position(imp.Pos())
		result.add(filename, pos.Line, msg)
		return
	}

	// Anything else not in the allowed list is flagged
	pos := fset.Position(imp.Pos())
	result.add(filename, pos.Line,
		fmt.Sprintf("import of %q not allowed in .ui.go files", path))
}

// checkElementConfig checks calls like html.Xxx(html.XxxConfig{...})
// for missing required fields.
func checkElementConfig(call *ast.CallExpr, filename string, fset *token.FileSet, result *Result) {
	// We look for patterns:
	//   html.Nav(html.NavConfig{...})
	//   Nav(NavConfig{...})
	var funcName string

	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		funcName = fn.Sel.Name
	case *ast.Ident:
		funcName = fn.Name
	default:
		return
	}

	rule, ok := elementRequiredFields[funcName]
	if !ok {
		return
	}

	if len(call.Args) == 0 {
		return
	}

	// The first argument should be a composite literal: XxxConfig{...}
	lit, ok := call.Args[0].(*ast.CompositeLit)
	if !ok {
		// Not a struct literal — could be a variable, skip static analysis
		return
	}

	// Collect which fields are explicitly set in the struct literal
	setFields := map[string]bool{}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		setFields[key.Name] = true
	}

	line := fset.Position(call.Lparen).Line

	// Check required fields (all must be set)
	for _, field := range rule.required {
		if !setFields[field] {
			result.add(filename, line,
				fmt.Sprintf("html.%s: missing required field %q in %sConfig", funcName, field, funcName))
		}
	}

	// Check OR fields (at least one must be set)
	if len(rule.orFields) > 0 {
		found := false
		for _, field := range rule.orFields {
			if setFields[field] {
				found = true
				break
			}
		}
		if !found {
			result.add(filename, line,
				fmt.Sprintf("html.%s: missing required field — must set one of %v in %sConfig", funcName, rule.orFields, funcName))
		}
	}
}
