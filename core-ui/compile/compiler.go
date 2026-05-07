package compile

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// JSOutput holds compiled JavaScript.
type JSOutput struct {
	JS      string   // compiled JavaScript code
	Imports []string // required imports/modules
	Errors  []string // compilation errors/warnings
}

func (o *JSOutput) addError(msg string) {
	o.Errors = append(o.Errors, msg)
}

// CompileFile compiles a .ui.go file to JavaScript.
// Only processes Render() and Actions() methods.
func CompileFile(filename string) (*JSOutput, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, nil, parser.AllErrors|parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	return compileAST(fset, file), nil
}

// CompilePackage compiles all .ui.go files in a directory.
func CompilePackage(dir string) (*JSOutput, error) {
	output := &JSOutput{}
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
		sub, err := CompileFile(filename)
		if err != nil {
			return nil, fmt.Errorf("compile %s: %w", name, err)
		}
		output.JS += sub.JS
		output.Imports = append(output.Imports, sub.Imports...)
		output.Errors = append(output.Errors, sub.Errors...)
	}
	return output, nil
}

// CompileComponent compiles a single component's methods.
func CompileComponent(fset *token.FileSet, file *ast.File, typeName string) (*JSOutput, error) {
	output := &JSOutput{}
	var sb strings.Builder

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		// Check if this method belongs to the target type
		if fn.Recv == nil || len(fn.Recv.List) == 0 {
			continue
		}
		recvType := recvTypeName(fn.Recv.List[0].Type)
		if recvType != typeName {
			continue
		}
		// Only compile Render and Actions methods
		if fn.Name.Name != "Render" && fn.Name.Name != "Actions" {
			continue
		}

		js, err := compileFuncDecl(fn)
		if err != nil {
			output.addError(fmt.Sprintf("method %s: %v", fn.Name.Name, err))
			continue
		}
		sb.WriteString(js)
	}

	output.JS = sb.String()
	return output, nil
}

func recvTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return recvTypeName(e.X)
	default:
		return ""
	}
}

func compileAST(fset *token.FileSet, file *ast.File) *JSOutput {
	output := &JSOutput{}
	var sb strings.Builder

	// Collect imports for reference
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		output.Imports = append(output.Imports, path)
	}

	// Find and compile all method declarations
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		// Only compile methods (with receivers)
		if fn.Recv == nil || len(fn.Recv.List) == 0 {
			continue
		}
		// Only compile Render and Actions methods
		if fn.Name.Name != "Render" && fn.Name.Name != "Actions" {
			continue
		}

		js, err := compileFuncDecl(fn)
		if err != nil {
			output.addError(fmt.Sprintf("method %s.%s: %v",
				recvTypeName(fn.Recv.List[0].Type), fn.Name.Name, err))
			continue
		}
		sb.WriteString(js)
	}

	output.JS = sb.String()
	return output
}

func compileFuncDecl(fn *ast.FuncDecl) (string, error) {
	var sb strings.Builder

	receiver := "this"
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		if len(fn.Recv.List[0].Names) > 0 {
			receiver = fn.Recv.List[0].Names[0].Name
		}
	}

	jsName := toCamelCase(fn.Name.Name)

	// Determine parameters
	params := []string{}
	if fn.Type.Params != nil {
		for _, param := range fn.Type.Params.List {
			for _, name := range param.Names {
				params = append(params, name.Name)
			}
		}
	}

	sb.WriteString(fmt.Sprintf("function %s(%s) {\n", jsName, strings.Join(params, ", ")))
	sb.WriteString(fmt.Sprintf("  const %s = this;\n", receiver))

	for _, stmt := range fn.Body.List {
		js, err := compileStmt(stmt, 1)
		if err != nil {
			return "", err
		}
		sb.WriteString(js)
	}

	sb.WriteString("}\n")
	return sb.String(), nil
}

