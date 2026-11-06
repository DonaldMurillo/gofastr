package suite

import (
	"github.com/DonaldMurillo/gofastr/internal/analyzers/allow"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestEveryAnalyzerIsWiredOrExplained closes the hole that let four
// finished analyzers (reqparamlimit, fmtformat, testgap, discardmutator)
// sit in internal/analyzers with fixture tests and no line in the registry:
// they passed their own tests forever while `make analyze`, the
// pre-commit hook, and CI's vet step never ran them over the repo, and
// nothing said why. A package here is either registered in
// suite.go or named in suite.go's prose with the measurement
// that justifies holding it back. Silence is the one thing it can't be.
//
//gofastr:test-reads internal/analyzers/
func TestEveryAnalyzerIsWiredOrExplained(t *testing.T) {
	const dir = ".."
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	src, err := os.ReadFile("suite.go")
	if err != nil {
		t.Fatalf("read suite.go: %v", err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "suite.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse suite.go: %v", err)
	}

	imported := map[string]bool{}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		if rest, ok := strings.CutPrefix(path, "github.com/DonaldMurillo/gofastr/internal/analyzers/"); ok {
			imported[rest] = true
		}
	}

	// Selectors reaching the registration list: pkg.Var for every
	// analyzer variable registered, so a package that exports several
	// analyzers (hygiene's two, unseated's three) is checked per
	// analyzer, not per package. An import that never lands here would
	// not compile, but reading the call is what makes the assertion
	// about REGISTRATION rather than about an import line.
	registered := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		// The repoAnalyzers composite literal is the registration list;
		// Analyzers hands it to both vettools.
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "repoAnalyzers" || len(vs.Values) != 1 {
			return true
		}
		lit, ok := vs.Values[0].(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, arg := range lit.Elts {
			// allow.Guard(pkg.Analyzer) is the registered spelling since
			// the marker filter landed; the bare selector still counts.
			if wrap, ok := arg.(*ast.CallExpr); ok && len(wrap.Args) == 1 {
				if fn, ok := wrap.Fun.(*ast.SelectorExpr); ok && fn.Sel.Name == "Guard" {
					arg = wrap.Args[0]
				}
			}
			sel, ok := arg.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			if pkg, ok := sel.X.(*ast.Ident); ok {
				registered[pkg.Name+"."+sel.Sel.Name] = true
			}
		}
		return false
	})

	// Prose that holds an analyzer back. Comments only: a bare mention
	// anywhere in the file would let an unused import pass as an
	// explanation. And only the held-back entry shape, a line that
	// OPENS with the analyzer's name and an em dash (`fmtformat — 4
	// findings, all false positives.`): a substring match let any
	// mention explain an analyzer away, so deleting rawtext's
	// registration passed on the strength of the comment above it
	// ("Arms and silences are in rawtext's doc comment").
	var comments strings.Builder
	for _, group := range file.Comments {
		comments.WriteString(group.Text())
	}
	explained := comments.String()

	declared := 0
	declaredVars := map[string]map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pkg := e.Name()
		for _, a := range declaredAnalyzers(t, filepath.Join(dir, pkg)) {
			declared++
			if declaredVars[pkg] == nil {
				declaredVars[pkg] = map[string]bool{}
			}
			declaredVars[pkg][a.varName] = true
			switch {
			case registered[pkg+"."+a.varName]:
				if !imported[pkg] {
					t.Errorf("%s.%s: registered under a qualifier that is not an internal/analyzers import", pkg, a.varName)
				}
			case heldBack(explained, a.name):
				// Held back on purpose, with the reason in suite.go.
			default:
				t.Errorf("internal/analyzers/%s.%s (%s) is neither registered in suite.go "+
					"nor held back by a `%s — <measurement>` comment there: it never runs "+
					"over the repo and nothing records why", pkg, a.varName, a.name, a.name)
			}
		}
	}
	// Anti-vacuity, package by package: comparing totals only let the
	// explained-but-unregistered packages (fmtformat, testgap) pad the
	// count while the walk skipped a registered package outright.
	// Every registered pkg.Var must be a declaration the walk saw.
	for ref := range registered {
		pkg, varName, ok := strings.Cut(ref, ".")
		if !ok || !declaredVars[pkg][varName] {
			t.Errorf("registered %s has no declaration the gate's walk can see under internal/analyzers/%s: the walk is broken for that package", ref, pkg)
		}
	}
	// Anti-vacuity on the totals: a walk that found nothing at all
	// proves nothing.
	if declared < len(repoAnalyzers) {
		t.Fatalf("found %d declared analyzers under %s, fewer than the %d registered: the walk is broken", declared, dir, len(repoAnalyzers))
	}
}

