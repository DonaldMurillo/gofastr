// Package astx holds the small go/analysis helpers that kept being
// copied verbatim between the repo analyzers under
// internal/analyzers. Every function here is behavior-neutral: it
// answers one syntactic or type question and decides nothing. The
// analyzers pick their sinks and postures; this package only names
// callees, strips parentheses of type structure, and splits words.
//
// Users today: asciifold, compositekey, controlbytes, discardmutator,
// divlimit, emitident, hygiene, intwrap, laxenvelope, mapwriter,
// nostore, recovercallback, recoverlog, reqparamlimit, secretcompare,
// timestampid, unboundedbody, unboundedresp, unseated, worldreadable.
// (Parenthesis stripping lives in the standard library's ast.Unparen
// and is not duplicated here.)
package astx

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"
)

// AllFuncs yields every function body in f: declarations plus
// literals (handlers are usually literals returned from factories).
// It replaces funcsOf in controlbytes (whose pass parameter was
// unused) and allFuncs in unboundedbody.
func AllFuncs(f *ast.File) []ast.Node {
	var out []ast.Node
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil {
			out = append(out, fn)
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && lit.Body != nil {
			out = append(out, lit)
		}
		return true
	})
	return out
}

// AllBodies yields every function body in the analyzed package:
// declaration bodies plus function literals (handlers are usually
// literals returned from factories). It replaces the identical
// collection loops in discardmutator.run and reqparamlimit.run.
func AllBodies(pass *analysis.Pass) []*ast.BlockStmt {
	var bodies []*ast.BlockStmt
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch fn := n.(type) {
			case *ast.FuncDecl:
				if fn.Body != nil {
					bodies = append(bodies, fn.Body)
				}
			case *ast.FuncLit:
				bodies = append(bodies, fn.Body)
			}
			return true
		})
	}
	return bodies
}

// BodyOf returns the function node's body, nil when it has none.
// It replaces body in controlbytes and bodyOf in unboundedbody.
func BodyOf(fn ast.Node) *ast.BlockStmt {
	switch fn := fn.(type) {
	case *ast.FuncDecl:
		return fn.Body
	case *ast.FuncLit:
		return fn.Body
	}
	return nil
}

// CalleeFunc resolves a callee expression to its *types.Func through
// the type checker, for a plain identifier or a selector. It replaces
// calleeFunc in asciifold, compositekey, and emitident.
func CalleeFunc(pass *analysis.Pass, fun ast.Expr) (*types.Func, bool) {
	switch f := fun.(type) {
	case *ast.Ident:
		fn, ok := pass.TypesInfo.Uses[f].(*types.Func)
		return fn, ok
	case *ast.SelectorExpr:
		fn, ok := pass.TypesInfo.Uses[f.Sel].(*types.Func)
		return fn, ok
	}
	return nil, false
}

// CalleeName renders a callee's base name (an identifier or a
// selector's Sel), "" for any other expression. It replaces calleeName
// in asciifold and worldreadable and calleeLastName in emitident
// (whose pass parameter was unused).
func CalleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

// Deref strips one pointer level from t. It replaces deref in
// controlbytes and recoverlog.
func Deref(t types.Type) types.Type {
	if ptr, ok := t.(*types.Pointer); ok {
		return ptr.Elem()
	}
	return t
}

// CollectLiterals appends body and every function-literal body nested
// inside it (recursively) to out. It replaces collectLiterals in
// nostore and unseated.
func CollectLiterals(body *ast.BlockStmt, out *[]*ast.BlockStmt) {
	ast.Inspect(body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok {
			*out = append(*out, lit.Body)
			CollectLiterals(lit.Body, out)
			return false
		}
		return true
	})
}

// EvenOffsetArgs returns the message and value arguments of a
// slog-style key-value call — the even offsets, shifted one for the
// *Context forms. It replaces the two inline copies in controlbytes'
// sink table and evenOffsetArgs in recoverlog.
func EvenOffsetArgs(call *ast.CallExpr) []ast.Expr {
	sel, _ := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	start := 0
	if sel != nil && strings.HasSuffix(sel.Sel.Name, "Context") {
		start = 1
	}
	var out []ast.Expr
	for i := start; i < len(call.Args); i += 2 {
		out = append(out, call.Args[i])
	}
	return out
}

// FlipComparison mirrors a comparison operator so the subject can be
// treated as the left operand. It replaces flipComparison in divlimit
// and intwrap.
func FlipComparison(op token.Token) token.Token {
	switch op {
	case token.LSS:
		return token.GTR
	case token.LEQ:
		return token.GEQ
	case token.GTR:
		return token.LSS
	case token.GEQ:
		return token.LEQ
	}
	return op
}

// IsNamed reports whether t is the named type pkgPath.name. It
// replaces isNamed in controlbytes, recovercallback, and recoverlog.
func IsNamed(t types.Type, pkgPath, name string) bool {
	n, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := n.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == pkgPath && obj.Name() == name
}

// MapUnderlying returns x's underlying *types.Map. It replaces
// mapUnderlying in compositekey and the unused copy in asciifold.
func MapUnderlying(pass *analysis.Pass, x ast.Expr) (*types.Map, bool) {
	tv, ok := pass.TypesInfo.Types[x]
	if !ok || tv.Type == nil {
		return nil, false
	}
	m, ok := tv.Type.Underlying().(*types.Map)
	return m, ok
}