func compileStmt(stmt ast.Stmt, indent int) (string, error) {
	pad := strings.Repeat("  ", indent)

	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		return compileReturn(s, pad)

	case *ast.AssignStmt:
		return compileAssign(s, pad, indent)

	case *ast.IfStmt:
		return compileIf(s, pad, indent)

	case *ast.ForStmt:
		return compileFor(s, pad, indent)

	case *ast.RangeStmt:
		return compileRange(s, pad, indent)

	case *ast.ExprStmt:
		js, err := compileExpr(s.X)
		if err != nil {
			return "", err
		}
		return pad + js + ";\n", nil

	case *ast.BlockStmt:
		return compileBlock(s, indent)

	case *ast.DeclStmt:
		return compileDeclStmt(s, pad, indent)

	case *ast.IncDecStmt:
		return compileIncDec(s, pad)

	case *ast.SwitchStmt:
		return compileSwitch(s, pad, indent)

	case *ast.CaseClause:
		return compileCaseClause(s, pad, indent)

	default:
		return pad + fmt.Sprintf("/* unsupported stmt: %T */\n", s), nil
	}
}

func compileReturn(s *ast.ReturnStmt, pad string) (string, error) {
	if len(s.Results) == 0 {
		return pad + "return;\n", nil
	}
	parts := make([]string, len(s.Results))
	for i, r := range s.Results {
		js, err := compileExpr(r)
		if err != nil {
			return "", err
		}
		parts[i] = js
	}
	return pad + "return " + strings.Join(parts, ", ") + ";\n", nil
}

func compileAssign(s *ast.AssignStmt, pad string, indent int) (string, error) {
	lhs := make([]string, len(s.Lhs))
	for i, e := range s.Lhs {
		js, err := compileExpr(e)
		if err != nil {
			return "", err
		}
		lhs[i] = js
	}

	rhs := make([]string, len(s.Rhs))
	for i, e := range s.Rhs {
		js, err := compileExpr(e)
		if err != nil {
			return "", err
		}
		rhs[i] = js
	}

	// Short variable declaration (:=)
	if s.Tok.String() == ":=" {
		return pad + "const " + strings.Join(lhs, ", ") + " = " + strings.Join(rhs, ", ") + ";\n", nil
	}

	return pad + strings.Join(lhs, ", ") + " " + s.Tok.String() + " " + strings.Join(rhs, ", ") + ";\n", nil
}

func compileIf(s *ast.IfStmt, pad string, indent int) (string, error) {
	var sb strings.Builder

	// Optional init statement
	if s.Init != nil {
		init, err := compileStmt(s.Init, indent)
		if err != nil {
			return "", err
		}
		sb.WriteString(init)
	}

	cond, err := compileExpr(s.Cond)
	if err != nil {
		return "", err
	}
	sb.WriteString(pad + "if (" + cond + ") {\n")

	body, err := compileBlock(s.Body, indent+1)
	if err != nil {
		return "", err
	}
	sb.WriteString(body)
	sb.WriteString(pad + "}")

	if s.Else != nil {
		sb.WriteString(" else ")
		switch e := s.Else.(type) {
		case *ast.IfStmt:
			elseIf, err := compileIf(e, "", 0)
			if err != nil {
				return "", err
			}
			// Remove leading padding since we're already inline
			sb.WriteString(strings.TrimLeft(elseIf, " \t"))
		case *ast.BlockStmt:
			sb.WriteString("{\n")
			body, err := compileBlock(e, indent+1)
			if err != nil {
				return "", err
			}
			sb.WriteString(body)
			sb.WriteString(pad + "}")
		default:
			elseStr, err := compileStmt(e, indent)
			if err != nil {
				return "", err
			}
			sb.WriteString("{\n" + elseStr + pad + "}")
		}
	}
	sb.WriteString("\n")

	return sb.String(), nil
}

func compileFor(s *ast.ForStmt, pad string, indent int) (string, error) {
	var sb strings.Builder

	// Traditional for loop: for init; cond; post { body }
	var initJS, condJS, postJS string
	var err error

	if s.Init != nil {
		init, err := compileStmt(s.Init, 0)
		if err != nil {
			return "", err
		}
		initJS = strings.TrimRight(init, ";\n ")
	}

	if s.Cond != nil {
		condJS, err = compileExpr(s.Cond)
		if err != nil {
			return "", err
		}
	}

	if s.Post != nil {
		post, err := compileStmt(s.Post, 0)
		if err != nil {
			return "", err
		}
		postJS = strings.TrimRight(post, ";\n ")
	}

	sb.WriteString(pad + fmt.Sprintf("for (%s; %s; %s) {\n", initJS, condJS, postJS))

	body, err := compileBlock(s.Body, indent+1)
	if err != nil {
		return "", err
	}
	sb.WriteString(body)
	sb.WriteString(pad + "}\n")

	return sb.String(), nil
}