// TestConstructorAnalyzerDeclared: the gate's walk must see an
// analyzer built by a constructor (`var Analyzer = newAnalyzer()`)
// too, not only the &analysis.Analyzer{...} literal. Until 2026-11-03
// the literal was the only shape collected, so a constructor-built
// analyzer passed the gate unregistered (finding R8-AZ10-03).
func TestConstructorAnalyzerDeclared(t *testing.T) {
	dir := t.TempDir()
	src := `package ctor

import "golang.org/x/tools/go/analysis"

var Analyzer = newAnalyzer()
var Other = newAnalyzer()

func newAnalyzer() *analysis.Analyzer {
	return &analysis.Analyzer{Name: "ctor"}
}
`
	if err := os.WriteFile(filepath.Join(dir, "ctor.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	got := declaredAnalyzers(t, dir)
	if len(got) != 2 {
		t.Fatalf("declaredAnalyzers found %d analyzers, want 2: %+v", len(got), got)
	}
	for _, a := range got {
		if a.varName != "Analyzer" && a.varName != "Other" {
			t.Errorf("unexpected var %q", a.varName)
		}
		if a.name != "ctor" {
			t.Errorf("%s: analyzer name = %q, want ctor (read off the constructor's returned literal)", a.varName, a.name)
		}
	}
}

// heldBack reports whether the suite.go prose holds name back: a line
// that opens with the name followed by an em dash and the reason.
func heldBack(prose, name string) bool {
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(name) + `\s+—\s+\S`)
	return re.MatchString(prose)
}

// declaredAnalyzer is one exported *analysis.Analyzer variable.
type declaredAnalyzer struct {
	varName string // the identifier a registration selects (Analyzer, RecheckAnalyzer)
	name    string // its Name field, the marker vocabulary; the package name when not a literal
}

// declaredAnalyzers returns every exported package-level variable in
// dir's non-test Go files that holds an *analysis.Analyzer: the
// &analysis.Analyzer{...} literal, an explicit *analysis.Analyzer
// declaration, or a constructor call `var Analyzer = newAnalyzer()`
// whose same-package function returns *analysis.Analyzer (the
// 2026-11-03 widening, R8-AZ10-03: the literal alone let a
// constructor-built analyzer pass the gate unregistered). Every file
// counts, not just <pkg>/<pkg>.go: unseated declares two of its three
// analyzers in arms.go, and hygiene declares two analyzers and no
// variable named Analyzer at all, which the old one-file, one-name
// probe read as a helper package and skipped outright. A constructor's
// analyzer name is read off the literal it returns; a bare explicit
// type falls back to the variable's own name.
func declaredAnalyzers(t *testing.T, dir string) []declaredAnalyzer {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("glob %s: %v", dir, err)
	}
	parsed := make([]*ast.File, 0, len(files))
	ctors := map[string]string{}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		parsed = append(parsed, f)
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Type == nil || fd.Type.Results == nil ||
				len(fd.Type.Results.List) != 1 || !analyzerTypeName(fd.Type.Results.List[0].Type) {
				continue
			}
			ctors[fd.Name.Name] = returnedAnalyzerName(fd, fd.Name.Name)
		}
	}
	var out []declaredAnalyzer
	for _, f := range parsed {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, n := range vs.Names {
					if !n.IsExported() || i >= len(vs.Values) {
						continue
					}
					if lit := analyzerLiteral(vs.Values[i]); lit != nil {
						out = append(out, declaredAnalyzer{varName: n.Name, name: nameField(lit, filepath.Base(dir))})
						continue
					}
					if vs.Type != nil && analyzerTypeName(vs.Type) {
						out = append(out, declaredAnalyzer{varName: n.Name, name: ctorName(vs.Values[i], ctors, n.Name)})
						continue
					}
					if call, ok := vs.Values[i].(*ast.CallExpr); ok && len(call.Args) == 0 {
						if id, ok := call.Fun.(*ast.Ident); ok {
							if name, ok := ctors[id.Name]; ok {
								out = append(out, declaredAnalyzer{varName: n.Name, name: name})
							}
						}
					}
				}
			}
		}
	}
	return out
}

// analyzerTypeName: e is spelled *analysis.Analyzer.
func analyzerTypeName(e ast.Expr) bool {
	star, ok := e.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Analyzer" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "analysis"
}

// ctorName: the initializer's analyzer name when it calls a known
// constructor, else fallback.
func ctorName(e ast.Expr, ctors map[string]string, fallback string) string {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return fallback
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok {
		return fallback
	}
	if name, ok := ctors[id.Name]; ok {
		return name
	}
	return fallback
}

// returnedAnalyzerName reads the analyzer name off the first literal
// the constructor returns, falling back to its function name.
func returnedAnalyzerName(fd *ast.FuncDecl, fallback string) string {
	name := fallback
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return true
		}
		if lit := analyzerLiteral(ret.Results[0]); lit != nil {
			name = nameField(lit, fallback)
			return false
		}
		return true
	})
	return name
}

// analyzerLiteral returns the composite literal of &analysis.Analyzer{...}.
func analyzerLiteral(e ast.Expr) *ast.CompositeLit {
	u, ok := e.(*ast.UnaryExpr)
	if !ok || u.Op != token.AND {
		return nil
	}
	lit, ok := u.X.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	sel, ok := lit.Type.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Analyzer" {
		return nil
	}
	if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "analysis" {
		return nil
	}
	return lit
}

// nameField returns the literal's Name field, or fallback.
func nameField(lit *ast.CompositeLit, fallback string) string {
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if k, ok := kv.Key.(*ast.Ident); !ok || k.Name != "Name" {
			continue
		}
		if bl, ok := kv.Value.(*ast.BasicLit); ok && bl.Kind == token.STRING {
			if s, err := strconv.Unquote(bl.Value); err == nil {
				return s
			}
		}
	}
	return fallback
}

// TestAllowNamesMatchRegistration pins allow.Names to the registered set,
// so a marker can name every analyzer that runs and nothing that does not.
func TestAllowNamesMatchRegistration(t *testing.T) {
	got := map[string]bool{}
	for _, a := range repoAnalyzers {
		got[a.Name] = true
		if !allow.Known(a.Name) {
			t.Errorf("%s is registered but missing from allow.Names", a.Name)
		}
	}
	for _, n := range allow.Names {
		if !got[n] {
			t.Errorf("allow.Names lists %s, which is not registered", n)
		}
	}
}
