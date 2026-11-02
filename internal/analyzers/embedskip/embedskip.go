// Package embedskip catches a struct field walk that maps fields to keys
// by their tags and never looks at an embedded struct. encoding/json
// promotes an untagged embedded struct's fields to the outer object, so a
// NumField loop that reads only the top level misses every promoted field
// the JSON form carries.
//
// The bug class: the 2026-10-26 bug bash fixed it in framework/
// typed_hooks.go mergeStructIntoMap (a typed hook's change to a field
// promoted from an embedded struct never reached the write), and the
// analyzer's first run found the same walk in core-ui/di Inject, the
// app's inject-tag count, and core/handler's query, path and header
// binders. The fix descends: an f.Anonymous field recurses into its type
// (a nil embedded pointer is skipped or allocated, as the caller needs).
//
// The loop fires when it is a for or range over X.NumField() (in a for
// condition, on either side: `t.NumField() > i` was a miss, ledger I2
// of the 2026-10-27 bug bash), or a range
// over the X.Fields() iterator (X a reflect.Type or reflect.Value), whose
// body reads a StructField's Tag or
// passes a StructField to a same-package function (a jsonFieldName-style
// helper), and stays silent when:
//   - the body reads .Anonymous — the fix posture;
//   - the body names reflect.Struct — a walk that descends into every
//     struct-kind field reaches embedded ones too;
//   - the body reads no tag and passes no StructField on: it counts or
//     copies fields, it does not map them to keys;
//   - the file is a _test.go file.
//
// A walk that is top-level-only on purpose carries
// //gofastr:allow(embedskip) <why>.
//
// The 2026-11 round added three arms (arms.go), each from a reflect
// tag-walker bug in core/handler or internal/jsonexact, each scoped to
// the walks this detector already recognizes:
//   - pointer gate (8708ddd5): in a function that reaches a tag-keyed
//     walk (itself or through same-package calls: Bind → bindQuery →
//     taggedFields), a `E.Kind() != reflect.Struct` (or ==) where E is one
//     Elem() below reflect.ValueOf/TypeOf of an interface parameter, with
//     no pointer-kind test on E after that Elem. A Handler[*T, O] input
//     binds into a **T, so the pointee is itself a pointer and every tag
//     was skipped. Fix: follow the pointer levels first (derefType).
//     Silent: a gate whose branch returns an errors.New / fmt.Errorf
//     refusal (core/config Load, core-ui/di Inject refuse a non-struct
//     out loud; the bug is the silent skip), a second Elem or a deref
//     helper before the gate, a gate on a typed parameter's Elem (a slice
//     element type), a function that reaches no tag walk;
//   - text binder (4cd4d974): in a package with a tag-keyed walk, a
//     function taking a string that switches on V.Kind() with a SetString
//     arm and a SetInt/SetUint/SetFloat/SetBool arm, when neither it, a
//     function it calls, nor a direct caller names
//     encoding.TextUnmarshaler or UnmarshalText (a package variable
//     initialized from reflect.TypeFor[encoding.TextUnmarshaler]() counts).
//     time.Time, UUID and slog.Level fields failed, and a validated enum
//     bound raw. Fix: call UnmarshalText first when the field's address
//     implements it. Silent in a package with no tag-keyed walk (a kind
//     switch over config strings is not a tag binder);
//   - pointer cycle (cdf2cb1b): in a function whose field walk keys by
//     tag or descends on Anonymous (the posture this analyzer pushes
//     toward, and the one that can loop), a self-call passing V.Elem()
//     under a pointer-kind case or if on V, with no visited set. Fix: a
//     map[uintptr]bool on v.Pointer(). Silent: a parameter or receiver
//     that is (or holds) a map, an integer parameter the body compares (a
//     depth bound), and walks that neither key by tag nor read Anonymous
//     (deep-equal style test helpers over plain config).
package embedskip

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "embedskip",
	Doc:  "forbids a tag-keyed struct field walk that skips fields promoted from an embedded struct; descend on f.Anonymous",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			var bound ast.Expr
			var body *ast.BlockStmt
			switch s := n.(type) {
			case *ast.ForStmt:
				if b, ok := s.Cond.(*ast.BinaryExpr); ok {
					// The bound may sit on either side: i < t.NumField()
					// and t.NumField() > i are the same walk.
					bound = b.Y
					if isNumFieldCall(pass, b.X) {
						bound = b.X
					}
				}
				body = s.Body
			case *ast.RangeStmt:
				bound, body = s.X, s.Body
			default:
				return true
			}
			if body == nil || !isNumFieldCall(pass, bound) {
				return true
			}
			if keyedByTag(pass, body) && !descends(pass, body) {
				pass.Reportf(n.Pos(),
					"struct field walk keyed by tag skips fields promoted from an embedded struct: descend when f.Anonymous, as encoding/json does")
			}
			return true
		})
	}
	runArms(pass)
	return nil, nil
}

// isNumFieldCall reports whether e is X.NumField() or the X.Fields()
// iterator on a reflect.Type or reflect.Value: both walk the direct
// fields only (reflect.VisibleFields is the walk that includes promoted
// ones).
func isNumFieldCall(pass *analysis.Pass, e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != "NumField" && sel.Sel.Name != "Fields") {
		return false
	}
	return isReflect(pass.TypesInfo.TypeOf(sel.X), "Type", "Value")
}

// keyedByTag reports whether the loop body reads a StructField's Tag or
// hands a StructField to a function of this package.
func keyedByTag(pass *analysis.Pass, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if x.Sel.Name == "Tag" && isReflect(pass.TypesInfo.TypeOf(x.X), "StructField") {
				found = true
			}
		case *ast.CallExpr:
			id := calleeIdent(x.Fun)
			if id == nil {
				return true
			}
			fn, ok := pass.TypesInfo.Uses[id].(*types.Func)
			if !ok || fn.Pkg() != pass.Pkg {
				return true
			}
			for _, a := range x.Args {
				if isReflect(pass.TypesInfo.TypeOf(a), "StructField") {
					found = true
				}
			}
		}
		return true
	})
	return found
}

// descends reports whether the loop body reads .Anonymous on a
// StructField or names reflect.Struct.
func descends(pass *analysis.Pass, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if found || !ok {
			return !found
		}
		switch sel.Sel.Name {
		case "Anonymous":
			if isReflect(pass.TypesInfo.TypeOf(sel.X), "StructField") {
				found = true
			}
		case "Struct":
			if c, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Const); ok && c.Pkg() != nil && c.Pkg().Path() == "reflect" {
				found = true
			}
		}
		return true
	})
	return found
}

func calleeIdent(fun ast.Expr) *ast.Ident {
	switch f := fun.(type) {
	case *ast.Ident:
		return f
	case *ast.SelectorExpr:
		return f.Sel
	}
	return nil
}

// isReflect reports whether t is reflect.<one of names>.
func isReflect(t types.Type, names ...string) bool {
	if t == nil {
		return false
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != "reflect" {
		return false
	}
	for _, n := range names {
		if obj.Name() == n {
			return true
		}
	}
	return false
}
