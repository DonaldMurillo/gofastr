// Package multipartparse catches a handler that accepts a
// multipart/form-data body and then parses it with ParseForm alone.
//
// The bug (84e4d4b1, the 2026-11-01 battery/auth bug bash): the auth
// battery's isFormRequest accepted "application/x-www-form-urlencoded"
// or "multipart/form-data", and decodeAuthCredentials then ran only
// r.ParseForm. ParseForm never reads a multipart body, so every
// multipart login or register arrived with no fields and answered 400
// "email and password required", though auth.md promised multipart
// works like urlencoded. The fix spelling parses the encoding that was
// accepted:
//
//	if bareContentType(r) != "multipart/form-data" {
//		return r.ParseForm()
//	}
//	return r.ParseMultipartForm(maxBytes)
//
// Shape: a function that calls (*http.Request).ParseForm and accepts
// the multipart content type — it compares a value with the constant
// "multipart/form-data" (==, !=, a switch case, strings.HasPrefix /
// EqualFold / Contains), or calls a bool-returning predicate that
// does, in this package or another (an exported fact) — and never
// reads a multipart body: no ParseMultipartForm, MultipartReader or
// FormFile on the request, no multipart.NewReader, and no call to a
// function (here or, by fact, elsewhere) that does one of those.
//
// FormValue and PostFormValue are deliberately NOT credited, though
// both call ParseMultipartForm on a fresh request: once ParseForm ran,
// r.Form and r.PostForm are non-nil and they never parse again.
// 84e4d4b1's successRedirect read PostFormValue("next") after
// ParseForm and saw nothing on a multipart body for exactly that
// reason.
//
// Silent postures, deliberately:
//   - a comparison that refuses multipart: an if whose condition is the
//     positive test (== or HasPrefix, alone or in a && chain) and whose
//     body leaves without reading anything (return, panic), and a
//     switch case on the constant whose body does the same;
//   - a function that compares the constant only to set it (a header
//     write or an enctype attribute assigns, it does not compare);
//   - _test.go files.
package multipartparse

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/dominance"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
)

var Analyzer = &analysis.Analyzer{
	Name:      "multipartparse",
	Doc:       "report ParseForm in a function that accepts multipart/form-data but never reads a multipart body",
	Run:       run,
	FactTypes: []analysis.Fact{(*acceptsMultipart)(nil), (*readsMultipart)(nil)},
}

// acceptsMultipart marks a bool-returning function that compares a
// value with "multipart/form-data": a content-type predicate.
type acceptsMultipart struct{}

func (*acceptsMultipart) AFact()         {}
func (*acceptsMultipart) String() string { return "acceptsMultipart" }

// readsMultipart marks a function that reads a multipart body itself
// or through a callee.
type readsMultipart struct{}

func (*readsMultipart) AFact()         {}
func (*readsMultipart) String() string { return "readsMultipart" }

const multipartType = "multipart/form-data"

type funcInfo struct {
	decl       *ast.FuncDecl
	obj        *types.Func
	compares   bool
	reads      bool
	parseForms []*ast.CallExpr
	callees    []*types.Func
}

