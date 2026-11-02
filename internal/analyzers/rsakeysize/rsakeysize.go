// Package rsakeysize catches an RSA public key built from a decoded
// modulus that nothing bounds from above.
//
// The bug (5c074b43, the 2026-11-01 battery/auth bug bash): passkey
// registration decoded the COSE RS256 modulus with
// new(big.Int).SetBytes and checked only BitLen() < 2048. Attestation
// "none" checks nothing about the key, so anyone could register a
// 4-Mbit modulus, and every anonymous sign-in attempt against it cost
// about a minute of CPU: RSA verification grows with the square of
// the modulus. crypto/tls caps a peer's RSA key at 8192 bits for the
// same reason (CVE-2023-29409). The fix spelling is an upper bound on
// the decoded modulus before the key is built:
//
//	if mod.BitLen() > maxRSABits { return errTooLarge }
//
// Shape: an rsa.PublicKey composite literal, or an assignment to an
// rsa.PublicKey's N field, whose modulus is a big.Int filled by
// SetBytes, or by SetString of a non-constant string, with no
// upper-bound BitLen() comparison on that modulus dominating the key
// construction. A bound counts when it is the condition (or a ||
// disjunct of the condition) of an earlier if statement whose body
// leaves (return, panic, continue), or a conjunct of an enclosing if
// whose then-branch holds the construction, or a call that passes the
// modulus to a same-package function whose body compares a parameter's
// BitLen() from above. A lower bound alone (BitLen() < 2048) is the
// pre-fix shape and does not count. A len() bound on the source bytes
// counts too: it bounds the modulus the same way.
//
// The x509 arm: a *rsa.PublicKey type assertion on the result of
// x509.ParsePKIXPublicKey, or x509.ParsePKCS1PublicKey's own result,
// with no upper-bound comparison on that key's N.BitLen() or Size()
// anywhere in the function. The x509 parsers bound nothing above.
//
// Silent postures, deliberately:
//   - a modulus from a constant (SetString of a literal): a key
//     compiled into the binary is not attacker input;
//   - a modulus that is not a SetBytes/SetString result in the
//     function (generated keys, rsa.GenerateKey, a key read from a
//     struct the code built itself);
//   - _test.go files: fixtures build keys from test vectors.
//
// Out of reach: a bound checked after construction (pub.Size() >
// limit before Verify) is not seen by the literal arm; check the
// modulus before building the key, which is also where a refusal
// reads best.
package rsakeysize

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
	Name: "rsakeysize",
	Doc:  "report an rsa.PublicKey built from a decoded modulus with no upper bound on its size",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	bounders := boundingFuncs(pass)
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		for _, fn := range astx.AllFuncs(f) {
			body := astx.BodyOf(fn)
			checkBody(pass, body, bounders)
		}
	}
	return nil, nil
}

func checkBody(pass *analysis.Pass, body *ast.BlockStmt, bounders map[*types.Func]bool) {
	parents := dominance.Parents(body)
	bound := pathflow.Bindings(pass, body)
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false // checked as its own body
		case *ast.CompositeLit:
			if !isRSAPublicKey(pass.TypesInfo.TypeOf(x)) {
				return true
			}
			if nExpr := modulusField(x); nExpr != nil {
				checkModulus(pass, body, parents, bound, bounders, x, nExpr)
			}
		case *ast.AssignStmt:
			for i, lhs := range x.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "N" || i >= len(x.Rhs) || len(x.Lhs) != len(x.Rhs) {
					continue
				}
				if isRSAPublicKey(pass.TypesInfo.TypeOf(sel.X)) {
					checkModulus(pass, body, parents, bound, bounders, x, x.Rhs[i])
				}
			}
		case *ast.TypeAssertExpr:
			checkX509(pass, body, bound, x)
		case *ast.CallExpr:
			if pathflow.QualifiedFunc(pass, x.Fun) == "crypto/x509.ParsePKCS1PublicKey" {
				checkX509Result(pass, body, parents, x)
			}
		}
		return true
	})
}