func compileRange(s *ast.RangeStmt, pad string, indent int) (string, error) {
	var sb strings.Builder

	x, err := compileExpr(s.X)
	if err != nil {
		return "", err
	}

	var keyVar, valVar string
	if s.Key != nil {
		keyVar, err = compileExpr(s.Key)
		if err != nil {
			return "", err
		}
	}
	if s.Value != nil {
		valVar, err = compileExpr(s.Value)
		if err != nil {
			return "", err
		}
	}

	// Generate appropriate for..of
	if keyVar != "" && valVar != "" {
		sb.WriteString(pad + fmt.Sprintf("for (const [%s, %s] of %s.entries()) {\n", keyVar, valVar, x))
	} else if keyVar != "" {
		sb.WriteString(pad + fmt.Sprintf("for (const %s of %s) {\n", keyVar, x))
	} else {
		sb.WriteString(pad + fmt.Sprintf("for (const _ of %s) {\n", x))
	}

	body, err := compileBlock(s.Body, indent+1)
	if err != nil {
		return "", err
	}
	sb.WriteString(body)
	sb.WriteString(pad + "}\n")

	return sb.String(), nil
}

func compileBlock(block *ast.BlockStmt, indent int) (string, error) {
	var sb strings.Builder
	for _, stmt := range block.List {
		js, err := compileStmt(stmt, indent)
		if err != nil {
			return "", err
		}
		sb.WriteString(js)
	}
	return sb.String(), nil
}

func compileDeclStmt(s *ast.DeclStmt, pad string, indent int) (string, error) {
	// Handle var declarations inside function bodies
	var sb strings.Builder
	if gen, ok := s.Decl.(*ast.GenDecl); ok {
		for _, spec := range gen.Specs {
			if vs, ok := spec.(*ast.ValueSpec); ok {
				for i, name := range vs.Names {
					var valJS string
					if i < len(vs.Values) {
						js, err := compileExpr(vs.Values[i])
						if err != nil {
							return "", err
						}
						valJS = " = " + js
					}
					sb.WriteString(pad + "let " + name.Name + valJS + ";\n")
				}
			}
		}
	}
	return sb.String(), nil
}

func compileIncDec(s *ast.IncDecStmt, pad string) (string, error) {
	x, err := compileExpr(s.X)
	if err != nil {
		return "", err
	}
	tok := "++"
	if s.Tok == token.DEC {
		tok = "--"
	}
	return pad + x + tok + ";\n", nil
}

func compileSwitch(s *ast.SwitchStmt, pad string, indent int) (string, error) {
	var sb strings.Builder

	if s.Init != nil {
		init, err := compileStmt(s.Init, indent)
		if err != nil {
			return "", err
		}
		sb.WriteString(init)
	}

	if s.Tag != nil {
		tag, err := compileExpr(s.Tag)
		if err != nil {
			return "", err
		}
		sb.WriteString(pad + "switch (" + tag + ") {\n")
	} else {
		sb.WriteString(pad + "switch (true) {\n")
	}

	if s.Body != nil {
		for _, stmt := range s.Body.List {
			js, err := compileStmt(stmt, indent)
			if err != nil {
				return "", err
			}
			sb.WriteString(js)
		}
	}

	sb.WriteString(pad + "}\n")
	return sb.String(), nil
}

func compileCaseClause(s *ast.CaseClause, pad string, indent int) (string, error) {
	var sb strings.Builder
	if s.List == nil {
		sb.WriteString(pad + "default:\n")
	} else {
		for _, expr := range s.List {
			js, err := compileExpr(expr)
			if err != nil {
				return "", err
			}
			sb.WriteString(pad + "case " + js + ":\n")
		}
	}
	for _, stmt := range s.Body {
		js, err := compileStmt(stmt, indent+1)
		if err != nil {
			return "", err
		}
		sb.WriteString(js)
	}
	return sb.String(), nil
}

