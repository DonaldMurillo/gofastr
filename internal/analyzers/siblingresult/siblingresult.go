// Package siblingresult catches a call site that blanks a result its
// function marks must-use: the flag one path dropped is the flag the
// other paths exist to honour.
//
// The bug: battery/auth's MintSession returns (sess, pending, err), and
// pending means "the account still owes its second factor". The password
// path read it and sent the browser to the challenge; the magic-link
// verify handler (b508cc0d) and the OAuth callback (b9b236eb) wrote
// `sess, _, err := MintSession(...)` and redirected to a landing page
// that refuses a pending session, so a 2FA account signing in by link or
// by provider dead-ended. Nothing about either call site looked wrong in
// isolation; next to the other callers it plainly was.
//
// The rule: a function whose doc comment carries
//
//	//gofastr:mustuse <result>
//
// (the result's declared name, or its index; several may be listed)
// fires on every call site, in any package of the module, that binds
// the call's results and blanks a marked one. The mark travels to other
// packages as a fact. A marker naming no result of the function is
// itself reported, so a renamed result cannot silently retire it.
// MintSession's pending carries the mark.
//
// The statistical version was written first and measured: fire on a
// blanked non-error result of a module-local function with at least
// three call sites, two of them binding and reading that result. Over
// ./... on 2026-11-02 it found 333 sites. MintSession's twofa step-up
// was one; nearly all the rest were callers picking the facets of a
// multi-result helper they need (SafeIdent's cleaned name, a resolver's
// second return, PathLocale's tag), which is what multiple results are
// for. A blanked result is a bug only where the result is a contract,
// and the contract is what the marker writes down.
//
// Silent postures, deliberately:
//   - `_ = f()`, `_, _, _ = f()` and a bare call statement: the whole
//     result discarded is a call made for its effect, not a result
//     picked over;
//   - a call whose results are forwarded (`return f()`) or passed on as
//     arguments: nothing is blanked;
//   - unmarked results and unmarked functions;
//   - _test.go files.
//
// The fix spelling is to read the result (`sess, pending, err :=` and a
// branch on pending). A site that blanks it on purpose (the 2FA step-up,
// which marks the fresh session cleared on the next line) says why with
// //gofastr:allow(siblingresult) <why>.
package siblingresult

import (
	"fmt"
	"go/ast"
	"go/types"
	"maps"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

var Analyzer = &analysis.Analyzer{
	Name:      "siblingresult",
	Doc:       "report a call site that blanks a result its function's doc marks //gofastr:mustuse",
	Run:       run,
	FactTypes: []analysis.Fact{(*mustUse)(nil)},
}

// mustUse lists the result indexes a function's doc marks must-use.
type mustUse struct{ Index []int }

func (*mustUse) AFact() {}

func (f *mustUse) String() string { return fmt.Sprintf("mustUse%v", f.Index) }

const directive = "//gofastr:mustuse"

func run(pass *analysis.Pass) (any, error) {
	marks := collectMarks(pass)
	for _, fn := range slices.SortedFunc(maps.Keys(marks), func(a, b *types.Func) int { return int(a.Pos() - b.Pos()) }) {
		pass.ExportObjectFact(fn, &mustUse{Index: marks[fn]})
	}
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			var lhs, rhs []ast.Expr
			switch st := n.(type) {
			case *ast.AssignStmt:
				lhs, rhs = st.Lhs, st.Rhs
			case *ast.ValueSpec:
				for _, id := range st.Names {
					lhs = append(lhs, id)
				}
				rhs = st.Values
			default:
				return true
			}
			if len(rhs) != 1 {
				return true
			}
			call, ok := ast.Unparen(rhs[0]).(*ast.CallExpr)
			if !ok {
				return true
			}
			fn := typeutil.StaticCallee(pass.TypesInfo, call)
			if fn == nil {
				return true
			}
			fn = fn.Origin()
			res := fn.Signature().Results()
			if res.Len() != len(lhs) || allBlank(lhs) {
				return true
			}
			idx := marks[fn]
			if fn.Pkg() != pass.Pkg {
				var fact mustUse
				if pass.ImportObjectFact(fn, &fact) {
					idx = fact.Index
				}
			}
			for _, i := range idx {
				if isBlank(lhs[i]) {
					pass.Reportf(call.Pos(), "%s's result %s is marked %s and this call blanks it: read it and act on it, the way the function's other callers do",
						fn.Name(), resultName(fn, i), directive)
				}
			}
			return true
		})
	}
	return nil, nil
}

func isBlank(e ast.Expr) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	return ok && id.Name == "_"
}

func allBlank(lhs []ast.Expr) bool {
	for _, l := range lhs {
		if !isBlank(l) {
			return false
		}
	}
	return true
}

// collectMarks reads the //gofastr:mustuse lines in this package's
// function doc comments, reporting a marker that names no result.
func collectMarks(pass *analysis.Pass) map[*types.Func][]int {
	out := map[*types.Func][]int{}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Doc == nil {
				continue
			}
			fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
			if !ok {
				continue
			}
			res := fn.Signature().Results()
			for _, c := range fd.Doc.List {
				rest, ok := strings.CutPrefix(c.Text, directive)
				if !ok || (rest != "" && rest[0] != ' ' && rest[0] != '\t') {
					continue
				}
				words := strings.Fields(rest)
				if len(words) == 0 {
					pass.Reportf(fd.Name.Pos(), "%s names no result of %s", directive, fn.Name())
				}
				for _, word := range words {
					idx := resultIndex(res, word)
					if idx < 0 {
						pass.Reportf(fd.Name.Pos(), "%s names %q, which is not a result of %s", directive, word, fn.Name())
						continue
					}
					if !slices.Contains(out[fn], idx) {
						out[fn] = append(out[fn], idx)
					}
				}
			}
		}
	}
	return out
}

// resultIndex resolves a marker word to a result index: the declared
// name, or a decimal index in range.
func resultIndex(res *types.Tuple, word string) int {
	for i := range res.Len() {
		if res.At(i).Name() == word {
			return i
		}
	}
	if n, err := strconv.Atoi(word); err == nil && n >= 0 && n < res.Len() {
		return n
	}
	return -1
}

// resultName is the result's declared name, or its position.
func resultName(fn *types.Func, i int) string {
	if n := fn.Signature().Results().At(i).Name(); n != "" && n != "_" {
		return n
	}
	return "#" + strconv.Itoa(i)
}