func run(pass *analysis.Pass) (any, error) {
	var infos []*funcInfo
	byObj := map[*types.Func]*funcInfo{}
	for _, decl := range pathflow.FuncsWithBodies(pass) {
		obj, _ := pass.TypesInfo.Defs[decl.Name].(*types.Func)
		if obj == nil {
			continue
		}
		fi := &funcInfo{decl: decl, obj: obj}
		scan(pass, decl.Body, fi)
		infos = append(infos, fi)
		byObj[obj] = fi
	}

	accepts := func(fn *types.Func) bool {
		if fi, ok := byObj[fn]; ok {
			return fi.compares && returnsBool(fn)
		}
		return pass.ImportObjectFact(fn, new(acceptsMultipart))
	}
	// reads is a fixed point over same-package callees.
	reads := map[*types.Func]bool{}
	for _, fi := range infos {
		if fi.reads {
			reads[fi.obj] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for _, fi := range infos {
			if reads[fi.obj] {
				continue
			}
			for _, c := range fi.callees {
				if reads[c] || (byObj[c] == nil && pass.ImportObjectFact(c, new(readsMultipart))) {
					reads[fi.obj] = true
					changed = true
					break
				}
			}
		}
	}

	for _, fi := range infos {
		if fi.compares && returnsBool(fi.obj) {
			pass.ExportObjectFact(fi.obj, &acceptsMultipart{})
		}
		if reads[fi.obj] {
			pass.ExportObjectFact(fi.obj, &readsMultipart{})
		}
	}

	for _, fi := range infos {
		if len(fi.parseForms) == 0 || reads[fi.obj] {
			continue
		}
		accepted := fi.compares
		for _, c := range fi.callees {
			if !accepted && accepts(c) {
				accepted = true
			}
		}
		if !accepted {
			continue
		}
		for _, call := range fi.parseForms {
			pass.Reportf(call.Pos(),
				"multipartparse: %s accepts multipart/form-data but only calls ParseForm, which never reads a multipart body, so every field reads empty; call ParseMultipartForm for the multipart encoding",
				fi.decl.Name.Name)
		}
	}
	return nil, nil
}

// returnsBool reports whether fn's only result is a bool.
func returnsBool(fn *types.Func) bool {
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Results().Len() != 1 {
		return false
	}
	b, ok := sig.Results().At(0).Type().Underlying().(*types.Basic)
	return ok && b.Kind() == types.Bool
}

// scan fills fi from body.
func scan(pass *analysis.Pass, body *ast.BlockStmt, fi *funcInfo) {
	parents := dominance.Parents(body)
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BinaryExpr:
			if (x.Op == token.EQL || x.Op == token.NEQ) && (isMultipart(pass, x.X) || isMultipart(pass, x.Y)) {
				if !(x.Op == token.EQL && refuses(x, parents)) {
					fi.compares = true
				}
			}
		case *ast.CaseClause:
			for _, v := range x.List {
				if isMultipart(pass, v) && !(stmtsDiverge(x.Body) && !readsIn(x.Body)) {
					fi.compares = true
				}
			}
		case *ast.CallExpr:
			scanCall(pass, x, parents, fi)
		}
		return true
	})
}

func scanCall(pass *analysis.Pass, call *ast.CallExpr, parents map[ast.Node]ast.Node, fi *funcInfo) {
	switch pathflow.QualifiedFunc(pass, call.Fun) {
	case "strings.HasPrefix", "strings.EqualFold", "strings.Contains", "strings.HasSuffix":
		for _, a := range call.Args {
			if isMultipart(pass, a) && !refuses(call, parents) {
				fi.compares = true
			}
		}
		return
	case "mime/multipart.NewReader":
		fi.reads = true
		return
	}
	fn, ok := astx.CalleeFunc(pass, ast.Unparen(call.Fun))
	if !ok {
		return
	}
	if isRequestMethod(fn) {
		switch fn.Name() {
		case "ParseForm":
			fi.parseForms = append(fi.parseForms, call)
		case "ParseMultipartForm", "MultipartReader", "FormFile":
			fi.reads = true
		}
		return
	}
	fi.callees = append(fi.callees, fn.Origin())
}

// isRequestMethod reports whether fn is a method of net/http.Request.
func isRequestMethod(fn *types.Func) bool {
	sig, ok := fn.Type().(*types.Signature)
	return ok && sig.Recv() != nil && astx.IsNamed(astx.Deref(sig.Recv().Type()), "net/http", "Request")
}

func isMultipart(pass *analysis.Pass, e ast.Expr) bool {
	s, ok := astx.StringConstant(pass, e)
	return ok && s == multipartType
}

// refuses reports whether the positive test e is an if condition (or a
// conjunct of one) whose body leaves without reading anything: the
// code turns multipart away rather than accepting it.
func refuses(e ast.Node, parents map[ast.Node]ast.Node) bool {
	cur := e
	for {
		p := parents[cur]
		if b, ok := p.(*ast.BinaryExpr); ok && b.Op == token.LAND {
			cur = p
			continue
		}
		if pe, ok := p.(*ast.ParenExpr); ok {
			cur = pe
			continue
		}
		iff, ok := p.(*ast.IfStmt)
		return ok && iff.Cond == cur && stmtsDiverge(iff.Body.List) && !parsesForm(iff.Body)
	}
}

func stmtsDiverge(stmts []ast.Stmt) bool {
	return len(stmts) > 0 && dominance.Diverges(stmts[len(stmts)-1])
}

// parsesForm reports whether body parses or reads the form itself: a
// refusal branch writes a status and leaves, so only a form-parsing
// request method counts against it.
func parsesForm(body ast.Node) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				switch sel.Sel.Name {
				case "ParseMultipartForm", "MultipartReader", "FormFile", "ParseForm":
					found = true
				}
			}
		}
		return !found
	})
	return found
}

// readsIn reports whether stmts parse the form directly.
func readsIn(stmts []ast.Stmt) bool {
	return parsesForm(&ast.BlockStmt{List: stmts})
}