func compileExpr(expr ast.Expr) (string, error) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return compileBasicLit(e)

	case *ast.Ident:
		if e.Name == "nil" {
			return "null", nil
		}
		return e.Name, nil

	case *ast.CallExpr:
		return compileCall(e)

	case *ast.BinaryExpr:
		return compileBinary(e)

	case *ast.SelectorExpr:
		return compileSelector(e)

	case *ast.IndexExpr:
		return compileIndex(e)

	case *ast.SliceExpr:
		return compileSlice(e)

	case *ast.ParenExpr:
		inner, err := compileExpr(e.X)
		if err != nil {
			return "", err
		}
		return "(" + inner + ")", nil

	case *ast.UnaryExpr:
		return compileUnary(e)

	case *ast.CompositeLit:
		return compileCompositeLit(e)

	case *ast.KeyValueExpr:
		return compileKeyValue(e)

	case *ast.StarExpr:
		inner, err := compileExpr(e.X)
		if err != nil {
			return "", err
		}
		return inner, nil

	case *ast.TypeAssertExpr:
		return compileExpr(e.X)

	case *ast.FuncLit:
		return compileFuncLit(e)

	case *ast.MapType:
		return "{}", nil

	case *ast.ArrayType:
		return "[]", nil

	case *ast.Ellipsis:
		inner, err := compileExpr(e.Elt)
		if err != nil {
			return "", err
		}
		return "..." + inner, nil

	default:
		return fmt.Sprintf("/* unsupported expr: %T */", e), nil
	}
}

func compileBasicLit(e *ast.BasicLit) (string, error) {
	return e.Value, nil
}

func compileCall(e *ast.CallExpr) (string, error) {
	// Check for fmt.Sprintf → template literal
	if isFmtSprintf(e) {
		return compileSprintfToTemplate(e)
	}

	// Check for elements.Xxx() calls
	if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
		if ident, ok := sel.X.(*ast.Ident); ok {
			// elements.Div(...) → div(...)
			if ident.Name == "elements" {
				return compileElementCall(e, sel)
			}
			// fmt.Sprintf handled above
		}
	}

	// Check for make()
	if ident, ok := e.Fun.(*ast.Ident); ok {
		if ident.Name == "make" {
			return compileMake(e)
		}
		if ident.Name == "len" {
			if len(e.Args) > 0 {
				arg, err := compileExpr(e.Args[0])
				if err != nil {
					return "", err
				}
				return arg + ".length", nil
			}
		}
		if ident.Name == "append" {
			if len(e.Args) >= 2 {
				slice, err := compileExpr(e.Args[0])
				if err != nil {
					return "", err
				}
				args := make([]string, len(e.Args)-1)
				for i, a := range e.Args[1:] {
					js, err := compileExpr(a)
					if err != nil {
						return "", err
					}
					args[i] = js
				}
				return fmt.Sprintf("[...%s, %s]", slice, strings.Join(args, ", ")), nil
			}
		}
		if ident.Name == "string" {
			if len(e.Args) > 0 {
				arg, err := compileExpr(e.Args[0])
				if err != nil {
					return "", err
				}
				return "String(" + arg + ")", nil
			}
		}
	}

	// General function call
	fun, err := compileExpr(e.Fun)
	if err != nil {
		return "", err
	}

	// Lowercase method names for method calls
	if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
		fun = compileSelectorExpr(sel)
	}

	args := make([]string, len(e.Args))
	for i, arg := range e.Args {
		// Handle ellipsis (spread)
		if _, ok := arg.(*ast.Ellipsis); ok {
			a, err := compileExpr(arg)
			if err != nil {
				return "", err
			}
			args[i] = a
		} else {
			a, err := compileExpr(arg)
			if err != nil {
				return "", err
			}
			args[i] = a
		}
	}

	return fun + "(" + strings.Join(args, ", ") + ")", nil
}

func compileElementCall(e *ast.CallExpr, sel *ast.SelectorExpr) (string, error) {
	// elements.Div(...) → div(...)
	name := toCamelCase(sel.Sel.Name)

	args := make([]string, len(e.Args))
	for i, arg := range e.Args {
		js, err := compileExpr(arg)
		if err != nil {
			return "", err
		}
		args[i] = js
	}

	return name + "(" + strings.Join(args, ", ") + ")", nil
}

