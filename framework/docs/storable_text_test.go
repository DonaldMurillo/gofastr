package docs

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestStorableTextHasOnePredicate fails on a hand-rolled copy of
// textsafe.Storable: a function that tests one value for valid UTF-8 and
// for a NUL byte. Such copies drifted apart before they merged;
// a site with extra conditions keeps them beside a textsafe.Storable call.
// The match is syntactic and per function: a copy split across two
// helpers, through an alias, or with a rune loop for the NUL half is not
// seen. It holds the shape the copies took, not every spelling.
//
//gofastr:test-reads *.go
func TestStorableTextHasOnePredicate(t *testing.T) {
	root := repoRoot(t)
	if root == "" {
		t.Skip("no source tree (embedded-only consumer)")
	}
	for _, directory := range []string{"core", "framework", "battery", "core-ui", "cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			if entry.IsDir() {
				if entry.Name() == "testdata" || relative == "framework/experimental" || relative == "core/textsafe" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			positions := token.NewFileSet()
			file, err := parser.ParseFile(positions, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			for _, position := range storableCopies(file) {
				t.Errorf("%s: test UTF-8 and NUL with textsafe.Storable; keep any other refusal (newlines, '=') beside the call", positions.Position(position))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestStorableTextMatcher(t *testing.T) {
	for _, test := range []struct {
		body string
		want int
	}{
		{`return utf8.ValidString(raw) && strings.IndexByte(raw, 0) < 0`, 1},
		{`return textsafe.Storable(raw)`, 0},
		{`return utf8.ValidString(raw) && strings.IndexByte(other, 0) < 0`, 0},
		{`return utf8.Valid(raw) && bytes.IndexByte(raw, '\x00') < 0`, 1},
		{`return utf8.ValidString(raw) && !strings.ContainsRune(raw, 0)`, 1},
		{`return utf8.ValidString(raw) && !strings.Contains(raw, "\x00")`, 1},
		{`return utf8.ValidString(raw) && !strings.Contains(raw, "a\x00")`, 0},
		{`return utf8.ValidString(raw) && strings.Index(raw, "a\x00") < 0`, 0},
		{`return utf8.Valid(raw) && !bytes.Contains(raw, []byte{1,0})`, 0},
		{`return utf8.Valid(raw) && bytes.Index(raw, []byte("a\x00")) < 0`, 0},
		{`return utf8.ValidString(raw) && !strings.ContainsAny(raw, "\x00\n")`, 1},
		{`return utf8.Valid([]byte(raw)) && !strings.ContainsRune(raw, 0)`, 1},
		{`return utf8.ValidString(raw)`, 0},
		{`return strings.IndexByte(raw, 0) < 0`, 0},
		{`return utf8.ValidString(raw) && strings.Index(raw, "\x00") < 0`, 1},
		{`return utf8.ValidString(raw) && strings.IndexAny(raw, "\n\x00") < 0`, 1},
		{`return utf8.Valid(raw) && !bytes.Contains(raw, []byte{0})`, 1},
		{`return utf8.Valid(raw) && bytes.Index(raw, []byte("\x00")) < 0`, 1},
		{`return utf8.Valid(raw) && !bytes.Contains(raw, []byte{1})`, 0},
		{`return utf8.ValidString(raw) && strings.IndexByte(raw, '\000') < 0`, 1},
	} {
		file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", "package fixture\nimport (\"unicode/utf8\"; \"strings\"; \"bytes\")\nfunc storableText(raw string) bool {"+test.body+"}", parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		if got := len(storableCopies(file)); got != test.want {
			t.Errorf("%s: got %d copies, want %d", test.body, got, test.want)
		}
	}
}

func storableCopies(file *ast.File) []token.Pos {
	packages := map[string]string{}
	for _, imported := range file.Imports {
		path, _ := strconv.Unquote(imported.Path.Value)
		name := filepath.Base(path)
		if imported.Name != nil {
			name = imported.Name.Name
		}
		packages[name] = path
	}
	var copies []token.Pos
	ast.Inspect(file, func(node ast.Node) bool {
		var body *ast.BlockStmt
		switch function := node.(type) {
		case *ast.FuncDecl:
			body = function.Body
		case *ast.FuncLit:
			body = function.Body
		}
		if body == nil {
			return true
		}
		valid, nul := map[string]bool{}, map[string]bool{}
		ast.Inspect(body, func(node ast.Node) bool {
			if _, nested := node.(*ast.FuncLit); nested {
				return false
			}
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			qualifier, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}
			path := packages[qualifier.Name]
			if path == "unicode/utf8" && (selector.Sel.Name == "ValidString" || selector.Sel.Name == "Valid") {
				valid[storableExpression(call.Args[0])] = true
			}
			if (path == "strings" || path == "bytes") && len(call.Args) == 2 && storableNULTest(selector.Sel.Name, call.Args[1]) {
				nul[storableExpression(call.Args[0])] = true
			}
			return true
		})
		for expression := range valid {
			if nul[expression] {
				copies = append(copies, body.Pos())
				break
			}
		}
		return true
	})
	return copies
}

func storableExpression(expression ast.Expr) string {
	expression = ast.Unparen(expression)
	if call, ok := expression.(*ast.CallExpr); ok && len(call.Args) == 1 {
		if array, ok := call.Fun.(*ast.ArrayType); ok && array.Len == nil {
			if element, ok := array.Elt.(*ast.Ident); ok && element.Name == "byte" {
				expression = ast.Unparen(call.Args[0])
			}
		}
	}
	var output bytes.Buffer
	_ = format.Node(&output, token.NewFileSet(), expression)
	return output.String()
}

func storableNULTest(name string, argument ast.Expr) bool {
	argument = ast.Unparen(argument)
	switch argument := argument.(type) {
	case *ast.CompositeLit: // []byte{0}
		if name != "Contains" && name != "Index" && name != "ContainsAny" && name != "IndexAny" {
			return false
		}
		if (name == "Contains" || name == "Index") && len(argument.Elts) != 1 {
			return false
		}
		for _, element := range argument.Elts {
			if storableNULLiteral(element) {
				return true
			}
		}
		return false
	case *ast.CallExpr: // []byte("\x00")
		if len(argument.Args) == 1 {
			if array, ok := argument.Fun.(*ast.ArrayType); ok && array.Len == nil {
				return storableNULTest(name, argument.Args[0])
			}
		}
		return false
	}
	switch name {
	case "IndexByte", "IndexRune", "ContainsRune":
		return storableNULLiteral(argument)
	case "Contains", "ContainsAny", "Index", "IndexAny":
		literal, ok := argument.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return false
		}
		value, err := strconv.Unquote(literal.Value)
		if name == "Contains" || name == "Index" {
			return err == nil && value == "\x00"
		}
		return err == nil && strings.IndexByte(value, 0) >= 0
	}
	return false
}

// storableNULLiteral reports whether expression is a literal zero byte or
// rune: 0, 0x00, '\x00' or '\000'.
func storableNULLiteral(expression ast.Expr) bool {
	literal, ok := ast.Unparen(expression).(*ast.BasicLit)
	if !ok {
		return false
	}
	switch literal.Kind {
	case token.INT:
		value, err := strconv.ParseUint(literal.Value, 0, 64)
		return err == nil && value == 0
	case token.CHAR:
		value, _, _, err := strconv.UnquoteChar(strings.Trim(literal.Value, "'"), '\'')
		return err == nil && value == 0
	}
	return false
}