// isRSAPublicKey reports whether t is rsa.PublicKey or *rsa.PublicKey.
func isRSAPublicKey(t types.Type) bool {
	return t != nil && astx.IsNamed(astx.Deref(t), "crypto/rsa", "PublicKey")
}

// modulusField returns the N value of an rsa.PublicKey literal.
func modulusField(lit *ast.CompositeLit) ast.Expr {
	for i, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "N" {
				return kv.Value
			}
			continue
		}
		if i == 0 {
			return el
		}
	}
	return nil
}

// checkModulus reports use when nExpr is a decoded modulus that no
// upper bound dominates.
func checkModulus(pass *analysis.Pass, body *ast.BlockStmt, parents map[ast.Node]ast.Node, bound map[types.Object]ast.Expr, bounders map[*types.Func]bool, use ast.Node, nExpr ast.Expr) {
	nExpr = ast.Unparen(nExpr)
	var obj types.Object
	var src ast.Expr // the SetBytes/SetString call
	if id, ok := nExpr.(*ast.Ident); ok {
		obj = pass.TypesInfo.ObjectOf(id)
		if b, ok := bound[obj]; ok {
			src = decodeCall(pass, b)
		}
		if src == nil {
			src = statementDecode(pass, body, obj)
		}
	} else {
		src = decodeCall(pass, nExpr)
	}
	if src == nil {
		return
	}
	call := src.(*ast.CallExpr)
	var srcBytes types.Object
	if id, ok := ast.Unparen(call.Args[0]).(*ast.Ident); ok {
		srcBytes = pass.TypesInfo.ObjectOf(id)
	}
	if obj != nil && dominatedByBound(pass, use, parents, body, obj, srcBytes, bounders) {
		return
	}
	pass.Reportf(use.Pos(),
		"rsakeysize: the RSA modulus %s comes from %s with no upper bound on its size: verification cost grows with the square of the modulus, so a huge key is a CPU DoS; refuse BitLen() > 8192 (crypto/tls's cap) before building the key",
		types.ExprString(nExpr), types.ExprString(call.Fun))
}

// decodeCall returns e when it is a (*big.Int).SetBytes call, or a
// SetString call with a non-constant string, nil otherwise.
func decodeCall(pass *analysis.Pass, e ast.Expr) ast.Expr {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return nil
	}
	fn, ok := astx.CalleeFunc(pass, ast.Unparen(call.Fun))
	if !ok {
		return nil
	}
	sig, _ := fn.Type().(*types.Signature)
	if sig == nil || sig.Recv() == nil || !astx.IsNamed(astx.Deref(sig.Recv().Type()), "math/big", "Int") {
		return nil
	}
	switch fn.Name() {
	case "SetBytes":
		return call
	case "SetString":
		if _, isConst := astx.StringConstant(pass, call.Args[0]); !isConst {
			return call
		}
	}
	return nil
}

// statementDecode finds obj.SetBytes(x) / obj.SetString(x, base) used
// as a statement (or its result discarded) in body.
func statementDecode(pass *analysis.Pass, body *ast.BlockStmt, obj types.Object) ast.Expr {
	var found ast.Expr
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || found != nil {
			return found == nil
		}
		sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := ast.Unparen(sel.X).(*ast.Ident); ok && pass.TypesInfo.ObjectOf(id) == obj {
			if d := decodeCall(pass, call); d != nil {
				found = d
			}
		}
		return true
	})
	return found
}