func compileSprintfToTemplate(e *ast.CallExpr) (string, error) {
	if len(e.Args) < 2 {
		// No args, just return the format string
		if len(e.Args) == 1 {
			if lit, ok := e.Args[0].(*ast.BasicLit); ok {
				return lit.Value, nil
			}
		}
		return "''", nil
	}

	// First arg is the format string
	fmtStr, ok := e.Args[0].(*ast.BasicLit)
	if !ok {
		// Fallback: can't convert dynamically
		fun, _ := compileExpr(e.Fun)
		args := make([]string, len(e.Args))
		for i, a := range e.Args {
			js, _ := compileExpr(a)
			args[i] = js
		}
		return fun + "(" + strings.Join(args, ", ") + ")", nil
	}

	// Parse the format string
	format := strings.Trim(fmtStr.Value, `"`)

	// Replace %s, %d, %v, %f etc. with ${arg}
	var sb strings.Builder
	argIdx := 1
	i := 0
	for i < len(format) {
		if format[i] == '%' && i+1 < len(format) {
			switch format[i+1] {
			case 's', 'd', 'v', 'f', 't', 'b', 'x', 'X', 'o', 'e', 'g':
				if argIdx < len(e.Args) {
					arg, err := compileExpr(e.Args[argIdx])
					if err != nil {
						return "", err
					}
					sb.WriteString("${" + arg + "}")
					argIdx++
				}
				i += 2
				continue
			case '%':
				sb.WriteString("%")
				i += 2
				continue
			}
		}
		sb.WriteByte(format[i])
		i++
	}

	return "`" + sb.String() + "`", nil
}

func compileMake(e *ast.CallExpr) (string, error) {
	if len(e.Args) == 0 {
		return "{}", nil
	}
	// make(map[...]) → {}
	if _, ok := e.Args[0].(*ast.MapType); ok {
		return "{}", nil
	}
	// make([]T, n) → new Array(n)
	if _, ok := e.Args[0].(*ast.ArrayType); ok {
		if len(e.Args) > 1 {
			n, err := compileExpr(e.Args[1])
			if err != nil {
				return "", err
			}
			return "new Array(" + n + ")", nil
		}
		return "[]", nil
	}
	return "{}", nil
}

func compileBinary(e *ast.BinaryExpr) (string, error) {
	left, err := compileExpr(e.X)
	if err != nil {
		return "", err
	}
	right, err := compileExpr(e.Y)
	if err != nil {
		return "", err
	}

	op := e.Op.String()
	// Convert Go-specific operators
	switch e.Op {
	case token.EQL:
		op = "==="
	case token.NEQ:
		op = "!=="
	case token.AND:
		op = "&&"
	case token.OR:
		op = "||"
	}

	return left + " " + op + " " + right, nil
}

func compileSelector(e *ast.SelectorExpr) (string, error) {
	return compileSelectorExpr(e), nil
}

func compileSelectorExpr(e *ast.SelectorExpr) string {
	x, err := compileExpr(e.X)
	if err != nil {
		x = "/* err */"
	}

	// Lowercase method names for common patterns
	sel := toCamelCase(e.Sel.Name)

	return x + "." + sel
}

func compileIndex(e *ast.IndexExpr) (string, error) {
	x, err := compileExpr(e.X)
	if err != nil {
		return "", err
	}
	idx, err := compileExpr(e.Index)
	if err != nil {
		return "", err
	}
	return x + "[" + idx + "]", nil
}

func compileSlice(e *ast.SliceExpr) (string, error) {
	x, err := compileExpr(e.X)
	if err != nil {
		return "", err
	}

	parts := []string{"", "", ""}
	if e.Low != nil {
		low, err := compileExpr(e.Low)
		if err != nil {
			return "", err
		}
		parts[0] = low
	}
	if e.High != nil {
		high, err := compileExpr(e.High)
		if err != nil {
			return "", err
		}
		parts[1] = high
	}
	if e.Slice3 {
		if e.Max != nil {
			max, err := compileExpr(e.Max)
			if err != nil {
				return "", err
			}
			parts[2] = max
		}
		return x + ".slice(" + parts[0] + ", " + parts[1] + ")", nil
	}

	return x + ".slice(" + parts[0] + ", " + parts[1] + ")", nil
}

