// Package testgap reports validator enumeration arms that no fixture
// in the package ever exercises.
//
// Defect class: allow/deny enumerations whose arms drift out of sync
// with the tests that pin them. A table test that covers two of four
// arms stays green forever; the uncovered arm is where bypasses live.
// Real instance: core/upload serve.go scriptableExt, the stored-XSS
// extension guard, enumerated {html, htm, xhtml, svg} while no literal
// in the package's tests ever exercised the xhtml arm — and the XML
// family was absent from the guard entirely until the pass-4 RED
// serve_security_test.go pinned the XML+XSLT two-file chain (commit
// 1fe13ef9). Arm-level fixture gaps in security enumerations are the
// hole this analyzer stands over.
//
// Lane: test-quality shape. Deciding coverage needs the package's
// whole test-file literal corpus next to its production files —
// whole-package, cross-file state that single-site pattern rules
// cannot see. Loading mode: the corpus is read from the *_test.go
// files found ON DISK next to the package's files rather than from
// pass.Files, because a driver that loads tests (x/tools checker flag
// -test, on by default in multichecker; analysistest always loads
// with Tests:true) analyzes several variants of one package, and the
// production-only variant's pass.Files carries no test files: a
// pass.Files corpus would emit different messages per variant for the
// same site. The on-disk corpus makes every variant compute identical
// diagnostics. Disk presence also means external-package tests
// (foo_test) and build-tag-excluded test files count as fixtures,
// deterministically on every platform. Validators are collected only
// from the non-test files of the pass; a package with no test files
// on disk has an empty corpus, so every arm is uncovered — the
// loudest genuine gap, not a clearance.
//
// Prototype limitation: because the corpus is globbed from disk,
// test files that exist only as overlays/synthesized sources (never
// written to the package directory) are invisible — analysistest
// results are fixture-local until this is revisited.
//
// What counts as a validator (heuristics, exactly as implemented):
//   - a function whose name contains valid/allow/deny/scriptab/forbid/
//     accept/reject (case-insensitive), or
//   - a function that returns a bool literal from inside a switch over
//     a string-tagged expression, or
//   - a var whose name contains allow/deny/ext(s)/head(s)/prefix/
//     scheme/type(s) (case-insensitive) initialized to a []string
//     composite whose elements are ALL string literals.
//
// Enumerated values are the string literals in the case lists of every
// string-tagged switch in the validator's body (function shapes), or
// the slice elements (var shape). A value is covered when it occurs as
// a case-insensitive substring of ANY string literal in ANY of the
// package's *_test.go files. One diagnostic per validator, at most 6
// uncovered values listed, in source order.
//
// Sanctioned postures that stay silent:
//   - substring coverage: testing "evil.html" exercises the html arm,
//     and testing "xhtml" also exercises the html arm — the corpus is
//     searched, not equality-matched.
//   - behavior dispatch: switches in functions with no validator name
//     that return non-bool values (channel routing, mode dispatch) are
//     not enumerations of a security posture.
//   - prefix-chain guards (strings.HasPrefix chains), tagless-switch
//     scheme guards, and map[string]bool deny-lists expose no
//     string-switch arms in the shapes enumerated here, so they stay
//     silent (cf. scriptableHead, isDangerousURLScheme,
//     dangerousExecExts).
//   - slices with non-literal elements are skipped whole: a partial
//     enumeration would misreport the denominator.
//   - functions defined inside test files are fixtures, not surface:
//     they are never treated as validators.
package testgap

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const Doc = "report validator enumeration arms that never appear in the package's test literals"

var Analyzer = &analysis.Analyzer{
	Name: "testgap",
	Doc:  Doc,
	Run:  run,
}

var (
	validatorName = regexp.MustCompile(`(?i)(valid|allow|deny|scriptab|forbid|accept|reject)`)
	sliceName     = regexp.MustCompile(`(?i)(allow|deny|exts?|heads?|prefix|scheme|types?)`)
)

// maxShown caps the values listed in one diagnostic; the rest are
// summarised as (+N more).
const maxShown = 6

func run(pass *analysis.Pass) (any, error) {
	var prod []*ast.File
	dirs := map[string]bool{}
	for _, f := range pass.Files {
		filename := pass.Fset.Position(f.Pos()).Filename
		if strings.HasSuffix(filename, "_test.go") {
			continue // test files are corpus, never validators
		}
		prod = append(prod, f)
		dirs[filepath.Dir(filename)] = true
	}
	if len(prod) == 0 {
		return nil, nil
	}
	corpus := corpusForDirs(dirs)
	for _, f := range prod {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if vals, ok := validatorFunc(pass, d); ok {
					report(pass, d.Pos(), d.Name.Name, vals, corpus)
				}
			case *ast.GenDecl:
				for _, v := range varValidators(pass, d) {
					report(pass, v.spec.Pos(), v.name, v.vals, corpus)
				}
			}
		}
	}
	return nil, nil
}