// dominatedByBound reports whether an upper bound on obj (or on the
// source bytes' len) dominates use.
func dominatedByBound(pass *analysis.Pass, use ast.Node, parents map[ast.Node]ast.Node, body ast.Node, obj, srcBytes types.Object, bounders map[*types.Func]bool) bool {
	for _, stmts := range dominance.Prefix(use, parents, body) {
		for _, st := range stmts {
			iff := dominance.IfStmtOf(st)
			if iff == nil || !dominance.Diverges(iff.Body) {
				continue
			}
			for _, d := range disjuncts(iff.Cond) {
				if upperBound(pass, d, obj, srcBytes, true) {
					return true
				}
			}
			if callsBounder(pass, iff, obj, bounders) {
				return true
			}
		}
	}
	for _, c := range dominance.EnclosingIfConds(use, parents, body) {
		for _, d := range conjuncts(c) {
			if upperBound(pass, d, obj, srcBytes, false) {
				return true
			}
		}
	}
	return false
}

func disjuncts(e ast.Expr) []ast.Expr { return split(e, token.LOR) }
func conjuncts(e ast.Expr) []ast.Expr { return split(e, token.LAND) }

func split(e ast.Expr, op token.Token) []ast.Expr {
	e = ast.Unparen(e)
	if b, ok := e.(*ast.BinaryExpr); ok && b.Op == op {
		return append(split(b.X, op), split(b.Y, op)...)
	}
	return []ast.Expr{e}
}

// upperBound reports whether e compares obj.BitLen() (or len(src)) from
// above. refusal selects the polarity: true for a condition whose
// branch refuses (size > K), false for one whose branch proceeds
// (size <= K).
func upperBound(pass *analysis.Pass, e ast.Expr, obj, srcBytes types.Object, refusal bool) bool {
	b, ok := ast.Unparen(e).(*ast.BinaryExpr)
	if !ok {
		return false
	}
	op := b.Op
	if !isSize(pass, b.X, obj, srcBytes) {
		op = astx.FlipComparison(op)
		if !isSize(pass, b.Y, obj, srcBytes) {
			return false
		}
	}
	if refusal {
		return op == token.GTR || op == token.GEQ
	}
	return op == token.LSS || op == token.LEQ
}

// isSize reports whether e is obj.BitLen() or len(srcBytes).
func isSize(pass *analysis.Pass, e ast.Expr, obj, srcBytes types.Object) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return false
	}
	switch f := ast.Unparen(call.Fun).(type) {
	case *ast.SelectorExpr:
		if f.Sel.Name != "BitLen" || len(call.Args) != 0 {
			return false
		}
		id, ok := ast.Unparen(f.X).(*ast.Ident)
		return ok && pass.TypesInfo.ObjectOf(id) == obj
	case *ast.Ident:
		if f.Name != "len" || len(call.Args) != 1 || srcBytes == nil {
			return false
		}
		if _, isBuiltin := pass.TypesInfo.Uses[f].(*types.Builtin); !isBuiltin {
			return false
		}
		id, ok := ast.Unparen(call.Args[0]).(*ast.Ident)
		return ok && pass.TypesInfo.ObjectOf(id) == srcBytes
	}
	return false
}

// callsBounder reports whether the if's Init or Cond passes obj to a
// same-package function that bounds a parameter's BitLen from above.
func callsBounder(pass *analysis.Pass, iff *ast.IfStmt, obj types.Object, bounders map[*types.Func]bool) bool {
	found := false
	check := func(n ast.Node) {
		if n == nil {
			return
		}
		ast.Inspect(n, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return !found
			}
			fn, ok := astx.CalleeFunc(pass, ast.Unparen(call.Fun))
			if !ok || !bounders[fn] {
				return true
			}
			for _, a := range call.Args {
				if id, ok := ast.Unparen(a).(*ast.Ident); ok && pass.TypesInfo.ObjectOf(id) == obj {
					found = true
				}
			}
			return !found
		})
	}
	check(iff.Init)
	check(iff.Cond)
	return found
}