func compileUnary(e *ast.UnaryExpr) (string, error) {
	x, err := compileExpr(e.X)
	if err != nil {
		return "", err
	}
	return e.Op.String() + x, nil
}

func compileCompositeLit(e *ast.CompositeLit) (string, error) {
	// Check if this looks like an attributes map
	// elements.Attrs{"key": "val"} → {key: "val"}
	if sel, ok := e.Type.(*ast.SelectorExpr); ok {
		if ident, ok := sel.X.(*ast.Ident); ok {
			if ident.Name == "elements" && sel.Sel.Name == "Attrs" {
				return compileKVListToObj(e.Elts)
			}
		}
	}

	// Map literal
	if _, ok := e.Type.(*ast.MapType); ok {
		return compileKVListToObj(e.Elts)
	}

	// Array/slice literal
	if _, ok := e.Type.(*ast.ArrayType); ok {
		return compileListToArr(e.Elts)
	}

	// If no type or unknown type, try as array or object
	if e.Type == nil || len(e.Elts) == 0 {
		return compileListToArr(e.Elts)
	}

	// Check if elements are KeyValueExpr → treat as object
	if len(e.Elts) > 0 {
		if _, ok := e.Elts[0].(*ast.KeyValueExpr); ok {
			return compileKVListToObj(e.Elts)
		}
	}

	return compileListToArr(e.Elts)
}

func compileKVListToObj(elts []ast.Expr) (string, error) {
	pairs := make([]string, len(elts))
	for i, elt := range elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			js, err := compileExpr(elt)
			if err != nil {
				return "", err
			}
			pairs[i] = js
			continue
		}
		key, err := compileExpr(kv.Key)
		if err != nil {
			return "", err
		}
		// Strip quotes from string keys for JS object syntax
		if len(key) >= 2 && key[0] == '"' && key[len(key)-1] == '"' {
			key = key[1 : len(key)-1]
		}
		val, err := compileExpr(kv.Value)
		if err != nil {
			return "", err
		}
		pairs[i] = key + ": " + val
	}
	return "{" + strings.Join(pairs, ", ") + "}", nil
}

func compileListToArr(elts []ast.Expr) (string, error) {
	items := make([]string, len(elts))
	for i, elt := range elts {
		js, err := compileExpr(elt)
		if err != nil {
			return "", err
		}
		items[i] = js
	}
	return "[" + strings.Join(items, ", ") + "]", nil
}

func compileKeyValue(e *ast.KeyValueExpr) (string, error) {
	key, err := compileExpr(e.Key)
	if err != nil {
		return "", err
	}
	val, err := compileExpr(e.Value)
	if err != nil {
		return "", err
	}
	return key + ": " + val, nil
}

func compileFuncLit(e *ast.FuncLit) (string, error) {
	var sb strings.Builder
	sb.WriteString("function(")

	params := []string{}
	if e.Type.Params != nil {
		for _, param := range e.Type.Params.List {
			for _, name := range param.Names {
				params = append(params, name.Name)
			}
		}
	}
	sb.WriteString(strings.Join(params, ", "))
	sb.WriteString(") {\n")

	if e.Body != nil {
		for _, stmt := range e.Body.List {
			js, err := compileStmt(stmt, 1)
			if err != nil {
				return "", err
			}
			sb.WriteString(js)
		}
	}

	sb.WriteString("}")
	return sb.String(), nil
}

func isFmtSprintf(e *ast.CallExpr) bool {
	sel, ok := e.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel.Name != "Sprintf" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return ident.Name == "fmt"
}

// CompileSource compiles Go source code from a string to JavaScript.
func CompileSource(src string) (*JSOutput, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.AllErrors|parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	return compileAST(fset, file), nil
}

// CompileFuncDecl compiles a single function declaration to JavaScript.
// This is the public API for compiling individual function nodes.
func CompileFuncDecl(fn *ast.FuncDecl, fset *token.FileSet) (string, error) {
	return compileFuncDecl(fn)
}

func writeFileErr(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// toCamelCase converts PascalCase to camelCase.
func toCamelCase(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