// corpusForDirs lowercases and joins every string literal from every
// *_test.go file on disk in the package's directory(ies) into one
// searchable blob. Newline separators keep adjacent values from gluing
// into false matches. Files that fail to parse are skipped.
func corpusForDirs(dirs map[string]bool) string {
	var sb strings.Builder
	for _, dir := range slices.Sorted(maps.Keys(dirs)) {
		testFiles, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
		if err != nil {
			continue
		}
		fset := token.NewFileSet()
		for _, path := range testFiles {
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				continue
			}
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				if s, err := strconv.Unquote(lit.Value); err == nil {
					sb.WriteString(strings.ToLower(s))
				} else {
					sb.WriteString(strings.ToLower(lit.Value))
				}
				sb.WriteByte('\n')
				return true
			})
		}
	}
	return sb.String()
}

// validatorFunc reports the ordered, deduplicated string-literal case
// values of every string-tagged switch in fd's body, and whether fd is
// a validator: name-matched, or returning a bool literal from inside
// such a switch.
func validatorFunc(pass *analysis.Pass, fd *ast.FuncDecl) ([]string, bool) {
	if fd.Body == nil {
		return nil, false
	}
	var (
		vals    []string
		seen    = map[string]bool{}
		boolRet bool
	)
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok {
			return true
		}
		if sw.Tag == nil || !isStringType(pass.TypesInfo.TypeOf(sw.Tag)) {
			return true
		}
		for _, stmt := range sw.Body.List {
			cc, ok := stmt.(*ast.CaseClause)
			if !ok {
				continue
			}
			for _, e := range cc.List {
				if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if s, err := strconv.Unquote(lit.Value); err == nil && s != "" && !seen[s] {
						seen[s] = true
						vals = append(vals, s)
					}
				}
			}
			// A bool literal returned from inside a case body marks
			// the switch as an enumeration decision.
			ast.Inspect(cc, func(n ast.Node) bool {
				if r, ok := n.(*ast.ReturnStmt); ok && len(r.Results) == 1 {
					if id, ok := r.Results[0].(*ast.Ident); ok && (id.Name == "true" || id.Name == "false") {
						boolRet = true
					}
				}
				return true
			})
		}
		return true
	})
	if !validatorName.MatchString(fd.Name.Name) && !boolRet {
		return nil, false
	}
	return vals, true
}

// varValidator is one []string-literal var whose name matches the
// slice-name filter.
type varValidator struct {
	spec *ast.ValueSpec
	name string
	vals []string
}

// varValidators collects the var-declared enumeration slices from gd.
// A composite with any non-literal element is skipped whole.
func varValidators(pass *analysis.Pass, gd *ast.GenDecl) []varValidator {
	if gd.Tok != token.VAR {
		return nil
	}
	var out []varValidator
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
			continue
		}
		name := vs.Names[0].Name
		if !sliceName.MatchString(name) {
			continue
		}
		cl, ok := vs.Values[0].(*ast.CompositeLit)
		if !ok || len(cl.Elts) == 0 {
			continue
		}
		slice, ok := pass.TypesInfo.TypeOf(cl).(*types.Slice)
		if !ok || !isStringType(slice.Elem()) {
			continue
		}
		vals := make([]string, 0, len(cl.Elts))
		for _, e := range cl.Elts {
			lit, ok := e.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				vals = nil
				break
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil || s == "" {
				vals = nil
				break
			}
			vals = append(vals, s)
		}
		if vals == nil {
			continue
		}
		out = append(out, varValidator{spec: vs, name: name, vals: vals})
	}
	return out
}

// report emits the single diagnostic for a validator whose enumerated
// values never occur in the corpus.
func report(pass *analysis.Pass, pos token.Pos, name string, vals []string, corpus string) {
	if len(vals) == 0 {
		return
	}
	var missing []string
	for _, v := range vals {
		if !strings.Contains(corpus, strings.ToLower(v)) {
			missing = append(missing, v)
		}
	}
	if len(missing) == 0 {
		return
	}
	shown, extra := missing, 0
	if len(shown) > maxShown {
		shown, extra = shown[:maxShown], len(shown)-maxShown
	}
	msg := fmt.Sprintf("testgap: %s: %d of %d enumerated values never appear in this package's tests: %s",
		name, len(missing), len(vals), strings.Join(shown, ", "))
	if extra > 0 {
		msg += fmt.Sprintf(" (+%d more)", extra)
	}
	pass.Report(analysis.Diagnostic{Pos: pos, Message: msg})
}

// isStringType reports whether t's underlying type is a string basic
// type, including named string types.
func isStringType(t types.Type) bool {
	if t == nil {
		return false
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}