// boundingFuncs finds the package's functions whose body compares a
// parameter's BitLen() from above in a refusing if.
func boundingFuncs(pass *analysis.Pass) map[*types.Func]bool {
	out := map[*types.Func]bool{}
	for _, decl := range pathflow.FuncsWithBodies(pass) {
		fn, _ := pass.TypesInfo.Defs[decl.Name].(*types.Func)
		if fn == nil {
			continue
		}
		params := pathflow.ParamObjects(pass, decl)
		ast.Inspect(decl.Body, func(n ast.Node) bool {
			iff, ok := n.(*ast.IfStmt)
			if !ok || !dominance.Diverges(iff.Body) {
				return true
			}
			for _, d := range disjuncts(iff.Cond) {
				for p := range params {
					if upperBound(pass, d, p, nil, true) {
						out[fn] = true
					}
				}
			}
			return true
		})
	}
	return out
}

// checkX509 handles `pk, ok := parsed.(*rsa.PublicKey)` where parsed is
// an x509.ParsePKIXPublicKey result.
func checkX509(pass *analysis.Pass, body *ast.BlockStmt, bound map[types.Object]ast.Expr, ta *ast.TypeAssertExpr) {
	if ta.Type == nil || !isRSAPublicKey(pass.TypesInfo.TypeOf(ta.Type)) {
		return
	}
	src := ast.Unparen(ta.X)
	if id, ok := src.(*ast.Ident); ok {
		if b, ok := bound[pass.TypesInfo.ObjectOf(id)]; ok {
			src = ast.Unparen(b)
		}
	}
	call, ok := src.(*ast.CallExpr)
	if !ok || pathflow.QualifiedFunc(pass, call.Fun) != "crypto/x509.ParsePKIXPublicKey" {
		return
	}
	parents := dominance.Parents(body)
	checkX509Result(pass, body, parents, ta)
}

// checkX509Result reports the parsed key expression e when the
// function never bounds the key it lands in from above.
func checkX509Result(pass *analysis.Pass, body *ast.BlockStmt, parents map[ast.Node]ast.Node, e ast.Expr) {
	// The key lands in the first left-hand side of the assignment
	// holding e.
	as, ok := parents[e].(*ast.AssignStmt)
	if !ok || len(as.Lhs) == 0 {
		return
	}
	id, ok := as.Lhs[0].(*ast.Ident)
	if !ok || id.Name == "_" {
		return
	}
	key := pass.TypesInfo.ObjectOf(id)
	if key == nil || keyBounded(pass, body, key) {
		return
	}
	pass.Reportf(e.Pos(),
		"rsakeysize: the RSA key %s is parsed from input with no upper bound on its size: x509 caps nothing, and verification cost grows with the square of the modulus; refuse %s.N.BitLen() > 8192 before using it",
		id.Name, id.Name)
}

// keyBounded reports whether body refuses key.N.BitLen() or key.Size()
// from above anywhere: an if whose body leaves and whose condition (or
// a || disjunct of it) is size > K or size >= K.
func keyBounded(pass *analysis.Pass, body *ast.BlockStmt, key types.Object) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		iff, ok := n.(*ast.IfStmt)
		if !ok || found || !dominance.Diverges(iff.Body) {
			return !found
		}
		for _, d := range disjuncts(iff.Cond) {
			b, ok := ast.Unparen(d).(*ast.BinaryExpr)
			if !ok {
				continue
			}
			switch {
			case keySize(pass, b.X, key) && (b.Op == token.GTR || b.Op == token.GEQ):
				found = true
			case keySize(pass, b.Y, key) && (b.Op == token.LSS || b.Op == token.LEQ):
				found = true
			}
		}
		return !found
	})
	return found
}

// keySize reports whether e is key.N.BitLen() or key.Size().
func keySize(pass *analysis.Pass, e ast.Expr, key types.Object) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	recv := ast.Unparen(sel.X)
	switch sel.Sel.Name {
	case "Size":
	case "BitLen":
		n, ok := recv.(*ast.SelectorExpr)
		if !ok || n.Sel.Name != "N" {
			return false
		}
		recv = ast.Unparen(n.X)
	default:
		return false
	}
	id, ok := recv.(*ast.Ident)
	return ok && pass.TypesInfo.ObjectOf(id) == key
}
