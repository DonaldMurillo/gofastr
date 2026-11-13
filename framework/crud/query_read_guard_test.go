package crud

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// softDeleteRequestCallers are the functions allowed to call the exported
// ApplySoftDeleteFilter/Count, which re-read ?trashed= from the request.
// Each is a write path or an in-process API with a synthetic request, never
// an HTTP read: those thread readQuery's values into applySoftDeleteFilterQ.
// A read path that re-reads the request instead parses it a second time.
var softDeleteRequestCallers = map[string]bool{
	"pivotLinked": true, "selectPreImage": true, "GetOne": true,
	"scopedSelect": true, "CountAll": true, "UpsertOne": true,
	"buildSelect": true, "Count": true,
}

// Only query_read.go may inspect the wire query. Multipart parsing reads
// the body and stays allowed; FormValue, r.Form and ParseForm read the
// query too and do not. The match is syntactic, so it errs wide: any
// RawQuery, any no-argument Query() call and url.ParseQuery count, however
// the URL reached the expression (u := r.URL; u.Query() included).
func TestReadQueryGuard(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "query_read.go" {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, msg := range queryReadViolations(fset, file) {
			t.Error(msg)
		}
	}
}

// queryReadViolations lists the raw query reads in one parsed file.
func queryReadViolations(fset *token.FileSet, file *ast.File) []string {
	imports := map[string]string{}
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		parts := strings.Split(path, "/")
		alias := parts[len(parts)-1]
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		imports[alias] = path
	}
	pkgFunc := func(sel *ast.SelectorExpr, path string, names ...string) bool {
		pkg, ok := sel.X.(*ast.Ident)
		return ok && imports[pkg.Name] == path && slices.Contains(names, sel.Sel.Name)
	}
	var out []string
	report := func(pos token.Pos, what string) {
		out = append(out, fmt.Sprintf("%s: query read %s must live in query_read.go", fset.Position(pos), what))
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
					switch {
					case sel.Sel.Name == "Query" && len(call.Args) == 0:
						report(sel.Pos(), "Query()")
					case (sel.Sel.Name == "ApplySoftDeleteFilter" || sel.Sel.Name == "ApplySoftDeleteFilterCount") && !softDeleteRequestCallers[fn.Name.Name]:
						report(sel.Pos(), sel.Sel.Name+" in "+fn.Name.Name+" (thread the parsed values into applySoftDeleteFilterQ, or add a write-path caller to softDeleteRequestCallers)")
					}
				}
				return true
			}
			sel, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch {
			case slices.Contains([]string{"RawQuery", "FormValue", "PostFormValue", "Form", "PostForm", "ParseForm"}, sel.Sel.Name),
				pkgFunc(sel, "net/url", "ParseQuery"),
				pkgFunc(sel, "github.com/DonaldMurillo/gofastr/framework/filter", "ParseFilters", "ParseSort"),
				pkgFunc(sel, "github.com/DonaldMurillo/gofastr/framework/pagination", "ParsePagination", "ParseCursorPagination"),
				pkgFunc(sel, "github.com/DonaldMurillo/gofastr/framework/softdelete", "WithTrashed"):
				report(sel.Pos(), sel.Sel.Name)
			}
			return true
		})
	}
	return out
}

// The guard's matcher fires on each spelling that re-reads the query and
// stays quiet on the threaded-values shape.
func TestReadQueryGuardMatcher(t *testing.T) {
	const src = `package crud
import (
	"net/url"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	pg "github.com/DonaldMurillo/gofastr/framework/pagination"
)
func viaAlias(r *http.Request) { u := r.URL; _ = u.Query() }
func viaRaw(r *http.Request) { u := r.URL; _, _ = url.ParseQuery(u.RawQuery) }
func viaForm(r *http.Request) { _ = r.ParseForm(); _ = r.Form }
func viaFilter(r *http.Request) { _, _ = filter.ParseFilters(r, nil) }
func viaPagination(r *http.Request) { _, _, _ = pg.ParseCursorPagination(r) }
func serveSomething(ch *CrudHandler, r *http.Request) { ch.ApplySoftDeleteFilter(nil, r) }
func selectPreImage(ch *CrudHandler, r *http.Request) { ch.ApplySoftDeleteFilter(nil, r) }
func threaded(ch *CrudHandler, q url.Values) { _ = q.Get("page"); ch.applySoftDeleteFilterQ(nil, q, nil) }
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(queryReadViolations(fset, file), "\n")
	for _, want := range []string{":7:", ":8:", ":9:", ":10:", ":11:", ":12:"} {
		if !strings.Contains(got, "fixture.go"+want) {
			t.Errorf("no violation on line %s; got:\n%s", want, got)
		}
	}
	for _, quiet := range []string{":13:", ":14:"} {
		if strings.Contains(got, "fixture.go"+quiet) {
			t.Errorf("violation on quiet line %s; got:\n%s", quiet, got)
		}
	}
}