// NamedOf returns the named type behind t, or the named type behind
// one pointer level (*T), or nil. It replaces namedOf in nostore and
// unseated.
func NamedOf(t types.Type) *types.Named {
	if named, ok := t.(*types.Named); ok {
		return named
	}
	if p, ok := t.(*types.Pointer); ok {
		if named, ok := p.Elem().(*types.Named); ok {
			return named
		}
	}
	return nil
}

// PkgFuncName renders a selector callee as "pkg.Func", resolving the
// package through the type checker so an aliased import still
// resolves to the real package name. It replaces qualifiedFunc in
// mapwriter and controlbytes (which took the selector directly),
// qualifiedName in unboundedbody, and qualified in hygiene.
// pathflow.QualifiedFunc is the import-path spelling of the same
// walk, for match strings written against full import paths.
func PkgFuncName(pass *analysis.Pass, fun ast.Expr) string {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	pkg, ok := pass.TypesInfo.Uses[id].(*types.PkgName)
	if !ok {
		return ""
	}
	return pkg.Imported().Name() + "." + sel.Sel.Name
}

// QualifiedCallee renders a call target as "pkg.Func" through the
// type checker: a selector through its import-resolved package
// (aliased imports included, PkgFuncName's walk), a plain identifier
// through its *types.Func's package — so a dot-imported or
// package-local function resolves the same way. It replaces
// qualifiedCallee in mapwriter and emitident. (compositekey's private
// copy adds a builtin arm and asciifold's is selector-only; both stay
// local on purpose.)
func QualifiedCallee(pass *analysis.Pass, fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		return PkgFuncName(pass, f)
	case *ast.Ident:
		if fn, ok := pass.TypesInfo.Uses[f].(*types.Func); ok && fn.Pkg() != nil {
			return fn.Pkg().Name() + "." + fn.Name()
		}
	}
	return ""
}

// ReceiverTypeName returns the name of the same-package named type
// behind x (one pointer level stripped), or "" for anything else —
// foreign-package types never match, so a method map keyed by this
// package's receiver names cannot collide with them. It replaces
// receiverTypeName in nostore and unseated. (pathflow keeps its own
// any-package variant for CalleeDecl.)
func ReceiverTypeName(pass *analysis.Pass, x ast.Expr) string {
	tv, ok := pass.TypesInfo.Types[x]
	if !ok {
		return ""
	}
	t := tv.Type
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if named, ok := t.(*types.Named); ok && named.Obj() != nil && named.Obj().Pkg() == pass.Pkg {
		return named.Obj().Name()
	}
	return ""
}

// RecvBaseName returns the identifier at the base of fd's receiver
// type (T or *T), or "" — including when fd has no receiver. It
// replaces recvBaseName in nostore, unseated, and pathflow.
func RecvBaseName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return ""
	}
	var t ast.Expr
	switch r := fd.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		t = r.X
	case *ast.Ident:
		t = r
	default:
		return ""
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// ResolveCall maps a callee expression to a package-local
// declaration: a plain function by name, a same-package qualified
// call by function name, a method by its receiver's named type.
// Ambiguity stays unresolved, which is the quiet direction. It
// replaces resolveCall in nostore and unseated.
func ResolveCall(pass *analysis.Pass, fun ast.Expr, funcs, methods map[string][]*ast.FuncDecl) *ast.FuncDecl {
	switch e := fun.(type) {
	case *ast.Ident:
		if decls := funcs[e.Name]; len(decls) == 1 {
			return decls[0]
		}
	case *ast.SelectorExpr:
		id, ok := e.X.(*ast.Ident)
		if !ok {
			return nil
		}
		use := pass.TypesInfo.Uses[id]
		if pn, isPkg := use.(*types.PkgName); isPkg {
			// Same-package qualified call (pkg.LocalFunc in the
			// package's own files): resolve by function name.
			if pn.Imported() == pass.Pkg {
				if decls := funcs[e.Sel.Name]; len(decls) == 1 {
					return decls[0]
				}
			}
			return nil
		}
		if base := ReceiverTypeName(pass, e.X); base != "" {
			if decls := methods[base+"."+e.Sel.Name]; len(decls) == 1 {
				return decls[0]
			}
		}
	}
	return nil
}

// SplitWords splits an identifier into its camelCase / underscore
// words: confirmationCode -> [confirmation Code], api_key ->
// [api key], APIKey -> [API Key], zipcode -> [zipcode]. It replaces
// splitWords in timestampid and secretcompare.
func SplitWords(name string) []string {
	runes := []rune(name)
	var words []string
	start := 0
	for i := 1; i < len(runes); i++ {
		prev, cur := runes[i-1], runes[i]
		switch {
		case cur == '_' || !unicode.IsLetter(cur) && !unicode.IsDigit(cur):
			if start < i {
				words = append(words, string(runes[start:i]))
			}
			start = i + 1
		case unicode.IsUpper(cur) && unicode.IsLower(prev),
			unicode.IsUpper(cur) && unicode.IsUpper(prev) && i+1 < len(runes) && unicode.IsLower(runes[i+1]):
			if start < i {
				words = append(words, string(runes[start:i]))
			}
			start = i
		}
	}
	if start < len(runes) {
		words = append(words, string(runes[start:]))
	}
	return words
}

// StringConstant returns the constant string value of e (a basic
// literal or a named string constant), reporting whether e has one.
// It replaces stringConstant in compositekey and stringLiteralValue
// in emitident.
func StringConstant(pass *analysis.Pass, e ast.Expr) (string, bool) {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}
