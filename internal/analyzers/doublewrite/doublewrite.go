// Package doublewrite catches a deferred response write that also runs
// after the function already answered on another path.
//
// The bug: a handler arms `defer func() { ...Encode(w)... }()` to send a
// uniform answer at every exit, and a later early return writes its own
// answer first. Both reach the client in one response: net/http drops
// the second status as superfluous and appends the second body to the
// first. Found by the 2026-11 bug bash:
//   - 84e4d4b1: battery/auth forgotHandler armed its uniform
//     {"sent":true} write, then ran emailWithinLimit, which answers 400
//     for an over-long address; the client read a 400 body followed by
//     {"sent":true}.
//
// The fix spelling is the one 84e4d4b1 took: run every check that can
// answer on its own before the defer is armed. Where an early answer
// after it is unavoidable, guard the deferred write with a flag the
// early path sets (`answered = true` then `if answered { return }` in
// the literal).
//
// Shape, within one function body: a `defer` of a function literal that
// writes the response at its top level (a WriteHeader or Write on a
// ResponseWriter, http.Error and friends, fmt.Fprint*/io.WriteString
// into one, json.NewEncoder(w).Encode, or a respond helper that writes
// its ResponseWriter parameter, computed to a fixed point and exported
// as a writesResponse fact), and, lexically after the defer statement
// and outside any nested function literal, another write to the same
// writer variable. Reported at the later write.
//
// Silent postures, deliberately:
//   - a deferred write under an if in the literal (a wrote/handled/
//     answered flag, an err check): the literal decided when it answers;
//   - a write before the defer statement: the function returned before
//     arming it, or the defer is the second write of a path the rule
//     does not follow;
//   - a write to a different writer (a recorder, a buffer) than the one
//     the defer writes;
//   - a write inside a nested function literal after the defer (a
//     callback that may never run on this path);
//   - a deferred call that is not a literal (`defer w.WriteHeader(...)`,
//     `defer finish(w)`): the rule reads literals only;
//   - _test.go files.
package doublewrite

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/rwcall"
)

var Analyzer = &analysis.Analyzer{
	Name:      "doublewrite",
	Doc:       "report a deferred response write that also runs after an earlier path already wrote the response",
	Run:       run,
	FactTypes: []analysis.Fact{(*writesResponse)(nil)},
}

// writesResponse marks a function that writes the ResponseWriter
// parameters at these argument indexes (a respond helper).
type writesResponse struct{ Index []int }

func (*writesResponse) AFact() {}

func (f *writesResponse) String() string { return fmt.Sprintf("writesResponse%v", f.Index) }

func run(pass *analysis.Pass) (any, error) {
	imported := func(fn *types.Func) []int {
		var f writesResponse
		if fn.Pkg() != pass.Pkg && pass.ImportObjectFact(fn, &f) {
			return f.Index
		}
		return nil
	}
	local := rwcall.Writers(pass, imported)
	for _, fn := range slices.SortedFunc(maps.Keys(local), func(a, b *types.Func) int { return int(a.Pos() - b.Pos()) }) {
		pass.ExportObjectFact(fn, &writesResponse{Index: local[fn]})
	}
	helpers := func(fn *types.Func) []int {
		if idx, ok := local[fn]; ok {
			return idx
		}
		return imported(fn)
	}
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		for _, fn := range astx.AllFuncs(f) {
			check(pass, astx.BodyOf(fn), helpers)
		}
	}
	return nil, nil
}

// check looks at one function body (nested literals are their own).
func check(pass *analysis.Pass, body *ast.BlockStmt, helpers rwcall.HelperIndex) {
	if body == nil {
		return
	}
	type armed struct {
		at     token.Pos
		writer types.Object
	}
	var defers []armed
	var writes []*ast.CallExpr
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.DeferStmt:
			if lit, ok := ast.Unparen(n.Call.Fun).(*ast.FuncLit); ok {
				for _, obj := range topLevelWriters(pass, lit.Body, helpers) {
					defers = append(defers, armed{n.End(), obj})
				}
			}
			return false
		case *ast.CallExpr:
			if rwcall.Target(pass, n, helpers) != nil {
				writes = append(writes, n)
			}
		}
		return true
	})
	reported := map[token.Pos]bool{}
	for _, d := range defers {
		for _, w := range writes {
			if w.Pos() < d.at || reported[w.Pos()] || writerObj(pass, rwcall.Target(pass, w, helpers)) != d.writer {
				continue
			}
			reported[w.Pos()] = true
			pass.Reportf(w.Pos(),
				"doublewrite: this response write runs after a deferred write was armed, and the deferred write still runs on this path; the client gets both bodies in one response (answer before arming the defer, or have the defer skip on a flag this path sets)")
		}
	}
}

// topLevelWriters: the writer variables a deferred literal writes
// unconditionally: in its own top-level statements, before the first
// statement that branches (an if, switch, loop, select or return: past
// `if answered { return }` every write is conditional), and not inside
// a nested literal.
func topLevelWriters(pass *analysis.Pass, body *ast.BlockStmt, helpers rwcall.HelperIndex) []types.Object {
	var out []types.Object
	for _, st := range body.List {
		switch st.(type) {
		case *ast.IfStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SelectStmt, *ast.ReturnStmt, *ast.BlockStmt, *ast.LabeledStmt, *ast.BranchStmt:
			return out
		}
		ast.Inspect(st, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncLit, *ast.BlockStmt, *ast.IfStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SelectStmt:
				return false
			case *ast.CallExpr:
				if obj := writerObj(pass, rwcall.Target(pass, n, helpers)); obj != nil && !slices.Contains(out, obj) {
					out = append(out, obj)
				}
			}
			return true
		})
	}
	return out
}

// writerObj: the variable a writer expression names, nil for anything
// else.
func writerObj(pass *analysis.Pass, e ast.Expr) types.Object {
	if e == nil {
		return nil
	}
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return nil
	}
	return pass.TypesInfo.Uses[id]
}
